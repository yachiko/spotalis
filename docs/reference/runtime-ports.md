# Runtime Ports & Endpoints

Stability: Stable

Quick reference for network-facing endpoints exposed by the Spotalis controller binary.

Related docs: [Configuration](./configuration.md), [Debug Mutation](../how-to/debug-mutation.md), [Metrics](./metrics.md)

## Port Summary
| Purpose | Default | Config Field | Env Override | Notes |
|---------|---------|--------------|--------------|-------|
| Metrics | :8080 | observability.metrics.bindAddress | SPOTALIS_METRICS_BIND_ADDRESS | Prometheus scrape `/metrics` |
| Health  | :8081 | observability.health.bindAddress | SPOTALIS_HEALTH_BIND_ADDRESS | `/healthz`, `/readyz` JSON |
| Webhook | 9443 (container port) | webhook.port | SPOTALIS_WEBHOOK_PORT | TLS required; service targetPort 9443 |

## Paths
| Endpoint | Port | Method | Description |
|----------|------|--------|-------------|
| /metrics | Metrics | GET | Prometheus text exposition |
| /healthz | Health | GET | Basic liveness status JSON |
| /readyz | Health | GET | Readiness (may check leader, metrics health) |
| (Admission) /mutate (internal) | Webhook | POST | Kubernetes AdmissionReview requests |

## TLS & Certificates
- Webhook server expects certs in `webhook.certDir` (default `/tmp/k8s-webhook-server/serving-certs`).
- Files: `tls.crt`, `tls.key` (PEM format).
- Rotation requires pod restart (no in-place reload yet).

## Adjusting Ports
Change YAML or set environment variables, e.g.:
```bash
export SPOTALIS_METRICS_BIND_ADDRESS=":9090"
export SPOTALIS_WEBHOOK_PORT=10443
```
Redeploy / restart controller for changes to take effect.

## Network Policies
Allow inside-cluster scrape from Prometheus to metrics port; restrict webhook port to kube-apiserver only if using network policies.

## Troubleshooting
| Symptom | Cause | Resolution |
|---------|-------|-----------|
| Connection refused on 9443 | Webhook disabled or port mismatch | Ensure `webhook.enabled=true` & service targetPort |
| Metrics 404 | Wrong service or path | Confirm `/metrics` path and bind address |
| Health endpoints time out | Pod not ready / crash looping | Inspect pod logs; readiness might fail if leader election pending |

## See Also
- [Configuration](./configuration.md)
- [Metrics](./metrics.md)
- [Debug Mutation](../how-to/debug-mutation.md)
- [Design Choices](../explanation/design-choices.md)
