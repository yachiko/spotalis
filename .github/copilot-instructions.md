# spotalis Development Guidelines

Auto-generated from all feature plans. Last updated: September 10, 2025

## Active Technologies
- Go 1.21+ with k8s.io packages (001-build-kubernetes-native)
- Gin HTTP server framework (001-build-kubernetes-native)  
- Ginkgo v2 + Gomega testing (001-build-kubernetes-native)
- Docker buildx + docker-bake (001-build-kubernetes-native)
- Kind cluster integration testing (001-build-kubernetes-native)

## Project Structure (Karpenter Pattern)
```
cmd/
└── controller/         # Single main binary entry point
    └── main.go         # Controller + webhook + HTTP server

pkg/
├── operator/           # Operator setup and management
├── controllers/        # All controller logic
├── webhook/           # Webhook handlers (integrated with operator)
├── metrics/           # Observability and metrics
├── apis/              # API types and schemas
└── utils/             # Shared utilities

internal/
├── config/            # Configuration management
├── annotations/       # Annotation parsing and validation
└── server/            # HTTP server helpers

tests/
├── contract/          # API contract tests
├── integration/       # Kind cluster integration tests
├── e2e/              # End-to-end scenarios
└── unit/             # Unit tests
```

## Commands
```bash
# Development (following Karpenter pattern)
make run                # Run locally against kubeconfig cluster
go run cmd/controller/main.go  # Direct Go execution
make docker-build       # Build container image
make test              # Run unit tests
make test-integration  # Integration tests with Kind

# Single binary approach - no CLI subcommands needed
```

## Code Style
Go: Follow standard Go conventions with controller-runtime patterns

## Architecture Decisions
- **Single Binary**: Following Karpenter's proven pattern - one main.go, integrated webhook, unified operator
- **No CLI Binary**: Development via Makefile targets, no separate CLI needed
- **Industry Standard**: Matches architecture of mature Kubernetes controllers like Karpenter
- **Simplified Deployment**: One container, one process, easier RBAC and resource management

## Recent Changes
- 001-build-kubernetes-native: Adopted Karpenter single binary architecture pattern (Sept 10, 2025)
- 001-build-kubernetes-native: Removed CLI binary complexity, integrated webhook with operator (Sept 10, 2025)
- 001-build-kubernetes-native: Simplified to cmd/controller/main.go entry point (Sept 10, 2025)

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->