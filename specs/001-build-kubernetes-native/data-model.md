# Data Model: Kubernetes Workload Replica Manager

## Overview
This document defines the key data structures and entities used by the Kubernetes Workload Replica Manager.

## Core Entities

### WorkloadConfiguration
Represents the parsed configuration from Kubernetes annotations on Deployments and StatefulSets.

**Fields**:
- `Enabled` (bool): Whether the workload is managed by spotalis
- `MinOnDemand` (int32): Minimum number of replicas on on-demand nodes
- `SpotPercentage` (int32): Target percentage of replicas on spot nodes (0-100)

**Validation Rules**:
- `MinOnDemand` must be >= 0 and <= total replicas
- `SpotPercentage` must be 0-100 and include '%' symbol
- At least one of `MinOnDemand` or `SpotPercentage` must be specified when enabled

**Source**: Parsed from workload annotations using annotation schema

### ReplicaState
Represents the current and desired state of replica distribution for a workload.

**Fields**:
- `WorkloadRef` (ObjectReference): Reference to the source Deployment/StatefulSet
- `TotalReplicas` (int32): Total desired replicas from workload spec
- `CurrentOnDemand` (int32): Current number of replicas on on-demand nodes
- `CurrentSpot` (int32): Current number of replicas on spot nodes
- `DesiredOnDemand` (int32): Target number of replicas on on-demand nodes
- `DesiredSpot` (int32): Target number of replicas on spot nodes
- `LastReconciled` (time.Time): Timestamp of last successful reconciliation

**Derived Calculations**:
```go
// Calculate desired distribution based on configuration
func (r *ReplicaState) CalculateDesiredDistribution(config WorkloadConfiguration) {
    // Apply minimum on-demand first
    r.DesiredOnDemand = max(config.MinOnDemand, 0)
    
    // Calculate spot replicas from percentage
    if config.SpotPercentage > 0 {
        spotReplicas := (r.TotalReplicas * config.SpotPercentage) / 100
        r.DesiredSpot = min(spotReplicas, r.TotalReplicas - r.DesiredOnDemand)
    }
    
    // Remaining replicas go to on-demand (safe default)
    r.DesiredOnDemand = r.TotalReplicas - r.DesiredSpot
}
```

### NodeClassification
Represents the categorization of cluster nodes based on their type.

**Fields**:
- `NodeName` (string): Kubernetes node name
- `NodeType` (NodeType): Spot or OnDemand classification
- `Labels` (map[string]string): Node labels used for classification
- `Schedulable` (bool): Whether the node accepts new pods
- `LastUpdated` (time.Time): When classification was last verified

**Node Type Enum**:
```go
type NodeType string

const (
    NodeTypeSpot     NodeType = "spot"
    NodeTypeOnDemand NodeType = "on-demand"
    NodeTypeUnknown  NodeType = "unknown"
)
```

**Classification Logic**:
- Nodes are classified based on configurable label selectors
- Default: Karpenter labels (`karpenter.sh/capacity-type`)
- Unknown nodes are treated as on-demand for safety

### LeadershipState
Represents the current leader election state for controller coordination.

**Fields**:
- `IsLeader` (bool): Whether this instance is the current leader
- `LeaderIdentity` (string): Identity of the current leader
- `LeaseName` (string): Name of the coordination lease
- `LeaseNamespace` (string): Namespace of the coordination lease
- `AcquiredAt` (time.Time): When leadership was acquired
- `RenewedAt` (time.Time): Last successful lease renewal

### DisruptionPolicy
Represents pod disruption budget constraints for safe rescheduling.

**Fields**:
- `WorkloadRef` (ObjectReference): Reference to the source workload
- `PDBRef` (ObjectReference): Reference to associated PodDisruptionBudget
- `MinAvailable` (int32): Minimum pods that must remain available
- `MaxUnavailable` (int32): Maximum pods that can be unavailable
- `CurrentlyDisrupted` (int32): Number of pods currently disrupted
- `CanDisrupt` (bool): Whether additional disruption is allowed

**Safety Calculations**:
```go
func (d *DisruptionPolicy) CanDisruptPod() bool {
    if d.PDBRef == nil {
        return true // No PDB constraints
    }
    
    availablePods := d.TotalReplicas - d.CurrentlyDisrupted
    
    if d.MinAvailable > 0 {
        return availablePods > d.MinAvailable
    }
    
    if d.MaxUnavailable > 0 {
        return d.CurrentlyDisrupted < d.MaxUnavailable
    }
    
    return true
}
```

### ControllerConfiguration
Represents the global controller configuration including rate limiting and multi-tenancy settings.

**Fields**:
- `ResyncPeriod` (time.Duration): Controller reconciliation interval (default 30s, min 5s)
- `Workers` (int): Number of concurrent worker goroutines
- `NamespaceSelector` (*metav1.LabelSelector): Optional namespace filtering for multi-tenant clusters
- `LeaderElection` (LeaderElectionConfig): Leader election settings
- `NodeLabels` (NodeLabelConfig): Node classification label configuration

**Validation Rules**:
- `ResyncPeriod` must be >= 5 seconds to prevent API server overload
- `Workers` must be > 0 and typically <= 10 for reasonable resource usage
- `NamespaceSelector` supports both matchLabels and matchExpressions

**Usage**:
```go
// Multi-tenant configuration example
func (c *ControllerConfiguration) ShouldMonitorNamespace(ns *corev1.Namespace) bool {
    if c.NamespaceSelector == nil {
        return true // Monitor all namespaces
    }
    
    selector, err := metav1.LabelSelectorAsSelector(c.NamespaceSelector)
    if err != nil {
        return false
    }
    
    return selector.Matches(labels.Set(ns.Labels))
}
```

### NamespaceFilter
Represents namespace-level filtering state for multi-tenant support.

**Fields**:
- `MonitoredNamespaces` ([]string): List of namespaces currently being monitored
- `LastUpdated` (time.Time): When the filter was last recalculated
- `TotalWorkloads` (int): Number of workloads across all monitored namespaces

**Operations**:
- Recalculated when namespace labels change
- Used to scope controller watches and reduce API load
- Supports label selector updates without restart

### APIRateLimiter
Represents API server load management state.

**Fields**:
- `RequestsPerSecond` (float64): Current request rate to API server
- `LastMeasurement` (time.Time): When rate was last calculated
- `BackoffUntil` (time.Time): When to retry after rate limiting
- `SuccessfulRequests` (int64): Count of successful API requests
- `FailedRequests` (int64): Count of failed API requests

**Rate Limiting Logic**:
```go
func (r *APIRateLimiter) ShouldBackoff() bool {
    return time.Now().Before(r.BackoffUntil)
}

func (r *APIRateLimiter) RecordSuccess() {
    r.SuccessfulRequests++
    r.LastMeasurement = time.Now()
}
```

## State Transitions

### Workload Lifecycle
1. **Discovery**: Workload with `spotalis.io/enabled: true` detected
2. **Configuration**: Annotations parsed into WorkloadConfiguration
3. **Assessment**: Current replica distribution analyzed
4. **Planning**: Desired state calculated based on configuration
5. **Execution**: Pod rescheduling performed (respecting PDBs)
6. **Monitoring**: Continuous reconciliation and state updates

### Pod Rescheduling States
1. **Identified**: Pod marked for rescheduling due to wrong node type
2. **PDB Check**: Verify disruption budget allows deletion
3. **Deletion**: Pod deleted to trigger rescheduling
4. **Scheduling**: New pod scheduled on correct node type
5. **Verification**: Confirm pod is running on target node type

### Leader Election States
1. **Candidate**: Instance attempting to acquire leadership
2. **Leader**: Active instance performing reconciliation
3. **Follower**: Standby instance waiting for leadership
4. **Renewal**: Leader refreshing lease to maintain leadership
5. **Transition**: Leadership changing between instances

## Data Persistence

### Kubernetes as State Store
- **Workload State**: Stored in Kubernetes resource annotations and status
- **Leadership State**: Stored in Kubernetes Lease objects
- **Node Classification**: Cached in memory, refreshed from Node API
- **Metrics State**: Exposed via Prometheus metrics, not persisted

### Caching Strategy
- **Controller Cache**: Uses controller-runtime cache for efficient API access
- **Node Cache**: In-memory cache with periodic refresh (5 minutes)
- **Configuration Cache**: Immediate update on annotation changes
- **PDB Cache**: Cached with watch-based invalidation

## Error Handling

### Configuration Errors
- Invalid annotation values logged and workload skipped
- Conflicting configurations resolved using precedence rules
- Missing required annotations treated as disabled workloads

### Operational Errors
- API failures trigger exponential backoff retry
- Leadership loss causes graceful shutdown of reconciliation
- Webhook failures logged with detailed error context
- Network errors handled with circuit breaker pattern

## Performance Considerations

### Memory Optimization
- Efficient caching with LRU eviction for large clusters
- Minimal object retention to prevent memory leaks
- Struct packing for memory-intensive data structures

### CPU Optimization
- Batch processing of multiple workloads
- Efficient label selector matching
- Minimal string allocations in hot paths

### Network Optimization
- Connection pooling for Kubernetes API calls
- Efficient watch-based updates vs polling
- Compressed JSON payloads for large responses
