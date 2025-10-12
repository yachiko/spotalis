---
title: Install Spotalis with Helm
description: Task-oriented guide to install, verify, configure, and uninstall Spotalis via Helm.
---

# Install Spotalis with Helm

This how-to walks you through installing Spotalis in a production-like namespace using Helm, verifying that it is healthy, enabling your first workload, and (optionally) uninstalling it cleanly. For a first local success on Kind, see the tutorial in `../tutorials/run-kind.md`.

> Spotalis is early-stage. Annotation keys and minor flags may evolve; check release notes when available.

## 1. Prerequisites

| Dependency | Requirement / Note |
|------------|--------------------|
| Kubernetes | (List tested versions here, e.g. >= 1.27) |
| Helm       | v3.x |
| Permissions | Cluster role to create namespace + RBAC resources |
| (Optional) Prometheus | To scrape metrics & build alerts |

## 2. Add the Helm Repository

Replace the repo URL once the chart is published.

```bash
helm repo add spotalis https://example.invalid/spotalis
helm repo update
```

List available versions:

```bash
helm search repo spotalis --versions
```

## 3. Install / Upgrade

```bash
helm upgrade --install spotalis spotalis/spotalis \
	--namespace spotalis-system \
	--create-namespace
```

Helm will perform an install the first time and upgrades on subsequent invocations. Add `--version <semver>` to pin a specific release.

### Common Overrides (Inline)

```bash
helm upgrade --install spotalis spotalis/spotalis \
	-n spotalis-system --create-namespace \
	--set controller.logLevel=info \
	--set controller.replicas=1 \
	--set leaderElection.enabled=true
```

Or supply a `values.yaml` file:

```bash
helm upgrade --install spotalis spotalis/spotalis -n spotalis-system -f my-values.yaml
```

> Full schema: see `../../reference/configuration.md` and the chart `values.yaml`.

## 4. Verify the Deployment

Check pods:

```bash
kubectl get pods -n spotalis-system
```

Check leader lease (ensures single active controller):

```bash
kubectl get lease -n spotalis-system spotalis-controller-leader -o yaml
```

Check metrics & health ports (port numbers documented in `../../reference/runtime-ports.md`). Example port-forward:

```bash
kubectl port-forward -n spotalis-system deploy/spotalis-controller 8080:8080 8081:8081
curl -sf localhost:8081/healthz
curl -sf localhost:8080/metrics | head
```

If using the webhook, confirm it registered:

```bash
kubectl get mutatingwebhookconfigurations | grep spotalis || echo "(webhook not enabled)"
```

## 5. Enable a Workload

Opt-in via a namespace label or an individual workload label. Example Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
	name: api
	labels:
		spotalis.io/enabled: "true"          # Opt-in
	annotations:
		spotalis.io/spot-percentage: "70"    # Target % spot capacity
		spotalis.io/min-on-demand: "1"       # Safety floor
spec:
	replicas: 5
	selector:
		matchLabels:
			app: api
	template:
		metadata:
			labels:
				app: api
		spec:
			containers:
				- name: api
					image: ghcr.io/example/api:latest
```

The controller will reconcile and shift replicas according to the distribution strategy (`../../explanation/replica-distribution-strategy.md`). All labels & annotations: `../../reference/labels-and-annotations.md`.

## 6. Observability Snapshot

Prometheus metrics (see `../../reference/metrics.md`) expose desired vs realized spot/on-demand counts. Health endpoints & port usage: `../../reference/runtime-ports.md`.

Suggested alert ideas:
* High divergence between desired and realized spot percentage
* Min on-demand floor violation (should not happen; indicates scheduling constraints)

## 7. Troubleshooting

| Symptom | Likely Cause | Next Step |
|---------|--------------|-----------|
| Webhook timeouts | TLS cert not mounted / DNS issues | See `../../how-to/debug-mutation.md` |
| No leader lease | RBAC or coordination API blocked | Check RBAC, reapply chart |
| Replica mix not changing | Missing opt-in label/annotation | Confirm `spotalis.io/enabled=true` |
| Excessive reconcile logs | Too aggressive settings or rapid cluster scaling | Tune annotations / review strategy doc |

Gather logs:

```bash
kubectl logs -n spotalis-system deploy/spotalis-controller | grep -i error
```

## 8. Uninstall

Remove all controller resources:

```bash
helm uninstall spotalis -n spotalis-system
```

Namespace cleanup (optional if dedicated):

```bash
kubectl delete namespace spotalis-system
```

Spotalis creates no CRDs, so uninstall is a clean removal of namespace-scoped objects.

## 9. Next Steps

* Fine-tune spot/on-demand mix: `../../how-to/tune-spot-percentage.md`
* Dive into internals & strategy: `../../explanation/architecture.md`, `../../explanation/replica-distribution-strategy.md`
* Explore state & safety: `../../reference/state-management.md`

---
_Feedback welcome—open an issue or PR to improve this guide._
