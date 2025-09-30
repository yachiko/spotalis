// Package controllers implements Kubernetes controllers for managing workload
// replica distribution across spot and on-demand instances.
package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
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
	// Fetch the Deployment
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).WithValues("deployment", req.NamespacedName).Info("Deployment not found, ignoring")
			// Clean up tracking data for deleted deployment
			r.lastDeletionTimes.Delete(req.String())
			return ctrl.Result{}, nil
		}
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName).Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// Add workload context to all logs
	var replicas int32
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}

	log.FromContext(ctx).WithValues(
		"deployment", req.NamespacedName,
		"replicas", replicas,
	).V(1).Info("Successfully fetched deployment")

	// Check if Spotalis is explicitly enabled for this deployment
	if !r.AnnotationParser.IsSpotalisEnabled(&deployment) {
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("Spotalis not enabled for deployment, skipping")
		return ctrl.Result{}, nil
	}
	log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("Spotalis enabled for deployment")

	// Check if the namespace is allowed for Spotalis management
	if r.NamespaceFilter != nil {
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName, "namespace", deployment.Namespace).V(1).Info("Checking namespace permissions with namespace filter")
		result, err := r.NamespaceFilter.IsNamespaceAllowed(ctx, deployment.Namespace)
		if err != nil {
			log.FromContext(ctx).WithValues("deployment", req.NamespacedName).Error(err, "Failed to check namespace permissions")
			return ctrl.Result{}, err
		}
		if !result.Allowed {
			log.FromContext(ctx).WithValues("deployment", req.NamespacedName, "reason", result.Reason, "rule", result.MatchedRule).V(1).Info("Namespace is not allowed for Spotalis management")
			return ctrl.Result{}, nil
		}
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName, "rule", result.MatchedRule).V(1).Info("Namespace is allowed for Spotalis management")
	} else {
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("No namespace filter configured, allowing all namespaces")
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
			log.FromContext(ctx).WithValues("deployment", req.NamespacedName, "deploymentKey", deploymentKey).Error(nil, "Invalid type for last deletion time")
			return ctrl.Result{}, fmt.Errorf("invalid type for last deletion time: %T", lastDeletionInterface)
		}
		cooldownPeriod := 5 * time.Second // Wait 10 seconds after deleting a pod
		timeSinceLastDeletion := time.Since(lastDeletion)

		if timeSinceLastDeletion < cooldownPeriod {
			remainingCooldown := cooldownPeriod - timeSinceLastDeletion
			log.FromContext(ctx).WithValues(
				"deployment", req.NamespacedName,
				"lastDeletion", lastDeletion,
				"timeSince", timeSinceLastDeletion,
				"remainingCooldown", remainingCooldown,
			).V(1).Info("In cooldown period after recent pod deletion, skipping rebalancing")
			return ctrl.Result{RequeueAfter: remainingCooldown}, nil
		}
	}

	// Check if deployment is stable and ready for rebalancing
	if !r.isDeploymentStableAndReady(ctx, &deployment) {
		log.FromContext(ctx).WithValues(
			"deployment", req.NamespacedName,
			"replicas", deployment.Status.Replicas,
			"readyReplicas", deployment.Status.ReadyReplicas,
			"updatedReplicas", deployment.Status.UpdatedReplicas,
			"availableReplicas", deployment.Status.AvailableReplicas,
			"unavailableReplicas", deployment.Status.UnavailableReplicas,
		).V(1).Info("Deployment is not stable or ready, skipping rebalancing")

		// Requeue with longer interval when deployment is not stable
		return ctrl.Result{RequeueAfter: 2 * r.ReconcileInterval}, nil
	}

	log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("Reconciling Deployment with Spotalis annotations")

	// Parse the workload configuration from annotations
	workloadConfig, err := r.AnnotationParser.ParseWorkloadConfiguration(&deployment)
	if err != nil {
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName).Error(err, "Failed to parse workload configuration")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
	}

	// Get the current replica state
	replicaState, err := r.calculateCurrentReplicaState(ctx, &deployment)
	if err != nil {
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName).Error(err, "Failed to calculate current replica state")
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
	log.FromContext(ctx).WithValues(
		"deployment", req.NamespacedName,
		"currentSpot", replicaState.CurrentSpot,
		"currentOnDemand", replicaState.CurrentOnDemand,
		"desiredSpot", replicaState.DesiredSpot,
		"desiredOnDemand", replicaState.DesiredOnDemand,
	).V(1).Info("Current replica distribution state")

	needsRebalancing := r.needsRebalancing(replicaState)

	if needsRebalancing {
		log.FromContext(ctx).WithValues(
			"deployment", req.NamespacedName,
			"currentSpot", replicaState.CurrentSpot,
			"currentOnDemand", replicaState.CurrentOnDemand,
			"desiredSpot", replicaState.DesiredSpot,
			"desiredOnDemand", replicaState.DesiredOnDemand,
		).Info("Rebalancing deployment pods")

		if err := r.performPodRebalancing(ctx, &deployment, replicaState, deploymentKey); err != nil {
			log.FromContext(ctx).WithValues("deployment", req.NamespacedName).Error(err, "Failed to rebalance deployment pods")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

		// After performing rebalancing, requeue sooner to check the results
		log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("Pod rebalancing initiated, will check status sooner")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval / 2}, nil
	}
	log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("No pod rebalancing needed")

	log.FromContext(ctx).WithValues("deployment", req.NamespacedName).V(1).Info("Deployment reconciliation completed successfully")

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

		nodeType, err := r.getNodeTypeForDeploymentPod(ctx, pod)
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
func (r *DeploymentReconciler) getNodeTypeForDeploymentPod(ctx context.Context, pod *corev1.Pod) (apis.NodeType, error) {
	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
		log.FromContext(ctx).WithValues("pod", pod.Name, "node", pod.Spec.NodeName).V(1).Info("Could not get node for pod")
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

	// Only delete the first pod to avoid overwhelming the system
	pod := podsToDelete[0]
	log.FromContext(ctx).WithValues(
		"pod", pod.Name,
		"node", pod.Spec.NodeName,
		"remaining", len(podsToDelete)-1,
	).V(1).Info("Deleting one pod for rebalancing (gradual approach)")

	if err := r.Delete(ctx, &pod); err != nil {
		log.FromContext(ctx).WithValues("pod", pod.Name).Error(err, "Failed to delete pod for rebalancing")
		return err
	}

	// Record the deletion time for cooldown tracking
	r.lastDeletionTimes.Store(deploymentKey, time.Now())

	log.FromContext(ctx).WithValues("deleted", pod.Name).V(1).Info("Deleted one pod for rebalancing - will continue with remaining pods in next reconcile")
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
