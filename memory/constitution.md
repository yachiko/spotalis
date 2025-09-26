# Spotalis Constitution

## Core Principles

### Article I: Test-First Development
TDD mandatory: Tests written → User approved → Tests fail → Then implement; Red-Green-Refactor cycle strictly enforced

**Sub-principles:**
- Unit tests must use Ginkgo/Gomega with controller-runtime envtest
- DI tests validate dependency resolution without Kubernetes connection (`pkg/di/application_test.go`)  
- Integration tests require live Kind cluster with Spotalis deployed
- All business logic must be testable in isolation through dependency injection

### Article II: Dependency Injection Architecture
All services managed through centralized DI container using Uber Dig v1.19.0

**Sub-principles:**
- All operator components resolved via DI container (`pkg/di/`)
- Service registration through `pkg/di/registry.go` with typed constructors
- Application lifecycle managed by `pkg/di/application.go` 
- No direct instantiation of core services - all through DI resolution
- DI container must be testable without Kubernetes cluster connection

### Article III: Configuration System
Consolidated YAML configuration with environment variable overrides

**Sub-principles:**
- Unified configuration structure in `pkg/config/types.go`
- Environment variables use `SPOTALIS_` prefix and override YAML values
- Configuration loading through `pkg/config/loader.go`
- No scattered configuration - all centralized through `SpotalisConfig`
- Configuration must be DI-injectable for testing

### Article IV: Single Binary Pattern
Following Karpenter architecture - one binary integrates all components

**Sub-principles:**
- `cmd/controller/main.go` as single entry point with DI integration
- Operator, webhook, and HTTP servers in unified executable
- No separate binaries for different components
- Multi-stage Docker builds with distroless runtime images
- Binary must support both config file and environment variable configuration

### Article V: Golang Standards
All code written in Go with modern practices

**Sub-principles:**
- Go 1.21+ required for generics and modern features
- Controller-runtime framework for Kubernetes interactions
- Structured logging with configurable levels
- Graceful shutdown with context cancellation
- Standard project layout following Go conventions

### Article VI: Annotation-Driven Configuration
No CRDs - workload configuration via Kubernetes annotations

**Sub-principles:**
- Primary annotations: `spotalis.io/enabled`, `spotalis.io/spot-percentage`, `spotalis.io/min-on-demand`
- Annotation parsing through `internal/annotations/parser.go`
- Node classification via `internal/config/node_classifier.go`
- Configuration validation at annotation parse time
- Backward compatibility for annotation schema changes

### Article VII: Observability Requirements
Multi-tier observability with configurable endpoints

**Sub-principles:**
- Metrics server on `:8080` with Prometheus format (`/metrics`)
- Health checks on `:8081` with structured endpoints (`/healthz`)
- Webhook server on `:9443` with TLS certificate management
- Structured logging with JSON/text format options
- All endpoints must be configurable and disableable

### Article VIII: Kubernetes Integration
Native Kubernetes controller following operator patterns

**Sub-principles:**
- Controller-runtime manager for workload reconciliation
- Leader election for high availability deployments  
- Namespace filtering with required annotations check
- Webhook for pod mutation with nodeSelector injection
- RBAC with minimal required permissions
- Deployment via standard Kubernetes manifests

### Article IX: Development Workflow
Standardized development and testing procedures

**Sub-principles:**
- Kind cluster for integration testing (`kubectl config use-context kind-spotalis`)
- Make targets: `run`, `test`, `test-integration`, `generate`, `docker-build`
- Docker Compose for local development environment
- Integration test cleanup via `make test-integration-cleanup`
- Code generation with controller-gen for manifests

## Enforcement and Amendments

### Enforcement
All code contributions must adhere to these constitutional principles. Violations must be addressed before merge approval.

### Amendment Process
Constitutional amendments require:
1. Documentation of why current principles are insufficient
2. Impact analysis on existing codebase
3. Update of dependent templates per `/memory/constitution_update_checklist.md`
4. Version increment in constitution header

### Current State Validation
This constitution reflects the project state as of September 26, 2025:
- ✅ Dependency injection system fully implemented with Uber Dig
- ✅ Consolidated configuration system operational
- ✅ Testing framework with unit/integration/DI test separation
- ✅ Single binary with multi-component architecture
- ✅ Kubernetes-native controller with webhook integration

---

**Constitution Version:** 3.0.0  
**Last Updated:** September 26, 2025  
**Next Review:** December 26, 2025 