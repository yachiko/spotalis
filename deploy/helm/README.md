# Spotalis Helm Chart

A Helm chart for deploying Spotalis - a Kubernetes controller that optimizes workload replica distribution across spot and on-demand instances.

## Prerequisites

- Kubernetes 1.20+
- Helm 3.8+
- **cert-manager 1.8+** (required for webhook TLS certificates, unless using existing secret)

## Installing cert-manager

If you don't have cert-manager installed:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

Wait for cert-manager pods to be ready:

```bash
kubectl wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-webhook
```

## Installing the Chart

### Quick Start (Cluster-Wide Deployment)

```bash
helm install spotalis ./deploy/helm \
  --namespace spotalis-system \
  --create-namespace
```

### Namespace-Scoped Deployment

```bash
helm install spotalis ./deploy/helm \
  --namespace spotalis-system \
  --create-namespace \
  --set rbac.scope=namespace \
  --set config.controllers.watchNamespaces='{app-namespace-1,app-namespace-2}'
```

### High Availability Deployment

```bash
helm install spotalis ./deploy/helm \
  --namespace spotalis-system \
  --create-namespace \
  --set replicaCount=3
```

### Using Existing TLS Certificate

```bash
# First, create the secret with your certificate
kubectl create secret tls spotalis-webhook-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key \
  -n spotalis-system

# Install with existing secret
helm install spotalis ./deploy/helm \
  --namespace spotalis-system \
  --create-namespace \
  --set webhook.tls.existingSecret=spotalis-webhook-tls \
  --set webhook.certManager.enabled=false
```

## Uninstalling the Chart

```bash
helm uninstall spotalis --namespace spotalis-system
```

**Note:** This will not remove the MutatingWebhookConfiguration. Remove it manually:

```bash
kubectl delete mutatingwebhookconfiguration spotalis-mutating-webhook
```

## Configuration

### Key Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of controller replicas | `1` |
| `image.registry` | Container image registry | `ghcr.io` |
| `image.repository` | Container image repository | `ahoma/spotalis` |
| `image.tag` | Container image tag (overrides appVersion) | `""` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `rbac.scope` | RBAC scope: `cluster` or `namespace` | `cluster` |
| `webhook.enabled` | Enable mutating webhook | `true` |
| `webhook.certManager.enabled` | Use cert-manager for TLS | `true` |
| `webhook.tls.existingSecret` | Use existing TLS secret | `""` |

### RBAC Configuration

#### Cluster-Wide (Default)

```yaml
rbac:
  scope: cluster  # Watches all namespaces
```

#### Namespace-Scoped

```yaml
rbac:
  scope: namespace
config:
  controllers:
    watchNamespaces:
      - production
      - staging
```

### Operator Configuration

```yaml
config:
  operator:
    namespace: "spotalis-system"
    leaderElection:
      enabled: true  # Auto-enabled when replicaCount > 1
      id: "spotalis-controller-leader"
    disruptionWindow:
      schedule: "0 2 * * *"  # Daily at 2 AM UTC
      duration: "4h"          # 4-hour maintenance window
```

### Controller Configuration

```yaml
config:
  controllers:
    reconcileInterval: "60s"
    maxConcurrentReconciles: 2
    rateLimit:
      qps: 50.0
      burst: 100
    nodeClassifier:
      spotLabels:
        - matchLabels:
            karpenter.sh/capacity-type: "spot"
      onDemandLabels:
        - matchLabels:
            karpenter.sh/capacity-type: "on-demand"
```

### Observability Configuration

```yaml
config:
  observability:
    logging:
      level: "info"      # debug, info, warn, error
      format: "json"     # json or console
    metrics:
      enabled: true
      bindAddress: ":8080"
    health:
      enabled: true
      bindAddress: ":8081"
```

### Resource Limits

```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

### Advanced Configuration

#### Custom Environment Variables

```yaml
extraEnv:
  - name: SPOTALIS_OPERATOR_LOGLEVEL
    value: "debug"
  - name: CUSTOM_VARIABLE
    value: "custom-value"
```

#### Pod Disruption Budget

```yaml
podDisruptionBudget:
  enabled: true  # Auto-enabled when replicaCount > 1
  minAvailable: 1
```

#### Security Contexts

```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
```

## Usage

### 1. Enable Spotalis in Target Namespaces

```bash
kubectl label namespace production spotalis.io/enabled=true
```

### 2. Annotate Workloads

#### Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "70"
    spotalis.io/min-on-demand: "1"
spec:
  replicas: 10
  # ... rest of spec
```

#### StatefulSet Example

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: database
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "50"
    spotalis.io/min-on-demand: "2"
spec:
  replicas: 4
  # ... rest of spec
```

### 3. Monitor Operations

```bash
# View controller logs
kubectl logs -n spotalis-system -l app.kubernetes.io/name=spotalis

# Check metrics
kubectl port-forward -n spotalis-system svc/spotalis 8080:8080
curl http://localhost:8080/metrics

# Verify webhook
kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook
```

## Upgrading

### Standard Upgrade

```bash
helm upgrade spotalis ./deploy/helm \
  --namespace spotalis-system \
  --reuse-values
```

### Upgrade with New Values

```bash
helm upgrade spotalis ./deploy/helm \
  --namespace spotalis-system \
  --values custom-values.yaml
```

**Note:** Configuration changes require pod restart. The Helm chart automatically includes a checksum annotation that triggers rolling updates when the ConfigMap changes.

## Troubleshooting

### Webhook Certificate Issues

```bash
# Check certificate status
kubectl get certificate -n spotalis-system
kubectl describe certificate -n spotalis-system spotalis-webhook-cert

# Check secret
kubectl get secret -n spotalis-system spotalis-webhook-cert

# Force certificate renewal
kubectl delete certificate -n spotalis-system spotalis-webhook-cert
```

### RBAC Permission Errors

```bash
# Verify ClusterRole/Role exists
kubectl get clusterrole spotalis  # For cluster scope
kubectl get role -n spotalis-system spotalis  # For namespace scope

# Check bindings
kubectl get clusterrolebinding spotalis
kubectl get rolebinding -n spotalis-system spotalis
```

### Leader Election Issues

```bash
# Check lease
kubectl get lease -n spotalis-system spotalis-controller-leader

# Delete stuck lease
kubectl delete lease -n spotalis-system spotalis-controller-leader
```

### Pod Mutation Not Working

1. Verify namespace label:
   ```bash
   kubectl get namespace production --show-labels
   ```

2. Check webhook configuration:
   ```bash
   kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook -o yaml
   ```

3. Test webhook connectivity:
   ```bash
   kubectl run test --image=nginx --dry-run=server -n production
   ```

4. Review controller logs for webhook errors

## Values Schema Validation

The chart includes JSON schema validation to catch common misconfigurations:

- **Namespace scope validation**: When `rbac.scope=namespace`, `config.controllers.watchNamespaces` must be non-empty
- **Certificate validation**: Ensures only one certificate source is configured (cert-manager OR existing secret)

## Examples

See the `examples/` directory for complete configuration examples:

- `examples/configs/development.yaml` - Development configuration
- `examples/configs/production.yaml` - Production configuration (if available)

## Contributing

For more information about Spotalis development and contribution guidelines, visit:
https://github.com/ahoma/spotalis

## License

See the LICENSE file in the repository root.
