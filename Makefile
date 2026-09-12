.PHONY: build test clean run dev docker-build docker-up docker-down fmt lint vet test-ci build-ci vulncheck trivy docker-ci ci release soak

# Build the application
build:
	@echo "Building the application..."
	go build -o ./bin/image-gallery ./cmd/image-gallery

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf ./bin
	rm -f coverage.out coverage.html

# Run the application locally
run: build
	@echo "Running the application..."
	./bin/image-gallery serve

# Run in development mode with hot reload (requires air)
dev:
	@if command -v air > /dev/null; then \
		air; \
	else \
		echo "Installing air for hot reload..."; \
		go install github.com/air-verse/air@latest; \
		air; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Lint code (using Dagger for consistency with CI)
lint:
	@echo "Running linting with Dagger (matches CI environment)..."
	@if command -v dagger > /dev/null; then \
		dagger call -m github.com/sagikazarmark/daggerverse/go@v0.9.0 exec \
			--src=. \
			--args=go,run,github.com/golangci/golangci-lint/cmd/golangci-lint@latest,run; \
	else \
		echo "Dagger not installed. Run 'make install-tools' first"; \
		exit 1; \
	fi

# Vet code
vet:
	@echo "Vetting code..."
	go vet ./...

# Docker build
docker-build:
	@echo "Building Docker image..."
	docker build -t image-gallery:latest .

# Docker compose up
docker-up:
	@echo "Starting services with Docker Compose..."
	docker-compose up --build -d

# Docker compose down
docker-down:
	@echo "Stopping services..."
	docker-compose down

# Local soak (success criterion 5): loadgen mixed at 25 req/s under memory
# limits, exporting 100% sampling to local VictoriaMetrics + VictoriaTraces.
# DURATION defaults to 15m; override with DURATION=5m ./scripts/soak.sh for a
# shorter run.
soak:
	@echo "Running local soak..."
	./scripts/soak.sh

# Install development dependencies
install-tools:
	@echo "Installing development tools..."
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing Atlas CLI..."
	@if ! command -v atlas > /dev/null; then \
		curl -sSf https://atlasgo.sh | sh -s -- --yes; \
	else \
		echo "Atlas CLI already installed"; \
	fi
	@echo "Installing Dagger CLI..."
	@if ! command -v dagger > /dev/null; then \
		curl -L https://dl.dagger.io/dagger/install.sh | sh; \
	else \
		echo "Dagger CLI already installed"; \
	fi

# Dagger-based CI/CD commands (using existing modules)

test-ci:
	@echo "Running tests with Dagger..."
	@if command -v dagger > /dev/null; then \
		dagger call -m github.com/sagikazarmark/daggerverse/go@v0.9.0 exec \
			--src=. \
			--args=go,test,./...,--coverprofile=coverage.out,--race,--short; \
	else \
		echo "Dagger not installed. Run 'make install-tools' first"; \
	fi

vulncheck:
	@echo "Running vulnerability scan with Dagger..."
	@if command -v dagger > /dev/null; then \
		dagger call -m github.com/sagikazarmark/daggerverse/go@v0.9.0 exec \
			--src=. \
			--args=go,run,golang.org/x/vuln/cmd/govulncheck@latest,./...; \
	else \
		echo "Dagger not installed. Run 'make install-tools' first"; \
		exit 1; \
	fi

build-ci:
	@echo "Building application with Dagger..."
	@if command -v dagger > /dev/null; then \
		dagger call -m github.com/sagikazarmark/daggerverse/go@v0.9.0 exec \
			--src=. \
			--args=go,build,-ldflags,"-w -s",-o,./bin/image-gallery,./cmd/image-gallery; \
	else \
		echo "Dagger not installed. Run 'make install-tools' first"; \
		exit 1; \
	fi

docker-ci:
	@echo "Building Docker image with Dagger..."
	@if command -v dagger > /dev/null; then \
		dagger -m github.com/sagikazarmark/daggerverse/go@v0.9.0 call \
			--source=. \
			--version=1.25 \
			with-platform linux/amd64,linux/arm64 \
			with-cgo-disabled \
			build \
			--package=./cmd/image-gallery \
			--ldflags="-w -s" \
			container \
			--base-image=gcr.io/distroless/static-debian12:nonroot \
			--binary-name=image-gallery \
			with-exposed-port 8080 \
			with-label org.opencontainers.image.source=https://github.com/smana/image-gallery \
			with-label org.opencontainers.image.title=image-gallery; \
	else \
		echo "Dagger not installed. Run 'make install-tools' first"; \
	fi


trivy:
	@echo "Running container image security scan with Trivy and Dagger..."
	@if command -v dagger > /dev/null; then \
		echo "Building image first..."; \
		make docker-ci > /dev/null 2>&1; \
		echo "Scanning image-gallery:latest..."; \
		dagger -m github.com/jpadams/daggerverse/trivy@v0.6.0 call \
			scan-image \
			--image-ref=image-gallery:latest \
			--severity=HIGH,CRITICAL \
			--format=table; \
	else \
		echo "Dagger not installed. Run 'make install-tools' first"; \
	fi

# Run complete CI pipeline locally
ci:
	@echo "Running complete CI pipeline with Dagger..."
	@make lint
	@make test-ci
	@make vulncheck
	@make trivy
	@make build-ci

# Release command - prepare and validate for release
release:
	@echo "🚀 Preparing for release..."
	@echo ""
	@echo "This command will:"
	@echo "  1. Run complete CI pipeline"
	@echo "  2. Check conventional commit format"
	@echo "  3. Validate release readiness"
	@echo "  4. Guide you through creating a release"
	@echo ""
	@read -p "Continue? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@echo ""
	@echo "📋 Step 1: Running CI pipeline..."
	@make ci
	@echo ""
	@echo "✅ CI pipeline completed successfully!"
	@echo ""
	@echo "📋 Step 2: Checking recent commits format..."
	@if command -v git > /dev/null; then \
		echo "Recent commits:"; \
		git log --oneline -10 --pretty=format:"  %C(yellow)%h%C(reset) %s" | head -10; \
		echo ""; \
		echo ""; \
		echo "📝 Conventional Commits format:"; \
		echo "  feat: add new feature (minor version bump)"; \
		echo "  fix: bug fix (patch version bump)"; \
		echo "  feat!: breaking change (major version bump)"; \
		echo "  docs: documentation changes"; \
		echo "  ci: CI/CD changes"; \
		echo "  refactor: code refactoring"; \
		echo ""; \
		echo "📋 Step 3: Checking git status..."; \
		if [ -n "$$(git status --porcelain)" ]; then \
			echo "❌ Working directory is not clean. Please commit or stash changes."; \
			git status --short; \
			exit 1; \
		else \
			echo "✅ Working directory is clean."; \
		fi; \
		echo ""; \
		echo "📋 Step 4: Checking current branch..."; \
		CURRENT_BRANCH=$$(git rev-parse --abbrev-ref HEAD); \
		if [ "$$CURRENT_BRANCH" != "main" ]; then \
			echo "❌ Not on main branch (currently on $$CURRENT_BRANCH)."; \
			echo "Please switch to main branch: git checkout main"; \
			exit 1; \
		else \
			echo "✅ On main branch."; \
		fi; \
		echo ""; \
		echo "📋 Step 5: Checking for unpushed commits..."; \
		UNPUSHED=$$(git log origin/main..HEAD --oneline | wc -l); \
		if [ $$UNPUSHED -gt 0 ]; then \
			echo "❌ There are $$UNPUSHED unpushed commits."; \
			echo "Please push your commits: git push origin main"; \
			exit 1; \
		else \
			echo "✅ All commits are pushed."; \
		fi; \
	else \
		echo "❌ Git not found. Please install git."; \
		exit 1; \
	fi
	@echo ""
	@echo "🎉 Release validation completed successfully!"
	@echo ""
	@echo "📋 Next steps to create a release:"
	@echo ""
	@echo "  1. Push any final commits to main branch:"
	@echo "     git add . && git commit -m 'feat: your feature description'"
	@echo "     git push origin main"
	@echo ""
	@echo "  2. The release-please action will automatically:"
	@echo "     - Create a Release PR with changelog"
	@echo "     - Update version numbers"
	@echo "     - Generate release notes"
	@echo ""
	@echo "  3. Review and merge the Release PR when ready"
	@echo ""
	@echo "  4. After merging, a GitHub release will be created with:"
	@echo "     - Multi-platform binaries"
	@echo "     - Container images"
	@echo "     - Security scan results"
	@echo "     - Automated release notes"
	@echo ""
	@echo "📖 For more details, see: docs/DEVELOPMENT.md#release-process"

# Atlas database schema management
atlas-validate:
	@echo "Validating Atlas configuration..."
	atlas schema validate --env local

atlas-inspect:
	@echo "Inspecting current database schema..."
	atlas schema inspect --env local

atlas-diff:
	@echo "Generating schema diff..."
	atlas migrate diff --env local

atlas-apply:
	@echo "Applying schema changes..."
	atlas schema apply --env local --auto-approve

# Run database migrations (Atlas only)
migrate:
	@echo "Running database migrations with Atlas..."
	@if command -v atlas > /dev/null; then \
		atlas migrate apply --env local; \
	else \
		echo "❌ Atlas CLI not found. Please install it first:"; \
		echo "   curl -sSf https://atlasgo.sh | sh"; \
		echo "   Or run: make install-tools"; \
		exit 1; \
	fi

# Database operations
db-start:
	@echo "Starting database services..."
	docker-compose up -d postgres

db-stop:
	@echo "Stopping database services..."
	docker-compose stop postgres

db-reset:
	@echo "Resetting database..."
	docker-compose down -v postgres
	docker-compose up -d postgres
	sleep 3
	make migrate

# Show help
help:
	@echo "Available commands:"
	@echo ""
	@echo "Build & Development:"
	@echo "  build           - Build the application (local)"
	@echo "  test            - Run tests (local, fast)"
	@echo "  test-coverage   - Run tests with coverage report"
	@echo "  clean           - Clean build artifacts"
	@echo "  run             - Run the application locally"
	@echo "  dev             - Run in development mode with hot reload"
	@echo "  fmt             - Format code"
	@echo "  lint            - Lint code (matches CI exactly)"
	@echo "  vet             - Vet code"
	@echo ""
	@echo "CI/CD (using Dagger for consistency):"
	@echo "  ci              - Run complete CI pipeline (lint + test-ci + vulncheck + trivy + build-ci)"
	@echo "  test-ci         - Run tests with Dagger (CI environment)"
	@echo "  vulncheck       - Run vulnerability scan with Dagger"
	@echo "  trivy           - Run container image security scan with Trivy"
	@echo "  build-ci        - Build application with Dagger (CI environment)"
	@echo "  docker-ci       - Build Docker image with Dagger (CI environment)"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-up       - Start services with Docker Compose"
	@echo "  docker-down     - Stop services"
	@echo ""
	@echo "Database (Atlas-powered):"
	@echo "  migrate         - Run database migrations via Atlas CLI"
	@echo "  atlas-validate  - Validate Atlas configuration"
	@echo "  atlas-inspect   - Inspect current database schema"
	@echo "  atlas-diff      - Generate schema diff"
	@echo "  atlas-apply     - Apply schema changes"
	@echo "  db-start        - Start database services only"
	@echo "  db-stop         - Stop database services"
	@echo "  db-reset        - Reset database with fresh schema"
	@echo ""
	@echo "Release:"
	@echo "  release         - Prepare and validate for release (run tests, check commits)"
	@echo ""
	@echo "Tools:"
	@echo "  install-tools   - Install development tools (including Dagger)"
	@echo "  help            - Show this help message"
