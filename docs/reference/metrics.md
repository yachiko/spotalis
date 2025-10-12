# Metrics Reference

Stability: Stable

Operational metrics exposed by the Spotalis controller for observability and alerting. Served in Prometheus format at the metrics bind address (default `:8080`) under path `/metrics`.

Related docs: [Runtime Ports](./runtime-ports.md), [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md), [State Management](./state-management.md)

## Endpoint
- Default address: `:8080`
- Full URL (cluster-internal typical): `http://spotalis-controller.spotalis-system.svc:8080/metrics`
- Protocol: HTTP (no auth built-in; restrict via network policy / RBAC proxied)

## Metric Inventory
| Name | Type | Labels | Purpose | Stability | Notes |
|------|------|--------|---------|-----------|-------|
| spotalis_workload_replicas | Gauge | namespace, workload, workload_type, replica_type(spot|on-demand) | Current replica layout per managed workload | Stable | Emit both spot & on-demand rows |
| spotalis_reconciliations_total | Counter | namespace, workload, workload_type, action, result(success|error) | Count of reconciliation attempts | Stable | action = e.g. `rebalance`,`noop` |
| spotalis_reconciliation_errors_total | Counter | namespace, workload, workload_type, error_type | Count of reconciliation errors | Stable | error_type = `reconciliation` currently |
| spotalis_webhook_requests_total | Counter | operation, resource_kind, result | Admission requests processed | Stable | operation=CREATE/UPDATE, result=mutated|skipped|error |
| spotalis_webhook_mutations_total | Counter | resource_kind, mutation_type | Number of actual field mutations applied | Stable | mutation_type examples: `nodeSelector`,`tolerations` (future) |
| spotalis_controller_last_seen_timestamp | Gauge | controller_name | Heartbeat timestamp (unix seconds) of controller loop | Stable | Exposed via SetToCurrentTime() |
| spotalis_leader_election_status | Gauge | controller_name | Leader flag (1=leader,0=follower) | Stable | Useful for HA alerts |

## Cardinality Guidance
- workload_replicas: Bounded by (#namespaces * #managed workloads * 2 replica types). Avoid unbounded workload labels—only enable on selected namespaces.
- *_total counters: `workload` + `namespace` cardinality; mitigate by namespace scoping Spotalis.
- leader and controller metrics: Low cardinality (controller_name typically `spotalis` or component-specific).

## Example Prometheus Rules
```yaml
# Alert if leader lost for >2m
- alert: SpotalisNoLeader
  expr: max_over_time(spotalis_leader_election_status[2m]) < 1
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Spotalis leader election lost"
    description: "No active leader for 2 minutes. Controller failover may be stuck."

# Alert if reconciliation errors spike
- alert: SpotalisReconciliationErrorBurst
  expr: rate(spotalis_reconciliation_errors_total[5m]) > 0
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Reconciliation errors occurring"
    description: "Spotalis reconciliation errors detected. Investigate controller logs."
```

## Grafana Panel Snippet
```promql
sum by (replica_type) (spotalis_workload_replicas)
```

## Collection Behavior
- Metrics registered at process start; zero-value rows seeded to ensure visibility.
- Counters never reset (except process restart).
- Gauges update during reconciliation & webhook events; heartbeat updates `controller_last_seen` periodically.

## Troubleshooting
| Symptom | Possible Cause | Action |
|---------|----------------|--------|
| No metrics endpoint reachable | metrics disabled | Ensure `observability.metrics.enabled=true` |
| Missing expected metric | Code path not exercised yet | Generate workload activity or send webhook request |
| High reconciliation error rate | Annotation parse or API errors | Inspect controller logs; verify workload labels |
| Leader status always 0 | Leader election disabled | Confirm config; enable in production |

## Stability Markers
All listed metrics are Stable. Label set considered stable; adding new labels will follow semantic versioning with documentation updates.

## See Also
- [Runtime Ports](./runtime-ports.md)
- [State Management](./state-management.md)
- [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md)
- [Configuration](./configuration.md)
