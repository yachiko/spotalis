# Spotalis Annotations Reference

This document provides comprehensive documentation for all Spotalis Kubernetes annotations and labels.

## Table of Contents

- [Overview](#overview)
- [Workload Annotations](#workload-annotations)
- [Namespace Labels](#namespace-labels)
- [Configuration Hierarchy](#configuration-hierarchy)
- [Validation Rules](#validation-rules)
- [Examples](#examples)

## Overview

Spotalis uses Kubernetes annotations and labels for configuration instead of Custom Resource Definitions (CRDs). This approach:

- **Reduces complexity**: No additional API resources to manage
- **Improves portability**: Works with any Kubernetes workload
- **Simplifies adoption**: Familiar annotation-based configuration
- **Enables gradual rollout**: Enable per-workload or per-namespace

## Workload Annotations

Annotations applied to `Deployment` or `StatefulSet` resources.

### `spotalis.io/enabled`

**Type:** Boolean (`"true"` or `"false"`)  
**Required:** Yes (to enable Spotalis optimization)  
**Default:** `"false"`

Enables Spotalis spot optimization for the workload.

**Example:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    spotalis.io/enabled: "true"
```

**Validation:**
- Must be exactly `"true"` or `"false"` (case-sensitive)
- Can be inherited from namespace label
- Workload annotation overrides namespace label

### `spotalis.io/spot-percentage`

**Type:** Integer (0-100)  
**Required:** No  
**Default:** `70`

Target percentage of replicas to run on spot instances.

**Example:**
```yaml
metadata:
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "80"
```

**Validation:**
- Must be integer between 0 and 100 (inclusive)
- `0` = all on-demand instances
- `100` = all spot instances (respects min-on-demand)
- Invalid values cause workload to be skipped with error log

**Calculation:**
```
spotReplicas = floor(totalReplicas * spotPercentage / 100)
onDemandReplicas = totalReplicas - spotReplicas

# Enforce minimum on-demand
onDemandReplicas = max(onDemandReplicas, minOnDemand)
spotReplicas = totalReplicas - onDemandReplicas
```

### `spotalis.io/min-on-demand`

**Type:** Integer (>= 0)  
**Required:** No  
**Default:** `1`

Minimum number of replicas that must run on on-demand instances.

**Example:**
```yaml
metadata:
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "90"
    spotalis.io/min-on-demand: "3"
```

**Validation:**
- Must be non-negative integer
- Cannot exceed total replicas
- Takes precedence over spot-percentage

**Use Cases:**
- **High availability**: Ensure minimum on-demand for critical workloads
- **SLA compliance**: Maintain baseline capacity on reliable instances
- **Cost optimization**: Balance cost savings with reliability

### `spotalis.io/disruption-schedule`

**Type:** Cron expression (5-field format)  
**Required:** No (both schedule and duration must be present together)  
**Default:** None (always allowed)

Cron schedule defining when pod disruptions are allowed (UTC timezone).

**Example:**
```yaml
metadata:
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/disruption-schedule: "0 2 * * *"    # Daily at 2 AM UTC
    spotalis.io/disruption-duration: "4h"
```

**Validation:**
- Must be valid 5-field cron expression
- Fields: minute hour day-of-month month day-of-week
- Always evaluated in UTC timezone
- Must be paired with `disruption-duration`

**Cron Examples:**
```
"0 2 * * *"      # Daily at 2 AM UTC
"0 0 * * 0"      # Sundays at midnight UTC
"0 */6 * * *"    # Every 6 hours
"30 2 * * 1-5"   # Weekdays at 2:30 AM UTC
"0 22 * * 6,0"   # Weekends at 10 PM UTC
```

### `spotalis.io/disruption-duration`

**Type:** Go duration string  
**Required:** No (both schedule and duration must be present together)  
**Default:** None (always allowed)

Duration of the disruption window starting from the scheduled time.

**Example:**
```yaml
metadata:
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/disruption-schedule: "0 2 * * *"
    spotalis.io/disruption-duration: "4h"           # 4-hour window
```

**Validation:**
- Must be valid Go duration format
- Common formats: `1h`, `30m`, `2h30m`, `90s`
- Must be paired with `disruption-schedule`

**Duration Examples:**
```
"4h"        # 4 hours
"30m"       # 30 minutes
"2h30m"     # 2 hours 30 minutes
"1h15m30s"  # 1 hour 15 minutes 30 seconds
```

**Behavior:**
- Rebalancing only occurs within: `[schedule_time, schedule_time + duration]`
- Outside window: reconciliation skips rebalancing
- Next window calculated automatically from cron schedule

## Namespace Labels

Labels applied to `Namespace` resources for namespace-wide defaults.

### `spotalis.io/enabled`

**Type:** Boolean (`"true"` or `"false"`)  
**Required:** No  
**Default:** `"false"`

Enable Spotalis for all workloads in the namespace (unless overridden).

**Example:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: production
  labels:
    spotalis.io/enabled: "true"
```

**Behavior:**
- Workloads inherit namespace-level enablement
- Workload annotation `spotalis.io/enabled: "false"` can opt-out
- Useful for enabling entire environments (staging, production)

### `spotalis.io/test`

**Type:** Boolean (`"true"` or `"false"`)  
**Required:** No  
**Default:** `"false"`

Mark namespace as test/temporary (used by integration tests).

**Example:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
  labels:
    spotalis.io/test: "true"
    spotalis.io/enabled: "true"
```

**Behavior:**
- Used by integration test cleanup
- No special controller behavior
- Helps identify test vs production namespaces

## Configuration Hierarchy

Spotalis resolves configuration using a precedence hierarchy:

### Precedence Order (Highest to Lowest)

1. **Workload-level annotations** (Deployment/StatefulSet)
2. **Namespace-level labels**
3. **Global configuration** (config.yaml)
4. **Built-in defaults**

### Resolution Examples

**Example 1: Workload Override**
```yaml
# Namespace
metadata:
  labels:
    spotalis.io/enabled: "true"
---
# Deployment  
metadata:
  annotations:
    spotalis.io/enabled: "false"    # ← This wins
```
**Result:** Spotalis disabled for this deployment (workload annotation wins).

**Example 2: Namespace Default**
```yaml
# Namespace
metadata:
  labels:
    spotalis.io/enabled: "true"
---
# Deployment (no annotations)
metadata:
  name: my-app
```
**Result:** Spotalis enabled via namespace inheritance. Uses default spot-percentage (70%).

**Example 3: Mixed Configuration**
```yaml
# Global config.yaml
operator:
  disruptionWindow:
    schedule: "0 2 * * *"
    duration: "4h"
---
# Deployment
metadata:
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "90"  # ← Overrides default
    # disruption window inherited from global
```
**Result:** 90% spot (from annotation), disruption window from global config.

## Validation Rules

### Annotation Parsing

**Boolean Values:**
- Only `"true"` and `"false"` (lowercase, quoted) are valid
- Empty string treated as `"false"`
- Invalid values logged and workload skipped

**Integer Values:**
- Must be valid decimal integers
- Leading zeros accepted: `"007"` → `7`
- Negative values rejected
- Out-of-range values logged and workload skipped

**Cron Expressions:**
- Validated using `github.com/robfig/cron/v3`
- Must be 5-field format (minute hour dom month dow)
- Invalid expressions logged and workload skipped

**Duration Strings:**
- Validated using `time.ParseDuration`
- Supports: `ns`, `us/µs`, `ms`, `s`, `m`, `h`
- Invalid durations logged and workload skipped

### Disruption Window Pairing

**Rule:** `disruption-schedule` and `disruption-duration` must be present together.

**Valid:**
```yaml
# Both present
spotalis.io/disruption-schedule: "0 2 * * *"
spotalis.io/disruption-duration: "4h"
```

```yaml
# Both absent (no window)
spotalis.io/enabled: "true"
spotalis.io/spot-percentage: "70"
```

**Invalid:**
```yaml
# Only schedule (ERROR)
spotalis.io/disruption-schedule: "0 2 * * *"
```

```yaml
# Only duration (ERROR)
spotalis.io/disruption-duration: "4h"
```

**Error Handling:**
- Partial disruption window → Error logged
- Workload skipped during reconciliation
- Requeued for retry after 1 minute

## Examples

### Basic Spot Optimization

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "70"
spec:
  replicas: 10
```

**Result:**
- 7 pods on spot nodes
- 3 pods on on-demand nodes

### High Availability Configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "50"
    spotalis.io/min-on-demand: "5"
spec:
  replicas: 10
```

**Result:**
- 5 pods on on-demand (minimum enforced)
- 5 pods on spot

### Maintenance Window

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: database
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "40"
    spotalis.io/disruption-schedule: "0 3 * * 0"     # Sunday 3 AM
    spotalis.io/disruption-duration: "2h"
spec:
  replicas: 5
```

**Result:**
- 2 pods on spot, 3 on on-demand
- Rebalancing only Sunday 3 AM - 5 AM UTC

### Namespace-Wide Enablement

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: staging
  labels:
    spotalis.io/enabled: "true"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-1
  namespace: staging
  annotations:
    spotalis.io/spot-percentage: "80"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-2
  namespace: staging
  annotations:
    spotalis.io/spot-percentage: "60"
```

**Result:**
- Both deployments inherit `enabled: "true"` from namespace
- app-1: 80% spot
- app-2: 60% spot

### Cost Optimization (Maximum Spot)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: batch-job
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "100"
    spotalis.io/min-on-demand: "0"
spec:
  replicas: 20
```

**Result:**
- All 20 pods on spot instances
- No on-demand instances (min set to 0)

### Reliability Focus (Minimal Spot)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-service
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "20"
    spotalis.io/min-on-demand: "8"
spec:
  replicas: 10
```

**Result:**
- 8 pods on on-demand (minimum enforced)
- 2 pods on spot

## Best Practices

### Choosing Spot Percentage

**High Spot (80-100%):**
- Batch processing
- Development/testing environments
- Stateless workloads
- Fault-tolerant applications

**Medium Spot (50-70%):**
- Web applications
- API servers
- Microservices
- General workloads

**Low Spot (20-40%):**
- Databases (with replication)
- Stateful services
- Critical services with SLAs
- Payment processing

### Setting Minimum On-Demand

**General Rule:** `min-on-demand >= (desired availability * total replicas)`

**Examples:**
```
95% availability, 10 replicas → min 5 on-demand
99% availability, 10 replicas → min 7 on-demand
99.9% availability, 10 replicas → min 9 on-demand
```

### Disruption Windows

**Use Cases:**
- **Databases**: Schedule during maintenance windows
- **Critical services**: Off-peak hours only
- **StatefulSets**: Controlled pod cycling
- **Batch jobs**: Align with job schedules

**Recommended Windows:**
```
Daily maintenance:     "0 2 * * *" + "4h"      # 2 AM - 6 AM
Weekend maintenance:   "0 0 * * 0" + "8h"      # Sunday midnight - 8 AM
Business hours only:   "0 9 * * 1-5" + "8h"    # Weekdays 9 AM - 5 PM
```

## Troubleshooting

### Annotation Not Working

**Check:**
1. Annotation spelling and case sensitivity
2. Value format (quotes for strings)
3. Namespace has `spotalis.io/enabled: "true"` label
4. Controller logs for parsing errors

**Debug:**
```bash
# Check deployment annotations
kubectl get deployment my-app -o jsonpath='{.metadata.annotations}'

# Check controller logs
kubectl logs -n spotalis-system deployment/spotalis-controller

# Check pod nodeSelector
kubectl get pods -l app=my-app -o jsonpath='{.items[*].spec.nodeSelector}'
```

### Invalid Percentage

**Error:** `invalid spot percentage "abc": parsing error`

**Fix:**
```yaml
# Wrong
spotalis.io/spot-percentage: "abc"

# Correct
spotalis.io/spot-percentage: "70"
```

### Partial Disruption Window

**Error:** `both spotalis.io/disruption-schedule and spotalis.io/disruption-duration must be specified together`

**Fix:**
```yaml
# Wrong
spotalis.io/disruption-schedule: "0 2 * * *"

# Correct
spotalis.io/disruption-schedule: "0 2 * * *"
spotalis.io/disruption-duration: "4h"
```

## API Reference

For programmatic annotation parsing:

```go
import "github.com/ahoma/spotalis/internal/annotations"

parser := annotations.NewParser()

// Check if enabled
enabled := parser.IsEnabled(annotations)

// Parse full configuration
config, err := parser.ParseConfiguration(annotations)

// Parse disruption window
window, err := annotations.ParseDisruptionWindow(annotations)
```

See [pkg.go.dev/github.com/ahoma/spotalis/internal/annotations](https://pkg.go.dev/github.com/ahoma/spotalis/internal/annotations) for full API documentation.

## Related Documentation

- [API Overview](./overview.md)
- [Configuration Guide](../deployment/production.md#configuration)
- [Architecture](../architecture/overview.md)
- [Troubleshooting](../troubleshooting/common-issues.md)
