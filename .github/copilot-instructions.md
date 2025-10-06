# Spotalis Development Guidelines

Spotalis is a Kubernetes controller for optimizing workload replica distribution across spot and on-demand instances using annotation-driven configuration.

## Architecture Overview

**Single Binary Pattern**: Following Karpenter architecture - one `cmd/controller/main.go` integrates controller, webhook, and HTTP server in a unified operator.

**Dependency Injection System**: Uses Uber Dig v1.19.0 for service lifecycle management:
- **DI Container** (`pkg/di/`): Centralized dependency injection with service registry
- **Configuration System** (`pkg/config/`): Consolidated YAML configuration with environment overrides
- **Application Lifecycle** (`pkg/di/application.go`): DI-managed operator startup and coordination

**Core Components**:
- **Operator** (`pkg/operator/`): Main lifecycle manager with leader election, shutdown handling
- **Controllers** (`pkg/controllers/`): Deployment/StatefulSet reconcilers with replica distribution logic
- **Webhook** (`pkg/webhook/`): Mutating admission webhook for pod nodeSelector injection
- **Annotation Parser** (`internal/annotations/`): Parses workload configuration from k8s annotations

**Data Flow**: Configuration loading → DI container registration → Operator resolution → Controller startup → Annotation processing → Pod mutation

## Development Workflow

```bash
# Essential commands
make run                    # Run locally against kubeconfig cluster  
make test                   # Unit tests with envtest
make test-integration       # Integration tests requiring Kind cluster
make generate              # Generate code/manifests with controller-gen
make docker-build          # Build container image

# Integration testing
kubectl config use-context kind-spotalis  # Connect to Kind cluster first
make test-integration-cleanup              # Clean test namespaces
```

## Key Patterns

**Dependency Injection Pattern**: All services managed through DI container:
```go
// Main entry point uses DI for operator lifecycle
app, err := di.NewApplication(ctx)
if err := app.Start(ctx); err != nil { ... }

// DI container resolves dependencies automatically
err := container.Invoke(func(op *operator.Operator) {
    startErr = op.Start(ctx)
})
```

**Configuration System**: Consolidated YAML with environment variable overrides:
```go
// Unified configuration structure in pkg/config/
type SpotalisConfig struct {
    Operator      OperatorConfig      `yaml:"operator"`
    Controllers   ControllersConfig   `yaml:"controllers"`  
    Webhook       WebhookConfig       `yaml:"webhook"`
    Observability ObservabilityConfig `yaml:"observability"`
}
```

**Annotation-Driven Configuration**: No CRDs - configuration via annotations like:
```go
// Primary annotations in internal/annotations/parser.go
"spotalis.io/enabled": "true"
"spotalis.io/spot-percentage": "70"  
"spotalis.io/min-on-demand": "1"
```

**Testing Architecture**: 
- **Unit tests**: Use Ginkgo/Gomega with controller-runtime envtest
- **DI tests**: Test dependency resolution without K8s connection (`pkg/di/application_test.go`)
- **Integration tests**: Require live Kind cluster with Spotalis deployed
- **Test helper**: `tests/integration/shared/helper.go` provides Kind cluster utilities

**Operator Lifecycle**: DI-managed startup with centralized configuration:
- Configuration loading through `pkg/config/loader.go`
- Service registration via `pkg/di/registry.go`  
- Operator resolution and startup through DI container
- HTTP servers for metrics (:8080), health (:8081), webhook (:9443)
- Leader election and graceful shutdown coordination

## Project Structure

```
cmd/controller/main.go     # Single binary entry point with DI integration
pkg/di/                    # Dependency injection system
  ├── application.go       # Main application lifecycle management  
  ├── registry.go          # Service registration for DI container
  └── container.go         # Uber Dig wrapper
pkg/config/                # Consolidated configuration system
  ├── loader.go            # YAML + environment variable loading
  └── types.go             # Unified configuration structures
pkg/operator/              # Operator lifecycle & coordination
pkg/controllers/           # Workload reconciliation logic  
pkg/webhook/               # Pod mutation for nodeSelector
pkg/apis/                  # Configuration & state types
internal/annotations/      # Annotation parsing logic
internal/config/           # Node classification & config
tests/integration/         # Kind cluster integration tests
```

## Common Issues & Debugging

**Nil Pointer Panics in Controllers**: Check leader election manager initialization in `pkg/operator/operator.go`. Controllers expect `LeaderElectionManager` to be non-nil when `config.LeaderElection` is true.

**DI Container Issues**: If services fail to resolve, check `pkg/di/registry.go` for proper service registration. All dependencies must be registered before container invoke.

**Integration Test Setup**: Tests require Kind cluster with Spotalis deployed. Use `kubectl config use-context kind-spotalis` before running integration tests.

**Webhook Certificate Issues**: For local development, disable webhooks in operator config or ensure certificates are properly mounted at `/tmp/k8s-webhook-server/serving-certs/`.

**Configuration Loading**: Check environment variable precedence in `pkg/config/loader.go`. Environment variables override YAML file values with `SPOTALIS_` prefix.

## Git Commit Convention

**Spotalis follows Angular commit message convention** for consistency and automated changelog generation.

### Commit Message Format

```
<type>(<scope>): <short summary>
  │       │             │
  │       │             └─> Summary in present tense. Not capitalized. No period.
  │       │
  │       └─> Scope: controllers|webhook|config|di|operator|metrics|etc.
  │
  └─> Type: feat|fix|docs|style|refactor|test|chore|perf|ci|build

<BLANK LINE>

<body: detailed explanation of what and why, not how>

<BLANK LINE>

<footer: breaking changes, issue references, etc.>
```

### Commit Types

- `feat`: New feature for the user or significant addition
- `fix`: Bug fix for the user
- `docs`: Documentation only changes
- `style`: Code style changes (formatting, missing semicolons, etc.)
- `refactor`: Code restructuring without behavior change
- `test`: Adding or updating tests
- `chore`: Maintenance tasks, dependency updates, build scripts
- `perf`: Performance improvements
- `ci`: CI/CD pipeline changes
- `build`: Build system or external dependency changes

### Commit Scopes

Scopes identify which part of the codebase is affected:

- `controllers`: DeploymentReconciler, StatefulSetReconciler, ControllerManager
- `webhook`: Admission webhook, mutating webhook logic
- `config`: Configuration loading, validation, types
- `di`: Dependency injection container, application lifecycle
- `operator`: Operator core, leader election, lifecycle management
- `metrics`: Prometheus metrics, collectors, recording
- `annotations`: Annotation parsing and validation
- `apis`: API types, configurations, state objects
- `logging`: Logging infrastructure and utilities
- `utils`: Shared utility functions
- `server`: HTTP servers (metrics, health, webhook endpoints)
- `nodeclassifier`: Node classification logic
- `tests`: Test infrastructure, helpers, integration tests
- `docs`: Documentation files
- `build`: Build scripts, Makefiles, Dockerfiles
- `ci`: GitHub Actions, CI/CD workflows

### Examples

```
feat(controllers): add atomic reconcile count tracking

Add thread-safe reconcile count tracking using atomic.Int64 to both
DeploymentReconciler and StatefulSetReconciler.

- Add reconcileCount and errorCount atomic.Int64 fields
- Increment reconcileCount at Reconcile() method entry
- Add GetReconcileCount() and GetErrorCount() getter methods

Related to Task 2 from improvements.md
```

```
fix(webhook): prevent nil pointer in pod mutation

Add nil check for pod.Spec.NodeSelector before mutation to prevent
panic when processing pods without existing nodeSelector.

Fixes #123
```

```
test(controllers): add reconcile count tracking tests

Add comprehensive test coverage for reconcile count tracking behavior
in both DeploymentReconciler and StatefulSetReconciler.

Closes Task 2 from improvements.md
```

<!-- MANUAL ADDITIONS START -->
Consult files from [specs/](../specs/001-build-kubernetes-native/) for additional context on design decisions and architecture.
<!-- MANUAL ADDITIONS END -->