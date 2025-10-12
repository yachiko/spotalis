---
title: How Spotalis Works
description: General usage and internal flow of the Spotalis controller, webhook, and reconciliation pipeline.
---

# How Spotalis Works (General Usage)

This document explains the end‑to‑end flow of Spotalis: how workloads are opted in, how replica distribution is calculated, how safety is enforced, how the mutating webhook participates, and what gets exposed for observation.

> For deeper architectural rationale see [architecture](architecture.md). For the mathematical strategy details see [replica distribution strategy](replica-distribution-strategy.md).

## 1. High-Level Data Flow

```mermaid
flowchart LR
  subgraph User Space
    A[Deployment/StatefulSet] -->|Label spotalis.io/enabled| B[Controller Watches]
  end
  subgraph Controller
    B --> C[Annotation Parser]\n(parses spot % & min on-demand)
    C --> D[State Snapshot]\n(Current replicas, desired spec, prior decisions)
    D --> E[Distribution Engine]\n(Calculate target spot vs on-demand)
    E --> F[Reconciliation]\n(Scale decisions / patch)
  end
  F --> G[API Server]
  G --> H[Scheduler]
  H --> I[Nodes (Spot / On-Demand)]
  subgraph Webhook
    J[Pod Admission] --> K[NodeSelector Injection]\n(optionally adds class hints)
  end
  K --> I
```

1. Workload opt‑in via label or namespace label.
2. Controller watches desired replica counts & annotations.
3. Distribution engine computes a safe split.
4. Reconciler patches workloads incrementally.
5. Webhook (if enabled) injects node affinity / selectors to steer placement.
6. Metrics exported for feedback loops.

## 2. Opt-In & Annotation Model

| Mechanism | Purpose |
|-----------|---------|
| `spotalis.io/enabled=true` (label) | Enables management (namespace or workload scope) |
| `spotalis.io/spot-percentage` | Target portion of replicas to place on spot capacity (integer %) |
| `spotalis.io/min-on-demand` | Hard floor of on‑demand replicas always retained |
| (Future) additional tuning keys | Iteration space for scheduling nuance |

Annotations are parsed once per reconcile; invalid values fall back to conservative defaults (all on‑demand) and emit a metric/log for visibility.

See full list: [labels & annotations](../reference/labels-and-annotations.md).

## 3. Distribution Engine (Conceptual)

Inputs:
* Desired replicas (spec.replicas)
* Target spot percentage (P)
* Min on-demand floor (F)

Independence of controls:
* You may specify only `spotalis.io/min-on-demand` (omit spot %). Result: the controller treats remaining replicas (replicas - floor) as eligible for spot but will not force migration—without an explicit `spot-percentage` it defaults to a conservative 0% target (all on‑demand) while still enforcing the floor.
* You may specify only `spotalis.io/spot-percentage`. Result: the engine computes the split; the implicit floor becomes 0 (unless a global default is configured). This allows pure percentage-based optimization.
* When both are present, the floor always takes precedence if the percentage math would push on‑demand below the floor.

Practical patterns:
| Desired Intent | Suggested Settings |
|----------------|-------------------|
| Guarantee at least one stable replica, no spot yet | `min-on-demand=1` (omit spot %) |
| Gradually introduce spot capacity | `spot-percentage=30`, optionally `min-on-demand=1` |
| Maximum savings for stateless service | `spot-percentage=80`, `min-on-demand=1` |
| Keep majority stable but test spot | `spot-percentage=40`, `min-on-demand=2` |

Steps (simplified):
1. Compute tentative spot = round(P% * replicas)
2. Compute tentative on‑demand = replicas - spot
3. Enforce floor: if on‑demand < F then on‑demand = F and spot = replicas - F
4. Clamp: ensure spot >= 0
5. Produce (desiredSpot, desiredOnDemand)

Edge cases considered:
* Very small replica counts (e.g. 1) – floor may consume all capacity -> 0 spot
* Percentage extremes (0 or 100)
* Updates where spec.replicas shrinks below floor: min floor is still enforced; spot reduced first.

Detailed rationale: [replica distribution strategy](replica-distribution-strategy.md).

## 4. Safety Model

Foundational invariants:
* Never place fewer on‑demand replicas than `min-on-demand`.
* Adjustments are monotonic toward target; avoid rapid oscillation under frequent spec churn.
* Fail closed: invalid annotations => safe all on‑demand outcome.

Leader election ensures only one active reconciler mutates workloads. Follower instances remain hot for rapid failover.

## 5. Reconciliation Steps

1. Fetch workload + cached state.
2. Parse / validate annotations.
3. Compute desired split.
4. Compare to current realized split (observed through labeled pods or internal state tracking).
5. If drift > threshold, issue patch/update (controller runtime client).
6. Record metrics + emit structured log.

Patches are minimal to reduce conflict with GitOps tools (no speculative metadata churn).

## 6. Webhook Participation

When enabled, the mutating admission webhook injects node selectors / labels that align with the node classification (e.g. `capacity.spotalis.io=spot|on-demand`). This steers spot‑designated pods without modifying the original deployment template more than necessary.

If the webhook is unavailable, controller falls back to reconciliation only; distribution decisions remain, but scheduling steering is weaker.

Debug assistance: [debug mutation how-to](../how-to/debug-mutation.md).

## 7. Internal State & Metrics

State: see [state management](../reference/state-management.md) for internal structs capturing observed vs desired splits and leadership info.

Key metrics (names illustrative):
| Metric | Type | Meaning |
|--------|------|---------|
| `spotalis_replicas_desired{type="spot|on_demand"}` | Gauge | Computed desired counts |
| `spotalis_replicas_realized{type="spot|on_demand"}` | Gauge | Observed running pods |
| `spotalis_reconcile_total` | Counter | Reconcile loop executions |
| `spotalis_reconcile_errors_total` | Counter | Errors in reconcile cycles |
| `spotalis_annotation_parse_failures_total` | Counter | Invalid annotation inputs |

Port & endpoint mapping: [runtime ports](../reference/runtime-ports.md) and [metrics reference](../reference/metrics.md).

## 8. Usage Patterns

| Scenario | Approach |
|----------|----------|
| Conservative adoption | Set low spot % (e.g. 30) with min-on-demand floor >= 1, monitor drift metrics |
| Aggressive savings | Higher spot % with multi‑AZ nodes to reduce eviction risk |
| Single replica services | Floor forces all on‑demand (safe) until scaling out |
| Burst scaling events | Distribution re-evaluates; on-demand floor preserved while spot absorbs elasticity |

## 9. Failure & Degradation Modes

| Condition | Effect | Mitigation |
|-----------|--------|-----------|
| Lost leader | Paused mutations until new leader acquired | Kubernetes lease ensures quick failover |
| Webhook down | No new pod steering; existing pods unaffected | Restore certs / restart; see debug guide |
| Invalid annotations | Workload treated as all on‑demand | Fix annotation; metric highlights occurrence |
| API rate limiting | Slower convergence | Backoff + eventual consistency; tune controller rate limits |

## 10. Compatibility & GitOps

Spotalis strives for minimal spec mutation to reduce drift noise. Distribution decisions prefer patching only necessary fields. This makes it friendly to tools like ArgoCD or Flux.

## 11. Extensibility Roadmap (Indicative)

Potential future enhancements (not commitments):
* Eviction budget integration
* Dynamic spot capacity weighting by node pool health
* Predictive scaling hints
* Pluggable strategy modules

---
Feedback welcome. Refine or extend this document via PR.