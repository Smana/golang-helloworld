package implementations

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"image-gallery/internal/config"
	"image-gallery/internal/observability"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/platform/storage/storetest"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStorageServiceEmitsUniformTelemetry(t *testing.T) {
	ctx := context.Background()
	exp := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	svc, err := storage.NewService(&config.StorageConfig{BucketName: "b", MaxUploadSize: 10 << 20}, storetest.NewMemStore())
	if err != nil {
		t.Fatal(err)
	}
	s := NewStorageService(svc)
	data := tinyPNG(t)
	path, err := s.Store(ctx, "cat.png", "image/png", bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	rc, err := s.Retrieve(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
	if ok, err := s.Exists(ctx, "missing.png"); ok || err != nil {
		t.Fatalf("Exists(missing) = %v, %v", ok, err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	ops := map[string]int64{}
	bytesByDir := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case observability.MetricStorageOps:
				for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
					op, _ := dp.Attributes.Value(observability.AttrStorageOperation)
					prov, _ := dp.Attributes.Value(observability.AttrStorageProvider)
					if prov.AsString() != "memory" {
						t.Errorf("storage.provider = %q", prov.AsString())
					}
					ops[op.AsString()] += dp.Value
				}
			case observability.MetricStorageTransferred:
				for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
					d, _ := dp.Attributes.Value(observability.AttrStorageDirection)
					bytesByDir[d.AsString()] += dp.Value
				}
			}
		}
	}
	if ops["put"] != 1 || ops["get"] != 1 || ops["stat"] != 1 {
		t.Errorf("storage.operations by op = %v", ops)
	}
	if bytesByDir["write"] != int64(len(data)) || bytesByDir["read"] != int64(len(data)) {
		t.Errorf("storage.transferred = %v, want %d each way", bytesByDir, len(data))
	}
	var put bool
	for _, sp := range exp.GetSpans() {
		if sp.Name == "storage.put" {
			set := attribute.NewSet(sp.Attributes...)
			put = sp.SpanKind == trace.SpanKindClient && set.HasValue(observability.AttrStorageProvider)
		}
	}
	if !put {
		t.Error("want a CLIENT span storage.put carrying storage.provider")
	}
}
