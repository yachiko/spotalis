# Research: Kubernetes Workload Replica Manager

## Overview
Research findings for implementing a Kubernetes-native controller that manages replica distribution between spot and on-demand nodes based on annotations.

## Key Technology Decisions

### Controller Framework
**Decision**: Use controller-runtime framework
**Rationale**: 
- Standard framework for Kubernetes controllers
- Provides built-in leader election, caching, and reconciliation patterns
- Well-tested with extensive community adoption
- Integrates seamlessly with client-go

**Alternatives considered**:
- Raw client-go: Too low-level, would require implementing caching and reconciliation manually
- Custom framework: Unnecessary complexity and maintenance burden

### HTTP Server Framework  
**Decision**: Gin framework for webhook server
**Rationale**:
- Lightweight and performant for webhook endpoints
- Excellent middleware ecosystem for logging, metrics, recovery
- Simple routing for health checks and webhook paths
- Low latency required for admission webhooks

**Alternatives considered**:
- net/http standard library: More verbose, would need additional middleware
- Fiber: Not as established in Go ecosystem
- Echo: Similar to Gin but Gin has better k8s community adoption

### Testing Framework
**Decision**: Ginkgo v2 + Gomega for BDD testing
**Rationale**:
- Behavior-driven development aligns with user story testing
- Excellent support for asynchronous operations (controller reconciliation)
- Rich assertion library with Gomega
- Strong integration test capabilities

**Alternatives considered**:
- Standard testing: Less expressive for complex controller scenarios
- Testify: Good but not as powerful for async controller testing

### Logging Framework
**Decision**: Go standard library slog with structured JSON output
**Rationale**:
- Native structured logging support in Go 1.21+
- Performance optimized with zero-allocation logging
- JSON output for easy parsing by log aggregators
- Context-aware logging for correlation

**Alternatives considered**:
- Logrus: Third-party dependency, performance overhead
- Zap: More complex API, overkill for this use case
- Klog: Kubernetes-specific but not structured by default

### Container Build System
**Decision**: Docker Buildx with docker-bake.hcl configuration
**Rationale**:
- Multi-platform build support (amd64, arm64)
- Efficient layer caching and build optimization
- Declarative build configuration
- Integration with container registries

**Alternatives considered**:
- Standard docker build: No multi-platform support
- Podman: Less ecosystem adoption in CI/CD
- Ko: Go-specific but less flexible for complex builds

### Integration Testing
**Decision**: Kind (Kubernetes in Docker) clusters
**Rationale**:
- Lightweight local Kubernetes cluster for testing
- Fast startup and teardown for CI/CD
- Supports admission webhooks and RBAC testing
- Real Kubernetes API behavior

**Alternatives considered**:
- Minikube: Heavier resource usage, slower startup
- k3s: Good but Kind more common in controller testing
- Mock clients: Not realistic for integration testing

## Architecture Patterns

### Binary Architecture
**Decision**: Single binary with integrated operator pattern (following Karpenter architecture)
**Rationale**:
- Industry standard approach used by Karpenter and other mature Kubernetes controllers
- Simplified deployment with one container, one process
- Resource efficiency - no separate webhook service consuming resources  
- Reduced complexity - no inter-service communication, unified RBAC
- Single manager handles controller registration, webhook endpoints, metrics, and health checks
- Development simplified with `make run` command and single main.go entry point

**Alternatives considered**:
- Separate controller and webhook binaries: Added complexity, resource overhead, communication concerns
- CLI binary alongside controller: Unnecessary for operator pattern, development handled by Makefile targets

**Implementation Pattern**:
```go
// cmd/controller/main.go - Single binary entry point
func main() {
    ctx, op := operator.NewOperator()
    // Register all controllers AND webhooks with same manager
    op.WithControllers(ctx, controllers.NewControllers(...)).Start(ctx)
}
```

### Controller Pattern
**Decision**: Implement standard Kubernetes controller pattern with reconciliation loops
**Rationale**:
- Event-driven reconciliation based on resource changes
- Built-in retry and error handling
- Declarative state management
- Standard pattern for Kubernetes operators

### Webhook Pattern
**Decision**: Implement mutating admission webhook for pod modification
**Rationale**:
- Intercepts pod creation to modify nodeSelector
- Synchronous operation ensuring pod placement
- Integrates with Kubernetes admission chain
- Required for dynamic pod modification

### Leader Election
**Decision**: Use controller-runtime's leader election implementation
**Rationale**:
- Prevents split-brain scenarios with multiple replicas
- Built-in lease management and health checking
- Standard pattern for HA controllers
- Automatic failover capabilities

## Configuration Management

### Configuration Format
**Decision**: YAML configuration file with environment variable overrides
**Rationale**:
- Standard format for Kubernetes applications
- Easy to version control and template
- Support for complex nested configurations
- Environment variable overrides for container deployment

### Configuration Structure
```yaml
controller:
  resyncPeriod: 30s          # Configurable loop interval (min 5s, default 30s)
  workers: 5
  leaderElection:
    enabled: true
    namespace: spotalis-system
  namespaceSelector:         # Optional namespace filtering for multi-tenant clusters
    matchLabels:
      spotalis.io/enabled: "true"
    # matchExpressions:       # Alternative selector syntax
    #   - key: environment
    #     operator: In
    #     values: ["production", "staging"]
    
webhook:
  port: 9443
  certDir: /tmp/k8s-webhook-server/serving-certs
  
nodeLabels:
  spot: karpenter.sh/capacity-type=spot
  onDemand: karpenter.sh/capacity-type=on-demand
  
logging:
  level: info
  format: json
  
metrics:
  enabled: true
  port: 8080
```

## Performance Considerations

### Memory Usage
**Target**: <100MB memory usage
**Strategy**:
- Use controller-runtime caching to avoid repeated API calls
- Implement proper garbage collection for cached objects
- Limit watch scope to annotated resources only
- Use memory-efficient data structures

### Reconciliation Performance  
**Target**: <5s reconciliation time
**Strategy**:
- Parallel processing of independent workloads
- Efficient node classification caching
- Batch operations where possible
- Early exit for workloads without changes

### Webhook Latency
**Target**: <200ms webhook response time
**Strategy**:
- Pre-computed node selector logic
- Minimal webhook processing
- Efficient JSON marshaling/unmarshaling
- Connection pooling for API calls

## Security Considerations

### RBAC Permissions
Required cluster roles:
- Read: Deployments, StatefulSets, Pods, Nodes, PodDisruptionBudgets
- Write: Pods (for deletion), Events (for logging)
- Create: Leases (for leader election)

### Webhook Security
- TLS certificate management for webhook endpoint
- Admission controller configuration with appropriate failure policy
- Namespace isolation and service account security

## Integration Points

### Karpenter Integration
- Default node labels for spot/on-demand detection
- Integration with Karpenter provisioning decisions
- Handling of node lifecycle events

### Prometheus Integration
- Controller metrics (reconciliation time, error rates)
- Webhook metrics (request duration, success rates)
- Workload distribution metrics

### Alerting Integration
- Critical error notifications
- Leader election state changes
- Webhook availability monitoring

## Risk Mitigation

### Split-Brain Prevention
- Leader election with proper lease configuration
- Health checks and readiness probes
- Graceful shutdown handling

### API Server Load
- **Configurable reconciliation intervals**: Default 30s, minimum 5s to prevent DDoS
- **Rate limiting for API operations**: Built-in controller-runtime rate limiting
- **Efficient caching strategies**: Watch-based caching with filtered scopes
- **Batch processing where appropriate**: Group similar operations
- **Namespace filtering**: Optional namespaceSelector to limit monitoring scope in multi-tenant clusters

### Multi-Tenant Cluster Support
- **Namespace isolation**: Optional namespaceSelector configuration
- **Label-based filtering**: Support for matchLabels and matchExpressions
- **Resource scoping**: Limit controller watch scope to reduce API load
- **Tenant boundaries**: Respect namespace-level RBAC and policies

### Webhook Reliability
- Proper failure policies in admission configuration
- Health check endpoints for webhook availability
- Certificate rotation automation
