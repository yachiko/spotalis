/*
Package webhook implements Kubernetes admission webhook for pod mutation.

The webhook package provides mutating admission webhook functionality to inject
node selectors into pods based on Spotalis workload configuration.

# Core Components

PodMutator handles pod mutation requests:
  - Receives AdmissionReview requests from Kubernetes API server
  - Determines pod owner (Deployment, StatefulSet, etc.)
  - Retrieves owner's Spotalis configuration
  - Calculates appropriate nodeSelector based on pod index
  - Returns JSONPatch mutations

AdmissionHandler manages HTTP endpoint:
  - Serves HTTPS webhook endpoint
  - Validates TLS certificates
  - Handles AdmissionReview serialization
  - Provides health and readiness probes

# Usage

Setting up webhook with dependency injection:

	import (
		"github.com/ahoma/spotalis/pkg/webhook"
		"github.com/ahoma/spotalis/pkg/di"
	)

	// DI container provides dependencies
	container.Provide(webhook.NewPodMutator)
	container.Provide(webhook.NewAdmissionHandler)

	// Start webhook server
	var handler webhook.AdmissionHandler
	container.Invoke(func(h webhook.AdmissionHandler) {
		handler = h
		handler.Start(ctx, ":9443", "/tmp/k8s-webhook-server/serving-certs")
	})

# Pod Mutation Logic

The webhook determines pod placement based on owner configuration:

 1. Extract pod owner reference (Deployment, StatefulSet, etc.)
 2. Retrieve owner's spotalis.io/* annotations
 3. Parse configuration (spot percentage, min on-demand)
 4. Calculate pod index from pod name
 5. Determine if pod should be spot or on-demand
 6. Inject nodeSelector: {"spotalis.io/capacity-type": "spot|on-demand"}

# StatefulSet Handling

StatefulSets receive special handling for ordinal ordering:

	# StatefulSet with 5 replicas, 60% spot
	my-statefulset-0  →  on-demand (ordinal 0, first pod)
	my-statefulset-1  →  on-demand (ordinal 1, within min-on-demand)
	my-statefulset-2  →  spot      (ordinal 2, exceeds min-on-demand)
	my-statefulset-3  →  spot      (ordinal 3)
	my-statefulset-4  →  spot      (ordinal 4)

Lower ordinals receive on-demand placement for stability.

# RFC 6901 JSON Pointer Escaping

The webhook properly escapes annotation keys for JSON Patch operations:

	# Original annotation key
	example.com/team~alpha

	# Escaped for JSON Pointer (RFC 6901)
	example.com~1team~0alpha

Escape order matters: ~ → ~0 THEN / → ~1

# Namespace Filtering

The webhook respects namespace label filtering:

	apiVersion: v1
	kind: Namespace
	metadata:
	  labels:
	    spotalis.io/enabled: "true"

Only pods in enabled namespaces are mutated.

# Error Handling

The webhook provides graceful degradation:
  - Invalid annotations: Pod admitted without mutation, error logged
  - Missing owner: Pod admitted without mutation
  - Parse errors: Pod admitted without mutation
  - Webhook failures: Fall back to Kubernetes default behavior

This ensures workloads continue functioning even if Spotalis fails.

# Kubernetes Configuration

Webhook requires MutatingWebhookConfiguration:

	apiVersion: admissionregistration.k8s.io/v1
	kind: MutatingWebhookConfiguration
	metadata:
	  name: spotalis-mutating-webhook
	webhooks:
	  - name: pods.spotalis.io
	    rules:
	      - operations: ["CREATE"]
	        apiGroups: [""]
	        apiVersions: ["v1"]
	        resources: ["pods"]
	    clientConfig:
	      service:
	        name: spotalis-webhook
	        namespace: spotalis-system
	        path: "/mutate-v1-pod"
	      caBundle: <base64-encoded-ca-cert>
	    admissionReviewVersions: ["v1"]
	    sideEffects: None
	    failurePolicy: Ignore  # Graceful degradation

# TLS Certificates

Webhook requires valid TLS certificates:
  - Certificate must be valid for webhook service DNS
  - CA bundle must be configured in MutatingWebhookConfiguration
  - Certificates auto-mounted at /tmp/k8s-webhook-server/serving-certs

Use cert-manager or manual certificate generation:

	# Using cert-manager
	kubectl apply -f deploy/k8s/certificate.yaml

	# Manual generation
	./scripts/generate-certs.sh

# Metrics

Webhook exposes Prometheus metrics:
  - spotalis_webhook_requests_total: Total admission requests
  - spotalis_webhook_mutations_total: Successful pod mutations
  - spotalis_webhook_errors_total: Mutation errors
  - spotalis_webhook_duration_seconds: Request latency

# Testing

Webhook is tested with controller-runtime's admission package:

	import (
		. "github.com/onsi/ginkgo/v2"
		. "github.com/onsi/gomega"
		admissionv1 "k8s.io/api/admission/v1"
	)

	var _ = Describe("PodMutator", func() {
		It("should inject nodeSelector for spot pods", func() {
			req := admissionv1.AdmissionRequest{...}
			resp := mutator.Handle(ctx, req)
			Expect(resp.Allowed).To(BeTrue())
		})
	})

See *_test.go files for comprehensive test examples.

# Related Packages

  - pkg/controllers: Workload reconciliation logic
  - internal/annotations: Annotation parsing
  - internal/config: Node classification
  - pkg/apis: Configuration types
*/
package webhook
