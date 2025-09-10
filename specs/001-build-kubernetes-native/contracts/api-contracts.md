# API Contracts: Kubernetes Workload Replica Manager

## Overview
This document defines the API contracts for the Kubernetes Workload Replica Manager, including HTTP endpoints, webhook interfaces, and Kubernetes resource schemas.

## HTTP Endpoints

### Health Check Endpoint
**Path**: `/healthz`  
**Method**: GET  
**Purpose**: Kubernetes liveness and readiness probe endpoint

**Request**: No parameters

**Response**:
```json
{
  "status": "ok",
  "timestamp": "2025-09-10T22:59:00Z",
  "version": "v0.1.0",
  "leader": true
}
```

**Status Codes**:
- 200: Service healthy and ready
- 503: Service unhealthy or not ready

### Metrics Endpoint
**Path**: `/metrics`  
**Method**: GET  
**Purpose**: Prometheus metrics scraping endpoint

**Request**: No parameters

**Response**: Prometheus text format
```
# HELP spotalis_reconciliations_total Total number of reconciliation attempts
# TYPE spotalis_reconciliations_total counter
spotalis_reconciliations_total{workload_type="deployment",status="success"} 42

# HELP spotalis_webhook_requests_total Total number of webhook requests
# TYPE spotalis_webhook_requests_total counter
spotalis_webhook_requests_total{operation="CREATE",status="success"} 15
```

### Ready Endpoint
**Path**: `/readyz`  
**Method**: GET  
**Purpose**: Kubernetes readiness probe endpoint

**Request**: No parameters

**Response**:
```json
{
  "status": "ready",
  "leader_election": "active",
  "webhook_server": "running",
  "controllers": "started"
}
```

## Webhook Interface

### Mutating Admission Webhook
**Path**: `/mutate`  
**Method**: POST  
**Purpose**: Modify pod nodeSelector based on workload configuration

**Request**: Kubernetes AdmissionReview v1
```json
{
  "apiVersion": "admission.k8s.io/v1",
  "kind": "AdmissionReview",
  "request": {
    "uid": "e911857d-c318-11e8-bbad-025000000001",
    "kind": {"group": "", "version": "v1", "kind": "Pod"},
    "resource": {"group": "", "version": "v1", "resource": "pods"},
    "operation": "CREATE",
    "object": {
      "apiVersion": "v1",
      "kind": "Pod",
      "metadata": {
        "name": "test-pod",
        "namespace": "default",
        "ownerReferences": [
          {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "name": "test-deployment",
            "uid": "deployment-uid"
          }
        ]
      },
      "spec": {
        "containers": [
          {
            "name": "app",
            "image": "nginx"
          }
        ]
      }
    }
  }
}
```

**Response**: Kubernetes AdmissionReview v1
```json
{
  "apiVersion": "admission.k8s.io/v1",
  "kind": "AdmissionReview",
  "response": {
    "uid": "e911857d-c318-11e8-bbad-025000000001",
    "allowed": true,
    "patchType": "JSONPatch",
    "patch": "W3sib3AiOiJhZGQiLCJwYXRoIjoiL3NwZWMvbm9kZVNlbGVjdG9yIiwidmFsdWUiOnsia2FycGVudGVyLnNoL2NhcGFjaXR5LXR5cGUiOiJzcG90In19XQ=="
  }
}
```

**Patch Operations**:
- Add nodeSelector for node type targeting
- Modify existing nodeSelector if present
- Add tolerations for spot node taints if needed

## Kubernetes Resource Schemas

### Workload Annotations Schema
Applied to Deployment and StatefulSet resources.

**Required Annotations**:
```yaml
metadata:
  annotations:
    spotalis.io/enabled: "true"
```

**Optional Annotations**:
```yaml
metadata:
  annotations:
    spotalis.io/min-on-demand: "2"
    spotalis.io/spot-percentage: "70%"
```

**Validation Rules**:
- `spotalis.io/enabled`: Must be "true" or "false"
- `spotalis.io/min-on-demand`: Must be integer 0-999
- `spotalis.io/spot-percentage`: Must be integer 0-100 with "%" suffix

### Status Annotations Schema
Added by the controller to track state.

```yaml
metadata:
  annotations:
    spotalis.io/last-reconciled: "2025-09-10T22:59:00Z"
    spotalis.io/current-distribution: "on-demand:3,spot:7"
    spotalis.io/desired-distribution: "on-demand:3,spot:7"
    spotalis.io/reconciliation-status: "success"
```

### ConfigMap Schema
Application configuration stored in Kubernetes ConfigMap.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: spotalis-config
  namespace: spotalis-system
data:
  config.yaml: |
    controller:
      resyncPeriod: 30s
      workers: 5
      leaderElection:
        enabled: true
        namespace: spotalis-system
        leaseName: spotalis-leader
        
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
```

## Event Schemas

### Controller Events
Events published by the controller for operational visibility.

**Successful Reconciliation**:
```yaml
apiVersion: v1
kind: Event
metadata:
  name: spotalis-reconciliation-success
  namespace: default
type: Normal
reason: ReconciliationSuccess
message: "Successfully reconciled deployment test-app: 3 on-demand, 7 spot replicas"
involvedObject:
  apiVersion: apps/v1
  kind: Deployment
  name: test-app
  namespace: default
```

**Configuration Error**:
```yaml
apiVersion: v1
kind: Event
type: Warning
reason: ConfigurationError
message: "Invalid annotation spotalis.io/spot-percentage: '150%' exceeds maximum of 100%"
```

**Pod Rescheduling**:
```yaml
apiVersion: v1
kind: Event
type: Normal
reason: PodRescheduled
message: "Deleted pod test-app-abc123 from on-demand node, will reschedule on spot node"
```

## Error Response Schemas

### HTTP Error Response
```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Webhook request missing required fields",
    "details": {
      "missing_fields": ["request.object", "request.operation"],
      "request_id": "req-12345"
    }
  },
  "timestamp": "2025-09-10T22:59:00Z"
}
```

### Webhook Error Response
```json
{
  "apiVersion": "admission.k8s.io/v1",
  "kind": "AdmissionReview",
  "response": {
    "uid": "e911857d-c318-11e8-bbad-025000000001",
    "allowed": false,
    "status": {
      "code": 400,
      "message": "Failed to parse workload annotations: invalid spot percentage format"
    }
  }
}
```

## Metrics Schema

### Controller Metrics
```
# Reconciliation metrics
spotalis_reconciliations_total{workload_type, status}
spotalis_reconciliation_duration_seconds{workload_type}
spotalis_reconciliation_errors_total{workload_type, error_type}

# Workload metrics  
spotalis_managed_workloads_total{namespace}
spotalis_replica_distribution{workload_name, namespace, node_type}
spotalis_pending_rescheduling_total{namespace}

# Leader election metrics
spotalis_leader_election_status{identity}
spotalis_leadership_changes_total
```

### Webhook Metrics
```
# Request metrics
spotalis_webhook_requests_total{operation, status}
spotalis_webhook_request_duration_seconds{operation}
spotalis_webhook_errors_total{error_type}

# Mutation metrics
spotalis_pod_mutations_total{mutation_type}
spotalis_webhook_admission_decisions_total{decision}
```

## Integration Contracts

### Karpenter Integration
Expected node labels on Karpenter-provisioned nodes:
```yaml
metadata:
  labels:
    karpenter.sh/capacity-type: "spot"        # or "on-demand"
    karpenter.sh/nodepool: "general-purpose"
    node.kubernetes.io/instance-type: "m5.large"
```

### Monitoring Integration
Expected Prometheus scrape configuration:
```yaml
scrape_configs:
- job_name: 'spotalis'
  static_configs:
  - targets: ['spotalis-controller.spotalis-system:8080']
  metrics_path: /metrics
  scrape_interval: 30s
```

### Alerting Integration
Expected AlertManager rule format:
```yaml
groups:
- name: spotalis
  rules:
  - alert: SpotalisControllerDown
    expr: up{job="spotalis"} == 0
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "Spotalis controller is down"
```
