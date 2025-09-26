# Spotalis - Kubernetes Workload Replica Manag## Configuration

Spotalis supports two configuration approaches:

1. **YAML Configuration Files** (Recommended): Comprehensive, environment-specific configuration
2. **Legacy Command-line Flags**: Backward compatible with existing deployments

### Configuration Files

Create YAML configuration files for different environments:

```bash
# Use predefined configuration templates
./controller -config=examples/configs/development.yaml   # Local development
./controller -config=examples/configs/production.yaml   # Production deployment
./controller -config=examples/configs/testing.yaml      # Integration testing
./controller -config=examples/configs/minimal.yaml      # Basic setup
```

Environment variables automatically override file settings:
```bash
export SPOTALIS_OPERATOR_NAMESPACE=my-namespace
export SPOTALIS_LOGGING_LEVEL=debug
./controller -config=examples/configs/production.yaml
```

See [Configuration Guide](CONFIGURATION.md) for complete migration instructions and examples.

### Node Classification
## Overview
Spotalis is a Kubernetes controller that manages workload replicas based on cost optimization and spot instance availability. Built with a modern dependency injection architecture and unified configuration system, following the single binary pattern pioneered by Karpenter.

## Features

- **Intelligent Replica Distribution**: Automatically distributes replicas across spot and on-demand instances based on cost optimization
- **Real-time Spot Termination Handling**: Gracefully handles spot instance terminations with fast failover
- **Multi-workload Support**: Manages both Deployments and StatefulSets with workload-specific policies
- **Annotation-based Configuration**: Simple annotation-driven configuration without CRDs
- **Multi-tenant Ready**: Namespace-based filtering for secure multi-tenant environments
- **Observability**: Built-in metrics, health checks, and comprehensive logging

## Installation

### Prerequisites
- Kubernetes 1.28+
- Appropriate RBAC permissions for workload management
- Node labels for spot/on-demand classification (see Configuration section)

### Quick Install

```bash
# Install using kubectl
kubectl apply -f https://github.com/your-org/spotalis/releases/latest/download/install.yaml

# Or using Helm
helm repo add spotalis https://your-org.github.io/spotalis
helm install spotalis spotalis/spotalis
```

### Development Install

```bash
# Clone repository
git clone https://github.com/your-org/spotalis.git
cd spotalis

# Build and deploy to current cluster
make deploy

# Or run locally for development
make run
```

## Configuration

### Node Classification

Spotalis requires nodes to be labeled for proper classification:

```bash
# Spot instances
kubectl label node <node-name> node.kubernetes.io/lifecycle=spot

# On-demand instances  
kubectl label node <node-name> node.kubernetes.io/lifecycle=normal

# Alternative: Use Karpenter-style labels
kubectl label node <node-name> karpenter.sh/capacity-type=spot
kubectl label node <node-name> karpenter.sh/capacity-type=on-demand
```

### Controller Configuration

Configure the controller via ConfigMap or command-line flags:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: spotalis-config
  namespace: spotalis-system
data:
  config.yaml: |
    # Namespace filtering for multi-tenant clusters
    namespaceSelector:
      matchLabels:
        spotalis.io/enabled: "true"
    
    # Controller behavior
    reconcileInterval: 30s
    leaderElection: true
    
    # Webhook configuration
    webhook:
      port: 9443
      certDir: /tmp/k8s-webhook-server/serving-certs
    
    # Health and metrics
    healthAddr: :8081
    metricsAddr: :8080
```

### Workload Configuration

Configure workloads using annotations:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    # Basic replica distribution
    spotalis.io/replica-distribution: |
      {
        "minSpotReplicas": 2,
        "maxSpotReplicas": 8,
        "spotPercentage": 70,
        "onDemandBuffer": 1
      }
    
    # Disruption policy
    spotalis.io/disruption-policy: |
      {
        "maxUnavailable": "25%",
        "gracefulShutdownTimeout": "30s",
        "enablePreemptiveScaling": true
      }
spec:
  replicas: 10
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: nginx:latest
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
```

## Usage

### Basic Usage

1. **Label your nodes** appropriately for spot/on-demand classification
2. **Enable Spotalis** for a namespace:
   ```bash
   kubectl label namespace my-app spotalis.io/enabled=true
   ```
3. **Annotate your workloads** with replica distribution policies
4. **Deploy workloads** normally - Spotalis will manage replica distribution automatically

### Advanced Usage

#### Custom Replica Distribution

```yaml
metadata:
  annotations:
    spotalis.io/replica-distribution: |
      {
        "minSpotReplicas": 3,
        "maxSpotReplicas": 12,
        "spotPercentage": 80,
        "onDemandBuffer": 2,
        "preferredZones": ["us-west-2a", "us-west-2b"]
      }
```

#### Disruption Policies

```yaml
metadata:
  annotations:
    spotalis.io/disruption-policy: |
      {
        "maxUnavailable": "20%",
        "gracefulShutdownTimeout": "60s",
        "enablePreemptiveScaling": true,
        "drainTimeoutSeconds": 300
      }
```

#### StatefulSet Considerations

```yaml
metadata:
  annotations:
    spotalis.io/replica-distribution: |
      {
        "minSpotReplicas": 1,
        "maxSpotReplicas": 3,
        "spotPercentage": 50,
        "onDemandBuffer": 1,
        "maintainOrderedScaling": true
      }
```

### Monitoring

Spotalis exposes Prometheus metrics on `:8080/metrics`:

```promql
# Replica distribution metrics
spotalis_replica_distribution_total
spotalis_spot_replicas_current
spotalis_ondemand_replicas_current

# Performance metrics
spotalis_reconcile_duration_seconds
spotalis_webhook_duration_seconds

# Health metrics
spotalis_controller_errors_total
spotalis_spot_terminations_handled_total
```

### Troubleshooting

#### Check Controller Status

```bash
# Check controller pod
kubectl get pods -n spotalis-system

# Check controller logs
kubectl logs -n spotalis-system deployment/spotalis-controller

# Check configuration
kubectl get configmap spotalis-config -n spotalis-system -o yaml
```

#### Validate Workload Configuration

```bash
# Check workload annotations
kubectl get deployment my-app -o jsonpath='{.metadata.annotations}'

# Check replica status
kubectl describe deployment my-app

# Check events
kubectl get events --field-selector involvedObject.name=my-app
```

#### Common Issues

1. **No spot instances being used**:
   - Verify node labels are correct
   - Check namespace has `spotalis.io/enabled=true` label
   - Ensure workload has proper annotations

2. **Frequent replica movements**:
   - Increase `reconcileInterval` in configuration
   - Adjust `gracefulShutdownTimeout` in disruption policy
   - Review spot instance termination frequency

3. **Webhook admission failures**:
   - Check webhook certificate validity
   - Verify webhook service and endpoints
   - Review webhook logs for specific errors

## Development

### Prerequisites
- Go 1.21+
- Docker with buildx
- Kind for local testing
- kubectl

### Setup

```bash
# Clone and setup
git clone https://github.com/your-org/spotalis.git
cd spotalis
make deps
make generate

# Run tests
make test              # Unit tests
make test-integration  # Integration tests with Kind
make test-e2e         # End-to-end tests

# Run locally (against current kubeconfig)
make run

# Build container image
make docker-build
```

### Architecture

Spotalis uses a modern dependency injection architecture for improved testability and maintainability:

- **DI Container**: [Uber Dig](https://pkg.go.dev/go.uber.org/dig) for runtime dependency injection
- **Unified Configuration**: YAML-based configuration with environment variable overrides  
- **Single Binary**: All components (controller, webhook, HTTP servers) in one executable
- **4-Phase Migration**: Systematic adoption of DI across the codebase

### Project Structure

```
├── cmd/controller/          # Main controller binary (DI-integrated)
├── pkg/
│   ├── apis/               # Core data models and APIs
│   ├── controllers/        # Controller implementations
│   ├── config/             # Configuration system (YAML + env vars)
│   ├── di/                 # Dependency injection container
│   ├── webhook/           # Admission webhook
│   ├── metrics/           # Prometheus metrics
│   └── operator/          # Operator lifecycle management
├── internal/
│   ├── config/            # Configuration management
│   └── annotations/       # Annotation parsing
├── examples/
│   └── configs/           # Configuration templates
├── tests/
│   ├── unit/             # Unit tests
│   ├── integration/      # Integration tests
│   └── e2e/             # End-to-end tests
└── build/               # Build and deployment files
```

## Contributing

See `specs/001-build-kubernetes-native/` for detailed implementation plans and specifications.

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Run the test suite: `make test`
5. Submit a pull request

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
