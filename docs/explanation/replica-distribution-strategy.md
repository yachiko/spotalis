# Replica Distribution Strategy

Stability: Stable (Algorithm) / Future Extensions Provisional

Explains how Spotalis determines the target allocation of workload replicas across spot and on-demand capacity.

Related docs: [Labels & Annotations](../reference/labels-and-annotations.md), [State Management](../reference/state-management.md), [Configuration](../reference/configuration.md)

## Goals
1. Respect operator safety constraints (minimum on-demand replicas)
2. Achieve user-declared spot percentage target
3. Minimize disruptive changes (migrate only when necessary)
4. Prefer reliability over cost when constraints conflict

## Inputs
| Source | Field | Description |
|--------|-------|-------------|
| Workload spec | replicas | Total desired replica count (T) |
| Annotation | spotalis.io/spot-percentage | Target percentage (P) of replicas on spot |
| Annotation | spotalis.io/min-on-demand | Minimum on-demand count (M) |

## Core Formula
Let T=total replicas, P=spot percentage (0-100), M=min on-demand.

```
targetSpot = min( ceil(T * P / 100), T - M )
```
Derived On-Demand:
```
targetOnDemand = T - targetSpot
```

Edge Handling:
- Clamp P < 0 to 0; P > 100 to 100 (implicit by parser validation)
- If T < M then all replicas forced on-demand (targetSpot=0, targetOnDemand=T)
- If P = 0 -> targetSpot=0 (all on-demand)

## Example Scenarios
| T | P | M | Computation | Result (Spot/OnDemand) |
|---|---|---|------------|------------------------|
| 10 | 70 | 1 | min( ceil(10*70/100)=7, 9 ) | 7 / 3 |
| 10 | 90 | 4 | min( ceil(9)=9, 6 ) | 6 / 4 |
| 3 | 80 | 2 | min( ceil(2.4)=3, 1 ) | 1 / 2 |
| 2 | 50 | 3 | min( ceil(1)=1, -1 ) -> clamp negative | 0 / 2 |
| 5 | 0  | 1 | min(0,4)=0 | 0 / 5 |

## Reconciliation Actions
The `ReplicaState.GetNextAction()` method selects next step:
1. Fix total count (scale up/down) if drift in total replicas
2. Adjust distribution (migrate) only when correct total achieved
3. Prefer scaling down spot first on surplus (resilience bias)

Actions:
- scale-up-on-demand / scale-up-spot
- scale-down-spot / scale-down-on-demand
- migrate-to-spot / migrate-to-on-demand
- none

## Drift Logic
A workload NeedsReconciliation when current (C_on, C_spot) != desired (D_on, D_spot).
- On-demand drift = C_on - D_on (positive => excess on-demand)
- Spot drift = C_spot - D_spot (positive => excess spot)

Migration chosen when totals match but distribution differs; direction determined by which side has excess.

## Safety Ordering
1. Never reduce on-demand below M
2. Scale up on-demand before spot if total under-provisioned and both deficits exist
3. Scale down spot before on-demand when over-provisioned
4. Migrations occur only if safe capacity available (implementation dependent future guard rails)

## Staleness & Cooldowns
- Cooldown period prevents rapid oscillations; controllers wait `controllers.workloadTiming.cooldownPeriod` after disruptive action.
- Disruption window (if configured) restricts when rebalancing actions may execute; outside window only observe state.

## Failure Modes & Mitigations
| Failure | Mitigation |
|---------|-----------|
| Spot capacity unavailable | M ensures baseline on-demand; migration halts until rebalance possible |
| Rapid workload scale changes | Periodic reconcile re-evaluates; cooldown enforces pacing |
| Extreme P after scale (e.g., T shrinks) | Formula recomputes; may shift replicas back to on-demand safely |

## Future Extensions (Provisional)
- Weighted historical stability factor for spot adoption ramp-up
- Burst scaling guard (limit migrations per window)
- Multi-pool strategy annotations (deferred)

## See Also
- [Labels & Annotations](../reference/labels-and-annotations.md)
- [State Management](../reference/state-management.md)
- [Configuration](../reference/configuration.md)
- [Design Choices](./design-choices.md)
- [Glossary](../reference/glossary.md)
