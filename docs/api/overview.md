# Spotalis API Documentation

Welcome to the Spotalis API documentation. This guide provides comprehensive information about the Spotalis Kubernetes controller's APIs, interfaces, and configuration options.

## Table of Contents

- [Overview](#overview)
- [Core Packages](#core-packages)
- [Annotations Reference](#annotations-reference)
- [Configuration API](#configuration-api)
- [Examples](#examples)
- [Generated Documentation](#generated-documentation)

## Overview

Spotalis is a Kubernetes controller that optimizes workload replica distribution across spot and on-demand instances. The API is designed around:

- **Annotation-driven configuration**: No CRDs required
- **Dependency injection**: Clean service interfaces
- **Type-safe configuration**: Strongly typed APIs
- **Webhook integration**: Automatic pod mutation

## Core Packages

### Controllers (`pkg/controllers`)

The controllers package implements the reconciliation logic for Kubernetes workloads.

**Key Types:**
- `DeploymentReconciler`: Manages Deployment workload optimization
- `StatefulSetReconciler`: Manages StatefulSet workload optimization  
- `ControllerManager`: Coordinates multiple controllers

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/controllers](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/controllers)

**Example:**
```go
import "github.com/ahoma/spotalis/pkg/controllers"

// Create deployment reconciler
reconciler := &controllers.DeploymentReconciler{
    Client: mgr.GetClient(),
    Scheme: mgr.GetScheme(),
    Log:    ctrl.Log.WithName("controllers").WithName("Deployment"),
}

// Setup with manager
if err := reconciler.SetupWithManager(mgr); err != nil {
    return err
}
```

### Webhook (`pkg/webhook`)

The webhook package implements the mutating admission webhook for pod nodeSelector injection.

**Key Types:**
- `PodMutator`: Handles pod mutation requests
- `AdmissionHandler`: Processes admission review requests

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/webhook](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/webhook)

**Example:**
```go
import "github.com/ahoma/spotalis/pkg/webhook"

// Create pod mutator
mutator := webhook.NewPodMutator(
    client,
    nodeClassifier,
    annotationParser,
)

// Handle admission request
response := mutator.Handle(ctx, admissionRequest)
```

### Dependency Injection (`pkg/di`)

The DI package provides dependency injection and application lifecycle management.

**Key Types:**
- `Application`: Main application with DI container
- `Container`: Service container interface
- `ServiceRegistry`: Service registration

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/di](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/di)

**Example:**
```go
import "github.com/ahoma/spotalis/pkg/di"

// Create application with DI
app, err := di.NewApplication(ctx)
if err != nil {
    log.Fatal(err)
}

// Start operator
if err := app.Start(ctx); err != nil {
    log.Fatal(err)
}
```

### Configuration (`pkg/config`)

The config package manages consolidated YAML configuration with environment variable overrides.

**Key Types:**
- `SpotalisConfig`: Root configuration structure
- `Loader`: Configuration loading with precedence

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/config](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/config)

**Example:**
```go
import "github.com/ahoma/spotalis/pkg/config"

// Load configuration
loader := config.NewLoader(
    config.WithConfigFile("/etc/spotalis/config.yaml"),
    config.WithEnvPrefix("SPOTALIS"),
)

cfg, err := loader.Load()
if err != nil {
    return err
}
```

### APIs (`pkg/apis`)

The apis package defines core data structures and state objects.

**Key Types:**
- `WorkloadConfiguration`: Workload-level configuration
- `DisruptionPolicy`: Pod disruption policy with windows
- `LeadershipState`: Leader election state tracking
- `NodeClassification`: Node type classification result

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/apis](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/apis)

### Interfaces (`pkg/interfaces`)

The interfaces package defines service contracts for dependency injection.

**Key Interfaces:**
- `AnnotationParser`: Parse Spotalis annotations
- `NodeClassifier`: Classify node types (spot/on-demand)
- `ConfigurationService`: Manage configuration
- `MetricsService`: Expose Prometheus metrics

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/interfaces](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/interfaces)

### Operator (`pkg/operator`)

The operator package provides the main operator lifecycle and coordination.

**Key Types:**
- `Operator`: Main operator orchestration
- `LeaderElectionManager`: Leader election coordination
- `ShutdownManager`: Graceful shutdown handling

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/operator](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/operator)

### Metrics (`pkg/metrics`)

The metrics package exposes Prometheus metrics for monitoring.

**Key Types:**
- `Collector`: Prometheus metrics collector
- `MetricsRecorder`: Record operational metrics

**Documentation:** [pkg.go.dev/github.com/ahoma/spotalis/pkg/metrics](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/metrics)

## Annotations Reference

See [annotations.md](./annotations.md) for detailed annotation documentation.

### Primary Annotations

| Annotation | Type | Description | Example |
|------------|------|-------------|---------|
| `spotalis.io/enabled` | boolean | Enable Spotalis optimization | `"true"` |
| `spotalis.io/spot-percentage` | integer (0-100) | Target spot percentage | `"70"` |
| `spotalis.io/min-on-demand` | integer | Minimum on-demand replicas | `"2"` |
| `spotalis.io/disruption-schedule` | cron | Disruption window schedule | `"0 2 * * *"` |
| `spotalis.io/disruption-duration` | duration | Disruption window duration | `"4h"` |

### Namespace Labels

| Label | Type | Description | Example |
|-------|------|-------------|---------|
| `spotalis.io/enabled` | boolean | Enable for namespace | `"true"` |
| `spotalis.io/test` | boolean | Mark as test namespace | `"true"` |

## Configuration API

### Global Configuration Structure

```yaml
# Operator configuration
operator:
  namespace: "spotalis-system"
  leaderElection:
    enabled: true
    leaseDuration: 15s
    renewDeadline: 10s
    retryPeriod: 2s
  disruptionWindow:
    schedule: "0 2 * * *"  # Cron expression (UTC)
    duration: "4h"          # Go duration format

# Controller configuration
controllers:
  deployment:
    enabled: true
    maxConcurrentReconciles: 3
    reconcileInterval: 30s
  statefulset:
    enabled: true
    maxConcurrentReconciles: 2
    reconcileInterval: 30s
    cooldownPeriod: 3m

# Webhook configuration
webhook:
  enabled: true
  port: 9443
  certDir: "/tmp/k8s-webhook-server/serving-certs"

# Observability configuration
observability:
  metrics:
    enabled: true
    port: 8080
    path: "/metrics"
  health:
    enabled: true
    port: 8081
    livenessPath: "/healthz"
    readinessPath: "/readyz"
  logging:
    level: "info"
    format: "json"
    development: false
```

### Environment Variable Overrides

Configuration can be overridden via environment variables with `SPOTALIS_` prefix:

```bash
# Override operator namespace
export SPOTALIS_OPERATOR_NAMESPACE="custom-namespace"

# Override metrics port
export SPOTALIS_OBSERVABILITY_METRICS_PORT="9090"

# Override log level
export SPOTALIS_OBSERVABILITY_LOGGING_LEVEL="debug"
```

## Examples

### Basic Deployment with Spot Optimization

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "70"
    spotalis.io/min-on-demand: "1"
spec:
  replicas: 10
  template:
    spec:
      containers:
      - name: app
        image: nginx:alpine
```

**Result:** 7 pods on spot nodes, 3 pods on on-demand nodes (maintaining min 1 on-demand).

### StatefulSet with Disruption Window

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-stateful-app
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "60"
    spotalis.io/disruption-schedule: "0 2 * * *"
    spotalis.io/disruption-duration: "4h"
spec:
  replicas: 5
  template:
    spec:
      containers:
      - name: app
        image: postgres:14
```

**Result:** Pod rebalancing only occurs daily between 2 AM - 6 AM UTC. 3 pods on spot, 2 on on-demand.

### Namespace-Level Configuration

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: production
  labels:
    spotalis.io/enabled: "true"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prod-app
  namespace: production
  annotations:
    spotalis.io/spot-percentage: "50"
spec:
  replicas: 4
  # ...
```

**Result:** Namespace-level enablement inherited by all workloads. Deployment-level percentage overrides default.

### Programmatic API Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/ahoma/spotalis/pkg/apis"
    "github.com/ahoma/spotalis/internal/annotations"
)

func main() {
    ctx := context.Background()
    
    // Parse annotations
    parser := annotations.NewParser()
    config, err := parser.ParseConfiguration(map[string]string{
        "spotalis.io/enabled": "true",
        "spotalis.io/spot-percentage": "70",
        "spotalis.io/min-on-demand": "2",
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Calculate replica distribution
    policy := apis.NewDisruptionPolicy(
        &apis.WorkloadReference{
            Kind: "Deployment",
            Name: "my-app",
            Namespace: "default",
        },
        config,
        5, // total replicas
    )
    
    log.Printf("Spot replicas: %d", policy.SpotReplicas)
    log.Printf("On-demand replicas: %d", policy.OnDemandReplicas)
}
```

## Generated Documentation

### pkg.go.dev

Official Go documentation is automatically published to [pkg.go.dev/github.com/ahoma/spotalis](https://pkg.go.dev/github.com/ahoma/spotalis).

**Browse by package:**
- [pkg/controllers](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/controllers)
- [pkg/webhook](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/webhook)
- [pkg/di](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/di)
- [pkg/config](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/config)
- [pkg/apis](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/apis)
- [pkg/interfaces](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/interfaces)
- [pkg/operator](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/operator)
- [pkg/metrics](https://pkg.go.dev/github.com/ahoma/spotalis/pkg/metrics)

### Local Documentation

Generate local documentation:

```bash
# Install godoc tool
go install golang.org/x/tools/cmd/godoc@latest

# Start local documentation server
godoc -http=:6060

# Open in browser
open http://localhost:6060/pkg/github.com/ahoma/spotalis/
```

## Additional Resources

- **Architecture Documentation**: [../architecture/overview.md](../architecture/overview.md)
- **Development Guide**: [../development/setup.md](../development/setup.md)
- **Troubleshooting**: [../troubleshooting/common-issues.md](../troubleshooting/common-issues.md)
- **Deployment Guide**: [../deployment/production.md](../deployment/production.md)

## API Stability

Spotalis follows semantic versioning:

- **Stable APIs** (v1.x.x): Breaking changes only in major versions
- **Experimental APIs**: Marked with comments, may change in minor versions
- **Internal packages** (`internal/`): No API stability guarantees

## Contributing

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for guidelines on contributing to Spotalis APIs.

## License

All APIs are licensed under the Apache License 2.0. See [LICENSE](../../LICENSE) for details.
