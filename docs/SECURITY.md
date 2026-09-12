# Security Practices

This document outlines the security practices, scanning tools, and best practices implemented in the Image Gallery application.

## 🔒 Security Overview

The Image Gallery application implements a **security-first approach** with multiple layers of protection:

- **Static Code Analysis** - Early detection of vulnerabilities
- **Dependency Scanning** - Continuous monitoring of third-party packages
- **Container Security** - Image vulnerability scanning
- **Secure Defaults** - Minimal attack surface configuration
- **Access Controls** - Proper authentication and authorization

## 🛡️ Security Scanning Tools

### 1. Govulncheck (Go Vulnerability Scanner)

**Purpose**: Scans Go code and dependencies for known vulnerabilities

**Usage**:
```bash
# Local scanning
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# Dagger-based scanning (recommended)
make dagger-vulncheck
```

**Integration**: 
- Runs in CI validation pipeline
- Scans both direct and indirect dependencies
- Provides detailed vulnerability reports

### 2. Trivy Security Scanner

**Purpose**: Multi-purpose security scanner for containers and filesystems

**Capabilities**:
- Container image vulnerability scanning
- Filesystem scanning for secrets and misconfigurations
- SBOM (Software Bill of Materials) generation
- Multiple output formats (SARIF, JSON, table)

**Usage**:
```bash
# Filesystem scanning
make dagger-trivy-fs

# Container image scanning
make dagger-trivy

# Manual Trivy usage
trivy fs .
trivy image your-image:tag
```

**Output Formats**:
- **SARIF**: For GitHub Security tab integration
- **Table**: Human-readable console output
- **JSON**: For programmatic processing

### 3. Golangci-lint

**Purpose**: Code quality and security linting

**Security Rules**:
- Detects potential security issues
- Enforces secure coding practices
- Prevents common vulnerabilities

**Usage**:
```bash
# Local linting
make lint

# Dagger-based linting (recommended)
make dagger-lint
```

## 🚀 CI/CD Security Integration

### Security Scanning Pipeline

The security scanning is integrated into our Dagger-based CI/CD pipeline:

```mermaid
graph LR
    A[Code Push] --> B[Lint & Test]
    B --> C[Govulncheck]
    C --> D[Trivy FS Scan]
    D --> E[Build]
    E --> F[Container Build]
    F --> G[Trivy Image Scan]
    G --> H[SARIF Upload]
    H --> I[GitHub Security]
```

### 1. Early Stage Security (CI Validation)

**Files**: `.github/workflows/ci.yml`

```bash
# Runs on all branches and PRs
- govulncheck scan (dependency vulnerabilities)
- Trivy filesystem scan (secrets, misconfigurations)
- golangci-lint security rules
```

### 2. Production Security (Build & Push)

**Files**: `.github/workflows/build-push.yml`

```bash
# Runs on main branch only
- Container image vulnerability scanning
- SARIF report generation
- GitHub Security tab integration
```

### 3. Local Security Testing

```bash
# Run complete security pipeline locally
make dagger-ci

# Individual security checks
make dagger-vulncheck    # Go vulnerabilities
make dagger-trivy-fs     # Filesystem scan
make dagger-trivy        # Container scan
```

## 🔧 Security Configuration

### Container Security

#### Base Image
```dockerfile
# Uses distroless for minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot
```

**Benefits**:
- Minimal base image (no shell, package managers)
- Non-root user by default
- Regular security updates
- Reduced attack surface

#### Multi-Architecture Builds
```bash
# Supports both architectures
- linux/amd64
- linux/arm64
```

### Application Security

#### Environment Variables

**Secure Defaults**:
```bash
# Database
DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=require

# Storage (AWS S3 with IAM roles)
STORAGE_ENDPOINT=s3.amazonaws.com
STORAGE_ACCESS_KEY=""      # Empty to use IAM roles/Pod Identity
STORAGE_SECRET_KEY=""      # Empty to use IAM roles/Pod Identity
STORAGE_USE_SSL=true       # Always use SSL in production

# Cache
CACHE_PASSWORD=strong_password
```

#### AWS EKS Pod Identity Support

The application supports AWS EKS Pod Identity for secure access to AWS services:

```go
// Automatic credentials chain when keys are empty
if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
    creds = credentials.NewChainCredentials([]credentials.Provider{
        &credentials.EnvAWS{},
        &credentials.FileAWSCredentials{},
        &credentials.IAM{},
    })
}
```

### Database Security

#### Connection Security
```bash
# Always use SSL in production
DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=require
```

#### Migration Security
- Atlas-managed schema migrations
- Version-controlled database changes
- Rollback capabilities

### Cache Security

#### Valkey/Redis Security
```bash
# Authentication
CACHE_PASSWORD=strong_password

# Network isolation
CACHE_ADDRESS=internal-cache:6379  # Internal network only
```

## 📊 Security Monitoring

### GitHub Security Tab

The application integrates with GitHub's Security tab through SARIF uploads:

1. **Vulnerability Alerts**: Automatic detection of dependency vulnerabilities
2. **Code Scanning**: Trivy and govulncheck results
3. **Secret Scanning**: Detection of accidentally committed secrets
4. **Dependency Review**: PR-based dependency analysis

### Security Artifacts

The CI/CD pipeline generates and stores security artifacts:

```bash
# Artifacts with retention policies
- coverage-report (30 days)
- security-scan-results (30 days)
  - trivy-results.sarif
  - trivy-results.json
- release-notes (90 days)
```

## 🚨 Vulnerability Response

### Critical Vulnerabilities

**Process**:
1. **Detection**: Automated scanning in CI/CD
2. **Assessment**: Review severity and impact
3. **Response**: Emergency fix or mitigation
4. **Verification**: Re-scan after fix
5. **Communication**: Update security advisories

### Dependency Updates

**Regular Updates**:
```bash
# Update Go dependencies
go get -u ./...
go mod tidy

# Update Dagger modules (check for new versions)
dagger install github.com/module@latest
```

**Automated Scanning**:
- Daily vulnerability scans in CI
- Dependabot integration (if configured)
- Security advisory notifications

## 🔍 Security Best Practices

### Code Security

#### Secure Coding Practices

1. **Input Validation**:
   ```go
   // Validate all inputs
   if err := validate.Struct(request); err != nil {
       return errors.New("invalid input")
   }
   ```

2. **Error Handling**:
   ```go
   // Don't expose internal errors
   log.Error("database error: %v", err)
   return errors.New("internal server error")
   ```

3. **Secrets Management**:
   ```go
   // Never log secrets
   log.Info("connecting to database at %s", maskURL(dbURL))
   ```

#### File Upload Security

```go
// Validate file types
allowedTypes := []string{"image/jpeg", "image/png", "image/gif"}

// Limit file sizes
maxSize := 10 << 20 // 10MB

// Scan uploaded files
// (Consider integrating virus scanning)
```

### Infrastructure Security

#### Docker Security

```dockerfile
# Use specific versions
FROM golang:1.26-alpine AS builder

# Create non-root user
RUN adduser -D -g '' appuser

# Use multi-stage builds
FROM gcr.io/distroless/static-debian12:nonroot
USER nonroot:nonroot
```

#### Network Security

```yaml
# docker-compose.yml - Isolated networks
networks:
  backend:
    driver: bridge
  frontend:
    driver: bridge
```

### Production Security

#### Environment Configuration

```bash
# Production environment
GO_ENV=production

# Secure headers
SECURE_HEADERS=true

# Rate limiting
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=1m
```

#### Monitoring and Logging

```go
// Structured logging
log.WithFields(log.Fields{
    "user_id": userID,
    "action":  "upload_image",
    "ip":      clientIP,
}).Info("image uploaded")

// Security events
log.WithFields(log.Fields{
    "event": "failed_login_attempt",
    "ip":    clientIP,
}).Warn("security event")
```

## 🔧 Security Tools Integration

### Local Security Testing

```bash
# Complete security check
make dagger-ci

# Quick security scan
make dagger-vulncheck && make dagger-trivy-fs

# Container security (after build)
make dagger-docker && make dagger-trivy
```

### IDE Integration

**Recommended VS Code Extensions**:
- Go (official) - Built-in security linting
- SonarLint - Additional security rules
- GitLens - Commit and change tracking

**Recommended Settings**:
```json
{
    "go.lintTool": "golangci-lint",
    "go.lintFlags": ["--enable=gosec,gas"]
}
```

## 📋 Security Checklist

### Development
- [ ] Run security scans before committing
- [ ] Review dependencies for vulnerabilities
- [ ] Validate all inputs
- [ ] Use secure defaults
- [ ] Never commit secrets

### CI/CD
- [ ] All security scans passing
- [ ] Container images scanned
- [ ] SARIF reports uploaded
- [ ] No high/critical vulnerabilities
- [ ] Security artifacts archived

### Production
- [ ] Environment variables secured
- [ ] SSL/TLS enabled
- [ ] Monitoring configured
- [ ] Backup and recovery tested
- [ ] Incident response plan ready

## 🆘 Security Incident Response

### Immediate Response

1. **Isolate**: Contain the affected systems
2. **Assess**: Determine scope and impact
3. **Communicate**: Notify stakeholders
4. **Document**: Record all actions taken

### Recovery Process

1. **Patch**: Apply security fixes
2. **Verify**: Re-run security scans
3. **Deploy**: Roll out fixes
4. **Monitor**: Watch for additional issues
5. **Review**: Post-incident analysis

### Contact Information

For security issues:
- **Internal**: Development team lead
- **External**: security@company.com
- **Emergency**: Follow incident response plan

## 📚 Additional Resources

- [OWASP Go Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Go_Secure_Coding_Cheat_Sheet.html)
- [CIS Docker Benchmark](https://www.cisecurity.org/benchmark/docker)
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/)
- [AWS Security Best Practices](https://aws.amazon.com/architecture/security-identity-compliance/)