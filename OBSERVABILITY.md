# Observability

This document describes the observability features of the image-gallery application, including metrics, traces, and structured logging.

## Overview

The application uses **OpenTelemetry** to provide comprehensive observability:

- **Metrics**: Application and business metrics exported via OTLP
- **Traces**: Distributed tracing across all layers
- **Logs**: Structured JSON logs with automatic trace correlation

### Technology Stack

- **OpenTelemetry SDK**: Industry-standard observability instrumentation
- **Zerolog**: High-performance structured logging
- **OTLP Protocol**: Open standard for telemetry export
- **VictoriaMetrics**: Recommended for metrics storage (Prometheus-compatible)
- **VictoriaTraces**: Recommended for distributed tracing (Jaeger-compatible)

## Quick Start

### Local Development

1. **Start infrastructure services**:
```bash
docker-compose up -d postgres minio valkey
```

2. **Run with observability disabled** (simplest):
```bash
export OTEL_TRACES_ENABLED=false
export OTEL_METRICS_ENABLED=false
make run
```

3. **Run with observability enabled** (requires OTLP collector):
```bash
# Start a local OpenTelemetry Collector or VictoriaMetrics/VictoriaTraces
# Then run the application (it will use .env configuration)
make run
```

### Configuration

All observability features are configured via environment variables. See `.env.example` for defaults.

#### Essential Settings

```bash
# Service identification
# OTEL_SERVICE_NAME=image-gallery   # leave unset — each role names itself when this is empty
OTEL_SERVICE_VERSION=1.3.0
OTEL_DEPLOYMENT_ENVIRONMENT=development

# Enable/disable features
OTEL_TRACES_ENABLED=true
OTEL_METRICS_ENABLED=true

# OTLP endpoints (HTTP)
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=localhost:4318
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=localhost:4318

# Logging
LOG_LEVEL=info        # debug, info, warn, error
LOG_FORMAT=json       # json or console
```

#### Disable Observability

To run without any observability overhead:

```bash
OTEL_TRACES_ENABLED=false
OTEL_METRICS_ENABLED=false
```

The application will start normally without connecting to any telemetry backends.

## Instrumentation Coverage

### HTTP Layer

All HTTP requests are automatically instrumented:

- **Spans**: One span per HTTP request with standard semantic conventions
- **Metrics**: duration, response size, active requests (no separate request-count instrument —
  the duration histogram's `_count` is the request count)
- **Logs**: Request logs include trace_id and span_id for correlation

**Instrumented by one middleware** (`observability.Middleware`), which continues the caller's
trace, names each span after the matched chi route pattern, and records the semconv RED metrics —
every route below and every other route gets this for free, except `/healthz` and `/readyz`,
which the middleware returns from before creating a span or recording any metric:

- `GET /api/images` - list images (pagination, tag filters)
- `POST /api/images` - upload images
- `GET /api/images/{id}/view` - proxy the original (no presigned URLs)
- `GET /api/images/{id}/thumbnail` - proxy the worker's thumbnail, or the original while `pending`/`processing`/`failed`
- `DELETE /api/images/{id}` - delete image
- `GET`/`PUT /api/settings`, `POST /api/settings/reset` - user display settings
- `GET`/`PUT /api/settings/demo`, `POST /api/settings/demo/reset` - demo fault controls
- `GET /api/tags/predefined` - predefined tag list

Note: no `PUT /api/images/{id}` route exists, despite the domain method
(`ImageService.UpdateImage`) being implemented — pre-existing gap, not part of this release.

### Service Layer

Business logic is instrumented with detailed traces and metrics:

- **ImageService**: image creation, retrieval, updates, deletion
- **StorageService**: storage operations behind one `ObjectStore` interface — S3 (`minio-go`) or
  GCS (`cloud.google.com/go/storage`), selected by `STORAGE_PROVIDER`; both emit identical
  `storage.*` spans and metrics, distinguished by the `storage.provider` attribute
- **CacheService**: Valkey cache hits/misses
- **Worker**: queue consumption, image processing steps (fetch/decode/thumbnail/store), job outcomes

### Database Layer

PostgreSQL queries are traced automatically via the `otelsql` wrapper (`db.system.name`,
`db.query.text`, `db.operation.name` semantic conventions, semconv v1.43.0), plus connection-pool
metrics.

## Metrics Reference

**Resource attributes, on every role:**
- `service.name` and `service.version`
- `deployment.environment.name` (from `OTEL_DEPLOYMENT_ENVIRONMENT`)
- `k8s.namespace.name` and `k8s.pod.name` (from `POD_NAMESPACE`/`POD_NAME`, Downward API in-cluster)
- `cloud.provider` (`aws`|`gcp`), set per cluster via `OTEL_RESOURCE_ATTRIBUTES`

| Metric | Kind | Unit | Attributes | Role |
|---|---|---|---|---|
| `http.server.request.duration` | Float64Histogram | `s` | `http.request.method`, `http.route`, `http.response.status_code` | web |
| `http.server.active_requests` | Int64UpDownCounter | `{request}` | `http.request.method` | web |
| `http.server.response.body.size` | Int64Histogram | `By` | as duration | web |
| `http.client.request.duration` (otelhttp) | Histogram | `s` | otelhttp defaults | loadgen |
| `image.uploads` | Int64Counter | `{upload}` | `image.content_type`, `outcome` | web |
| `image.deletions` | Int64Counter | `{deletion}` | `outcome` | web |
| `cache.lookups` | Int64Counter | `{lookup}` | `cache.name` (`image`,`list`,`settings`,`demo`), `cache.result` (`hit`,`miss`) | web, worker |
| `settings.operations` | Int64Counter | `{operation}` | `settings.operation` (`read`,`write`), `settings.source` (`cache`,`database`) | web |
| `storage.operations` | Int64Counter | `{operation}` | `storage.provider`, `storage.operation` (`put`,`get`,`stat`,`delete`,`list`), `outcome` | web, worker |
| `storage.operation.duration` | Float64Histogram | `s` | as `storage.operations` | web, worker |
| `storage.transferred` | Int64Counter | `By` | `storage.provider`, `storage.direction` (`read`,`write`) | web, worker |
| `messaging.client.sent.messages` | Int64Counter | `{message}` | `messaging.system=valkey`, `messaging.destination.name`, `error.type` (on failure) | web |
| `messaging.process.duration` | Float64Histogram | `s` | `messaging.system`, `messaging.destination.name`, `job.type`, `outcome` | worker |
| `worker.jobs` | Int64Counter | `{job}` | `job.type`, `outcome` (`success`,`retry`,`dead_letter`,`skipped`) | worker |
| `queue.depth` | Int64ObservableGauge | `{message}` | `messaging.destination.name`, `messaging.consumer.group.name` | worker |
| `queue.pending` | Int64ObservableGauge | `{message}` | same | worker |
| `queue.lag` | Float64ObservableGauge | `s` | same | worker |
| `image.processing.duration` | Float64Histogram | `s` | `image.processing.step` (`fetch`,`decode`,`thumbnail`,`store`) | worker |
| `demo.faults.injected` | Int64Counter | `{fault}` | `demo.fault` (`latency`,`error`,`slow_db`,`worker_failure`,`worker_slowdown`) | web, worker |
| `telemetry.spans.ended` | Int64Counter | `{span}` | — | all |
| `telemetry.spans.exported` | Int64Counter | `{span}` | `outcome` (`success`,`failure`) | all |
| `go.memory.used`, `go.memory.limit`, `go.goroutine.count`, … (runtime contrib) | | | | all |

### Dropped spans

> `dropped ratio = 1 - telemetry.spans.exported{outcome="success"} / telemetry.spans.ended`

The BatchSpanProcessor silently drops spans when its queue is full; this ratio turns that into a
visible metric. Success criterion: dropped spans stay under 0.1 % of exported spans at the load
generator's top rate (25 req/s, 100 % sampling).

## Traces Reference

| Span | Kind | Where | Key attributes |
|---|---|---|---|
| `<METHOD> <route>` (e.g. `GET /api/images/{id}/view`) | SERVER | web middleware | `http.request.method`, `http.route`, `http.response.status_code` |
| `send image-gallery:jobs` | PRODUCER | web queue producer | `messaging.system`, `messaging.destination.name`, `messaging.operation.type=send`, `messaging.message.id`, `image.id`, `job.type` |
| `process image-gallery:jobs` | CONSUMER (child of the propagated context, plus a link to it) | worker | the above plus `messaging.consumer.group.name`, `messaging.operation.type=process` |
| `job.attempt` | INTERNAL, one per attempt | worker | `job.attempt`, `error.type` |
| `image.fetch`, `image.decode`, `image.thumbnail`, `image.store` | INTERNAL | worker | `image.id` |
| `storage.put`, `.get`, `.stat`, `.delete`, `.list` | CLIENT | web, worker | `storage.provider`, `storage.key` |
| `loadgen <op>` | INTERNAL root | loadgen | `loadgen.scenario`, `loadgen.op` |
| redisotel and otelsql spans | CLIENT | web, worker | library defaults |

This table is the pinned contract (`internal/observability/names.go`) sub-project 2's dashboards
and rules build on, not an exhaustive inventory — incidental spans the handlers and services also
emit (e.g. `UploadImages`, `listImagesHandler`, the settings handlers) are real but unpinned, and
can change without notice; look at the code for those.

A trace for one upload spans loadgen (or the browser) → web → the queue producer span → the
worker's consumer span → its storage and database child spans — one trace ID end to end.

## Faults

The Demo settings group (`GET`/`PUT /api/settings/demo`, `POST /api/settings/demo/reset`) injects
faults for the observability demo. The endpoints exist while `DEMO_CONTROLS_ENABLED` is `true`
(the default); `false` removes them and injects nothing. Every control is off by default:

| Control | Effect |
|---|---|
| Latency | Adds a delay (`latency_ms`), with probability `latency_probability`, to matched routes |
| Errors | A 5xx response, with probability `error_probability` |
| Slow DB | A slow list query (`slow_db_ms`), on cache misses only; at most 4 run at once, and requests past that are not delayed |
| Worker failure | A job fails with probability `worker_failure_probability`: it retries, then dead-letters |
| Worker slowdown | A processing delay (`worker_delay_ms`), so the queue grows |

Every injected fault sets the span attribute `demo.fault=<type>`, adds a span event "demo fault
injected" carrying the same attribute, logs a `warn` line ("demo fault injected"), and increments
`demo.faults.injected`. When one request draws several faults (latency, then an error) the
attribute holds the latest and the events list all of them in order. Only `/api/*` requests are faulted at all, and
`/api/settings/demo*` is exempt within that, so a fault can always be switched off.

## Logging

### Log Format

All logs are structured JSON with consistent fields:

```json
{
  "level": "info",
  "service.name": "xplane-image-gallery",
  "service.version": "1.3.0",
  "deployment.environment.name": "development",
  "time": "2025-10-28T21:35:21+01:00",
  "message": "web role listening"
}
```

### Trace Correlation

When inside a trace context, logs automatically include:

```json
{
  "level": "info",
  "trace_id": "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "span_id": "a1b2c3d4e5f6g7h8",
  "trace_sampled": true,
  "message": "Processing image upload"
}
```

This allows you to:
1. Find all logs for a specific trace in your log aggregation system
2. Jump from traces to logs and vice versa
3. Correlate application behavior across distributed systems

### Console vs JSON Format

**Development** (human-readable):
```bash
LOG_FORMAT=console
```

**Production** (structured):
```bash
LOG_FORMAT=json
```

## Deployment

### Kubernetes with VictoriaMetrics Operator

1. **Install VictoriaMetrics Operator**:
```bash
helm repo add vm https://victoriametrics.github.io/helm-charts/
helm install victoria-metrics-operator vm/victoria-metrics-operator
```

2. **Deploy VictoriaMetrics components**:
```yaml
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMCluster
metadata:
  name: victoria-metrics
spec:
  retentionPeriod: "30d"
  replicationFactor: 2
  vmstorage:
    replicaCount: 2
  vmselect:
    replicaCount: 2
  vminsert:
    replicaCount: 2
```

3. **Deploy VictoriaTraces**:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: victoria-traces
spec:
  template:
    spec:
      containers:
      - name: victoria-traces
        image: victoriametrics/victoria-traces:latest
        ports:
        - containerPort: 4318  # OTLP HTTP
```

4. **Configure application**:
```yaml
env:
- name: OTEL_TRACES_ENABLED
  value: "true"
- name: OTEL_METRICS_ENABLED
  value: "true"
- name: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
  value: "victoria-traces:4318"
- name: OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
  value: "victoria-metrics-vminsert:8480"
```

### Docker Compose (Local Testing)

Add to `docker-compose.yml`:

```yaml
services:
  otel-collector:
    image: otel/opentelemetry-collector:latest
    command: ["--config=/etc/otel-collector-config.yaml"]
    volumes:
      - ./otel-collector-config.yaml:/etc/otel-collector-config.yaml
    ports:
      - "4318:4318"  # OTLP HTTP
```

Example `otel-collector-config.yaml`:

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

exporters:
  logging:
    loglevel: debug
  prometheusremotewrite:
    endpoint: http://victoria-metrics:8428/api/v1/write
  jaeger:
    endpoint: victoria-traces:14250
    tls:
      insecure: true

service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging, jaeger]
    metrics:
      receivers: [otlp]
      exporters: [logging, prometheusremotewrite]
```

## Querying and Visualization

### VictoriaMetrics (PromQL)

VictoriaMetrics stores every series under its OTLP dot name (e.g. `{__name__="cache.lookups"}`,
as `scripts/soak.sh` queries them); the `foo_bar_total` underscore form the examples below use
only exists when VictoriaMetrics runs with `-usePromCompatibleNaming` — check which mode the
target cluster uses before copying an example verbatim.

**Request rate by route**:
```promql
sum(rate(http_server_request_duration_count[5m])) by (http_route)
```

**95th percentile latency**:
```promql
histogram_quantile(0.95, rate(http_server_request_duration_bucket[5m]))
```

**Image upload success rate**:
```promql
sum(rate(image_uploads_total{outcome="success"}[5m]))
/
sum(rate(image_uploads_total[5m]))
```

**Cache hit ratio**:
```promql
sum(rate(cache_lookups_total{cache_result="hit"}[5m]))
/
sum(rate(cache_lookups_total[5m]))
```

**Storage throughput by provider**:
```promql
sum(rate(storage_transferred_total[5m])) by (storage_provider, storage_direction)
```

### VictoriaTraces (Jaeger UI)

Access the Jaeger-compatible UI to:

1. **Find traces by service**: Filter by `xplane-image-gallery` (web) or
   `xplane-image-gallery-worker` (worker)
2. **Find slow requests**: Filter by duration > 1s
3. **Find errors**: Filter by `error=true`
4. **Trace comparison**: Compare similar operations
5. **Dependency graph**: Visualize service dependencies

### Example Queries

**Find all failed image uploads**:
- Service: `xplane-image-gallery`
- Operation: `CreateImage`
- Tags: `error=true`

**Find slow storage operations**:
- Service: `xplane-image-gallery` or `xplane-image-gallery-worker`
- Operation: `storage.put` (or `storage.get`, `storage.stat`, `storage.delete`, `storage.list`)
- Min Duration: `1000ms`

## Troubleshooting

### Observability Not Working

1. **Check configuration**:
```bash
# Verify environment variables are set
env | grep OTEL
```

2. **Check logs for initialization**:
```bash
# Should see: "OpenTelemetry provider initialized"
tail -f /tmp/server.log
```

3. **Check OTLP endpoint connectivity**:
```bash
# Test if endpoint is reachable
curl -v http://localhost:4318/v1/traces
```

### High Overhead

If observability is causing performance issues:

1. **Reduce trace sampling**:
```bash
# Sample 10% of traces (configure in future iteration)
OTEL_TRACES_SAMPLER=traceidratio
OTEL_TRACES_SAMPLER_ARG=0.1
```

2. **Disable metrics**:
```bash
OTEL_METRICS_ENABLED=false
```

3. **Adjust log level**:
```bash
LOG_LEVEL=warn  # Only log warnings and errors
```

### Connection Refused Errors

If you see:
```
failed to upload metrics: Post "http://localhost:4318/": connect: connection refused
```

This is **expected** when the OTLP endpoint is not available. The application uses graceful degradation and will continue running normally. To fix:

1. Start an OTLP collector (see Deployment section)
2. Or disable observability if not needed

## Best Practices

### Development

- Use `LOG_FORMAT=console` for readable logs
- Use `LOG_LEVEL=debug` for detailed information
- Enable observability only when actively debugging
- Use local OTLP collector with logging exporter

### Production

- Use `LOG_FORMAT=json` for structured logging
- Use `LOG_LEVEL=info` or `LOG_LEVEL=warn`
- Always enable observability in production
- Configure appropriate retention policies
- Set up alerting on key metrics
- Monitor trace sampling rates

### Monitoring

Key metrics to alert on:

- `http_server_request_duration_count{http_response_status_code=~"5.."}` - server errors
- `image_uploads_total{outcome="error"}` - failed uploads
- `http_server_request_duration` p95 > 1s - slow requests
- `storage_operations_total{outcome="error"}` - storage failures
- `sum(rate(cache_lookups_total{cache_result="hit"}[5m])) / sum(rate(cache_lookups_total[5m]))` < 0.5 - low cache hit ratio (`sum()` on both sides — without it, the `hit` series only ever matches itself and the ratio is always 1)
- `1 - sum(increase({__name__="telemetry.spans.exported",outcome="success"}[20m])) / sum(increase({__name__="telemetry.spans.ended"}[20m]))` > 0.001 - dropped spans over budget (the exact query `scripts/soak.sh` verifies against a live VictoriaMetrics)

## Architecture

### Observability Components

```
┌─────────────────────────────────────────────────────────┐
│                   Application Code                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │ HTTP Handler │  │    Service   │  │  Repository  │  │
│  │  (Traced)    │→ │   (Traced)   │→ │   (Traced)   │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
│         ↓                  ↓                  ↓          │
│  ┌────────────────────────────────────────────────────┐ │
│  │         OpenTelemetry SDK (Provider)               │ │
│  │  • TracerProvider  • MeterProvider  • Logger       │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                          ↓
                 ┌────────────────┐
                 │  OTLP Exporter │
                 │   (HTTP/4318)  │
                 └────────────────┘
                          ↓
         ┌────────────────┴────────────────┐
         ↓                                  ↓
┌──────────────────┐            ┌──────────────────┐
│ VictoriaMetrics  │            │ VictoriaTraces   │
│  (Metrics Store) │            │  (Trace Store)   │
└──────────────────┘            └──────────────────┘
```

### Graceful Degradation

The application is designed to function normally even when observability backends are unavailable:

1. **Provider initialization fails**: Application continues without tracing/metrics
2. **OTLP endpoint unreachable**: Telemetry is buffered then dropped
3. **Export failures**: Logged but don't affect business logic
4. **Shutdown**: ForceFlush ensures buffered telemetry is exported

## Further Reading

- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/instrumentation/go/)
- [VictoriaMetrics Documentation](https://docs.victoriametrics.com/)
- [VictoriaTraces Documentation](https://docs.victoriametrics.com/victorialogs/)
- [OTLP Specification](https://opentelemetry.io/docs/specs/otlp/)
- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)
