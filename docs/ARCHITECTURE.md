# Architecture Documentation

This document provides a comprehensive overview of the Image Gallery application architecture, design patterns, and implementation decisions.

## 🏗️ High-Level Architecture

The Image Gallery follows **Clean Architecture** principles with clear separation of concerns and dependency inversion.

```mermaid
graph TB
    subgraph "External"
        User[User/Browser]
        S3[S3 Storage<br/>MinIO/AWS]
        DB[(PostgreSQL<br/>Database)]
        Cache[(Valkey<br/>Cache)]
    end
    
    subgraph "Application Layers"
        subgraph "Web Layer"
            HTTP[HTTP Handlers<br/>Chi Router]
            MW[Middleware<br/>Auth, CORS, etc.]
        end
        
        subgraph "Service Layer"
            IS[Image Service]
            CS[Cache Service]
            SS[Storage Service]
            Container[DI Container]
        end
        
        subgraph "Domain Layer"
            Models[Domain Models<br/>Image, Tag, Album]
            Interfaces[Repository Interfaces]
            Events[Domain Events]
            Validation[Business Rules]
        end
        
        subgraph "Platform Layer"
            ImageRepo[Image Repository]
            CacheImpl[Cache Implementation]
            StorageImpl[Storage Implementation]
            Migrations[Database Migrations]
        end
    end
    
    User --> HTTP
    HTTP --> MW
    MW --> IS
    IS --> Models
    IS --> CS
    IS --> SS
    CS --> Cache
    SS --> S3
    ImageRepo --> DB
    Container --> IS
    Container --> CS
    Container --> SS
```

## 🎯 Design Principles

### 1. Clean Architecture

**Dependency Rule**: Dependencies point inward toward the domain layer.

```
┌─────────────────────────────────────────┐
│               Web Layer                 │
│  (HTTP Handlers, Middleware, Routing)  │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│            Service Layer                │
│   (Application Logic, Use Cases)       │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│            Domain Layer                 │
│  (Business Logic, Models, Interfaces)  │
└─────────────────┬───────────────────────┘
                  │
┌─────────────────▼───────────────────────┐
│           Platform Layer                │
│ (Database, Storage, External Services)  │
└─────────────────────────────────────────┘
```

### 2. Dependency Injection

All services are wired through a centralized container:

```go
// Container manages all application dependencies
type Container struct {
    ImageService   domain.ImageService
    StorageService domain.StorageService
    CacheService   domain.CacheService
    ImageRepo      domain.ImageRepository
}
```

### 3. Test-Driven Development

- **Unit Tests**: Fast tests with mocking for business logic
- **Integration Tests**: Real database/storage with testcontainers
- **End-to-End Tests**: Complete application flow testing

## 📁 Project Structure

```
image-gallery/
├── cmd/image-gallery/          # Application entry point
│   └── main.go                # Role dispatch: serve, worker, loadgen
├── internal/
│   ├── config/                # Configuration management
│   │   ├── config.go          # Config structure and loading
│   │   └── config_test.go     # Config tests
│   ├── domain/                # Domain layer (pure business logic)
│   │   └── image/             # Image domain
│   │       ├── image.go       # Image entity and business rules
│   │       ├── repository.go  # Repository interface
│   │       ├── service.go     # Service interface
│   │       ├── events.go      # Domain events
│   │       └── errors.go      # Domain-specific errors
│   ├── platform/              # Platform layer (infrastructure)
│   │   ├── database/          # Database implementations
│   │   │   ├── models/        # Database models
│   │   │   ├── repository/    # Repository implementations
│   │   │   └── migrations/    # Database migrations
│   │   ├── storage/           # Object storage implementations
│   │   │   ├── objectstore.go # ObjectStore interface, provider selection
│   │   │   ├── s3store.go     # S3/MinIO implementation
│   │   │   ├── gcsstore.go    # GCS implementation
│   │   │   └── service.go     # Storage service wrapper
│   │   ├── cache/             # Cache implementations
│   │   │   ├── redis.go       # Valkey/Redis implementation
│   │   │   └── cache_test.go
│   │   └── server/            # HTTP server setup
│   │       └── server.go      # Server configuration
│   ├── services/              # Service layer (application logic)
│   │   ├── implementations/   # Service implementations
│   │   │   ├── image_service.go
│   │   │   ├── cache_service.go
│   │   │   └── *_test.go      # Unit tests
│   │   ├── integrationtests/  # Integration tests
│   │   │   └── *_test.go
│   │   └── container.go       # Dependency injection container
│   ├── testutils/             # Testing utilities
│   │   ├── containers.go      # Testcontainer setup
│   │   └── helpers.go         # Test helpers
│   └── web/                   # Web layer (HTTP)
│       ├── handlers/          # HTTP handlers
│       ├── middleware/        # HTTP middleware
│       └── router.go          # Route definitions
├── docs/                      # Documentation
├── .github/workflows/         # CI/CD pipelines
└── docker-compose.yml         # Development environment
```

## 🔧 Core Components

### Domain Layer (`internal/domain/`)

**Purpose**: Contains pure business logic with no external dependencies.

#### Image Entity

```go
type Image struct {
    ID               int       `json:"id"`
    Filename         string    `json:"filename"`
    OriginalFilename string    `json:"original_filename"`
    ContentType      string    `json:"content_type"`
    FileSize         int64     `json:"file_size"`
    StoragePath      string    `json:"storage_path"`
    UploadedAt       time.Time `json:"uploaded_at"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}
```

#### Repository Interface

```go
type ImageRepository interface {
    Create(ctx context.Context, image *Image) error
    GetByID(ctx context.Context, id int) (*Image, error)
    List(ctx context.Context, request ListImagesRequest) (*ListImagesResponse, error)
    Update(ctx context.Context, image *Image) error
    Delete(ctx context.Context, id int) error
}
```

#### Service Interface

```go
type ImageService interface {
    UploadImage(ctx context.Context, request UploadImageRequest) (*Image, error)
    GetImage(ctx context.Context, id int) (*Image, error)
    ListImages(ctx context.Context, request ListImagesRequest) (*ListImagesResponse, error)
    DeleteImage(ctx context.Context, id int) error
}
```

### Service Layer (`internal/services/`)

**Purpose**: Orchestrates domain logic and coordinates between layers.

#### Image Service Implementation

```go
type ImageService struct {
    repo    domain.ImageRepository
    storage domain.StorageService
    cache   domain.CacheService
}

func (s *ImageService) UploadImage(ctx context.Context, req UploadImageRequest) (*Image, error) {
    // 1. Validate input
    // 2. Store file in S3/MinIO
    // 3. Save metadata to database
    // 4. Invalidate cache
    // 5. Return result
}
```

#### Dependency Injection Container

```go
type Container struct {
    ImageService   domain.ImageService
    StorageService domain.StorageService
    CacheService   domain.CacheService
    ImageRepo      domain.ImageRepository
}

func NewContainer(cfg config.Config) (*Container, error) {
    // Wire all dependencies
    return &Container{
        ImageRepo:      repository.NewImageRepository(db),
        StorageService: storage.NewObjectStore(ctx, cfg.Storage),
        CacheService:   cache.NewCacheService(redisClient),
        ImageService:   implementations.NewImageService(repo, storage, cache),
    }
}
```

### Platform Layer (`internal/platform/`)

**Purpose**: Implements infrastructure concerns and external integrations.

#### Database Repository

```go
type ImageRepository struct {
    db *sql.DB
}

func (r *ImageRepository) Create(ctx context.Context, image *domain.Image) error {
    query := `INSERT INTO images (filename, content_type, file_size, storage_path) 
              VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`
    // Implementation...
}
```

#### Storage Service

```go
// One interface, two backends, selected by STORAGE_PROVIDER.
type ObjectStore interface {
    Provider() string
    Put(ctx context.Context, key, contentType string, r io.Reader, size int64, metadata map[string]string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Stat(ctx context.Context, key string) (ObjectInfo, error)
    Delete(ctx context.Context, key string) error
    List(ctx context.Context, prefix string, max int) ([]ObjectInfo, error)
    Health(ctx context.Context) error
}

// NewObjectStore(ctx, cfg) returns *S3Store (minio-go) or *GCSStore
// (cloud.google.com/go/storage) depending on cfg.Provider ("s3"|"gcs").
```

#### Cache Service

```go
type RedisClient struct {
    client *redis.Client
    ttl    time.Duration
}

func (r *RedisClient) SetImage(ctx context.Context, img *domain.Image, expiry int64) error {
    data, err := json.Marshal(img)
    // Cache implementation...
}
```

### Web Layer (`internal/web/`)

**Purpose**: HTTP handlers, routing, and middleware.

#### HTTP Handlers

```go
type ImageHandler struct {
    service domain.ImageService
}

func (h *ImageHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
    // 1. Parse multipart form
    // 2. Validate request
    // 3. Call service
    // 4. Return response
}
```

#### Router Setup

```go
func SetupRouter(container *services.Container) http.Handler {
    r := chi.NewRouter()
    
    // Middleware
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.CORS)
    
    // Routes
    r.Route("/api/images", func(r chi.Router) {
        r.Post("/", handler.UploadImage)
        r.Get("/", handler.ListImages)
        r.Get("/{id}", handler.GetImage)
        r.Delete("/{id}", handler.DeleteImage)
    })
    
    return r
}
```

## 🗄️ Data Flow

### Upload Image Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant H as Handler
    participant S as Service
    participant R as Repository
    participant St as Storage
    participant Ca as Cache
    participant DB as Database
    
    C->>H: POST /api/images
    H->>S: UploadImage(request)
    S->>St: Upload file to S3/MinIO
    St-->>S: Storage path
    S->>R: Create image record
    R->>DB: INSERT image
    DB-->>R: Image with ID
    R-->>S: Image entity
    S->>Ca: Invalidate cache
    Ca-->>S: Success
    S-->>H: Image response
    H-->>C: JSON response
```

### Get Image Flow (with Cache)

```mermaid
sequenceDiagram
    participant C as Client
    participant H as Handler
    participant S as Service
    participant Ca as Cache
    participant R as Repository
    participant DB as Database
    
    C->>H: GET /api/images/123
    H->>S: GetImage(123)
    S->>Ca: GetImage(123)
    
    alt Cache Hit
        Ca-->>S: Cached image
        S-->>H: Image response
    else Cache Miss
        Ca-->>S: Not found
        S->>R: GetByID(123)
        R->>DB: SELECT image
        DB-->>R: Image data
        R-->>S: Image entity
        S->>Ca: SetImage(image)
        Ca-->>S: Cached
        S-->>H: Image response
    end
    
    H-->>C: JSON response
```

## ⚙️ Async Processing (v2)

```text
loadgen ──HTTP──▶ web (main container) ──XADD job + traceparent──▶ Valkey stream
 (Job/CLI)          │  UI + API, uploads                                 │
                    │                                    XREADGROUP      ▼
                    ├──▶ Postgres (images, status)  ◀── worker (sidecar, same image)
                    └──▶ bucket (S3 or GCS) ◀──┘  thumbnails + metadata
```

| Role | Command | `service.name` |
|---|---|---|
| web | `image-gallery serve` | `xplane-image-gallery` |
| worker | `image-gallery worker` | `xplane-image-gallery-worker` |
| load generator | `image-gallery loadgen` | `image-gallery-loadgen` |

### Upload flow

```mermaid
sequenceDiagram
    participant C as Client
    participant W as web
    participant St as Storage
    participant DB as Database
    participant Q as Valkey stream

    C->>W: POST /api/images
    W->>St: store the original object
    W->>DB: INSERT image, status=pending
    W->>Q: XADD image-gallery:jobs (traceparent, image_id)
    W-->>C: 201 response (does not wait for processing)
```

### Worker flow

```mermaid
sequenceDiagram
    participant Q as Valkey stream (group: workers)
    participant Wk as worker
    participant St as Storage
    participant DB as Database

    Q->>Wk: XREADGROUP
    Wk->>St: fetch original, build thumbnail
    Wk->>St: store thumbnail
    Wk->>DB: write metadata (dimensions, format), status=ready
    Wk->>Q: XACK
```

A freshly uploaded image is `pending` until the worker processes it — its `/thumbnail` route
serves the original in the meantime. Both the upload and the worker always write the object to
storage *before* the row that points to it (the diagrams above), so the database never claims an
object exists before it does; `/thumbnail` returns 502 only for a genuine storage failure, and 404
only when the image row itself does not exist. Failures retry with backoff; exhausted jobs move to
`image-gallery:jobs:dead`. Processing is idempotent by `image_id`.

## 🔄 Caching Strategy

### Cache-Aside Pattern

The application implements the cache-aside pattern:

1. **Read**: Check cache first, fallback to database
2. **Write**: Update database, then invalidate cache
3. **Cache Population**: Lazy loading on cache miss

### Cache Keys

```go
// Image cache keys
imageKey := fmt.Sprintf("image:%d", imageID)

// List cache keys
listKey := fmt.Sprintf("images:page:%d:size:%d", page, pageSize)

// Stats cache keys
statsKey := "images:stats"
```

### Cache TTL

```go
// Configuration-based TTL
defaultTTL := 1 * time.Hour

// Different TTL for different data types
imageTTL := 2 * time.Hour    // Individual images
listTTL := 30 * time.Minute  // Image lists
statsTTL := 5 * time.Minute  // Statistics
```

## 🗄️ Database Design

### Image Table Schema

```sql
CREATE TABLE images (
    id SERIAL PRIMARY KEY,
    filename VARCHAR(255) NOT NULL,
    original_filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    file_size BIGINT NOT NULL,
    storage_path VARCHAR(500) NOT NULL,
    uploaded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for performance
CREATE INDEX idx_images_uploaded_at ON images(uploaded_at DESC);
CREATE INDEX idx_images_content_type ON images(content_type);
```

### Migration Management

The application uses **Atlas** for schema management:

```hcl
# atlas.hcl
env "local" {
  src = "file://internal/platform/database/migrations"
  url = "postgres://testuser:testpass@localhost:5432/image_gallery_test?sslmode=disable"
  dev = "docker://postgres/15"
}
```

## 🏪 Storage Architecture

### S3-Compatible or GCS Storage

The application supports MinIO (development), AWS S3 (production) and GCS, selected by `Provider`:

```go
type StorageConfig struct {
    Provider        string // "s3" (default) or "gcs"
    Endpoint        string // localhost:9000 or s3.amazonaws.com (s3 only)
    AccessKeyID     string // Empty for IAM roles (s3 only)
    SecretAccessKey string // Empty for IAM roles (s3 only)
    BucketName      string
    Region          string // s3 only
    UseSSL          bool   // s3 only
}
```

### File Organization

```
bucket/
├── images/
│   ├── 2024/
│   │   ├── 01/
│   │   │   ├── image-123-uuid.jpg
│   │   │   └── image-124-uuid.png
│   │   └── 02/
│   └── thumbnails/
│       ├── small/
│       ├── medium/
│       └── large/
```

### AWS EKS Integration

For production deployment on AWS EKS:

```go
// Automatic credential chain for EKS Pod Identity
if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
    creds = credentials.NewChainCredentials([]credentials.Provider{
        &credentials.EnvAWS{},           // Environment variables
        &credentials.FileAWSCredentials{}, // AWS credentials file
        &credentials.IAM{},              // IAM roles (EKS Pod Identity)
    })
}
```

## 🧪 Testing Architecture

### Testing Strategy

```mermaid
pyramid
    "E2E Tests" : 10
    "Integration Tests" : 30
    "Unit Tests" : 60
```

### Unit Tests

**Location**: Alongside implementation files
**Approach**: Mocking external dependencies

```go
func TestImageService_UploadImage(t *testing.T) {
    // Arrange
    mockRepo := &mocks.ImageRepository{}
    mockStorage := &mocks.StorageService{}
    mockCache := &mocks.CacheService{}
    
    service := NewImageService(mockRepo, mockStorage, mockCache)
    
    // Act & Assert
    // Test logic...
}
```

### Integration Tests

**Location**: `internal/services/integrationtests/`
**Approach**: Real services with testcontainers

```go
func TestImageServiceIntegration(t *testing.T) {
    // Setup testcontainers
    containers := testutils.SetupContainers(t)
    defer containers.Cleanup()
    
    // Real database, storage, cache
    service := setupRealImageService(containers)
    
    // Integration tests...
}
```

### Testcontainers Setup

```go
type TestContainers struct {
    PostgreSQL testcontainers.Container
    MinIO      testcontainers.Container
    Valkey     testcontainers.Container
}

func SetupContainers(t *testing.T) *TestContainers {
    // Start PostgreSQL
    postgres := setupPostgreSQL(t)
    
    // Start MinIO
    minio := setupMinIO(t)
    
    // Start Valkey
    valkey := setupValkey(t)
    
    return &TestContainers{
        PostgreSQL: postgres,
        MinIO:      minio,
        Valkey:     valkey,
    }
}
```

## 🚀 Performance Considerations

### Caching Strategy

1. **Hot Data**: Frequently accessed images cached with longer TTL
2. **List Pagination**: Cached list responses for common queries
3. **Cache Invalidation**: Smart invalidation on data changes

### Database Optimization

1. **Indexes**: Strategic indexing for common queries
2. **Connection Pooling**: Efficient database connection management
3. **Query Optimization**: Optimized queries for list operations

### Storage Optimization

1. **Multipart Uploads**: For large files
2. **CDN Integration**: CloudFront or similar for global distribution
3. **Image Optimization**: Automatic resizing and format conversion

## 🔧 Configuration Management

### Environment-Based Configuration

```go
type Config struct {
    Server   ServerConfig   `env:",prefix="`
    Database DatabaseConfig `env:",prefix=DATABASE_"`
    Storage  StorageConfig  `env:",prefix=STORAGE_"`
    Cache    CacheConfig    `env:",prefix=CACHE_"`
}
```

### Configuration Loading

```go
func Load() (Config, error) {
    var cfg Config
    
    // Load from environment variables
    if err := env.Parse(&cfg); err != nil {
        return cfg, err
    }
    
    // Validate configuration
    if err := cfg.Validate(); err != nil {
        return cfg, err
    }
    
    return cfg, nil
}
```

## 🎯 Future Architecture Enhancements

### Planned Improvements

1. **HTMX Integration**: Server-side rendered UI with HTMX
2. **Event Sourcing**: Domain events for audit trail
3. **CQRS**: Command Query Responsibility Segregation
4. **Microservices**: Potential split into focused services
5. **Observability**: Metrics, tracing, and monitoring

### Scalability Considerations

1. **Horizontal Scaling**: Stateless application design
2. **Database Sharding**: For large-scale deployments
3. **Cache Clustering**: Redis/Valkey cluster mode
4. **Load Balancing**: Multiple application instances

### Monitoring and Observability

1. **Structured Logging**: JSON-formatted logs
2. **Metrics Collection**: Prometheus-compatible metrics
3. **Distributed Tracing**: OpenTelemetry integration
4. **Health Checks**: Comprehensive health endpoints