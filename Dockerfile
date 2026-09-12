# Multi-stage build for optimized production image
FROM golang:1.25-alpine AS builder

# Install git for go mod download
RUN apk add --no-cache git ca-certificates

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files first for better caching
COPY go.mod go.sum ./

# Download dependencies (cached if go.mod/go.sum unchanged)
RUN go mod download

# Copy the source code
COPY . .

# Build the Go app with optimizations and verify it exists
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o image-gallery ./cmd/image-gallery

# Verify the binary was created
RUN ls -la /app/image-gallery

# Start a new stage from scratch
FROM alpine:3.19

# Install ca-certificates for HTTPS requests and timezone data
RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

# Create non-root user first
RUN addgroup -g 1001 appgroup && \
    adduser -D -u 1001 -G appgroup appuser

# Copy the binary from the builder stage
COPY --from=builder /app/image-gallery /app/image-gallery

# Copy web assets if they exist (optional)
RUN mkdir -p /app/web

# Verify binary was copied and make it executable
RUN ls -la /app/ && chmod +x /app/image-gallery

# Change ownership to non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port 8080
EXPOSE 8080

# Add healthcheck (commented out for now to avoid curl dependency issues)
# HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
#     CMD curl -f http://localhost:8080/health || exit 1

# ENTRYPOINT + CMD: a Kubernetes `args` (the worker sidecar's ["worker"]) replaces CMD only.
ENTRYPOINT ["/app/image-gallery"]
CMD ["serve"]


