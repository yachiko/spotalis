# Quickstart Guide: Kubernetes Workload Replica Manager

## Overview
This quickstart guide walks you through deploying and testing the Kubernetes Workload Replica Manager (Spotalis) to automatically manage replica distribution between spot and on-demand nodes.

## Prerequisites

### Cluster Requirements
- Kubernetes cluster v1.26+
- Admission webhooks enabled
- Karpenter or similar node provisioner with spot/on-demand node labeling
- kubectl configured with cluster admin access

### Node Labeling
Ensure your cluster nodes are labeled with capacity type:
```bash
# Verify node labels (Karpenter default)
kubectl get nodes -L karpenter.sh/capacity-type

# Expected output:
NAME                                       STATUS   ROLES    AGE   VERSION            CAPACITY-TYPE
ip-10-0-1-100.us-west-2.compute.internal   Ready    <none>   5d    v1.28.0-eks-123    spot
ip-10-0-1-200.us-west-2.compute.internal   Ready    <none>   5d    v1.28.0-eks-123    on-demand
```

## Installation

### 1. Create Namespace
```bash
kubectl create namespace spotalis-system
```

### 2. Apply RBAC Configuration
```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: spotalis-controller
  namespace: spotalis-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: spotalis-controller
rules:
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets"]
  verbs: ["get", "list", "watch", "update", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch", "delete"]
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "create", "update", "patch", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: spotalis-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: spotalis-controller
subjects:
- kind: ServiceAccount
  name: spotalis-controller
  namespace: spotalis-system
EOF
```

### 3. Create Configuration
```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: spotalis-config
  namespace: spotalis-system
data:
  config.yaml: |
    controller:
      resyncPeriod: 30s              # Reconciliation interval (min 5s, default 30s)
      workers: 5
      leaderElection:
        enabled: true
        namespace: spotalis-system
      # Optional: Limit monitoring to specific namespaces (multi-tenant support)
      # namespaceSelector:
      #   matchLabels:
      #     spotalis.io/enabled: "true"
      #   matchExpressions:
      #     - key: environment
      #       operator: In
      #       values: ["production", "staging"]
        
    webhook:
      port: 9443
      certDir: /tmp/k8s-webhook-server/serving-certs
      
    nodeLabels:
      spot: "karpenter.sh/capacity-type=spot"
      onDemand: "karpenter.sh/capacity-type=on-demand"
      
    logging:
      level: "info"
      format: "json"
      
    metrics:
      enabled: true
      port: 8080
EOF
```

### 4. Deploy Controller
```bash
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spotalis-controller
  namespace: spotalis-system
  labels:
    app: spotalis-controller
spec:
  replicas: 3
  selector:
    matchLabels:
      app: spotalis-controller
  template:
    metadata:
      labels:
        app: spotalis-controller
    spec:
      serviceAccountName: spotalis-controller
      containers:
      - name: spotalis
        image: spotalis/controller:v0.1.0
        # Single binary - no subcommands needed (Karpenter pattern)
        ports:
        - name: webhook
          containerPort: 9443
          protocol: TCP
        - name: metrics
          containerPort: 8080
          protocol: TCP
        env:
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        volumeMounts:
        - name: config
          mountPath: /etc/spotalis
          readOnly: true
        - name: webhook-certs
          mountPath: /tmp/k8s-webhook-server/serving-certs
          readOnly: true
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
      volumes:
      - name: config
        configMap:
          name: spotalis-config
      - name: webhook-certs
        secret:
          secretName: spotalis-webhook-certs
---
apiVersion: v1
kind: Service
metadata:
  name: spotalis-webhook
  namespace: spotalis-system
spec:
  selector:
    app: spotalis-controller
  ports:
  - name: webhook
    port: 443
    targetPort: webhook
    protocol: TCP
---
apiVersion: v1
kind: Service
metadata:
  name: spotalis-metrics
  namespace: spotalis-system
  labels:
    app: spotalis-controller
spec:
  selector:
    app: spotalis-controller
  ports:
  - name: metrics
    port: 8080
    targetPort: metrics
    protocol: TCP
EOF
```

### 5. Configure Webhook Admission
```bash
cat <<EOF | kubectl apply -f -
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionWebhook
metadata:
  name: spotalis-mutating-webhook
webhooks:
- name: pod-mutation.spotalis.io
  clientConfig:
    service:
      name: spotalis-webhook
      namespace: spotalis-system
      path: "/mutate"
  rules:
  - operations: ["CREATE"]
    apiGroups: [""]
    apiVersions: ["v1"]
    resources: ["pods"]
  admissionReviewVersions: ["v1", "v1beta1"]
  failurePolicy: Fail
  sideEffects: None
EOF
```

## Basic Usage

### 1. Deploy a Test Application
```bash
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/min-on-demand: "2"
    spotalis.io/spot-percentage: "70%"
spec:
  replicas: 10
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        ports:
        - containerPort: 80
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: test-app-pdb
  namespace: default
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: test-app
EOF
```

### 2. Verify Replica Distribution
```bash
# Check pod distribution across node types
kubectl get pods -l app=test-app -o wide

# Check node types
kubectl get nodes -L karpenter.sh/capacity-type

# Expected result: 2+ pods on on-demand nodes, ~7 pods on spot nodes
```

### 3. Monitor Controller Status
```bash
# Check controller logs
kubectl logs -n spotalis-system -l app=spotalis-controller -f

# Check controller metrics
kubectl port-forward -n spotalis-system svc/spotalis-metrics 8080:8080
curl http://localhost:8080/metrics | grep spotalis

# Check events
kubectl get events --field-selector source=spotalis-controller
```

## Testing Scenarios

### Scenario 1: Spot Node Termination
```bash
# Simulate spot node termination
kubectl drain <spot-node-name> --ignore-daemonsets --delete-emptydir-data

# Verify: New pods scheduled on other spot nodes or on-demand as fallback
kubectl get pods -l app=test-app -o wide
```

### Scenario 2: Configuration Change
```bash
# Update replica distribution
kubectl annotate deployment test-app spotalis.io/spot-percentage=50% --overwrite

# Verify: Pods rescheduled to match new 50% spot target
kubectl get pods -l app=test-app -o wide
```

### Scenario 3: Scale Up/Down
```bash
# Scale deployment up
kubectl scale deployment test-app --replicas=15

# Verify: New pods follow distribution rules (2+ on-demand, rest spot)
kubectl get pods -l app=test-app -o wide

# Scale deployment down
kubectl scale deployment test-app --replicas=5

# Verify: Distribution maintained within new replica count
```

### Scenario 4: Leader Election
```bash
# Scale controller to test leader election
kubectl scale deployment -n spotalis-system spotalis-controller --replicas=1

# Check leader election status
kubectl get lease -n spotalis-system spotalis-leader -o yaml

# Scale back up
kubectl scale deployment -n spotalis-system spotalis-controller --replicas=3

# Verify only one instance is actively reconciling
kubectl logs -n spotalis-system -l app=spotalis-controller | grep "leader"
```

## Multi-Tenant Configuration

### Scenario 5: Namespace Filtering (Multi-Tenant Clusters)
For multi-tenant clusters, you can configure the controller to only monitor specific namespaces:

```bash
# 1. Label target namespaces
kubectl label namespace production spotalis.io/enabled=true
kubectl label namespace staging spotalis.io/enabled=true

# 2. Update controller configuration
kubectl patch configmap spotalis-config -n spotalis-system --type merge -p '
{
  "data": {
    "config.yaml": "controller:\n  resyncPeriod: 30s\n  workers: 5\n  leaderElection:\n    enabled: true\n    namespace: spotalis-system\n  namespaceSelector:\n    matchLabels:\n      spotalis.io/enabled: \"true\"\nwebhook:\n  port: 9443\n  certDir: /tmp/k8s-webhook-server/serving-certs\nnodeLabels:\n  spot: \"karpenter.sh/capacity-type=spot\"\n  onDemand: \"karpenter.sh/capacity-type=on-demand\"\nlogging:\n  level: \"info\"\n  format: \"json\"\nmetrics:\n  enabled: true\n  port: 8080"
  }
}'

# 3. Restart controller to apply new configuration
kubectl rollout restart deployment spotalis-controller -n spotalis-system

# 4. Test: Deploy to monitored namespace
kubectl create namespace production
kubectl label namespace production spotalis.io/enabled=true
kubectl apply -n production -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tenant-app
  annotations:
    spotalis.io/enabled: "true" 
    spotalis.io/min-on-demand: "1"
    spotalis.io/spot-percentage: "80%"
spec:
  replicas: 5
  selector:
    matchLabels:
      app: tenant-app
  template:
    metadata:
      labels:
        app: tenant-app
    spec:
      containers:
      - name: nginx
        image: nginx:latest
EOF

# 5. Test: Deploy to non-monitored namespace (should be ignored)
kubectl create namespace development
# No label applied - controller will ignore this namespace
kubectl apply -n development -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ignored-app
  annotations:
    spotalis.io/enabled: "true"
    spotalis.io/spot-percentage: "50%"
spec:
  replicas: 3
  selector:
    matchLabels:
      app: ignored-app
  template:
    metadata:
      labels:
        app: ignored-app
    spec:
      containers:
      - name: nginx
        image: nginx:latest
EOF

# Verify: Only production namespace workloads are managed
kubectl get pods -n production -o wide
kubectl get pods -n development -o wide  # Should not have nodeSelector applied
```

### Performance Tuning
```bash
# Adjust reconciliation interval for high-traffic clusters
kubectl patch configmap spotalis-config -n spotalis-system --type merge -p '
{
  "data": {
    "config.yaml": "controller:\n  resyncPeriod: 60s\n  workers: 10\n..."
  }
}'

# For development/testing (faster reconciliation)
kubectl patch configmap spotalis-config -n spotalis-system --type merge -p '
{
  "data": {
    "config.yaml": "controller:\n  resyncPeriod: 10s\n  workers: 3\n..."
  }
}'
```

## Validation Commands

### Health Checks
```bash
# Controller health
kubectl get pods -n spotalis-system -l app=spotalis-controller

# Webhook availability
kubectl get mutatingwebhookconfigurations spotalis-mutating-webhook

# Service endpoints
kubectl get endpoints -n spotalis-system
```

### Metrics Validation
```bash
# Port forward metrics
kubectl port-forward -n spotalis-system svc/spotalis-metrics 8080:8080

# Check reconciliation metrics
curl -s http://localhost:8080/metrics | grep spotalis_reconciliations_total

# Check webhook metrics
curl -s http://localhost:8080/metrics | grep spotalis_webhook_requests_total

# Check workload distribution metrics
curl -s http://localhost:8080/metrics | grep spotalis_replica_distribution
```

### Configuration Validation
```bash
# Check workload annotations
kubectl get deployment test-app -o yaml | grep -A 10 annotations

# Verify node classification
kubectl get nodes --show-labels | grep capacity-type

# Check pod node selectors
kubectl get pods -l app=test-app -o yaml | grep -A 5 nodeSelector
```

## Troubleshooting

### Common Issues

**Controller Not Starting**:
```bash
# Check RBAC permissions
kubectl auth can-i --list --as=system:serviceaccount:spotalis-system:spotalis-controller

# Check configuration
kubectl logs -n spotalis-system -l app=spotalis-controller
```

**Webhook Not Working**:
```bash
# Check webhook configuration
kubectl get mutatingwebhookconfigurations -o yaml

# Check certificate validity
kubectl get secret -n spotalis-system spotalis-webhook-certs

# Test webhook endpoint
kubectl port-forward -n spotalis-system svc/spotalis-webhook 9443:443
```

**Pods Not Rescheduling**:
```bash
# Check PodDisruptionBudget constraints
kubectl get pdb test-app-pdb -o yaml

# Check node availability
kubectl get nodes -L karpenter.sh/capacity-type

# Check controller events
kubectl get events --field-selector source=spotalis-controller
```

### Cleanup
```bash
# Remove test application
kubectl delete deployment test-app
kubectl delete pdb test-app-pdb

# Remove controller
kubectl delete -f spotalis-manifests.yaml

# Remove RBAC
kubectl delete clusterrolebinding spotalis-controller
kubectl delete clusterrole spotalis-controller

# Remove webhook configuration
kubectl delete mutatingwebhookconfigurations spotalis-mutating-webhook

# Remove namespace
kubectl delete namespace spotalis-system
```

## Next Steps

1. **Configure Monitoring**: Set up Prometheus scraping and Grafana dashboards
2. **Set Up Alerting**: Configure alerts for controller health and webhook availability
3. **Production Deployment**: Review resource requests/limits and security settings
4. **Multi-Cluster**: Deploy across multiple clusters with appropriate configurations
5. **Custom Node Labels**: Configure custom node classification rules if not using Karpenter

For more detailed information, see the full documentation and API reference.

## Development

### Local Development (Karpenter Pattern)
The controller follows the industry-standard single binary architecture used by Karpenter:

```bash
# Run locally against your kubeconfig cluster
make run

# Or run directly with Go
go run cmd/controller/main.go

# Build the container
make docker-build

# Run tests
make test

# Integration tests with Kind
make test-integration
```

### Single Binary Architecture
- **No CLI subcommands** - main.go directly starts the operator
- **Integrated webhook** - runs in the same process as controllers  
- **Unified configuration** - single config file and environment variables
- **Simplified deployment** - one container, one process, easier RBAC

This follows the proven pattern used by Karpenter and other mature Kubernetes controllers.
````
