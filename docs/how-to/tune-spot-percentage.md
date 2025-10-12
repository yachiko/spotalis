# How-To: Tune Spot Percentage

Stability: Stable

Goal: Adjust `spotalis.io/spot-percentage` to reach a target spot/on-demand replica mix without inducing unnecessary churn.

## Before You Start (Safety Checklist)
- Current replica count >= `min-on-demand + 2` (gives headroom for movement)
- No active rollout (`kubectl rollout status` reports complete)
- Recent pod restart rate acceptable

## 1. Decide Adjustment Increment
Recommended step size: 10–15 percentage points. Larger jumps may cause burst evictions or re-scheduling.

## 2. Apply New Percentage
```bash
kubectl annotate deployment/<name> spotalis.io/spot-percentage="65" --overwrite
```

If enabling for the first time add the label (not an annotation):
```bash
kubectl label deployment/<name> spotalis.io/enabled=true --overwrite
```

## 3. Observe Drift Resolution
After a reconcile cycle, re-check logs:
```bash
kubectl -n spotalis-system logs -l app=spotalis-controller --since=2m | grep -i "distribution" || true
```
Interpretation Guide:
State | Meaning | Action
----- | ------- | ------
`actualSpot < desiredSpot` | Not enough spot replicas yet | Wait; controller may shift on next reconcile
`actualSpot > desiredSpot` | Excess spot; may rebalance | Allow cooldown to avoid thrash
`actualSpot == desiredSpot` | Target achieved | No action

## 4. Iterate
If still far from target after cooldown period, adjust again in smaller increments.

## 5. Rollback (If Needed)
If error rate increases or instability observed:
```bash
kubectl annotate deployment/<name> spotalis.io/spot-percentage="previousValue" --overwrite
```
Consider temporarily raising `min-on-demand` if capacity volatility is high.

## Tips
- Avoid 100% spot unless workload is trivially restartable.
- Combine with disruption windows (when implemented) for sensitive workloads.

## Related
- Workload labels & annotations reference: `../reference/labels-and-annotations.md`
- Strategy explanation: `../explanation/replica-distribution-strategy.md`
- State management: `../reference/state-management.md`
