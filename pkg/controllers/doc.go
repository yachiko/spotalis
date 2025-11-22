/*
Package controllers implements Kubernetes workload reconciliation for Spotalis.

The controllers package provides reconciliation logic for Deployments and StatefulSets,
managing spot/on-demand replica distribution based on Spotalis annotations.

# Core Components

DeploymentReconciler handles Deployment workload optimization:
  - Parses spotalis.io/* annotations
  - Calculates spot vs on-demand replica distribution
  - Manages pod disruption within configured windows
  - Updates deployment with nodeSelector targeting

StatefulSetReconciler handles StatefulSet workload optimization:
  - Similar to Deployments but with StatefulSet-specific handling
  - Respects ordinal pod naming (pod-0, pod-1, ...)
  - Enforces cooldown periods between changes
  - Maintains stable network identities

ControllerManager orchestrates all controllers:
  - Registers controllers with controller-runtime manager
  - Configures reconciliation rate limiting
  - Manages shared dependencies (parser, classifier)

# Usage

Setting up controllers with dependency injection:

	import (
		"github.com/yachiko/spotalis/pkg/controllers"
		"github.com/yachiko/spotalis/pkg/di"
	)

	// DI container automatically provides dependencies
	container.Provide(controllers.NewDeploymentReconciler)
	container.Provide(controllers.NewStatefulSetReconciler)
	container.Provide(controllers.NewControllerManager)

	// Start controllers
	var mgr controllers.ControllerManager
	container.Invoke(func(m controllers.ControllerManager) {
		mgr = m
		mgr.Start(ctx)
	})

# Annotation + Label Driven Configuration

Controllers read enablement from a label and tuning from workload annotations:

	apiVersion: apps/v1
	kind: Deployment
	metadata:
	  labels:
	    spotalis.io/enabled: "true"   # enable Spotalis (label only)
	  annotations:
	    spotalis.io/spot-percentage: "70"
	    spotalis.io/min-on-demand: "2"
	spec:
	  replicas: 10

Result: 7 pods on spot nodes, 3 pods on on-demand nodes.

# Disruption Windows

Controllers respect disruption windows for controlled rebalancing:

	metadata:
	  annotations:
	    spotalis.io/disruption-schedule: "0 2 * * *"  # Daily at 2 AM UTC
	    spotalis.io/disruption-duration: "4h"

Rebalancing only occurs within the scheduled window (2 AM - 6 AM UTC).

# Rate Limiting

Controllers use workqueue-based rate limiting:
  - Base delay: 1 second
  - Max delay: 5 minutes
  - Automatic exponential backoff on errors

# Metrics

Controllers expose Prometheus metrics:
  - spotalis_reconcile_total: Total reconciliations
  - spotalis_reconcile_errors_total: Reconciliation errors
  - spotalis_reconcile_duration_seconds: Reconciliation latency

See package metrics for full metrics documentation.

# Testing

Controllers are tested with controller-runtime's envtest:

	import (
		. "github.com/onsi/ginkgo/v2"
		. "github.com/onsi/gomega"
	)

	var _ = Describe("DeploymentReconciler", func() {
		It("should distribute replicas across spot and on-demand", func() {
			// Test implementation
		})
	})

See *_test.go files for comprehensive test examples.

# Related Packages

  - pkg/webhook: Pod mutation for nodeSelector injection
  - internal/annotations: Annotation parsing logic
  - internal/config: Node classification (spot vs on-demand)
  - pkg/apis: Configuration and state types
*/
package controllers
