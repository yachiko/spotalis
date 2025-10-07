// Package controllers provides utilities for enhanced structured logging in controllers
package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// LoggingContext contains structured logging fields for controller operations
type LoggingContext struct {
	Controller  string `json:"controller"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	ReconcileID string `json:"reconcile_id"`
	RequestID   string `json:"request_id,omitempty"`
}

// ControllerLogger provides enhanced structured logging for controllers
type ControllerLogger struct {
	logr.Logger
	Context LoggingContext
}

// NewControllerLogger creates a logger with controller-specific structured fields
func NewControllerLogger(ctx context.Context, controllerName string, req ctrl.Request, kind string) *ControllerLogger {
	baseLogger := log.FromContext(ctx)

	loggingContext := LoggingContext{
		Controller:  controllerName,
		Namespace:   req.Namespace,
		Name:        req.Name,
		Kind:        kind,
		ReconcileID: uuid.New().String()[:8], // Short UUID for readability
	}

	// Add request ID from context if available
	if reqID := ctx.Value("request-id"); reqID != nil {
		if id, ok := reqID.(string); ok {
			loggingContext.RequestID = id
		}
	}

	// Create structured logger with all context fields
	structuredLogger := baseLogger.WithValues(
		"controller", loggingContext.Controller,
		"namespace", loggingContext.Namespace,
		"name", loggingContext.Name,
		"kind", loggingContext.Kind,
		"reconcile_id", loggingContext.ReconcileID,
	)

	if loggingContext.RequestID != "" {
		structuredLogger = structuredLogger.WithValues("request_id", loggingContext.RequestID)
	}

	return &ControllerLogger{
		Logger:  structuredLogger,
		Context: loggingContext,
	}
}

// WithWorkload adds workload-specific fields to the logger
func (cl *ControllerLogger) WithWorkload(workloadType string, replicas int32) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.WithValues(
			"workload_type", workloadType,
			"replicas", replicas,
		),
		Context: cl.Context,
	}
}

// WithPhase adds reconciliation phase information
func (cl *ControllerLogger) WithPhase(phase string) *ControllerLogger {
	return &ControllerLogger{
		Logger:  cl.WithValues("phase", phase),
		Context: cl.Context,
	}
}

// WithReplicaState adds replica state information to the logger
func (cl *ControllerLogger) WithReplicaState(spotReplicas, onDemandReplicas int32) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.WithValues(
			"spot_replicas", spotReplicas,
			"on_demand_replicas", onDemandReplicas,
		),
		Context: cl.Context,
	}
}

// WithDuration adds timing information to log entries
func (cl *ControllerLogger) WithDuration(duration time.Duration) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.WithValues(
			"duration_ms", duration.Milliseconds(),
		),
		Context: cl.Context,
	}
}

// WithError adds error context while preserving the error for controller-runtime
func (cl *ControllerLogger) WithError(err error) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.WithValues(
			"error_type", fmt.Sprintf("%T", err),
		),
		Context: cl.Context,
	}
}

// ReconcileStarted logs the start of reconciliation with standard fields
func (cl *ControllerLogger) ReconcileStarted(msg string) {
	cl.Info(msg, "event", "reconcile_started")
}

// ReconcileCompleted logs successful reconciliation completion
func (cl *ControllerLogger) ReconcileCompleted(msg string, requeue bool, requeueAfter time.Duration) {
	logger := cl.WithValues(
		"event", "reconcile_completed",
		"requeue", requeue,
	)

	if requeueAfter > 0 {
		logger = logger.WithValues("requeue_after_ms", requeueAfter.Milliseconds())
	}

	logger.Info(msg)
}

// ReconcileFailed logs failed reconciliation
func (cl *ControllerLogger) ReconcileFailed(err error, msg string) {
	cl.Error(err, msg,
		"event", "reconcile_failed",
	)
}

// WorkloadProcessed logs when a workload has been processed
func (cl *ControllerLogger) WorkloadProcessed(msg string, rebalanced bool, podsDeleted int) {
	cl.Info(msg,
		"event", "workload_processed",
		"rebalanced", rebalanced,
		"pods_deleted", podsDeleted,
	)
}

// RebalancePerformed logs when rebalancing has been performed
func (cl *ControllerLogger) RebalancePerformed(msg string, deletedCount int, targetSpot, targetOnDemand int32) {
	cl.Info(msg,
		"event", "rebalance_performed",
		"pods_deleted", deletedCount,
		"target_spot_replicas", targetSpot,
		"target_on_demand_replicas", targetOnDemand,
	)
}

// NamespaceCheck logs namespace permission check results
func (cl *ControllerLogger) NamespaceCheck(allowed bool, reason, rule string) {
	logger := cl.WithValues(
		"event", "namespace_check",
		"allowed", allowed,
	)

	if reason != "" {
		logger = logger.WithValues("reason", reason)
	}
	if rule != "" {
		logger = logger.WithValues("matched_rule", rule)
	}

	if allowed {
		logger.Info("Namespace is allowed for Spotalis management")
	} else {
		logger.Info("Namespace is not allowed for Spotalis management")
	}
}

// AnnotationCheck logs annotation parsing results
func (cl *ControllerLogger) AnnotationCheck(enabled bool, spotPercentage int) {
	cl.Info("Checked Spotalis annotations",
		"event", "annotation_check",
		"spotalis_enabled", enabled,
		"spot_percentage", spotPercentage,
	)
}

// WebhookLogger provides enhanced structured logging for webhooks
type WebhookLogger struct {
	logr.Logger
	Kind      string
	Namespace string
	Name      string
	Operation string
	RequestID string
}

// NewWebhookLogger creates a logger with webhook-specific structured fields
func NewWebhookLogger(ctx context.Context, req interface{}) *WebhookLogger {
	baseLogger := log.FromContext(ctx)

	// Extract admission.Request fields if available
	var kind, namespace, name, operation, requestID string

	// Use type assertion to get admission.Request fields
	if admissionReq, ok := req.(interface {
		GetKind() interface{ GetKind() string }
		GetNamespace() string
		GetName() string
		GetOperation() interface{ String() string }
		GetUID() interface{ String() string }
	}); ok {
		kind = admissionReq.GetKind().GetKind()
		namespace = admissionReq.GetNamespace()
		name = admissionReq.GetName()
		operation = admissionReq.GetOperation().String()
		requestID = admissionReq.GetUID().String()[:8]
	}

	// Create structured logger with all context fields
	structuredLogger := baseLogger.WithValues(
		"webhook", "mutation",
		"kind", kind,
		"namespace", namespace,
		"name", name,
		"operation", operation,
		"request_id", requestID,
	)

	return &WebhookLogger{
		Logger:    structuredLogger,
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Operation: operation,
		RequestID: requestID,
	}
}

// WithWorkload adds workload-specific context
func (wl *WebhookLogger) WithWorkload(workloadKind, workloadName string) *WebhookLogger {
	return &WebhookLogger{
		Logger: wl.WithValues(
			"workload_kind", workloadKind,
			"workload_name", workloadName,
		),
		Kind:      wl.Kind,
		Namespace: wl.Namespace,
		Name:      wl.Name,
		Operation: wl.Operation,
		RequestID: wl.RequestID,
	}
}

// WithConfig adds workload configuration context
func (wl *WebhookLogger) WithConfig(spotPercentage int, minOnDemand int32) *WebhookLogger {
	return &WebhookLogger{
		Logger: wl.WithValues(
			"spot_percentage", spotPercentage,
			"min_on_demand", minOnDemand,
		),
		Kind:      wl.Kind,
		Namespace: wl.Namespace,
		Name:      wl.Name,
		Operation: wl.Operation,
		RequestID: wl.RequestID,
	}
}

// MutationApplied logs successful mutation with patch count
func (wl *WebhookLogger) MutationApplied(msg string, patchCount int, mutationTypes []string) {
	wl.Info(msg,
		"event", "mutation_applied",
		"patch_count", patchCount,
		"mutation_types", mutationTypes,
	)
}

// MutationSkipped logs when mutation is skipped with reason
func (wl *WebhookLogger) MutationSkipped(reason string) {
	wl.V(1).Info("Mutation skipped",
		"event", "mutation_skipped",
		"reason", reason,
	)
}

// RequestDenied logs denied admission requests
func (wl *WebhookLogger) RequestDenied(reason string, err error) {
	wl.Error(err, "Admission request denied",
		"event", "request_denied",
		"reason", reason,
	)
}
