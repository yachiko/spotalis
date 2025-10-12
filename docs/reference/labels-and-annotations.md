# Reference: Workload Labels & Annotations (Stability: Stable)

This document defines the workload-level configuration surface for Spotalis.

Scope:
- Enablement (single label)
- Tuning annotations (replica distribution parameters)

Why rename: previously this lived at `annotations.md`; since enablement is label-only we now make the distinction explicit. (If you followed an older link, you are in the right place.)

## Enablement (Label)
Add the following label (NOT an annotation) to either the namespace (for inheritance) or the individual workload:
```
spotalis.io/enabled=true
```
If the label is absent or not "true", the workload is ignored even if tuning annotations are present.

## Summary Table
Kind | Key | Type | Default | Required | Description | Stability | Since
-----|-----|------|---------|----------|-------------|----------|------
Label | `spotalis.io/enabled` | string (`"true"` to enable) | (absent = disabled) | Yes (to activate) | Opts workload into management | Stable | v0.x
Annotation | `spotalis.io/spot-percentage` | int (0–100) | 70 | No | Target % of replicas on spot (capped by safety floor) | Stable | v0.x
Annotation | `spotalis.io/min-on-demand` | int (>=0) | 1 | No | Minimum on-demand replica floor | Stable | v0.x

## Tuning Annotation Details
Use annotations only after the label enables the workload. Absent annotations fall back to defaults.

## Precedence & Inheritance
1. Workload annotations (highest)
2. Namespace labels (opt-in / defaults)
3. Global configuration file values
4. Built-in defaults (lowest)

Workload-level annotations override namespace label–provided defaults where applicable.

## Validation Rules
Rule | Condition | Result
---- | --------- | ------
Range check | `spot-percentage` < 0 or > 100 | Rejected / clamped (implementation-dependent)
Floor feasibility | `min-on-demand` > replicas | Controller clamps spot target to 0
Enablement missing | enablement label absent or != "true" | Annotations ignored

## Rounding & Calculation
Distribution formula (see strategy doc for full detail):
```
targetSpot = min( ceil(totalReplicas * spotPercentage/100), totalReplicas - minOnDemand )
```
Edge Cases:
- `totalReplicas == 0`: no action.
- `totalReplicas < minOnDemand`: force all replicas to on-demand (targetSpot = 0).
- High percentage with small replica count may collapse after ceiling; verify logs.

## Examples
Namespace defaulting example (label only):
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: workloads
  labels:
    spotalis.io/enabled: "true"
```

Workload with tuning annotations (label used for enablement):
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  labels:
    spotalis.io/enabled: "true"  # enable management
  annotations:
    spotalis.io/spot-percentage: "70"
    spotalis.io/min-on-demand: "1"
spec:
  replicas: 6
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
      - name: app
        image: nginx:alpine
```

## Migration / Backward Compatibility
- Historical enabling via an annotation was never officially supported and is now explicitly documented as label-only.
- Old references to `annotations.md` should be updated to `labels-and-annotations.md` (or generic wording: "Workload labels & annotations").

## Quick Reference Cheat Sheet
Action | What to Change | Example
------ | --------------- | -------
Enable workload | Add label | `kubectl label deploy/api spotalis.io/enabled=true`
Adjust spot % | Update annotation | `kubectl annotate deploy/api spotalis.io/spot-percentage="60" --overwrite`
Raise on-demand floor | Update annotation | `kubectl annotate deploy/api spotalis.io/min-on-demand="3" --overwrite`
Disable management | Remove label | `kubectl label deploy/api spotalis.io/enabled-`

## Related
- Replica distribution strategy: `../explanation/replica-distribution-strategy.md`
- Tuning guide: `../how-to/tune-spot-percentage.md`
- State management (drift interpretation): `state-management.md`
