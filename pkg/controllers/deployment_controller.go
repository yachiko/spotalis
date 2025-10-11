// Package controllers implements Kubernetes controllers for managing workload
// replica distribution across spot and on-demand instances.
package controllers

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
	pkgconfig "github.com/ahoma/spotalis/pkg/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ControllerError represents a detailed error that occurred during reconciliation
type ControllerError struct {
	Error     error
	Timestamp time.Time
	Request   types.NamespacedName
	Recovered bool
}

// DeploymentReconciler reconciles Deployment objects for spot/on-demand pod distribution management
type DeploymentReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	AnnotationParser             *annotations.AnnotationParser
	NodeClassifier               *config.NodeClassifierService
	NamespaceFilter              *NamespaceFilter // Filter for namespace-level permissions
	ReconcileInterval            time.Duration
	MaxConcurrentRecons          int
	MetricsCollector             MetricsRecorder           // Interface for recording metrics
	Config                       *pkgconfig.SpotalisConfig // Global configuration
	CooldownPeriod               time.Duration
	DisruptionRetryInterval      time.Duration
	DisruptionWindowPollInterval time.Duration

	// Track last pod deletion time per deployment to implement cooldown
	lastDeletionTimes sync.Map // map[string]time.Time

	// Metrics tracking
	reconcileCount atomic.Int64
	errorCount     atomic.Int64

	// Error tracking
	lastError     *ControllerError
	lastErrorLock sync.RWMutex
}

// MetricsRecorder interface for recording workload metrics
type MetricsRecorder interface {
	RecordWorkloadMetrics(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState)
	RecordReconciliation(namespace, workloadName, workloadType, action string, err error)
}

// NewDeploymentReconciler creates a new DeploymentReconciler
func NewDeploymentReconciler(client client.Client, scheme *runtime.Scheme) *DeploymentReconciler {
	defaults := defaultWorkloadTimingConfig()
	return &DeploymentReconciler{
		Client:                       client,
		Scheme:                       scheme,
		AnnotationParser:             annotations.NewAnnotationParser(),
		ReconcileInterval:            5 * time.Minute,
		MaxConcurrentRecons:          10,
		CooldownPeriod:               defaults.CooldownPeriod,
		DisruptionRetryInterval:      defaults.DisruptionRetryInterval,
		DisruptionWindowPollInterval: defaults.DisruptionWindowPollInterval,
	}
}

// SetNodeClassifier sets the node classifier service
func (r *DeploymentReconciler) SetNodeClassifier(classifier *config.NodeClassifierService) {
	r.NodeClassifier = classifier
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile handles the reconciliation of Deployment objects
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Increment reconcile counter
	r.reconcileCount.Add(1)

	// Create structured logger for this reconciliation
	logger := NewControllerLogger(ctx, "deployment-controller", req, "Deployment")
	logger.V(2).Info("Starting reconciliation")

	// Fetch the Deployment
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Deployment not found, cleaning up")
			// Clean up tracking data for deleted deployment
			r.lastDeletionTimes.Delete(req.String())
			return ctrl.Result{}, nil
		}
		r.errorCount.Add(1)
		r.setLastError(&ControllerError{
			Error:     err,
			Timestamp: time.Now(),
			Request:   req.NamespacedName,
			Recovered: false,
		})
		logger.ReconcileFailed(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// Add workload context to logger
	var replicas int32
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	logger = logger.WithWorkload("deployment", replicas)

	// Check if Spotalis is explicitly enabled for this deployment
	if !r.AnnotationParser.IsSpotalisEnabled(&deployment) {
		logger.V(2).Info("Spotalis not enabled, skipping")
		return ctrl.Result{}, nil
	}

	// Check if the namespace is allowed for Spotalis management
	if r.NamespaceFilter != nil {
		logger.V(1).Info("Checking namespace permissions")
		result, err := r.NamespaceFilter.IsNamespaceAllowed(ctx, deployment.Namespace)
		if err != nil {
			r.errorCount.Add(1)
			r.setLastError(&ControllerError{
				Error:     err,
				Timestamp: time.Now(),
				Request:   req.NamespacedName,
				Recovered: false,
			})
			logger.ReconcileFailed(err, "Failed to check namespace permissions")
			return ctrl.Result{}, err
		}
		if !result.Allowed {
			logger.V(1).Info("Namespace not allowed for Spotalis management",
				"reason", result.Reason,
				"rule", result.MatchedRule)
			return ctrl.Result{}, nil
		}
		logger.V(1).Info("Namespace allowed for management", "rule", result.MatchedRule)
	}

	// Record that this workload is now managed (for metrics) - do this early
	// so we count all deployments with Spotalis annotations, not just stable ones
	if r.MetricsCollector != nil {
		// Create a basic replica state for metrics - actual rebalancing happens later
		replicaState := &apis.ReplicaState{
			TotalReplicas: 0,
		}
		if deployment.Spec.Replicas != nil {
			replicaState.TotalReplicas = *deployment.Spec.Replicas
		}
		r.MetricsCollector.RecordWorkloadMetrics(deployment.Namespace, deployment.Name, "deployment", replicaState)
	}

	// Implement cooldown period after pod deletion to avoid constant rescheduling
	deploymentKey := req.String()
	if lastDeletionInterface, exists := r.lastDeletionTimes.Load(deploymentKey); exists {
		lastDeletion, ok := lastDeletionInterface.(time.Time)
		if !ok {
			err := fmt.Errorf("invalid type for last deletion time: %T", lastDeletionInterface)
			logger.Error(err, "Invalid deletion timestamp type")
			return ctrl.Result{}, err
		}
		cooldownPeriod := r.getCooldownPeriod()
		timeSinceLastDeletion := time.Since(lastDeletion)

		if timeSinceLastDeletion < cooldownPeriod {
			remainingCooldown := cooldownPeriod - timeSinceLastDeletion
			logger.V(1).Info("In cooldown period, skipping rebalancing",
				"remaining_ms", remainingCooldown.Milliseconds())
			return ctrl.Result{RequeueAfter: remainingCooldown}, nil
		}
	}

	// Check if deployment is stable and ready for rebalancing
	if !r.isDeploymentStableAndReady(ctx, &deployment) {
		logger.V(1).Info("Deployment not stable, skipping rebalancing",
			"replicas", deployment.Status.Replicas,
			"ready", deployment.Status.ReadyReplicas,
			"updated", deployment.Status.UpdatedReplicas,
			"available", deployment.Status.AvailableReplicas,
			"unavailable", deployment.Status.UnavailableReplicas)

		// Requeue with longer interval when deployment is not stable
		return ctrl.Result{RequeueAfter: 2 * r.ReconcileInterval}, nil
	}

	logger.V(1).Info("Processing Spotalis-enabled deployment")

	// Parse the workload configuration from annotations
	workloadConfig, err := r.AnnotationParser.ParseWorkloadConfiguration(&deployment)
	if err != nil {
		r.errorCount.Add(1)
		r.setLastError(&ControllerError{
			Error:     err,
			Timestamp: time.Now(),
			Request:   req.NamespacedName,
			Recovered: false,
		})
		logger.ReconcileFailed(err, "Failed to parse workload configuration")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
	}

	// Get the current replica state
	replicaState, err := r.calculateCurrentReplicaState(ctx, &deployment)
	if err != nil {
		r.errorCount.Add(1)
		r.setLastError(&ControllerError{
			Error:     err,
			Timestamp: time.Now(),
			Request:   req.NamespacedName,
			Recovered: false,
		})
		logger.ReconcileFailed(err, "Failed to calculate replica state")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
	}

	// Calculate desired distribution based on configuration
	// Set total replicas in the state
	replicaState.TotalReplicas = deployment.Status.Replicas
	if deployment.Spec.Replicas != nil {
		replicaState.TotalReplicas = *deployment.Spec.Replicas
	}

	replicaState.CalculateDesiredDistribution(*workloadConfig)

	// Check if rebalancing is needed by comparing current vs desired state
	logger.V(1).Info("Calculated replica distribution",
		"current_spot", replicaState.CurrentSpot,
		"current_on_demand", replicaState.CurrentOnDemand,
		"desired_spot", replicaState.DesiredSpot,
		"desired_on_demand", replicaState.DesiredOnDemand,
		"spot_percentage", workloadConfig.SpotPercentage,
		"min_on_demand", workloadConfig.MinOnDemand)

	needsRebalancing := r.needsRebalancing(replicaState)

	if needsRebalancing {
		// Check disruption window before performing rebalancing
		disruptionWindow, err := r.resolveDisruptionWindow(ctx, &deployment)
		if err != nil {
			r.errorCount.Add(1)
			r.setLastError(&ControllerError{
				Error:     err,
				Timestamp: time.Now(),
				Request:   req.NamespacedName,
				Recovered: false,
			})
			logger.ReconcileFailed(err, "Failed to resolve disruption window")
			return ctrl.Result{RequeueAfter: r.getDisruptionRetryInterval()}, nil
		}

		// Check if we're within the disruption window
		if disruptionWindow != nil && !disruptionWindow.IsWithinWindow(time.Now().UTC()) {
			nextWindow := disruptionWindow.Schedule.Next(time.Now().UTC())
			logger.Info("Outside disruption window, deferring rebalancing",
				"next_window", nextWindow)
			return ctrl.Result{RequeueAfter: r.getDisruptionWindowPollInterval()}, nil
		}

		logger.Info("Rebalancing deployment pods",
			"current_spot", replicaState.CurrentSpot,
			"current_on_demand", replicaState.CurrentOnDemand,
			"target_spot", replicaState.DesiredSpot,
			"target_on_demand", replicaState.DesiredOnDemand)

		// Track rebalancing metrics
		var rebalanceErr error

		if err := r.performPodRebalancing(ctx, &deployment, replicaState, deploymentKey); err != nil {
			rebalanceErr = err
			r.errorCount.Add(1)
			r.setLastError(&ControllerError{
				Error:     err,
				Timestamp: time.Now(),
				Request:   req.NamespacedName,
				Recovered: false,
			})
			logger.ReconcileFailed(err, "Failed to rebalance pods")
		}

		// Record rebalancing metrics
		if r.MetricsCollector != nil {
			r.MetricsCollector.RecordReconciliation(
				req.Namespace,
				req.Name,
				"deployment",
				"rebalance",
				rebalanceErr,
			)
		}

		if rebalanceErr != nil {
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, rebalanceErr
		}

		// After performing rebalancing, requeue sooner to check the results
		logger.V(1).Info("Rebalancing initiated, will check status soon")

		// Mark as recovered since we successfully completed rebalancing
		r.markRecovered()

		return ctrl.Result{RequeueAfter: r.ReconcileInterval / 2}, nil
	}
	logger.V(2).Info("No rebalancing needed")

	// Mark as recovered since reconcile completed successfully
	r.markRecovered()

	logger.V(2).Info("Reconciliation completed successfully")

	// Use longer interval when no action is needed to reduce load
	return ctrl.Result{RequeueAfter: r.ReconcileInterval * 2}, nil
}

// isDeploymentStableAndReady checks if deployment is in a stable state for rebalancing
func (r *DeploymentReconciler) isDeploymentStableAndReady(_ context.Context, deployment *appsv1.Deployment) bool {
	// Don't rebalance if deployment doesn't have desired replica count set
	if deployment.Spec.Replicas == nil {
		return false
	}

	desiredReplicas := *deployment.Spec.Replicas

	// Don't rebalance if there are no desired replicas
	if desiredReplicas == 0 {
		return false
	}

	status := deployment.Status

	// Check if deployment is fully ready:
	// 1. All replicas are ready
	// 2. All replicas are updated (no ongoing rollout)
	// 3. All replicas are available
	// 4. No unavailable replicas
	isStable := status.ReadyReplicas == desiredReplicas &&
		status.UpdatedReplicas == desiredReplicas &&
		status.AvailableReplicas == desiredReplicas &&
		status.UnavailableReplicas == 0

	return isStable
}

// calculateCurrentReplicaState analyzes the current state of deployment replicas
func (r *DeploymentReconciler) calculateCurrentReplicaState(ctx context.Context, deployment *appsv1.Deployment) (*apis.ReplicaState, error) {
	// Get all pods for this deployment
	podList := &corev1.PodList{}
	selector, err := deploymentLabelSelector(deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace: deployment.Namespace,
	}, selector); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	nodeNameSet := make(map[string]struct{})
	var spotReplicas, onDemandReplicas int32

	// Classify each pod based on its node
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Spec.NodeName == "" {
			continue // Skip pending pods
		}

		nodeNameSet[pod.Spec.NodeName] = struct{}{}
	}

	nodeNames := make([]string, 0, len(nodeNameSet))
	for name := range nodeNameSet {
		nodeNames = append(nodeNames, name)
	}

	classifications, err := r.NodeClassifier.ClassifyNodesByName(ctx, nodeNames)
	if err != nil {
		log.FromContext(ctx).WithValues(
			"deployment", deployment.Name,
			"namespace", deployment.Namespace,
		).Error(err, "Failed to classify nodes for deployment pods")
		classifications = map[string]apis.NodeType{}
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Spec.NodeName == "" {
			continue
		}

		nodeType := classifications[pod.Spec.NodeName]
		switch nodeType {
		case apis.NodeTypeSpot:
			spotReplicas++
		case apis.NodeTypeOnDemand:
			onDemandReplicas++
		case apis.NodeTypeUnknown:
			// Treat unknown nodes as on-demand for safety
			onDemandReplicas++
		}
	}

	return &apis.ReplicaState{
		WorkloadRef: corev1.ObjectReference{
			APIVersion: deployment.APIVersion,
			Kind:       deployment.Kind,
			Name:       deployment.Name,
			Namespace:  deployment.Namespace,
			UID:        deployment.UID,
		},
		CurrentSpot:     spotReplicas,
		CurrentOnDemand: onDemandReplicas,
		LastReconciled:  time.Now(),
	}, nil
}

// resolveDisruptionWindow resolves the disruption window from the configuration hierarchy:
// 1. Workload-level annotations (highest priority)
// 2. Namespace-level annotations
// 3. Global configuration
// 4. No window (always allowed)
func (r *DeploymentReconciler) resolveDisruptionWindow(
	ctx context.Context,
	deployment *appsv1.Deployment,
) (*annotations.DisruptionWindow, error) {
	logger := log.FromContext(ctx).WithValues(
		"deployment", deployment.Name,
		"namespace", deployment.Namespace,
	)

	// Priority 1: Workload-level annotations
	if window, err := annotations.ParseDisruptionWindow(deployment.Annotations); err != nil {
		logger.Error(err, "Invalid disruption window annotations on Deployment")
		return nil, err
	} else if window != nil {
		logger.V(1).Info("Using Deployment-level disruption window",
			"schedule", deployment.Annotations[annotations.DisruptionScheduleAnnotation],
			"duration", deployment.Annotations[annotations.DisruptionDurationAnnotation])
		return window, nil
	}

	// Priority 2: Namespace-level annotations (using labels)
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: deployment.Namespace}, ns); err != nil {
		return nil, fmt.Errorf("getting namespace: %w", err)
	}

	if window, err := annotations.ParseDisruptionWindow(ns.Labels); err != nil {
		logger.Error(err, "Invalid disruption window labels on Namespace")
		return nil, err
	} else if window != nil {
		logger.V(1).Info("Using Namespace-level disruption window",
			"schedule", ns.Labels[annotations.DisruptionScheduleAnnotation],
			"duration", ns.Labels[annotations.DisruptionDurationAnnotation])
		return window, nil
	}

	// Priority 3: Global configuration
	if r.Config != nil && r.Config.Operator.DisruptionWindow.Schedule != "" {
		window, err := annotations.ParseDisruptionWindow(map[string]string{
			annotations.DisruptionScheduleAnnotation: r.Config.Operator.DisruptionWindow.Schedule,
			annotations.DisruptionDurationAnnotation: r.Config.Operator.DisruptionWindow.Duration,
		})
		if err != nil {
			// Should never happen - validated at startup
			return nil, fmt.Errorf("invalid global disruption window: %w", err)
		}
		logger.V(1).Info("Using global disruption window",
			"schedule", r.Config.Operator.DisruptionWindow.Schedule,
			"duration", r.Config.Operator.DisruptionWindow.Duration)
		return window, nil
	}

	// No window configured = always allowed
	logger.V(2).Info("No disruption window configured, disruptions always allowed")
	return nil, nil
}

// needsRebalancing checks if pod rebalancing is needed based on target distribution
func (r *DeploymentReconciler) needsRebalancing(state *apis.ReplicaState) bool {
	// Check if current distribution significantly differs from desired
	currentTotal := state.CurrentSpot + state.CurrentOnDemand
	if currentTotal == 0 {
		return false // No pods to rebalance
	}

	spotDiff := state.DesiredSpot - state.CurrentSpot
	onDemandDiff := state.DesiredOnDemand - state.CurrentOnDemand

	needsRebalancing := spotDiff != 0 || onDemandDiff != 0

	log.Log.Info("needsRebalancing check",
		"spotDiff", spotDiff,
		"onDemandDiff", onDemandDiff,
		"needsRebalancing", needsRebalancing,
		"currentTotal", currentTotal)

	return needsRebalancing
}

// performPodRebalancing deletes pods that are on wrong node types to achieve target distribution
func (r *DeploymentReconciler) performPodRebalancing(ctx context.Context, deployment *appsv1.Deployment, desiredState *apis.ReplicaState, deploymentKey string) error {
	// Get deployment pods
	spotPods, onDemandPods, err := r.categorizeDeploymentPods(ctx, deployment)
	if err != nil {
		return err
	}

	// Determine which pods need to be deleted for rebalancing
	podsToDelete := r.selectPodsForDeletion(spotPods, onDemandPods, desiredState)

	// Execute gradual rebalancing (one pod at a time)
	return r.executeDeploymentRebalancing(ctx, podsToDelete, deploymentKey)
}

// categorizeDeploymentPods retrieves and categorizes all pods for a deployment by node type
func (r *DeploymentReconciler) categorizeDeploymentPods(ctx context.Context, deployment *appsv1.Deployment) ([]corev1.Pod, []corev1.Pod, error) {
	// Get all pods for this deployment
	podList := &corev1.PodList{}
	selector, err := deploymentLabelSelector(deployment)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace: deployment.Namespace,
	}, selector); err != nil {
		return nil, nil, fmt.Errorf("failed to list pods: %w", err)
	}

	filteredPods := make([]*corev1.Pod, 0, len(podList.Items))
	nodeNameSet := make(map[string]struct{})
	// Categorize pods by current node type
	var spotPods, onDemandPods []corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil {
			continue // Skip pending or terminating pods
		}

		nodeNameSet[pod.Spec.NodeName] = struct{}{}
		filteredPods = append(filteredPods, pod)
	}

	nodeNames := make([]string, 0, len(nodeNameSet))
	for name := range nodeNameSet {
		nodeNames = append(nodeNames, name)
	}

	classifications, err := r.NodeClassifier.ClassifyNodesByName(ctx, nodeNames)
	if err != nil {
		log.FromContext(ctx).WithValues(
			"deployment", deployment.Name,
			"namespace", deployment.Namespace,
		).Error(err, "Failed to classify nodes while categorizing pods")
		classifications = map[string]apis.NodeType{}
	}

	for _, pod := range filteredPods {
		nodeType := classifications[pod.Spec.NodeName]
		switch nodeType {
		case apis.NodeTypeSpot:
			spotPods = append(spotPods, *pod)
		case apis.NodeTypeOnDemand, apis.NodeTypeUnknown:
			// Unknown node type - treat as on-demand for safety
			onDemandPods = append(onDemandPods, *pod)
		}
	}

	return spotPods, onDemandPods, nil
}

// selectPodsForDeletion identifies excess pods that should be deleted for rebalancing
func (r *DeploymentReconciler) selectPodsForDeletion(spotPods, onDemandPods []corev1.Pod, desiredState *apis.ReplicaState) []corev1.Pod {
	var podsToDelete []corev1.Pod

	// If we have too many spot pods, delete the excess
	spotPodsLen := len(spotPods)
	if spotPodsLen > 0 && spotPodsLen <= int(^uint32(0)) { // Check for reasonable bounds
		spotPodsCount := int32(spotPodsLen) // #nosec G115 - Bounds checked above
		if spotPodsCount > desiredState.DesiredSpot {
			excess := spotPodsCount - desiredState.DesiredSpot
			for i := int32(0); i < excess && i < spotPodsCount; i++ {
				podsToDelete = append(podsToDelete, spotPods[i])
			}
		}
	}

	// If we have too many on-demand pods, delete the excess
	onDemandPodsLen := len(onDemandPods)
	if onDemandPodsLen > 0 && onDemandPodsLen <= int(^uint32(0)) { // Check for reasonable bounds
		onDemandPodsCount := int32(onDemandPodsLen) // #nosec G115 - Bounds checked above
		if onDemandPodsCount > desiredState.DesiredOnDemand {
			excess := onDemandPodsCount - desiredState.DesiredOnDemand
			for i := int32(0); i < excess && i < onDemandPodsCount; i++ {
				podsToDelete = append(podsToDelete, onDemandPods[i])
			}
		}
	}

	return podsToDelete
}

// executeDeploymentRebalancing performs gradual pod deletion (one at a time)
func (r *DeploymentReconciler) executeDeploymentRebalancing(ctx context.Context, podsToDelete []corev1.Pod, deploymentKey string) error {
	if len(podsToDelete) == 0 {
		return nil
	}

	logger := log.FromContext(ctx)

	// Only delete the first pod to avoid overwhelming the system
	pod := podsToDelete[0]
	logger.Info("Deleting pod for rebalancing",
		"pod", pod.Name,
		"node", pod.Spec.NodeName,
		"remaining", len(podsToDelete)-1)

	if err := r.Delete(ctx, &pod); err != nil {
		logger.Error(err, "Failed to delete pod", "pod", pod.Name)
		return err
	}

	// Record the deletion time for cooldown tracking
	r.lastDeletionTimes.Store(deploymentKey, time.Now())

	logger.V(1).Info("Pod deleted successfully, will continue in next reconcile", "pod", pod.Name)
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}

// SetupWithManagerNamed sets up the controller with the Manager using a custom name
func (r *DeploymentReconciler) SetupWithManagerNamed(mgr ctrl.Manager, name string) error {
	skipNameValidation := true
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Named(name).
		WithOptions(controller.Options{
			SkipNameValidation: &skipNameValidation,
		}).
		Complete(r)
}

// deploymentLabelSelector returns the label selector for a deployment
func deploymentLabelSelector(deployment *appsv1.Deployment) (client.MatchingLabels, error) {
	if deployment.Spec.Selector == nil {
		return nil, fmt.Errorf("deployment has no selector")
	}

	return client.MatchingLabels(deployment.Spec.Selector.MatchLabels), nil
}

// GetReconcileCount returns the total number of reconciliations performed
func (r *DeploymentReconciler) GetReconcileCount() int64 {
	return r.reconcileCount.Load()
}

// GetErrorCount returns the total number of reconciliation errors
func (r *DeploymentReconciler) GetErrorCount() int64 {
	return r.errorCount.Load()
}

// setLastError records a controller error with timestamp and context
func (r *DeploymentReconciler) setLastError(err *ControllerError) {
	r.lastErrorLock.Lock()
	defer r.lastErrorLock.Unlock()
	r.lastError = err
}

// GetLastError returns the most recent controller error, if any
func (r *DeploymentReconciler) GetLastError() *ControllerError {
	r.lastErrorLock.RLock()
	defer r.lastErrorLock.RUnlock()
	return r.lastError
}

// markRecovered marks the last error as recovered if one existed
func (r *DeploymentReconciler) markRecovered() {
	r.lastErrorLock.Lock()
	defer r.lastErrorLock.Unlock()
	if r.lastError != nil && !r.lastError.Recovered {
		r.lastError.Recovered = true
	}
}

func (r *DeploymentReconciler) getCooldownPeriod() time.Duration {
	if r.CooldownPeriod > 0 {
		return r.CooldownPeriod
	}
	return defaultWorkloadTimingConfig().CooldownPeriod
}

func (r *DeploymentReconciler) getDisruptionRetryInterval() time.Duration {
	if r.DisruptionRetryInterval > 0 {
		return r.DisruptionRetryInterval
	}
	return defaultWorkloadTimingConfig().DisruptionRetryInterval
}

func (r *DeploymentReconciler) getDisruptionWindowPollInterval() time.Duration {
	if r.DisruptionWindowPollInterval > 0 {
		return r.DisruptionWindowPollInterval
	}
	return defaultWorkloadTimingConfig().DisruptionWindowPollInterval
}
