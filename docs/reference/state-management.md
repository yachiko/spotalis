# State Management Reference

Stability: Mixed (see markers)

Describes in-memory state structures used internally by Spotalis controllers for reconciliation, leadership, namespace scoping, and API rate limiting. These are not user-facing APIs, but understanding them aids debugging & performance tuning.

Related docs: [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md), [Metrics](./metrics.md), [Design Choices](../explanation/design-choices.md)

## Overview
| Structure | Purpose | Key Fields | Stability |
|-----------|---------|-----------|-----------|
| ReplicaState | Tracks current vs desired replica distribution for a workload | current/desired counts, drift, actions | Stable |
| LeadershipState | Tracks leader election timings & transitions | IsLeader, RenewedAt, Transitions | Stable |
| NamespaceFilter | Maintains set of monitored namespaces via selector | MonitoredNamespaces, Selector, LastUpdated | Stable |
| AdmissionState | Tracks pending spot/on-demand admissions per workload to prevent burst races | PendingSpot/PendingOnDemand, LastUpdated, Generation | Stable |
| APIRateLimiter | Regulates API server request velocity with backoff | RequestsPerSecond, BackoffUntil, RateLimitHits | Provisional |
## AdmissionState (Stable)
Lightweight, thread‑safe in‑memory tracker used by the webhook to avoid race conditions during burst admissions.

Key fields:
- PendingSpot / PendingOnDemand: per‑workload counters for decisions made but not yet reflected in live pods
- LastUpdated: timestamp used by TTL cleanup to evict stale entries
- Generation: workload generation; when it changes, pending counters are reset to avoid staleness

Behavior:
- Admission decisions use effective counts = observed pods + pending counters.
- On decision, increment the relevant counter immediately.
- Background cleanup loop removes entries older than the TTL.
- On process restart, state is empty; controllers converge distribution over time.

## Eviction vs Deletion (Stable)
Controllers rebalance using the Kubernetes Eviction API (pods/eviction) rather than direct deletion. This respects PodDisruptionBudgets and graceful termination.

Outcomes:
- Evicted: eviction accepted; pod terminates per grace period.
- AlreadyGone: pod not found; continue without error.
- PDBBlocked: HTTP 429; controller logs and retries later without exponential backoff.


## ReplicaState (Stable)
Represents distribution state for a single workload.

Key fields:
- TotalReplicas: desired spec replicas
- CurrentOnDemand / CurrentSpot: live counts observed
- DesiredOnDemand / DesiredSpot: target allocation after formula
- LastReconciled: timestamp of last successful reconcile
- Pending*, FailedSpot: transitional health indicators (future enrichment)

Core methods:
- CalculateDesiredDistribution(config) – applies percentage & minOnDemand
- NeedsReconciliation() – drift detection
- GetNextAction() – high-level recommended operation (scale, migrate, none)
- GetSpotPercentage() / GetDesiredSpotPercentage()

Action precedence:
1. Fix total (scale up/down) before distribution migration
2. Prefer scaling down spot first (resilience bias)
3. Use migration actions only when totals correct but mix wrong

## LeadershipState (Stable)
Encapsulates lease-based leadership lifecycle.

Fields:
- IsLeader, LeaderIdentity
- LeaseName / LeaseNamespace
- AcquiredAt / RenewedAt / LastTransition
- LeaseDuration / RenewDeadline / RetryPeriod
- Transitions (count of role changes)

Health evaluation:
- healthy: renewal < retryPeriod
- warning: retryPeriod <= sinceRenew < renewDeadline
- unhealthy: sinceRenew >= renewDeadline

Methods: BecomeLeader(), StepDown(), RenewLease(), NeedsRenewal(), IsLeaseExpired(), GetMetrics()

## NamespaceFilter (Stable)
Determines which namespaces are considered for workload discovery.

Characteristics:
- Optional LabelSelector; nil selector => AllowAll=true
- UpdateNamespaces() recalculates and sorts list for deterministic order
- ShouldMonitorNamespace(ns) matches runtime labels
- IsStale(maxAge) triggers refresh scheduling

Use Cases:
- Multi-tenant scoping
- Performance control (limit watch set)

## APIRateLimiter (Provisional)
Adaptive rate control to avoid API server overload.

Mechanism:
- Tracks request timestamps (sliding 1m window) to compute RequestsPerSecond
- On failure path, if rate > MaxRequestsPerSecond → trigger exponential backoff
- BackoffUntil gate consulted before new request batches

Fields:
- MaxRequestsPerSecond (default 50.0)
- BackoffMultiplier (2.0), BaseBackoffDuration (1s), capped at 5m
- RateLimitHits counts escalations
- Success/Failure counters for success rate insight

Status API via GetRateLimitStatus(): exposes current rate, hits, backoff schedule.

## Thread Safety
| Structure | Concurrency Strategy |
|-----------|----------------------|
| ReplicaState | External synchronization (not internally locked) |
| LeadershipState | External synchronization (atomic usage pattern) |
| NamespaceFilter | RWMutex internal guard |
| APIRateLimiter | Mutex internal guard |

## Drift & Staleness Detection
- ReplicaState.IsStale(maxAge) via reconciliation age threshold (zero timestamp → stale)
- NamespaceFilter.IsStale(maxAge) compares LastUpdated
- LeadershipState.ShouldStepDown() if renewal missed beyond RenewDeadline

## Metric Correlation
| Internal Field | Metric | Relationship |
|----------------|--------|--------------|
| DesiredSpot/DesiredOnDemand | spotalis_workload_replicas | Desired counts influence reconciliation actions updating metric |
| Transitions (LeadershipState) | spotalis_leader_election_status | Status gauge set to 1 for leader, 0 follower; transitions not directly counted yet |
| RequestsPerSecond (APIRateLimiter) | (none yet) | Potential future metric export |

## Extensibility Notes
- APIRateLimiter may emit metrics & configurable thresholds in future (treat as Provisional).
- Additional ReplicaAction variants (e.g., preempt-shift) may be introduced; rely on semantic names.

## See Also
- [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md)
- [Metrics](./metrics.md)
- [Design Choices](../explanation/design-choices.md)
- [Configuration](./configuration.md)
