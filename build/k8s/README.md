# Spotalis Kubernetes Deployment

This directory contains the Kubernetes manifests for deploying Spotalis.

## Files

- `namespace.yaml` - Creates the spotalis-system namespace
- `rbac.yaml` - Service account, cluster role, and role binding for the controller
- `configmap.yaml` - Controller configuration
- `deployment.yaml` - Main controller deployment
- `service.yaml` - Services for webhook and metrics
- `webhook.yaml` - Admission webhook configurations
- `certs.yaml` - TLS certificates for the webhook (placeholder)
- `install.yaml` - Combined manifest for easy installation

## Installation

### Quick Install

```bash
# Install all components
kubectl apply -f install.yaml

# Or install individually
kubectl apply -f namespace.yaml
kubectl apply -f rbac.yaml
kubectl apply -f configmap.yaml
kubectl apply -f certs.yaml
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f webhook.yaml
```

### Certificate Management

The webhook requires TLS certificates. You have several options:

#### Option 1: cert-manager (Recommended)

```bash
# Install cert-manager first
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Create self-signed issuer
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: spotalis-webhook-certs
  namespace: spotalis-system
spec:
  secretName: spotalis-webhook-certs
  issuerRef:
    name: selfsigned-issuer
    kind: ClusterIssuer
  dnsNames:
  - spotalis-webhook.spotalis-system.svc
  - spotalis-webhook.spotalis-system.svc.cluster.local
EOF
```

#### Option 2: Manual Certificate Generation

```bash
# Generate self-signed certificate
openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt -days 365 -nodes \
  -subj "/CN=spotalis-webhook.spotalis-system.svc"

# Create secret
kubectl create secret tls spotalis-webhook-certs \
  --cert=tls.crt --key=tls.key -n spotalis-system
```

#### Option 3: Use admission-webhook-cert-injector

The deployment includes an init container approach that can generate certificates automatically.

### Configuration

Edit `configmap.yaml` to customize the controller behavior:

```yaml
data:
  config.yaml: |
    namespaceSelector:
      matchLabels:
        spotalis.io/enabled: "true"
    reconcileInterval: 30s
    # ... other configuration options
```

### Verification

```bash
# Check deployment status
kubectl get pods -n spotalis-system

# Check logs
kubectl logs -n spotalis-system deployment/spotalis-controller

# Check webhook configuration
kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook
kubectl get validatingwebhookconfiguration spotalis-validating-webhook

# Test health endpoints
kubectl port-forward -n spotalis-system deployment/spotalis-controller 8080:8080
curl http://localhost:8080/healthz
curl http://localhost:8080/metrics
```

### Enable for Namespaces

To enable Spotalis for a namespace:

```bash
kubectl label namespace my-app spotalis.io/enabled=true
```

## Uninstallation

```bash
# Remove all components
kubectl delete -f install.yaml

# Or remove individually (reverse order)
kubectl delete -f webhook.yaml
kubectl delete -f service.yaml
kubectl delete -f deployment.yaml
kubectl delete -f certs.yaml
kubectl delete -f configmap.yaml
kubectl delete -f rbac.yaml
kubectl delete -f namespace.yaml
```

## Customization

### Resource Limits

Adjust resource requests and limits in `deployment.yaml`:

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

### High Availability

For HA deployment, increase replicas and adjust leader election:

```yaml
spec:
  replicas: 3
  # In configmap.yaml:
  leaderElectionLeaseDuration: 60s
  leaderElectionRenewDeadline: 40s
```

### Security

The deployment follows security best practices:

- Non-root user (65532)
- Read-only root filesystem
- Dropped capabilities
- Security context configuration
- Network policies (add network-policy.yaml if needed)
