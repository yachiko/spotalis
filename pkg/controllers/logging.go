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
		Logger: cl.Logger.WithValues(
			"workload_type", workloadType,
			"replicas", replicas,
		),
		Context: cl.Context,
	}
}

// WithPhase adds reconciliation phase information
func (cl *ControllerLogger) WithPhase(phase string) *ControllerLogger {
	return &ControllerLogger{
		Logger:  cl.Logger.WithValues("phase", phase),
		Context: cl.Context,
	}
}

// WithReplicaState adds replica state information to the logger
func (cl *ControllerLogger) WithReplicaState(spotReplicas, onDemandReplicas int32) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.Logger.WithValues(
			"spot_replicas", spotReplicas,
			"on_demand_replicas", onDemandReplicas,
		),
		Context: cl.Context,
	}
}

// WithDuration adds timing information to log entries
func (cl *ControllerLogger) WithDuration(duration time.Duration) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.Logger.WithValues(
			"duration_ms", duration.Milliseconds(),
		),
		Context: cl.Context,
	}
}

// WithError adds error context while preserving the error for controller-runtime
func (cl *ControllerLogger) WithError(err error) *ControllerLogger {
	return &ControllerLogger{
		Logger: cl.Logger.WithValues(
			"error_type", fmt.Sprintf("%T", err),
		),
		Context: cl.Context,
	}
}

// ReconcileStarted logs the start of reconciliation with standard fields
func (cl *ControllerLogger) ReconcileStarted(msg string) {
	cl.Logger.Info(msg, "event", "reconcile_started")
}

// ReconcileCompleted logs successful reconciliation completion
func (cl *ControllerLogger) ReconcileCompleted(msg string, requeue bool, requeueAfter time.Duration) {
	logger := cl.Logger.WithValues(
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
	cl.Logger.Error(err, msg,
		"event", "reconcile_failed",
	)
}

// WorkloadProcessed logs when a workload has been processed
func (cl *ControllerLogger) WorkloadProcessed(msg string, rebalanced bool, podsDeleted int) {
	cl.Logger.Info(msg,
		"event", "workload_processed",
		"rebalanced", rebalanced,
		"pods_deleted", podsDeleted,
	)
}

// RebalancePerformed logs when rebalancing has been performed
func (cl *ControllerLogger) RebalancePerformed(msg string, deletedCount int, targetSpot, targetOnDemand int32) {
	cl.Logger.Info(msg,
		"event", "rebalance_performed",
		"pods_deleted", deletedCount,
		"target_spot_replicas", targetSpot,
		"target_on_demand_replicas", targetOnDemand,
	)
}

// NamespaceCheck logs namespace permission check results
func (cl *ControllerLogger) NamespaceCheck(allowed bool, reason, rule string) {
	logger := cl.Logger.WithValues(
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
	cl.Logger.Info("Checked Spotalis annotations",
		"event", "annotation_check",
		"spotalis_enabled", enabled,
		"spot_percentage", spotPercentage,
	)
}
