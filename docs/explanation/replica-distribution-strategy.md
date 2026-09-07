# Replica Distribution Strategy

Stability: **Stable policy contract**

This document is the authoritative contract for Spotalis placement targets. It describes a target, not a promise that Kubernetes can schedule or keep that target healthy.

## Policy inputs and defaults

For a managed workload, the policy has two independent fields:

| Field | Symbol | Valid values | Omitted / explicit zero |
|---|---:|---|---|
| `spotalis.io/min-on-demand` | M | integer >= 0 | Omitted is available for inheritance; `0` explicitly removes a lower-priority floor. The resolved default is `0`. |
| `spotalis.io/spot-percentage` | P | integer 0 through 100 | Omitted is available for inheritance; `0` explicitly requests no Spot target. The resolved default is `0`. |
| Workload desired replicas | T | integer >= 0 | Taken from the Deployment or StatefulSet desired replica count. |

The workload API preserves an omitted field separately from an explicit zero (`apis.WorkloadPolicy`). A resolver applies explicit fields over inherited defaults before allocation. Until inheritance is resolved, an omitted value must not be converted to zero.

A managed workload with both resolved values at zero is valid and targets all replicas to on-demand. Invalid negative counts, negative percentages, percentages over 100, and negative desired replicas are rejected. A floor larger than the current desired replica count is valid: it is bounded below.

## One allocation engine

`apis.AllocateReplicaDistribution(T, ReplicaAllocationPolicy)` is the only calculation of desired targets. It uses integer arithmetic with an `int64` intermediate; consumers must not use floating-point percentage arithmetic.

For validated inputs:

```text
effectiveFloor = min(M, T)
targetSpot     = min(floor(T * P / 100), T - effectiveFloor)
targetOnDemand = T - targetSpot
```

Therefore both targets are non-negative, sum to `T`, and `targetOnDemand >= effectiveFloor`. Percentage fractions always truncate toward zero.

| T | P | M | Effective floor | Spot / On-demand |
|---:|---:|---:|---:|---:|
| 0 | 70 | 1 | 0 | 0 / 0 |
| 3 | 80 | 5 | 3 | 0 / 3 |
| 3 | 100 | 3 | 3 | 0 / 3 |
| 10 | 0 | 1 | 1 | 0 / 10 |
| 10 | 73 | 1 | 1 | 7 / 3 |
| 10 | 100 | 0 | 0 | 10 / 0 |
| 10 | 90 | 4 | 4 | 6 / 4 |

`ReplicaState.CalculateDesiredDistribution` remains a compatibility wrapper around this engine while callers migrate.

## Four different counts

Do not conflate these quantities:

1. **Desired targets** are the `targetSpot` and `targetOnDemand` calculated from workload desired replicas.
2. **Admission placement intents** are node-selector decisions for Pods that have been admitted, including short-lived reservations for requests not yet observable as Pods.
3. **Actual scheduled capacity** is the observed placement of Pods. A Pending Pod may have an intent but no scheduled node yet.
4. **Ready capacity** is the observed ready subset. It is the relevant safety signal for disruptive controller actions.

A Deployment rollout can create surge Pods, and an HPA, user, or native workload controller can change desired replicas independently. These situations can temporarily differ from the target. Spotalis converges as Pods are admitted, observed, created, and removed; it does not rewrite `spec.replicas` or promise an instantaneous exact percentage.

## Admission contract

Admission compares placement intents to the desired target for the workload's desired `T`; it must not redefine `T` from every incoming Pod or from the current burst total.

For an admission with observed counts plus unobserved reservations `(onDemand, spot)`, choose in this order:

1. If `onDemand < targetOnDemand`, select on-demand. This is the floor-first rule.
2. Otherwise, if `spot < targetSpot`, select Spot.
3. Otherwise select on-demand as the deterministic tie-breaker.

The decision and reservation must be atomic per workload. Reservations are separate temporary accounting for rollout surge or concurrent admission; they do not modify the desired target. For example, with `T=10`, `P=70`, and `M=2`, the target is 7 Spot / 3 on-demand. If the observed/reserved intent is 2 on-demand and 6 Spot, the next Pod is on-demand. Once it is 3 on-demand and 6 Spot, the next is Spot. Any additional surge Pod after 3/7 gets the deterministic on-demand intent until observation and reconciliation converge.

Reservation identity, expiry, idempotence, and multi-replica authority are admission-accounting concerns; see the implementation work for those guarantees.

## Limits of the floor

The floor is a placement target. It cannot guarantee that the floor is Ready or healthy, that compute quota or on-demand nodes exist, or that an external actor will not delete Pods or scale the workload down. Spot interruption, scheduling failure, failed startup, PDB restrictions, rollouts, and external scaling can all leave actual or Ready on-demand capacity below the target. Controllers must observe the difference and converge conservatively rather than claim the policy guarantees availability.

## Reconciliation direction

When reconciling observed placement, repair a deficit before elective cost rebalancing. In particular, do not voluntarily reduce on-demand placement below the effective floor. Reconciliation operates incrementally and may be blocked when capacity is unknown or not Ready; it is not evidence that replacement capacity exists.

## See also

- [Workload labels and annotations](../reference/labels-and-annotations.md)
- [State management](../reference/state-management.md)
- [Design choices](./design-choices.md)
