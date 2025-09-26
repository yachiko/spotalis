/*
Copyright 2024 The Spotalis Authors.// DeploymentReconciler reconciles Deployment objects for spot/on-demand	// Check leader election status - DISABLED for	if lastDeletionInterface, exists := r.lastPodDeletions.Load(deploymentKey); exists {
		lastDeletion, ok := lastDeletionInterface.(time.Time)
		if !ok {
			logger.Error(nil, "Invalid type for last deletion time", "deploymentKey", deploymentKe		nodeType := r.nodeClassifier.ClassifyPod(pod)
		switch nodeType {
		case apis.NodeTypeSpot:
			spotPods = append(spotPods, pod)
		case apis.NodeTypeOnDemand:
			onDemandPods = append(onDemandPods, pod)
		case apis.NodeTypeUnknown:
			// Unknown node type - treat as on-demand for safety
			onDemandPods = append(onDemandPods, pod)
			// Note: pod.Name in pod.Namespace has unknown node type, treating as on-demand
		}eturn fmt.Errorf("invalid type for last deletion time: %T", lastDeletionInterface)
		}ow to get basic functionality working
	// var isLeader bool
	// if r.LeaderElectionManager != nil {
	// 	isLeader = r.LeaderElectionManager.IsLeader()
	// 	logger.Info("Checking leader election status",
	// 		"leaderElectionManager", r.LeaderElectionManager,
	// 		"isLeader", isLeader)
	// } else {
	// 	logger.Info("No leader election manager configured")
	// 	isLeader = true // Default to leader if no leader election
	// }

	// if r.LeaderElectionManager != nil && !isLeader {
	// 	logger.Info("Not leader, skipping reconcile")
	// 	return ctrl.Result{}, nil
	// }

	logger.Info("Leader election temporarily disabled - proceeding with reconciliation")n management
type DeploymentReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	AnnotationParser    *annotations.AnnotationParser
	NodeClassifier      *config.NodeClassifierService
	ReconcileInterval   time.Duration
	MaxConcurrentRecons int
	LeaderElectionManager LeaderChecker // Interface for checking leader status
	MetricsCollector      MetricsRecorder // Interface for recording metrics

	// Track last pod deletion time per deployment to implement cooldown
	lastDeletionTimes sync.Map // map[string]time.Time
}

// LeaderChecker interface for checking if instance is leader
type LeaderChecker interface {
	IsLeader() bool
}

// MetricsRecorder interface for recording workload metrics
type MetricsRecorder interface {
	RecordWorkloadMetrics(namespace, workloadName, workloadType string, replicaState interface{})
}he Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
)

// DeploymentReconciler reconciles Deployment objects for spot/on-demand pod distribution management
type DeploymentReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	AnnotationParser    *annotations.AnnotationParser
	NodeClassifier      *config.NodeClassifierService
	NamespaceFilter     *NamespaceFilter // Filter for namespace-level permissions
	ReconcileInterval   time.Duration
	MaxConcurrentRecons int
	MetricsCollector    MetricsRecorder // Interface for recording metrics

	// Track last pod deletion time per deployment to implement cooldown
	lastDeletionTimes sync.Map // map[string]time.Time
}

// MetricsRecorder interface for recording workload metrics
type MetricsRecorder interface {
	RecordWorkloadMetrics(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState)
}

// NewDeploymentReconciler creates a new DeploymentReconciler
func NewDeploymentReconciler(client client.Client, scheme *runtime.Scheme) *DeploymentReconciler {
	return &DeploymentReconciler{
		Client:              client,
		Scheme:              scheme,
		AnnotationParser:    annotations.NewAnnotationParser(),
		ReconcileInterval:   5 * time.Minute,
		MaxConcurrentRecons: 10,
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
	logger := log.FromContext(ctx).WithValues("deployment", req.NamespacedName)
	logger.Info("Starting reconciliation for deployment", "namespace", req.Namespace, "name", req.Name)

	// Fetch the Deployment
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Deployment not found, ignoring")
			// Clean up tracking data for deleted deployment
			r.lastDeletionTimes.Delete(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}
	logger.Info("Successfully fetched deployment", "replicas", deployment.Spec.Replicas)

	// Check if Spotalis is explicitly enabled for this deployment
	if !r.AnnotationParser.IsSpotalisEnabled(&deployment) {
		logger.Info("Deployment does not have spotalis.io/enabled=true, skipping")
		return ctrl.Result{}, nil
	}
	logger.Info("Deployment has Spotalis enabled, checking namespace permissions")

	// Check if the namespace is allowed for Spotalis management
	if r.NamespaceFilter != nil {
		logger.Info("Checking namespace permissions with namespace filter", "namespace", deployment.Namespace)
		result, err := r.NamespaceFilter.IsNamespaceAllowed(ctx, deployment.Namespace)
		if err != nil {
			logger.Error(err, "Failed to check namespace permissions")
			return ctrl.Result{}, err
		}
		if !result.Allowed {
			logger.Info("Namespace is not allowed for Spotalis management", "reason", result.Reason, "rule", result.MatchedRule)
			return ctrl.Result{}, nil
		}
		logger.Info("Namespace is allowed for Spotalis management", "rule", result.MatchedRule)
	} else {
		logger.Info("No namespace filter configured, allowing all namespaces")
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
	deploymentKey := req.NamespacedName.String()
	if lastDeletionInterface, exists := r.lastDeletionTimes.Load(deploymentKey); exists {
		lastDeletion, ok := lastDeletionInterface.(time.Time)
		if !ok {
			logger.Error(nil, "Invalid type for last deletion time", "deploymentKey", deploymentKey)
			return ctrl.Result{}, fmt.Errorf("invalid type for last deletion time: %T", lastDeletionInterface)
		}
		cooldownPeriod := 5 * time.Second // Wait 10 seconds after deleting a pod
		timeSinceLastDeletion := time.Since(lastDeletion)

		if timeSinceLastDeletion < cooldownPeriod {
			remainingCooldown := cooldownPeriod - timeSinceLastDeletion
			logger.Info("In cooldown period after recent pod deletion, skipping rebalancing",
				"lastDeletion", lastDeletion,
				"timeSince", timeSinceLastDeletion,
				"remainingCooldown", remainingCooldown)
			return ctrl.Result{RequeueAfter: remainingCooldown}, nil
		}
	}

	// Check if deployment is stable and ready for rebalancing
	if !r.isDeploymentStableAndReady(ctx, &deployment) {
		logger.Info("Deployment is not stable or ready, skipping rebalancing",
			"replicas", deployment.Status.Replicas,
			"readyReplicas", deployment.Status.ReadyReplicas,
			"updatedReplicas", deployment.Status.UpdatedReplicas,
			"availableReplicas", deployment.Status.AvailableReplicas,
			"unavailableReplicas", deployment.Status.UnavailableReplicas)

		// Requeue with longer interval when deployment is not stable
		return ctrl.Result{RequeueAfter: 2 * r.ReconcileInterval}, nil
	}

	logger.Info("Reconciling Deployment with Spotalis annotations")

	// Parse the workload configuration from annotations
	workloadConfig, err := r.AnnotationParser.ParseWorkloadConfiguration(&deployment)
	if err != nil {
		logger.Error(err, "Failed to parse workload configuration")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
	}

	// Get the current replica state
	replicaState, err := r.calculateCurrentReplicaState(ctx, &deployment)
	if err != nil {
		logger.Error(err, "Failed to calculate current replica state")
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
	logger.Info("State:", "currentSpot", replicaState.CurrentSpot, "currentOnDemand", replicaState.CurrentOnDemand, "desiredSpot", replicaState.DesiredSpot, "desiredOnDemand", replicaState.DesiredOnDemand)

	needsRebalancing := r.needsRebalancing(replicaState)

	if needsRebalancing {
		logger.Info("Rebalancing deployment pods",
			"currentSpot", replicaState.CurrentSpot,
			"currentOnDemand", replicaState.CurrentOnDemand,
			"desiredSpot", replicaState.DesiredSpot,
			"desiredOnDemand", replicaState.DesiredOnDemand)

		if err := r.performPodRebalancing(ctx, &deployment, replicaState, deploymentKey); err != nil {
			logger.Error(err, "Failed to rebalance deployment pods")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

		// After performing rebalancing, requeue sooner to check the results
		logger.Info("Pod rebalancing initiated, will check status sooner")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval / 2}, nil
	} else {
		logger.Info("No pod rebalancing needed")
	}

	logger.Info("Deployment reconciliation completed successfully")

	// Use longer interval when no action is needed to reduce load
	return ctrl.Result{RequeueAfter: r.ReconcileInterval * 2}, nil
}

// isDeploymentStableAndReady checks if deployment is in a stable state for rebalancing
func (r *DeploymentReconciler) isDeploymentStableAndReady(ctx context.Context, deployment *appsv1.Deployment) bool {
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

	// Debug logging to understand stability check failures
	if !isStable {
		logger := log.FromContext(ctx)
		logger.Info("Deployment stability check details",
			"deployment", deployment.Name,
			"namespace", deployment.Namespace,
			"desiredReplicas", desiredReplicas,
			"readyReplicas", status.ReadyReplicas,
			"readyCheck", status.ReadyReplicas == desiredReplicas,
			"updatedReplicas", status.UpdatedReplicas,
			"updatedCheck", status.UpdatedReplicas == desiredReplicas,
			"availableReplicas", status.AvailableReplicas,
			"availableCheck", status.AvailableReplicas == desiredReplicas,
			"unavailableReplicas", status.UnavailableReplicas,
			"unavailableCheck", status.UnavailableReplicas == 0)
	}

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

	var spotReplicas, onDemandReplicas int32

	// Classify each pod based on its node
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Spec.NodeName == "" {
			continue // Skip pending pods
		}

		// Get the node for this pod
		var node corev1.Node
		if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
			continue // Skip if we can't get the node
		}

		// Classify the node
		nodeType := r.NodeClassifier.ClassifyNode(&node)
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

	// Add debug logging to help diagnose the issue
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
	logger := log.FromContext(ctx)

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

	// Categorize pods by current node type
	var spotPods, onDemandPods []corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil {
			continue // Skip pending or terminating pods
		}

		nodeType, err := r.getNodeTypeForDeploymentPod(ctx, pod, logger)
		if err != nil {
			continue // Skip pods with node lookup errors
		}

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

// getNodeTypeForDeploymentPod determines the node type for a given pod
func (r *DeploymentReconciler) getNodeTypeForDeploymentPod(ctx context.Context, pod *corev1.Pod, logger logr.Logger) (apis.NodeType, error) {
	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
		logger.Info("Could not get node for pod", "pod", pod.Name, "node", pod.Spec.NodeName)
		return apis.NodeTypeUnknown, err
	}

	return r.NodeClassifier.ClassifyNode(&node), nil
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
	logger.Info("Deleting one pod for rebalancing (gradual approach)", "pod", pod.Name, "node", pod.Spec.NodeName, "remaining", len(podsToDelete)-1)
	if err := r.Delete(ctx, &pod); err != nil {
		logger.Error(err, "Failed to delete pod for rebalancing", "pod", pod.Name)
		return err
	}

	// Record the deletion time for cooldown tracking
	r.lastDeletionTimes.Store(deploymentKey, time.Now())

	logger.Info("Deleted one pod for rebalancing - will continue with remaining pods in next reconcile", "deleted", pod.Name)
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
