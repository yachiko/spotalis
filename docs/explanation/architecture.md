# Explanation: Architecture Overview (Stability: Stable)

High-level flow (happy path):
1. Configuration loaded (YAML + env overrides) into unified struct.
2. Dependency Injection container (dig) wires services (logging, operator, controllers, webhook server, metrics/health servers).
3. Operator starts, participates in leader election (Lease `spotalis-controller-leader`).
4. Active leader registers controllers with controller-runtime manager.
5. Controllers reconcile annotated workloads, computing desired spot vs on-demand distribution.
6. Mutating webhook injects node selectors (or taint tolerations) to steer scheduling.
7. Metrics and state are updated; health endpoints reflect readiness/liveness.

## Components
| Component              | Responsibility                                                                     | Notes                             |
| ---------------------- | ---------------------------------------------------------------------------------- | --------------------------------- |
| Operator               | Process lifecycle, leader election, startup sequencing                             | Single binary pattern             |
| Controllers            | Reconcile Deployments/StatefulSets, compute spot/on-demand targets                 | Idempotent logic                  |
| Webhook                | Admission mutation for Pod templates                                               | TLS certs mounted at runtime path |
| State Management       | In-memory structs (ReplicaState, LeadershipState, NamespaceFilter, APIRateLimiter) | Avoids CRDs initially             |
| Metrics/Health Servers | Expose Prometheus metrics & readiness/liveness probes                              | Ports 8080/8081                   |

## Detailed Data Flow
Config → DI Wiring: Loader reads file + env, populates `SpotalisConfig`. DI container provides typed config to constructors.

Startup: Operator builds manager; if leader election enabled, only leader continues to start controllers & webhook.

Reconciliation: For each workload with `spotalis.io/enabled: true`, controller:
1. Reads desired total replicas.
2. Fetches annotations (spot percentage, min-on-demand).
3. Calculates target spot count using formula.
4. Compares with current distribution (via ReplicaState / live pods).
5. Emits metrics and logs describing drift and planned actions.

Mutation: Incoming pod creations for enabled workloads trigger admission logic; nodeSelector injection ensures pods schedule onto desired capacity type pools.

## Concurrency & Leader Election
- Only the leader executes reconciliation loops to prevent duplicate scaling decisions.
- Leadership handover triggers a short pause while new leader warms caches.
- Leadership state object tracks last heartbeat, enabling health diagnostics.

## Failure Modes & Resilience
| Failure                         | Impact                 | Mitigation / Signal                          |
| ------------------------------- | ---------------------- | -------------------------------------------- |
| Webhook certificate missing     | Pod mutations skipped  | Admission errors; readiness probe may fail   |
| Loss of leadership              | Reconciliation halts   | Lease renewal logs + leadership state metric |
| API rate limiting / backoff     | Delayed reconciliation | APIRateLimiter metrics surface throttling    |
| Config parse failure at startup | Process exit           | Clear fatal log; container restarts          |

## Design Principles
- Annotation-first: reduce operational burden vs early CRD design.
- Deterministic idempotent reconciles: safe repeated execution.
- Minimal state persistence: rely on live cluster + in-memory snapshots.

## Observability
- Metrics: distribution calculations, reconciliation counts (see `reference/metrics.md`).
- Health: `/readyz` gated by critical dependency initialization; `/healthz` always lightweight.

## Related
- State management: `../reference/state-management.md`
- Replica distribution strategy: `replica-distribution-strategy.md`
- Design choices: `design-choices.md`
- Glossary: `../reference/glossary.md`
