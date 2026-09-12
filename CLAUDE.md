# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Building and Running
- `make build` - Build the application binary to `./bin/image-gallery` (`make run` starts it with `serve`; `worker` and `loadgen` are the other two subcommands)
- `make run` - Build and run the application locally
- `make dev` - Run with hot reload using air (installs air if needed)

### Testing
- `make test` - Run all unit tests
- `make test-coverage` - Run tests with coverage report (generates coverage.html)

### Code Quality
- `make fmt` - Format Go code
- `make lint` - Run golangci-lint (installs if needed)
- `make vet` - Run go vet

### Docker and Infrastructure
- `docker-compose up -d` - Start PostgreSQL, MinIO, and Redis services
- `docker-compose up -d postgres minio` - Start only database and storage for local development
- `make docker-build` - Build Docker image
- `make docker-up` - Start all services including the app
- `make docker-down` - Stop all services

### Database Management (Atlas-Powered)
- `make migrate` - Run database migrations via Atlas CLI
- `make db-start` - Start only PostgreSQL service
- `make db-reset` - Reset database with fresh schema
- Atlas commands: `atlas-validate`, `atlas-inspect`, `atlas-diff`, `atlas-apply`

#### Migration Strategy
**Local Development:** Use `make migrate` (Atlas CLI directly)
**Kubernetes:** Atlas Operator handles migrations automatically
**Application:** Never runs migrations - Atlas handles all schema management

## Architecture Overview

This is a clean architecture Go application with strict separation of concerns:

### Core Layers
- **Domain** (`internal/domain/`): Pure business logic with no external dependencies
  - `image/`: Contains Image, Tag, Album domain models, interfaces, events, and validation
- **Services** (`internal/services/`): Application logic orchestrating domain operations
  - `implementations/`: Concrete service implementations and repository adapters
  - `integrationtests/`: Integration test suites using testcontainers
  - `container.go`: Dependency injection container
- **Platform** (`internal/platform/`): Infrastructure implementations
  - `database/`: PostgreSQL repositories and migration management
  - `storage/`: object storage service — S3 (MinIO/AWS) or GCS, selected by `STORAGE_PROVIDER`
  - `server/`: HTTP server configuration
- **Web** (`internal/web/`): HTTP handlers and routing

### Key Design Patterns
- **Dependency Injection**: All services are wired through a container in `internal/services/container.go`
- **Repository Pattern**: Domain interfaces implemented by platform layer
- **Clean Architecture**: Dependencies point inward toward domain
- **Test-Driven Development**: Comprehensive unit and integration tests

### Technology Stack
- **Runtime**: Go 1.25
- **Database**: PostgreSQL 15 with Atlas schema management
- **Storage**: S3 (MinIO/AWS) or GCS object storage, behind one `ObjectStore` interface
- **Testing**: Testcontainers for isolated integration tests
- **HTTP**: Chi router for REST API endpoints

### Development Environment
- PostgreSQL runs on port 5432 (user: testuser, password: testpass, db: image_gallery_test)
- MinIO runs on port 9000 (console on 9001, credentials: minioadmin/minioadmin)
- Valkey runs on port 6379 (caching layer, can be disabled)
- Application runs on port 8080

### Storage Configuration
The application supports multiple storage backends:

#### Local Development (MinIO)
```bash
STORAGE_ENDPOINT=localhost:9000
STORAGE_ACCESS_KEY=minioadmin
STORAGE_SECRET_KEY=minioadmin
STORAGE_USE_SSL=false
```

#### AWS S3 with Static Credentials
```bash
STORAGE_ENDPOINT=s3.amazonaws.com
STORAGE_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
STORAGE_SECRET_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
STORAGE_USE_SSL=true
```

#### AWS S3 with EKS Pod Identity / IAM Roles
```bash
STORAGE_ENDPOINT=s3.amazonaws.com
STORAGE_ACCESS_KEY=
STORAGE_SECRET_KEY=
STORAGE_USE_SSL=true
```
When credentials are empty, the application uses AWS credentials chain (Pod Identity, IAM roles, credentials file, environment variables).

### Caching Configuration
Valkey caching is enabled by default but can be disabled:

#### Valkey Enabled (Default)
```bash
CACHE_ENABLED=true
CACHE_ADDRESS=localhost:6379
CACHE_PASSWORD=
CACHE_DATABASE=0
CACHE_DEFAULT_TTL=1h
```

#### Valkey Disabled
```bash
CACHE_ENABLED=false
```

The application gracefully degrades when Valkey is unavailable - caching errors don't break functionality.

### Observability with OpenTelemetry

The application includes comprehensive observability using OpenTelemetry with support for **traces**, **metrics**, and **structured logs** correlated with traces.

#### Architecture
- **Traces**: Distributed tracing using OTLP → VictoriaTraces
- **Metrics**: Custom business and infrastructure metrics → VictoriaMetrics Operator
- **Logs**: Structured logging with zerolog, correlated with traces via trace_id/span_id

#### Configuration

##### Development (Local)
```bash
# OTEL_SERVICE_NAME=image-gallery   # leave unset for the per-role default
OTEL_SERVICE_VERSION=1.3.0
OTEL_DEPLOYMENT_ENVIRONMENT=development
OTEL_TRACES_ENABLED=true
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=localhost:4318
OTEL_TRACES_SAMPLER=always_on                    # Sample 100% of traces (development default)
OTEL_TRACES_SAMPLER_ARG=1.0                      # Sampling ratio (only used with ratio-based samplers)
OTEL_METRICS_ENABLED=true
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=localhost:4318
LOG_LEVEL=info
LOG_FORMAT=json
```

##### Kubernetes with VictoriaMetrics Operator
```bash
# OTEL_SERVICE_NAME=image-gallery   # leave unset for the per-role default
OTEL_SERVICE_VERSION=1.3.0
OTEL_DEPLOYMENT_ENVIRONMENT=production
OTEL_TRACES_ENABLED=true
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=victoriametrics-victoria-logs-single-server:4318
OTEL_TRACES_SAMPLER=parentbased_traceidratio     # Parent-based sampling with 10% ratio (production recommended)
OTEL_TRACES_SAMPLER_ARG=0.1                      # Sample 10% of traces
OTEL_METRICS_ENABLED=true
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=vmagent:8429
LOG_LEVEL=info
LOG_FORMAT=json
```

#### Instrumentation Coverage

**HTTP Layer** (`internal/web/handlers/`)
- Automatic HTTP request tracing with `otelhttp` middleware
- HTTP metrics: request count, duration, response size, active requests
- Custom spans for business logic in handlers
- Error tracking with span status and error recording

**Service Layer** (`internal/services/implementations/`)
- ImageService: Traces for create, get, list operations with business metrics
- Metrics: `image.uploads`, `image.processing.duration`, `cache.lookups` (`cache.result`: `hit`/`miss`)
- StorageService: Traces and metrics for all storage operations
- Metrics: `storage.operations`, `storage.operation.duration`, `storage.transferred`

**Infrastructure Layer**
- Database query tracing with automatic PostgreSQL instrumentation via `otelsql`
  - Automatic span creation for all SQL queries (SELECT, INSERT, UPDATE, DELETE)
  - Semantic conventions: `db.system.name`, `db.query.text`, `db.operation.name` (semconv v1.43.0)
  - Connection pool metrics: `db.sql.connection.open`, `db.sql.connection.max_open` (`otelsql`)
- Storage operation tracing (S3/MinIO or GCS)
- Cache operation tracing (Valkey/Redis)

**Logging**
- Structured JSON logging with zerolog
- Automatic trace context injection (trace_id, span_id, trace_sampled)
- Log levels: debug, info, warn, error, fatal, panic
- Console format available for local development

#### Key Metrics

**HTTP Metrics:**
- `http.server.request.duration` - Request latency histogram (its `_count` is the request count; there is no separate count instrument)
- `http.server.response.body.size` - Response size histogram
- `http.server.active_requests` - Active request gauge

**Business Metrics:**
- `image.uploads` - Total image uploads by content type and outcome
- `image.processing.duration` - Image processing time
- `cache.lookups` (`cache.result`: `hit`/`miss`) - Cache hit rate

**Infrastructure Metrics:**
- `storage.operations` - Storage operations by type
- `storage.operation.duration` - Storage operation latency
- `storage.transferred` - Data transfer volume

**Database Metrics (Connection Pool, from `otelsql`):**
- `db.sql.connection.max_open` - Maximum number of open connections to the database
- `db.sql.connection.open` - Established connections, both in use and idle
- `db.sql.connection.wait` - Total number of connections waited for
- `db.sql.connection.wait_duration` - Total time blocked waiting for a new connection

#### Exemplars: Connecting Metrics to Traces

The application uses **OpenTelemetry Exemplars** to create powerful correlations between metrics and traces. Exemplars are sample data points attached to histogram metrics that include trace_id and span_id references.

**How It Works:**
1. All histogram metrics (`*.duration`, `*.size`) use **exponential histograms** for better precision
2. When a histogram measurement is recorded within a traced operation, an **exemplar** is automatically captured
3. The exemplar includes the trace_id and span_id of the active span
4. In VictoriaMetrics/Grafana, you can click on an exemplar to jump directly to the trace

**Exemplar Configuration:**
- **Filter**: `TraceBasedFilter` - Only samples measurements from traced requests
- **Reservoir**: Histogram bucket-based - Stores one exemplar per histogram bucket
- **Automatic**: No code changes needed - works automatically when metrics are recorded within spans

**Example Use Cases:**
- **Slow Request Investigation**: See a spike in `http.server.request.duration`? Click the exemplar to view the exact slow trace
- **Failed Upload Debugging**: High error rate in `image.uploads`? Jump to failing traces instantly
- **Storage Performance**: Identify specific slow S3 operations via `storage.operation.duration` exemplars

**Histogram Type: Exponential Histograms**
- Auto-adjusting bucket boundaries based on observed values
- Better precision for tail latencies (p95, p99)
- Smaller data size compared to explicit bucket histograms
- Fully supported by VictoriaMetrics and Prometheus

#### Trace Sampling Strategies

The application supports multiple sampling strategies via environment variables:

**Available Samplers:**
- `always_on` - Sample 100% of traces (default for development)
- `always_off` - Disable tracing completely
- `traceidratio` - Sample a percentage based on trace ID (e.g., 0.1 = 10%)
- `parentbased_always_on` - Respect parent span sampling, default to sampling
- `parentbased_always_off` - Respect parent span sampling, default to not sampling
- `parentbased_traceidratio` - Respect parent span, use ratio for root spans (recommended for production)

**Production Recommendation:**
Use `parentbased_traceidratio` with a 10% sampling rate:
```bash
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=0.1
```

This ensures:
- Child spans respect parent sampling decisions (maintains complete traces)
- Root spans are sampled at 10% (reduces costs while maintaining coverage)
- Exemplars still work even with sampling (metrics remain accurate)

#### Example Queries

**VictoriaMetrics (PromQL):**
```promql
# HTTP request rate by route
sum(rate(http_server_request_duration_count[5m])) by (http_route)

# Image upload rate by outcome
rate(image_uploads_total{outcome="success"}[5m])

# Storage operation latency p95
histogram_quantile(0.95, rate(storage_operation_duration_bucket[5m]))

# Cache hit rate
sum(rate(cache_lookups_total{cache_result="hit"}[5m])) / sum(rate(cache_lookups_total[5m]))
```

**VictoriaTraces (Trace Queries):**
```
# Find slow image uploads
service.name="xplane-image-gallery" AND name="CreateImage" AND duration > 1s

# Find failed storage operations
service.name="xplane-image-gallery" AND name="storage.put" AND status.code="ERROR"

# Trace image retrieval with cache
service.name="xplane-image-gallery" AND name="GetImage"
```

#### Observability Best Practices
- **Context Propagation**: Trace context automatically propagated through all layers (HTTP → Service → Database)
- **Semantic Conventions**: Following OpenTelemetry semantic conventions for HTTP, database, and storage
- **Exemplars**: Automatic trace correlation via histogram exemplars - no code changes needed
- **Exponential Histograms**: Better precision and smaller payload size compared to fixed-bucket histograms
- **Database Instrumentation**: Automatic PostgreSQL query tracing with `otelsql` wrapper
- **Graceful Degradation**: Observability failures don't impact application functionality
- **Resource Detection**: Automatic service name, version, environment detection
- **Sampling**: Configurable via environment variables (default: 100% for development, recommend 10% for production)
- **Batching**: Traces batched every 5s, metrics exported every 30s
- **Shutdown**: Graceful shutdown with ForceFlush before exit

#### Local Testing
For local testing without VictoriaMetrics/VictoriaTraces, you can:
1. Disable observability:
   ```bash
   OTEL_TRACES_ENABLED=false
   OTEL_METRICS_ENABLED=false
   ```
2. Use Jaeger for traces (requires Jaeger running on :4318)
3. Use Prometheus for metrics scraping

### API Endpoints
- `GET /api/images` - List images with pagination
- `POST /api/images` - Upload new image
- `GET /api/images/:id` - Get specific image
- `GET /api/images/:id/view` - View image (proxy endpoint)
- `PUT /api/images/:id` - Update image metadata
- `DELETE /api/images/:id` - Delete image

### Testing Strategy
- **Unit Tests**: Fast, isolated tests alongside implementation files
- **Integration Tests**: Real database/storage/Valkey using testcontainers in `internal/services/integrationtests/`
- **Test Utilities**: Shared test infrastructure in `internal/testutils/` with PostgreSQL, MinIO, and Valkey containers
- **Cache Tests**: Valkey cache tests skip gracefully if Valkey unavailable during development

### Important Notes
- Always run infrastructure services before running the application locally
- For full functionality, start all services: `docker-compose up -d` (PostgreSQL, MinIO, Valkey)
- For local development without Valkey: Set `CACHE_ENABLED=false` or start only: `docker-compose up -d postgres minio`
- **Always use Atlas for migrations**: `make migrate` (local) or Atlas Operator (Kubernetes)
- The application never runs migrations - it's purely application logic
- Integration tests require Docker to be running and will start containers automatically
- The application follows strict TDD methodology with comprehensive test coverage
- All new code requires corresponding tests
- Use the dependency injection container for service resolution
- Use Dagger for all CI steps when possible. These tests should also be ran locally using Dagger


### Testing GoReleaser Build and Push Workflow

To test the build-push workflow locally (matches GitHub Actions exactly):

```bash
# Test GoReleaser binary build only
GITHUB_REPOSITORY=Smana/image-gallery GITHUB_REPOSITORY_OWNER=Smana GITHUB_REPOSITORY_NAME=image-gallery bash -c 'curl -sfL https://goreleaser.com/static/run | bash -s -- build --snapshot --clean --skip=validate'

# Test GoReleaser full release (includes Docker images)
GITHUB_REPOSITORY=Smana/image-gallery GITHUB_REPOSITORY_OWNER=Smana GITHUB_REPOSITORY_NAME=image-gallery bash -c 'curl -sfL https://goreleaser.com/static/run | bash -s -- release --snapshot --clean --skip=validate'
```

The release command builds binaries, creates Docker images, and prepares archives - exactly matching the build-push workflow.
- always run make lint after a change