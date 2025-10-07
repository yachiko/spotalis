/*
Package operator provides the main operator orchestration and lifecycle management.

The operator package coordinates all Spotalis components, managing startup,
leader election, graceful shutdown, and component lifecycle.

# Core Components

Operator is the main orchestrator:
  - Coordinates controller manager, webhook, and metrics
  - Manages HTTP servers (metrics :8080, health :8081, webhook :9443)
  - Handles leader election (if enabled)
  - Orchestrates graceful shutdown

LeaderElectionManager handles leader election:
  - Kubernetes lease-based leader election
  - Automatic failover on leader failure
  - Coordinates controller startup/shutdown

ShutdownManager handles graceful shutdown:
  - Catches SIGTERM/SIGINT signals
  - Stops accepting new requests
  - Completes in-flight reconciliations
  - Cleans up resources

# Architecture

Spotalis follows a single-binary operator pattern:

	┌─────────────────────────────────────┐
	│          Operator Process           │
	│                                     │
	│  ┌───────────────────────────────┐ │
	│  │   Leader Election Manager     │ │
	│  │  (Kubernetes Lease)           │ │
	│  └───────────────────────────────┘ │
	│              ↓                      │
	│  ┌───────────────────────────────┐ │
	│  │     HTTP Servers              │ │
	│  │  - Metrics     (:8080)        │ │
	│  │  - Health      (:8081)        │ │
	│  │  - Webhook     (:9443)        │ │
	│  └───────────────────────────────┘ │
	│              ↓                      │
	│  ┌───────────────────────────────┐ │
	│  │    Controller Manager         │ │
	│  │  - DeploymentReconciler       │ │
	│  │  - StatefulSetReconciler      │ │
	│  └───────────────────────────────┘ │
	│              ↓                      │
	│  ┌───────────────────────────────┐ │
	│  │    Kubernetes API Server      │ │
	│  └───────────────────────────────┘ │
	└─────────────────────────────────────┘

# Usage

Starting the operator with dependency injection:

	import (
		"context"
		"github.com/ahoma/spotalis/pkg/operator"
		"github.com/ahoma/spotalis/pkg/di"
	)

	func main() {
		ctx := context.Background()

		// DI container provides operator
		app, err := di.NewApplication(ctx)
		if err != nil {
			log.Fatal(err)
		}

		// Start operator (blocks until shutdown)
		if err := app.Start(ctx); err != nil {
			log.Fatal(err)
		}
	}

Direct operator usage (without DI):

	// Create operator manually
	op := operator.NewOperator(
		controllerManager,
		webhookHandler,
		metricsCollector,
		config,
		logger,
	)

	// Start operator
	if err := op.Start(ctx); err != nil {
		log.Fatal(err)
	}

# Leader Election

Enable leader election for high availability:

	# config.yaml
	operator:
	  leaderElection:
	    enabled: true
	    leaseName: "spotalis-controller-leader"
	    leaseDuration: "15s"
	    renewDeadline: "10s"
	    retryPeriod: "2s"

Behavior:
  - Multiple replicas elect a single leader
  - Only leader runs controllers
  - Automatic failover on leader failure
  - Non-leaders serve webhook and metrics

Check leader status:

	kubectl get lease -n spotalis-system spotalis-controller-leader -o yaml

# Graceful Shutdown

Operator handles shutdown gracefully:

 1. Signal received (SIGTERM/SIGINT)
 2. Stop HTTP servers (no new requests)
 3. Complete in-flight reconciliations (30s timeout)
 4. Stop controller manager
 5. Release leader lease (if leader)
 6. Close Kubernetes client connections
 7. Exit process

Shutdown timeout (from config):

	operator:
	  shutdownTimeout: "30s"

# HTTP Servers

Operator starts three HTTP servers:

**Metrics Server (:8080)**
  - Prometheus metrics endpoint: /metrics
  - Runtime metrics (go_*, process_*)
  - Application metrics (spotalis_*)

**Health Server (:8081)**
  - Liveness probe: /healthz
  - Readiness probe: /readyz
  - Leader election status: /leader

**Webhook Server (:9443)**
  - Admission webhook: /mutate-v1-pod
  - TLS-enabled (cert-manager or manual certs)
  - Kubernetes MutatingWebhookConfiguration

Configure ports:

	operator:
	  metricsAddr: ":8080"
	  healthAddr: ":8081"
	webhook:
	  port: 9443
	  certDir: "/tmp/k8s-webhook-server/serving-certs"

# Kubernetes Probes

Configure liveness and readiness probes:

	apiVersion: apps/v1
	kind: Deployment
	metadata:
	  name: spotalis-controller
	spec:
	  template:
	    spec:
	      containers:
	        - name: controller
	          livenessProbe:
	            httpGet:
	              path: /healthz
	              port: 8081
	            initialDelaySeconds: 15
	            periodSeconds: 20
	          readinessProbe:
	            httpGet:
	              path: /readyz
	              port: 8081
	            initialDelaySeconds: 5
	            periodSeconds: 10

# Error Handling

Operator provides comprehensive error handling:

**Startup Errors:**
  - Configuration loading fails → Fatal exit
  - Kubernetes client init fails → Fatal exit
  - Leader election fails → Retry with backoff
  - HTTP server start fails → Fatal exit

**Runtime Errors:**
  - Controller reconciliation fails → Requeue with backoff
  - Webhook mutation fails → Admit pod without mutation
  - Metrics collection fails → Log error, continue

**Shutdown Errors:**
  - Graceful shutdown timeout → Force exit after 30s
  - Resource cleanup fails → Log error, continue

# Observability

Operator exposes comprehensive observability:

**Logs (structured JSON):**

	{"level":"info","msg":"Starting Spotalis operator","version":"v0.1.0"}
	{"level":"info","msg":"Leader election enabled","leaseName":"spotalis-controller-leader"}
	{"level":"info","msg":"HTTP servers started","metrics":":8080","health":":8081","webhook":":9443"}

**Metrics (Prometheus):**

	# Operator status
	spotalis_operator_ready{} 1
	spotalis_operator_leader{} 1

	# Controller metrics
	spotalis_reconcile_total{controller="deployment"} 150
	spotalis_reconcile_errors_total{controller="deployment"} 2

	# Webhook metrics
	spotalis_webhook_requests_total{} 500
	spotalis_webhook_mutations_total{} 480

**Events (Kubernetes):**

	kubectl get events -n spotalis-system

# Testing

Operator is tested with integration tests:

	import (
		. "github.com/onsi/ginkgo/v2"
		. "github.com/onsi/gomega"
	)

	var _ = Describe("Operator", func() {
		It("should start successfully", func() {
			op := operator.NewOperator(...)
			err := op.Start(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle graceful shutdown", func() {
			// Test shutdown behavior
		})
	})

See operator_test.go for test examples.

# High Availability

Run multiple replicas with leader election:

	apiVersion: apps/v1
	kind: Deployment
	metadata:
	  name: spotalis-controller
	spec:
	  replicas: 3  # HA with 3 replicas
	  selector:
	    matchLabels:
	      app: spotalis-controller
	  template:
	    spec:
	      containers:
	        - name: controller
	          image: spotalis:latest
	          env:
	            - name: SPOTALIS_OPERATOR_LEADER_ELECTION_ENABLED
	              value: "true"

Behavior:
  - 1 replica is leader (runs controllers)
  - 2 replicas are standby (serve webhook/metrics)
  - Automatic failover on leader failure (< 15s)

# Resource Management

Configure operator resource limits:

	spec:
	  containers:
	    - name: controller
	      resources:
	        requests:
	          cpu: 100m
	          memory: 64Mi
	        limits:
	          cpu: 200m
	          memory: 128Mi

Recommended:
  - Small clusters (<100 workloads): 100m CPU, 64Mi RAM
  - Medium clusters (100-500 workloads): 200m CPU, 128Mi RAM
  - Large clusters (>500 workloads): 500m CPU, 256Mi RAM

# Related Packages

  - pkg/controllers: Workload reconciliation
  - pkg/webhook: Admission webhook
  - pkg/di: Dependency injection
  - pkg/config: Configuration management
  - pkg/metrics: Metrics collection
  - pkg/logging: Structured logging
*/
package operator
