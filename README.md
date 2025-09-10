# Spotalis - Kubernetes Workload Replica Manager

## Overview
Spotalis is a Kubernetes controller that manages workload replicas based on cost optimization and spot instance availability. Following the single binary architecture pattern pioneered by Karpenter.

## Quick Start

### Prerequisites
- Go 1.21+
- Docker with buildx
- Kind for local testing
- kubectl

### Development

```bash
# Setup
make deps
make generate

# Run locally (against current kubeconfig)
make run

# Testing
make test              # Unit tests
make test-integration  # Integration tests with Kind

# Build
make docker-build     # Build container image
```

### Architecture

Single binary deployment with integrated components:
- Controller: Workload replica management 
- Webhook: Admission validation
- HTTP Server: Health checks and metrics

### Configuration

```yaml
# Namespace filtering for multi-tenant clusters
namespaceSelector:
  matchLabels:
    spotalis.io/enabled: "true"

# Controller intervals (default 30s, minimum 5s)
reconcileInterval: 30s
```

## Development

See `specs/001-build-kubernetes-native/` for detailed implementation plans and specifications.
