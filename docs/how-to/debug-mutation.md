# How-To: Debug Pod Mutation

Goal: Determine why a pod was not mutated (node selector / scheduling hints) by Spotalis.

Related docs: [Labels & Annotations](../reference/labels-and-annotations.md), [Runtime Ports](../reference/runtime-ports.md), [Configuration](../reference/configuration.md)

## Decision Checklist
Follow in order; stop when the cause is found.

1. Namespace Labeled?
   - Required label: `spotalis.io/enabled=true` on the namespace.
   - Command:
     ```bash
     kubectl get ns <ns> -o jsonpath='{.metadata.labels.spotalis\.io/enabled}'
     ```
   - If empty → add label and re-create or restart workload.
2. Workload Labeled?
   - Deployment/StatefulSet must have `spotalis.io/enabled=true`.
   - Check:
     ```bash
     kubectl get deploy <name> -n <ns> -o jsonpath='{.metadata.labels.spotalis\.io/enabled}'
     ```
3. Webhook Enabled?
   - Config: `webhook.enabled` must be true.
   - Verify controller args / configmap or `SPOTALIS_WEBHOOK_ENABLED`.
4. Webhook Registered?
   - Resource present:
     ```bash
     kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook
     ```
5. TLS Certificates Present?
   - Default path inside pod: `/tmp/k8s-webhook-server/serving-certs` contains `tls.crt` & `tls.key`.
   - If missing → check cert generation job or disable webhook for local dev.
6. Pod Creation Path Uses Matching Namespace?
   - Mutation only on CREATE / sometimes UPDATE; if pod already existed prior to enabling labels, recreate it.
7. Annotation Requirements Present?
   - While enablement is via labels, tuning annotations may affect behavior (e.g., spot percentage). Missing tuning does not block mutation but may affect distribution logs.
8. Check Controller Logs
   - Look for lines containing `mutation skipped` or validation errors.
   - Example:
     ```bash
     kubectl logs deploy/spotalis-controller -n spotalis-system | grep mutation
     ```
9. Admission Request Count
   - Prometheus metric `spotalis_webhook_requests_total` increments on each attempt. If zero → traffic not reaching webhook (service/DNS/network).
10. Conflicting Admission Controllers
    - Other mutation webhooks might reject or alter pod early. Inspect `MutatingWebhookConfiguration` ordering.

## Common Causes Table
| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| No webhook configuration | Helm template not applied | Re-deploy chart / run deploy script |
| Connection refused in apiserver events | Wrong `service` or port mismatch | Verify service targetPort=9443 |
| Repeated TLS handshake errors | Mismatched or expired certs | Regenerate cert secret; restart controller |
| Workload labeled but pods untouched | Pod existed pre-label | Rollout restart workload |
| Metrics show requests but zero mutations | Conditions not met / internal skip | Increase log level to debug |

## Increase Log Verbosity

Set `SPOTALIS_LOGGING_LEVEL` or `observability.logging.level` to `debug`.

Look for detailed webhook decision logs after restart.

## See Also
- [Labels & Annotations](../reference/labels-and-annotations.md)
- [Runtime Ports](../reference/runtime-ports.md)
- [Configuration](../reference/configuration.md)
- [Replica Distribution Strategy](../explanation/replica-distribution-strategy.md)
