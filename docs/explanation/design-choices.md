# Design Choices & Trade-offs

Stability: Stable (narrative) / Some implementation details Provisional where noted.

Explains why Spotalis adopts its current architectural patterns and rejects alternatives.

Related docs: [Architecture](./architecture.md), [Replica Distribution Strategy](./replica-distribution-strategy.md), [Configuration Reference](../reference/configuration.md)

## Annotations & Labels vs CRDs
**Choice**: Use workload labels & annotations (no custom CRDs) for enablement and tuning.
- Pros: Zero additional cluster-scoped RBAC, trivial adoption, aligns with existing deployment workflows.
- Cons: No server-side schema/validation; discoverability depends on docs.
- Mitigations: Single authoritative reference table; validation logic in parsers; fail-safe defaults.
- Rejected: CRD-based policy object (added complexity, versioning overhead) until semantics stabilize.

## Label-Only Enablement
**Choice**: `spotalis.io/enabled=true` required on namespace AND workload.
- Rationale: The webhook supports namespace scoping exclusively via labels, so using the same enablement label on both the namespace and each workload preserves one consistent mechanism (no split between labels vs annotations), enforces explicit opt-in, and keeps accidental cluster‑wide mutation blast radius low.

## Single Binary Pattern
**Choice**: One controller binary combining operator core, webhook server, and metrics/health endpoints.
- Pros: Simplified deployment & version alignment; reduced operational surface.
- Cons: Larger process; all components share lifecycle.
- Mitigation: Feature toggles (webhook.enabled, metrics.enabled) allow partial functionality when needed.

## Dependency Injection (Uber Dig)
**Choice**: Construct services via DI container.
- Pros: Explicit wiring, testability (swap mocks), lifecycle orchestration.
- Cons: Indirection cost; compile-time visibility reduced.
- Mitigations: Thin wrapper; registry centralization; limited scope.

## Leader Election
**Choice**: Standard Lease object with short (15s) default lease duration.
- Pros: Fast failover; familiar controller-runtime pattern.
- Trade-off: More leadership churn risk if API server is slow; configurable via config/env.

## Replica Distribution Algorithm
**Choice**: Algebraic calculation: `targetSpot = min( ceil(total * spotPercentage/100), total - minOnDemand )`.
- Pros: Deterministic, O(1), easy reasoning.
- Alternatives Considered: Iterative convergence loop (higher complexity), historical utilization weighting (needs telemetry).
- See: [Replica Distribution Strategy](./replica-distribution-strategy.md).

## No External Scheduler Extension
**Choice**: Mutate pods (nodeSelector) instead of scheduler framework plugins.
- Pros: Reuses existing scheduling semantics; simpler RBAC; decoupled from scheduler internals.
- Cons: Less dynamic runtime decisioning; depends on node labels accuracy.
- Future: Could evolve to scheduler plugin if richer constraints needed.

## Metrics Scope
**Choice**: Focus on reconciliation, webhook, leader, and replica counts; omit high-cardinality per-pod metrics.
- Rationale: Keep initial Prometheus footprint low; ensure scalability.
- Future: Add optional histograms (reconciliation latency) once stable.

## Configuration Strategy
**Choice**: Consolidated YAML + env overlay with `SPOTALIS_` prefix.
- Pros: Simple mental model; easy container overrides; explicit precedence.
- Cons: No dynamic reload yet.
- Future: Signal-based reload or configmap watch (tracked out-of-scope for now).

## Node Classification Heuristics
**Choice**: Label selectors (default Karpenter capacity-type) rather than provider API queries.
- Pros: Cloud-agnostic; offloads discovery to existing node provisioner.
- Cons: Requires consistent node labeling; mislabels degrade accuracy.

## Read-Only Mode
**Choice**: Global `readOnlyMode` gate.
- Use Cases: Dry runs in staging; audit evaluation.
- Limitation: Does not simulate exact disruption timing—only suppresses writes.

## Trade-offs Summary Table
| Area | Decision | Primary Benefit | Main Trade-off | Status |
|------|----------|-----------------|---------------|--------|
| Enablement | Dual label gate | Safety | Extra labeling step | Stable |
| Config | YAML + env overlay | Simplicity | No live reload | Stable |
| Distribution | Closed-form formula | Predictability | Less adaptive | Stable |
| Mutation | Admission webhook | Leverages scheduler | Cert mgmt | Stable |
| DI | Uber Dig | Testability | Indirection | Stable |
| Metrics | Minimal core set | Low cardinality | Limited latency insight | Stable |
| Node Classifier | Label selectors | Cloud agnostic | Relies on provisioner | Stable |

## Rejected Alternatives
| Alternative | Reason Rejected | Revisit Trigger |
|------------|-----------------|-----------------|
| CRD Policy Objects | Added complexity before semantics stable | Need for multi-policy layering |
| Scheduler Framework Plugin | Higher implementation/maintenance cost | Need for fine-grained placement constraints |
| Dynamic Config Reload | Complexity vs early-stage stability | Frequent runtime config change requests |
| Provider API Spot Detection | Cloud coupling | Cross-cloud feature divergence |

## Provisional / Future Considerations
- Disruption window semantics may expand (multiple windows list).
- Additional mutation types (tolerations, affinities) gated by configuration.
- Latency histograms & percentile SLIs for reconciliations.

## Recent Decisions

### Admission Race Prevention via Pending Counters
Burst admissions (e.g., HPA scaling +10) can lead to multiple concurrent webhook requests making identical decisions from a stale view. Spotalis uses an in-memory AdmissionStateTracker to record pending spot/on-demand decisions per workload with a short TTL and workload generation awareness. Decisions are based on effective counts = current pods + pending decisions, eliminating race-induced over-allocation while remaining lightweight and stateless across restarts.

Trade-offs:
- Pros: Simple and fast; no CRDs; robust during bursts; low operational overhead.
- Cons: Transient in-memory state; controller convergence still required after restarts.

### Eviction API vs Direct Delete
Controllers rebalance using the Kubernetes Eviction API (pods/eviction) rather than directly deleting pods.

Reasons:
- Respects PodDisruptionBudgets (PDBs); blocked evictions return HTTP 429.
- Honors graceful termination and lifecycle hooks.
- Provides better auditability via API server events.

Operational behavior:
- Evict at most one pod per reconcile cycle to limit disruption.
- If eviction is blocked by a PDB, log and retry on subsequent reconciles without treating it as an error (avoids exponential backoff).

## See Also
- [Architecture](./architecture.md)
- [Replica Distribution Strategy](./replica-distribution-strategy.md)
- [Configuration Reference](../reference/configuration.md)
- [Metrics](../reference/metrics.md)
