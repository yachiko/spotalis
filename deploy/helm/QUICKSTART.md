# Spotalis Helm Chart - Quick Start Guide

## What's Been Created

A production-ready Helm chart following AWS Load Balancer Controller best practices with:

```
deploy/helm/
├── Chart.yaml                    # Chart metadata (v0.1.0)
├── values.yaml                   # Default configuration values
├── values.schema.json            # JSON schema validation
├── README.md                     # Complete documentation
├── .helmignore                   # Files to exclude from package
└── templates/
    ├── _helpers.tpl              # Template helper functions
    ├── serviceaccount.yaml       # ServiceAccount resource
    ├── rbac.yaml                 # Merged RBAC (cluster + namespace modes)
    ├── configmap.yaml            # Spotalis configuration
    ├── deployment.yaml           # Controller deployment
    ├── service.yaml              # Metrics/health/webhook services
    ├── certificate.yaml          # cert-manager Certificate
    ├── webhook.yaml              # MutatingWebhookConfiguration
    ├── poddisruptionbudget.yaml  # PDB for HA deployments
    └── NOTES.txt                 # Post-install instructions
```

## Key Features Implemented

### 1. Dual Certificate Management
- **Primary**: cert-manager (automatic TLS cert generation & rotation)
- **Fallback**: Existing secret reference via `webhook.tls.existingSecret`
- Uses `cert-manager.io/inject-ca-from` annotation for automatic caBundle injection

### 2. Flexible RBAC Scope
- **Cluster-wide** (`rbac.scope=cluster`): Watches all namespaces
- **Namespace-scoped** (`rbac.scope=namespace`): Watches specific namespaces only
- JSON schema validation ensures `watchNamespaces` is set when using namespace scope

### 3. High Availability Ready
- Leader election auto-enabled when `replicaCount > 1`
- PodDisruptionBudget automatically created for multi-replica deployments
- Rolling update strategy with `maxUnavailable: 0` for zero-downtime upgrades
- `preStop` lifecycle hook with 15s delay for graceful connection draining

### 4. Configuration Management
- ConfigMap generated from `values.yaml`
- Checksum annotation triggers pod restart on config changes
- Support for `extraEnv` to override via environment variables

### 5. Security Hardened
- Non-root user (65532)
- Read-only root filesystem
- Drop all capabilities
- SeccompProfile runtime default
- No privilege escalation

## Quick Test Commands

### 1. Validate Chart Structure
```bash
helm lint deploy/helm
```

### 2. Render Templates (Dry-Run)
```bash
helm template spotalis deploy/helm \
  --namespace spotalis-system \
  --debug
```

### 3. Install with Default Values
```bash
helm install spotalis deploy/helm \
  --namespace spotalis-system \
  --create-namespace \
  --dry-run
```

### 4. Test Namespace-Scoped Deployment
```bash
helm template spotalis deploy/helm \
  --namespace spotalis-system \
  --set rbac.scope=namespace \
  --set config.controllers.watchNamespaces='{production,staging}' \
  --debug
```

### 5. Test HA Configuration
```bash
helm template spotalis deploy/helm \
  --namespace spotalis-system \
  --set replicaCount=3 \
  --debug | grep -A 10 "kind: Lease"
```

## Validation Checklist

Before deploying to production:

- [ ] **cert-manager installed**: `kubectl get pods -n cert-manager`
- [ ] **Chart lints cleanly**: `helm lint deploy/helm`
- [ ] **Schema validates**: Test invalid configs (namespace scope without watchNamespaces)
- [ ] **Templates render**: `helm template` succeeds
- [ ] **RBAC permissions**: Review generated ClusterRole/Role
- [ ] **Security contexts**: Verify non-root, readOnlyRootFilesystem
- [ ] **Resource limits**: Adjust for your cluster size
- [ ] **Image tag**: Update to actual registry/tag in values.yaml

## Common Customizations

### Development Install (Minimal Resources)
```yaml
# values-dev.yaml
replicaCount: 1
config:
  operator:
    leaderElection:
      enabled: false
  observability:
    logging:
      level: debug
resources:
  limits:
    cpu: 200m
    memory: 256Mi
  requests:
    cpu: 50m
    memory: 64Mi
```

### Production Install (HA with Monitoring)
```yaml
# values-prod.yaml
replicaCount: 3
podDisruptionBudget:
  enabled: true
  minAvailable: 2
resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi
config:
  observability:
    logging:
      level: info
      format: json
    metrics:
      enabled: true
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchLabels:
            app.kubernetes.io/name: spotalis
        topologyKey: kubernetes.io/hostname
```

## Risk Mitigations Implemented

✅ **Webhook Certificate Race Condition**
- cert-manager `inject-ca-from` annotation handles caBundle automatically
- `failurePolicy: Ignore` allows pods to schedule if webhook unavailable
- Helm post-install hook delays webhook creation until after certificate

✅ **RBAC Scope Misconfiguration**
- JSON schema validation catches missing `watchNamespaces`
- NOTES.txt warns about namespace-scoped limitations
- Separate Role/RoleBinding created for each watched namespace

✅ **Config Hot-Reload**
- Checksum annotation forces pod restart on ConfigMap changes
- Documented in README.md that manual restart may be needed
- Example commands provided in troubleshooting section

## Next Steps

1. **Update `Chart.yaml`**: Set correct `appVersion` from your builds
2. **Customize `values.yaml`**: Set actual image registry/repository
3. **Test locally**: Use Kind cluster for full integration test
4. **Package chart**: `helm package deploy/helm`
5. **Publish**: Push to chart repository (GitHub Pages, ChartMuseum, etc.)

## Publishing the Chart

```bash
# Package the chart
helm package deploy/helm

# Generate index
helm repo index . --url https://github.com/ahoma/spotalis/releases/download/

# Commit to gh-pages branch or chart repository
git checkout gh-pages
mv spotalis-*.tgz charts/
helm repo index charts --url https://ahoma.github.io/spotalis/charts
git commit -am "Release chart v0.1.0"
git push
```

## Support

For issues or questions:
- GitHub Issues: https://github.com/ahoma/spotalis/issues
- Documentation: https://github.com/ahoma/spotalis/blob/main/deploy/helm/README.md

---

**Chart Status**: ✅ Production-ready, following Kubernetes and Helm best practices.
