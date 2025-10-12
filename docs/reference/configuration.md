# Configuration Reference

Stability: Stable

Authoritative reference for the operator's consolidated runtime configuration. This page shows:
- Canonical YAML structure with defaults
- Environment variable override matrix (prefix `SPOTALIS_`)
- Field descriptions, types, defaults, and validation rules
- Override precedence and examples

Related docs: [Labels & Annotations](./labels-and-annotations.md), [Runtime Ports](./runtime-ports.md), [Design Choices](../explanation/design-choices.md)

## Canonical YAML (with defaults)
```yaml
operator:
  namespace: spotalis-system
  readOnlyMode: false
  leaderElection:
    enabled: true
    id: spotalis-controller-leader
    leaseDuration: 15s   # time.Duration
  disruptionWindow:
    schedule: ""         # 5-field cron in UTC; empty => always allowed
    duration: ""         # Go duration (e.g. 30m, 4h)
controllers:
  maxConcurrentReconciles: 1
  reconcileInterval: 5m
  nodeClassifier:
    spotLabels:
      - matchLabels:
          karpenter.sh/capacity-type: spot
    onDemandLabels:
      - matchLabels:
          karpenter.sh/capacity-type: on-demand
  workloadTiming:
    cooldownPeriod: 10s
    disruptionRetryInterval: 1m
    disruptionWindowPollInterval: 10m
webhook:
  enabled: true
  port: 9443
  certDir: /tmp/k8s-webhook-server/serving-certs
observability:
  metrics:
    enabled: true
    bindAddress: ":8080"
  logging:
    level: info    # debug|info|warn|error
    format: json   # json|console
  health:
    enabled: true
    bindAddress: ":8081"
```

## Precedence
1. Built-in defaults
2. YAML file (if provided via `-config` flag)
3. Environment variables (prefix `SPOTALIS_`) — highest precedence

## Environment Variable Mapping
| Env Var                                                       | Field Path                                              | Type     | Default                               | Notes                                  |
| ------------------------------------------------------------- | ------------------------------------------------------- | -------- | ------------------------------------- | -------------------------------------- |
| SPOTALIS_OPERATOR_NAMESPACE                                   | operator.namespace                                      | string   | spotalis-system                       | Must be non-empty                      |
| SPOTALIS_OPERATOR_READ_ONLY_MODE                              | operator.readOnlyMode                                   | bool     | false                                 | If true, no mutating actions performed |
| SPOTALIS_LEADER_ELECTION_ENABLED                              | operator.leaderElection.enabled                         | bool     | true                                  | Disable only for single-replica dev    |
| SPOTALIS_LEADER_ELECTION_ID                                   | operator.leaderElection.id                              | string   | spotalis-controller-leader            | Lease name                             |
| SPOTALIS_LEADER_ELECTION_LEASE_DURATION                       | operator.leaderElection.leaseDuration                   | duration | 15s                                   | Go duration syntax                     |
| SPOTALIS_CONTROLLERS_MAX_CONCURRENT_RECONCILES                | controllers.maxConcurrentReconciles                     | int      | 1                                     | >1 increases parallelism               |
| SPOTALIS_CONTROLLERS_RECONCILE_INTERVAL                       | controllers.reconcileInterval                           | duration | 5m                                    | Interval between periodic reconciles   |
| SPOTALIS_CONTROLLERS_WORKLOAD_COOLDOWN_PERIOD                 | controllers.workloadTiming.cooldownPeriod               | duration | 10s                                   | Minimum wait after disruption          |
| SPOTALIS_CONTROLLERS_WORKLOAD_DISRUPTION_RETRY_INTERVAL       | controllers.workloadTiming.disruptionRetryInterval      | duration | 1m                                    | Retry when window resolution fails     |
| SPOTALIS_CONTROLLERS_WORKLOAD_DISRUPTION_WINDOW_POLL_INTERVAL | controllers.workloadTiming.disruptionWindowPollInterval | duration | 10m                                   | Polling cadence outside window         |
| SPOTALIS_WEBHOOK_ENABLED                                      | webhook.enabled                                         | bool     | true                                  | Disable to skip mutation (tests/dev)   |
| SPOTALIS_WEBHOOK_PORT                                         | webhook.port                                            | int      | 9443                                  | >0 required if enabled                 |
| SPOTALIS_WEBHOOK_CERT_DIR                                     | webhook.certDir                                         | string   | /tmp/k8s-webhook-server/serving-certs | Mounted secret path                    |
| SPOTALIS_METRICS_ENABLED                                      | observability.metrics.enabled                           | bool     | true                                  | Metrics off reduces insight            |
| SPOTALIS_METRICS_BIND_ADDRESS                                 | observability.metrics.bindAddress                       | string   | :8080                                 | host:port or :port                     |
| SPOTALIS_LOGGING_LEVEL                                        | observability.logging.level                             | string   | info                                  | debug                                  | info | warn | error |
| SPOTALIS_LOGGING_FORMAT                                       | observability.logging.format                            | string   | json                                  | json or console                        |
| SPOTALIS_HEALTH_ENABLED                                       | observability.health.enabled                            | bool     | true                                  | Health endpoints disabled if false     |
| SPOTALIS_HEALTH_BIND_ADDRESS                                  | observability.health.bindAddress                        | string   | :8081                                 | host:port or :port                     |

## Field Details & Validation
### operator
- namespace (string, required) – Must be non-empty.
- readOnlyMode (bool) – If true, controllers compute but do not apply changes.
- leaderElection.enabled (bool) – Recommended true in any HA or production use.
- leaderElection.id (string) – Name of Lease resource; change only to isolate clusters.
- leaderElection.leaseDuration (duration>0) – Typical 15s–60s; shorter → faster failover, more churn.
- disruptionWindow.schedule (cron) & disruptionWindow.duration (duration) – Both empty => disruptions always allowed; else both required and validated.

### controllers
- maxConcurrentReconciles (int>0) – Parallel reconcile workers.
- reconcileInterval (duration>0) – Periodic requeue interval for workloads.
- nodeClassifier.spotLabels / onDemandLabels ([]LabelSelector) – Label selectors (same structure as Kubernetes). Multiple selectors ORed.
- workloadTiming.cooldownPeriod (duration>0) – Minimum time between successive disruptive actions for same workload.
- workloadTiming.disruptionRetryInterval (duration>0) – Wait before re-resolving disruption window on failure.
- workloadTiming.disruptionWindowPollInterval (duration>0) – Poll interval while outside allowed window.

### webhook
- enabled (bool) – If false, no admission mutation; labels/annotations still parsed by controllers.
- port (int>0 when enabled) – Container port; service must expose if using TLS admission.
- certDir (string) – Directory containing tls.key & tls.crt; must match deployment volume.

### observability.metrics
- enabled (bool) – Export Prometheus metrics when true.
- bindAddress (string) – Typically :8080; set host for binding to specific interface.

### observability.logging
- level (enum) – debug|info|warn|error; invalid values ignored (fallback to previous).
- format (enum) – json|console; json recommended for production aggregation.

### observability.health
- enabled (bool) – Serve /healthz & /readyz endpoints when true.
- bindAddress (string) – Typically :8081.

## Override Examples
### YAML File
```yaml
operator:
  leaderElection:
    leaseDuration: 30s
controllers:
  maxConcurrentReconciles: 2
observability:
  logging:
    level: debug
```

### Environment Variables
```bash
export SPOTALIS_LOGGING_LEVEL=debug
export SPOTALIS_CONTROLLERS_MAX_CONCURRENT_RECONCILES=4
```

### Combined
File defines baseline; env raises concurrency without editing file.

## Common Misconfigurations
| Symptom                                       | Likely Cause                          | Fix                                      |
| --------------------------------------------- | ------------------------------------- | ---------------------------------------- |
| Startup validation error about cooldownPeriod | Non-positive duration                 | Provide value >0 e.g. 5s                 |
| Webhook fails to start                        | enabled=true but missing certs        | Mount secret or disable webhook          |
| No metrics exposed                            | metrics.enabled=false or port blocked | Enable and ensure service/port reachable |

## Stability Markers
All fields documented are Stable unless noted. Disruption window semantics may evolve; treat schedule/duration as Provisional until first minor release after introduction.

## See Also
- [Labels & Annotations](./labels-and-annotations.md)
- [Runtime Ports](./runtime-ports.md)
- [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md)
- [Design Choices](../explanation/design-choices.md)
- [Glossary](./glossary.md)
