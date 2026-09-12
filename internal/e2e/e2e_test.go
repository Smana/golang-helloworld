// Package e2e runs web, worker and loadgen in one process against real
// Postgres, MinIO and Valkey containers and asserts the telemetry contract.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"image-gallery/internal/config"
	"image-gallery/internal/loadgen"
	"image-gallery/internal/observability"
	"image-gallery/internal/platform/cache"
	"image-gallery/internal/platform/database"
	"image-gallery/internal/platform/queue"
	"image-gallery/internal/services"
	"image-gallery/internal/services/implementations"
	"image-gallery/internal/testutils"
	"image-gallery/internal/web/handlers"
	"image-gallery/internal/worker"
)

func TestEndToEndTraceAndInstrumentContract(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// go.memory.limit is only emitted by the runtime instrumentation when a
	// memory limit is configured; bracket it like Task 5's own provider test so
	// the assertion is not host-dependent.
	prevLimit := debug.SetMemoryLimit(512 << 20)
	defer debug.SetMemoryLimit(prevLimit)

	// Telemetry first: the instruments are created at construction.
	spans := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	var logs bytes.Buffer
	ocfg := observability.Config{ServiceName: "image-gallery-e2e", ServiceVersion: "test", Environment: "test",
		TracesEnabled: true, TracesEndpoint: "http://unused", TracesSampler: observability.SamplerAlwaysOn, TracesSamplerArg: "1",
		MetricsEnabled: true, MetricsEndpoint: "http://unused", LogLevel: "info", LogFormat: "json"}
	logger := observability.NewLoggerTo(&logs, ocfg)
	prov, err := observability.NewProviderWith(ctx, ocfg, logger, spans, reader)
	if err != nil {
		t.Fatal(err)
	}
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tc, err := testutils.SetupTestContainers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tc.Cleanup(ctx) }()
	db, err := database.NewConnection(tc.GetDatabaseURL()) // otelsql-instrumented, like production
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Environment: "test", DatabaseURL: tc.GetDatabaseURL(), DemoControlsEnabled: true,
		Storage: config.StorageConfig{Provider: "s3", BucketName: "test-images", MaxUploadSize: 10 << 20},
		Cache:   config.CacheConfig{Enabled: true, Address: tc.RedisEndpoint, DefaultTTL: time.Hour, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second}}
	rdb, err := cache.NewInstrumentedClient(cfg.Cache)
	if err != nil {
		t.Fatal(err)
	}

	// Web role.
	container, err := services.NewContainerWithObservability(cfg, db, tc.ObjectStore, logger)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := queue.NewProducer(rdb)
	if err != nil {
		t.Fatal(err)
	}
	container.UseJobPublisher(implementations.NewQueueJobPublisher(producer))
	srv := httptest.NewServer(handlers.NewWithContainer(container).Routes())
	defer srv.Close()

	// Worker role.
	proc := worker.NewProcessor(container.ImageRepository(), container.StorageService(), container.DemoInjector(), logger)
	consumer, err := queue.NewConsumer(rdb, queue.WithDeadLetterHook(proc.OnDeadLetter), queue.WithBlock(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	gauges := redis.NewClient(&redis.Options{Addr: tc.RedisEndpoint})
	defer func() { _ = gauges.Close() }()
	gaugeReg, err := queue.RegisterGauges(gauges, otel.Meter("image-gallery/queue"), queue.DefaultStream, queue.DefaultGroup)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gaugeReg.Unregister() }() // before gauges.Close, as in the worker
	wctx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { _ = consumer.Run(wctx, proc.Handle); close(workerDone) }()

	// Loadgen drives the traffic: uploads, then the mix (list/view/thumbnail/delete/settings).
	if _, err := loadgen.Run(ctx, loadgen.Options{Target: srv.URL, Scenario: "upload", Rate: 2, Duration: 2 * time.Second, Concurrency: 2, Seed: 1}, io.Discard); err != nil {
		t.Fatal(err)
	}
	readyIDs := waitAllReady(t, srv.URL)
	if _, err := loadgen.Run(ctx, loadgen.Options{Target: srv.URL, Scenario: "mixed", Rate: 10, Duration: 3 * time.Second, Concurrency: 4, Seed: 2}, io.Discard); err != nil {
		t.Fatal(err)
	}
	// One fault, so demo.faults.injected exists: a slow list query.
	put(t, srv.URL+"/api/settings/demo", `{"slow_db_ms": 20}`)
	get(t, srv.URL+"/api/images")
	post(t, srv.URL+"/api/settings/demo/reset")

	// Ruling R3: settings.operations and image.deletions otherwise only come
	// from loadgen's 5%-weight operations over ~30 requests, so they are
	// probabilistic and the SDK omits instruments that recorded nothing. Drive
	// both explicitly (on an image the upload phase produced, never touched by
	// the mixed scenario's own delete pool) so the contract assertion below is
	// deterministic rather than flaky.
	get(t, srv.URL+"/api/settings")
	if len(readyIDs) == 0 {
		t.Fatal("no ready image IDs to delete")
	}
	del(t, fmt.Sprintf("%s/api/images/%d", srv.URL, readyIDs[0]))

	stopWorker()
	<-workerDone
	if err := prov.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}

	// Criterion 3: one trace spans loadgen -> web -> producer -> consumer, with DB, cache and storage children.
	byTrace := map[trace.TraceID][]tracetest.SpanStub{}
	for _, s := range spans.GetSpans() {
		byTrace[s.SpanContext.TraceID()] = append(byTrace[s.SpanContext.TraceID()], s)
	}
	var found trace.TraceID
	for id, ss := range byTrace {
		if has(ss, func(s tracetest.SpanStub) bool { return s.Name == "loadgen upload" && !s.Parent.IsValid() }) &&
			has(ss, func(s tracetest.SpanStub) bool {
				return s.Name == "POST /api/images" && s.SpanKind == trace.SpanKindServer
			}) &&
			has(ss, func(s tracetest.SpanStub) bool {
				return strings.HasPrefix(s.Name, "send ") && s.SpanKind == trace.SpanKindProducer
			}) &&
			has(ss, func(s tracetest.SpanStub) bool {
				return strings.HasPrefix(s.Name, "process ") && s.SpanKind == trace.SpanKindConsumer && len(s.Links) == 1
			}) &&
			has(ss, func(s tracetest.SpanStub) bool { return s.Name == "storage.put" }) &&
			has(ss, func(s tracetest.SpanStub) bool { return s.Name == "image.thumbnail" }) &&
			has(ss, func(s tracetest.SpanStub) bool { return strings.Contains(s.InstrumentationScope.Name, "otelsql") }) &&
			has(ss, func(s tracetest.SpanStub) bool { return strings.Contains(s.InstrumentationScope.Name, "redisotel") }) {
			found = id
			break
		}
	}
	if !found.IsValid() {
		t.Fatalf("no trace spans loadgen -> web -> queue -> worker with DB, Valkey and storage children (%d traces)", len(byTrace))
	}
	if !strings.Contains(logs.String(), found.String()) {
		t.Errorf("no log line carries trace_id %s", found)
	}

	// Criterion 4: every contract instrument exists under its dot name.
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			have[m.Name] = true
		}
	}
	want := append(append(append([]string{}, observability.ExpectedInstruments["web"]...), observability.ExpectedInstruments["worker"]...), observability.RuntimeInstruments...)
	for _, name := range want {
		if !have[name] {
			t.Errorf("instrument %q missing (contract drift)", name)
		}
	}
}

func has(ss []tracetest.SpanStub, pred func(tracetest.SpanStub) bool) bool {
	for i := range ss {
		if pred(ss[i]) {
			return true
		}
	}
	return false
}

// waitAllReady polls until every uploaded image reached status "ready" and
// returns their IDs.
func waitAllReady(t *testing.T, base string) []int {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var body struct {
			Images []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"images"`
		}
		resp, err := http.Get(base + "/api/images")
		if err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			ready := len(body.Images) > 0
			for _, im := range body.Images {
				ready = ready && im.Status == "ready"
			}
			if ready {
				ids := make([]int, 0, len(body.Images))
				for _, im := range body.Images {
					if id, err := strconv.Atoi(im.ID); err == nil {
						ids = append(ids, id)
					}
				}
				return ids
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("uploaded images never reached status ready")
	return nil
}

func get(t *testing.T, url string)       { t.Helper(); do(t, http.MethodGet, url, "") }
func post(t *testing.T, url string)      { t.Helper(); do(t, http.MethodPost, url, "") }
func put(t *testing.T, url, body string) { t.Helper(); do(t, http.MethodPut, url, body) }
func del(t *testing.T, url string)       { t.Helper(); do(t, http.MethodDelete, url, "") }

func do(t *testing.T, method, url, body string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %v %v", method, url, err, resp)
	}
	_ = resp.Body.Close()
}
