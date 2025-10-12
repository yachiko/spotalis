# Glossary

Stability: Stable (content expands incrementally)

Canonical definitions for recurring Spotalis terms. Use these spellings & meanings across documentation.

| Term | Definition | Notes |
|------|------------|-------|
| Enablement Label | `spotalis.io/enabled=true` label applied to namespace & workload to opt-in management | Dual gate required |
| Spot Percentage | Annotation `spotalis.io/spot-percentage` specifying target % of replicas on spot | 0–100, default 70 |
| Min On-Demand | Annotation `spotalis.io/min-on-demand` specifying minimum on-demand replicas | Safety floor |
| ReplicaState | Internal in-memory struct tracking current vs desired replica distribution | Not persisted |
| LeadershipState | In-memory struct tracking leader election timing & status | Drives health signals |
| NamespaceFilter | In-memory selector-based namespace inclusion list | Optimizes scope |
| APIRateLimiter | In-memory adaptive rate control state for API requests | Provisional feature |
| Reconciliation | Controller loop determining and applying drift corrections | Idempotent design |
| Migration (Replica) | Redistribution action shifting replicas between spot and on-demand | Occurs after totals correct |
| Cooldown Period | `controllers.workloadTiming.cooldownPeriod`; minimum wait between disruptive actions | Prevents thrash |
| Disruption Window | Scheduled window during which disruptive actions are allowed | Optional (cron + duration) |
| Drift | Difference between current and desired replica counts per capacity type | Triggers actions |
| Leader Election | Process choosing active controller instance (Lease object) | Ensures single actor |
| Mutation (Admission) | Webhook modification of Pod spec (node selectors etc.) | Requires TLS certs |
| Workload | Deployment or StatefulSet managed by Spotalis | Future: other controllers |
| Distribution Formula | `targetSpot = min( ceil(T * P / 100), T - M )` computing desired spot replicas | See strategy page |
| Read-Only Mode | Config flag preventing mutating actions while still computing state | Safe dry-run |

## See Also
- [Labels & Annotations](./labels-and-annotations.md)
- [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md)
- [State Management](./state-management.md)
- [Configuration](./configuration.md)
