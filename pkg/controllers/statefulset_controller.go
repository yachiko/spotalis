/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
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
	"strconv"
	"sync"
	"time"

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

// StatefulSetReconciler reconciles StatefulSet objects for spot/on-demand pod distribution management
type StatefulSetReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	AnnotationParser    *annotations.AnnotationParser
	NodeClassifier      *config.NodeClassifierService
	ReconcileInterval   time.Duration
	MaxConcurrentRecons int
	MetricsCollector    MetricsRecorder // Interface for recording metrics

	// Track last pod deletion time per statefulset to implement cooldown
	lastDeletionTimes sync.Map // map[string]time.Time
}

// NewStatefulSetReconciler creates a new StatefulSetReconciler
func NewStatefulSetReconciler(client client.Client, scheme *runtime.Scheme) *StatefulSetReconciler {
	return &StatefulSetReconciler{
		Client:              client,
		Scheme:              scheme,
		AnnotationParser:    annotations.NewAnnotationParser(),
		ReconcileInterval:   5 * time.Minute, // Increased from 30s - pod rebalancing is less time-sensitive
		MaxConcurrentRecons: 10,
	}
}

// SetNodeClassifier sets the node classifier service
func (r *StatefulSetReconciler) SetNodeClassifier(classifier *config.NodeClassifierService) {
	r.NodeClassifier = classifier
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile handles the reconciliation of StatefulSet objects
func (r *StatefulSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("statefulset", req.NamespacedName)

	// With controller-runtime's built-in leader election, only the leader will receive reconcile events
	// so we don't need manual leader election checks here

	// Fetch the StatefulSet
	var statefulSet appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &statefulSet); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("StatefulSet not found, ignoring")
			// Clean up tracking data for deleted statefulset
			r.lastDeletionTimes.Delete(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get StatefulSet")
		return ctrl.Result{}, err
	}

	// Check if this StatefulSet has Spotalis annotations
	if !r.AnnotationParser.HasSpotalisAnnotations(&statefulSet) {
		logger.Info("StatefulSet has no Spotalis annotations, skipping")
		return ctrl.Result{}, nil
	}

	// Implement cooldown period after pod deletion to avoid constant rescheduling
	statefulsetKey := req.NamespacedName.String()
	if lastDeletionInterface, exists := r.lastDeletionTimes.Load(statefulsetKey); exists {
		lastDeletion := lastDeletionInterface.(time.Time)
		cooldownPeriod := 3 * time.Minute // Wait 3 minutes after deleting a pod
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

	// Check if StatefulSet is stable and ready for rebalancing
	if !r.isStatefulSetStableAndReady(&statefulSet) {
		logger.Info("StatefulSet is not stable or ready, skipping rebalancing",
			"replicas", statefulSet.Status.Replicas,
			"readyReplicas", statefulSet.Status.ReadyReplicas,
			"currentReplicas", statefulSet.Status.CurrentReplicas,
			"updatedReplicas", statefulSet.Status.UpdatedReplicas)

		// Requeue with longer interval when StatefulSet is not stable
		return ctrl.Result{RequeueAfter: 2 * r.ReconcileInterval}, nil
	}

	logger.Info("Reconciling StatefulSet with Spotalis annotations")

	// Parse the workload configuration from annotations
	workloadConfig, err := r.AnnotationParser.ParseWorkloadConfiguration(&statefulSet)
	if err != nil {
		logger.Error(err, "Failed to parse workload configuration")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
	}

	// Get the current replica state
	replicaState, err := r.calculateCurrentReplicaState(ctx, &statefulSet)
	if err != nil {
		logger.Error(err, "Failed to calculate current replica state")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
	}

	// Calculate desired distribution based on configuration
	desiredReplicas := statefulSet.Status.Replicas
	if statefulSet.Spec.Replicas != nil {
		desiredReplicas = *statefulSet.Spec.Replicas
	}

	replicaState.TotalReplicas = desiredReplicas
	replicaState.CalculateDesiredDistribution(*workloadConfig)

	// Check if rebalancing is needed by comparing current vs desired state
	needsRebalancing := r.needsRebalancing(replicaState)

	if needsRebalancing {
		logger.Info("Rebalancing StatefulSet pods",
			"currentSpot", replicaState.CurrentSpot,
			"currentOnDemand", replicaState.CurrentOnDemand,
			"desiredSpot", replicaState.DesiredSpot,
			"desiredOnDemand", replicaState.DesiredOnDemand)

		if err := r.performPodRebalancing(ctx, &statefulSet, replicaState, statefulsetKey); err != nil {
			logger.Error(err, "Failed to rebalance StatefulSet pods")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

		// After performing rebalancing, requeue sooner to check the results
		logger.Info("Pod rebalancing initiated, will check status sooner")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval / 2}, nil
	} else {
		logger.Info("No pod rebalancing needed")
	}

	logger.Info("StatefulSet reconciliation completed successfully")

	// Use longer interval when no action is needed to reduce load
	return ctrl.Result{RequeueAfter: r.ReconcileInterval * 2}, nil
}

// calculateCurrentReplicaState analyzes the current state of StatefulSet replicas
func (r *StatefulSetReconciler) calculateCurrentReplicaState(ctx context.Context, statefulSet *appsv1.StatefulSet) (*apis.ReplicaState, error) {
	// Get all pods for this StatefulSet
	podList := &corev1.PodList{}
	selector, err := statefulSetLabelSelector(statefulSet)
	if err != nil {
		return nil, fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace: statefulSet.Namespace,
	}, selector); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var spotReplicas, onDemandReplicas int32

	// Classify each pod based on its node
	for _, pod := range podList.Items {
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
		}
	}

	return &apis.ReplicaState{
		WorkloadRef: corev1.ObjectReference{
			APIVersion: statefulSet.APIVersion,
			Kind:       statefulSet.Kind,
			Name:       statefulSet.Name,
			Namespace:  statefulSet.Namespace,
			UID:        statefulSet.UID,
		},
		CurrentSpot:     spotReplicas,
		CurrentOnDemand: onDemandReplicas,
		LastReconciled:  time.Now(),
	}, nil
}

// needsRebalancing checks if pod rebalancing is needed based on target distribution
func (r *StatefulSetReconciler) needsRebalancing(state *apis.ReplicaState) bool {
	// Check if current distribution significantly differs from desired
	currentTotal := state.CurrentSpot + state.CurrentOnDemand
	if currentTotal == 0 {
		return false // No pods to rebalance
	}

	// Calculate tolerance (allow some variance to avoid constant rebalancing)
	tolerance := int32(1)
	if currentTotal > 10 {
		tolerance = currentTotal / 10 // 10% tolerance for larger StatefulSets
	}

	spotDiff := state.DesiredSpot - state.CurrentSpot
	onDemandDiff := state.DesiredOnDemand - state.CurrentOnDemand

	// Need rebalancing if difference exceeds tolerance
	return (spotDiff > tolerance || spotDiff < -tolerance) ||
		(onDemandDiff > tolerance || onDemandDiff < -tolerance)
}

// performPodRebalancing deletes pods that are on wrong node types to achieve target distribution
func (r *StatefulSetReconciler) performPodRebalancing(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState, statefulsetKey string) error {
	logger := log.FromContext(ctx)

	// Get all pods for this StatefulSet
	podList := &corev1.PodList{}
	selector, err := statefulSetLabelSelector(statefulSet)
	if err != nil {
		return fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace: statefulSet.Namespace,
	}, selector); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Categorize pods by current node type
	var spotPods, onDemandPods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil {
			continue // Skip pending or terminating pods
		}

		// Get the node for this pod
		var node corev1.Node
		if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
			logger.Info("Could not get node for pod", "pod", pod.Name, "node", pod.Spec.NodeName)
			continue
		}

		// Classify the node
		nodeType := r.NodeClassifier.ClassifyNode(&node)
		switch nodeType {
		case apis.NodeTypeSpot:
			spotPods = append(spotPods, pod)
		case apis.NodeTypeOnDemand:
			onDemandPods = append(onDemandPods, pod)
		}
	}

	// Delete excess pods from over-represented node types
	var podsToDelete []corev1.Pod

	// If we have too many spot pods, delete the excess
	if int32(len(spotPods)) > desiredState.DesiredSpot {
		excess := int32(len(spotPods)) - desiredState.DesiredSpot
		for i := int32(0); i < excess && i < int32(len(spotPods)); i++ {
			podsToDelete = append(podsToDelete, spotPods[i])
		}
	}

	// If we have too many on-demand pods, delete the excess
	if int32(len(onDemandPods)) > desiredState.DesiredOnDemand {
		excess := int32(len(onDemandPods)) - desiredState.DesiredOnDemand
		for i := int32(0); i < excess && i < int32(len(onDemandPods)); i++ {
			podsToDelete = append(podsToDelete, onDemandPods[i])
		}
	}

	// Delete the selected pods - but only one at a time to avoid chaos
	if len(podsToDelete) > 0 {
		// Only delete the first pod to avoid overwhelming the system
		pod := podsToDelete[0]
		logger.Info("Deleting one pod for rebalancing (gradual approach)", "pod", pod.Name, "node", pod.Spec.NodeName, "remaining", len(podsToDelete)-1)
		if err := r.Delete(ctx, &pod); err != nil {
			logger.Error(err, "Failed to delete pod for rebalancing", "pod", pod.Name)
			return err
		}

		// Record the deletion time for cooldown tracking
		r.lastDeletionTimes.Store(statefulsetKey, time.Now())

		logger.Info("Deleted one pod for rebalancing - will continue with remaining pods in next reconcile", "deleted", pod.Name)

		// Update pod template to ensure proper placement for new pods
		if err := r.updatePodTemplateForNodePlacement(statefulSet, desiredState); err != nil {
			return fmt.Errorf("failed to update pod template: %w", err)
		}

		// Trigger a rolling update annotation
		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = make(map[string]string)
		}
		statefulSet.Spec.Template.Annotations["spotalis.io/rebalance-timestamp"] = time.Now().Format(time.RFC3339)

		if err := r.Update(ctx, statefulSet); err != nil {
			return fmt.Errorf("failed to update StatefulSet for rebalancing: %w", err)
		}
	}

	return nil
}

// updatePodTemplateForNodePlacement updates pod template with appropriate node placement
func (r *StatefulSetReconciler) updatePodTemplateForNodePlacement(statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
	template := &statefulSet.Spec.Template

	// Add spot instance tolerations if needed
	if desiredState.DesiredSpot > 0 {
		if template.Spec.Tolerations == nil {
			template.Spec.Tolerations = make([]corev1.Toleration, 0)
		}

		// Add common spot instance tolerations
		spotTolerations := []corev1.Toleration{
			{
				Key:      "spot",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			},
			{
				Key:      "aws.amazon.com/spot",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			},
		}

		// Check if tolerations already exist before adding
		existingKeys := make(map[string]bool)
		for _, toleration := range template.Spec.Tolerations {
			existingKeys[toleration.Key] = true
		}

		for _, toleration := range spotTolerations {
			if !existingKeys[toleration.Key] {
				template.Spec.Tolerations = append(template.Spec.Tolerations, toleration)
				existingKeys[toleration.Key] = true
			}
		}
	}

	// Add scheduling annotations for the desired distribution
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}

	template.Annotations["spotalis.io/desired-spot-replicas"] = strconv.Itoa(int(desiredState.DesiredSpot))
	template.Annotations["spotalis.io/desired-ondemand-replicas"] = strconv.Itoa(int(desiredState.DesiredOnDemand))

	return nil
}

// isStatefulSetStableAndReady checks if StatefulSet is in a stable state for rebalancing
func (r *StatefulSetReconciler) isStatefulSetStableAndReady(statefulSet *appsv1.StatefulSet) bool {
	// Don't rebalance if StatefulSet doesn't have desired replica count set
	if statefulSet.Spec.Replicas == nil {
		return false
	}

	desiredReplicas := *statefulSet.Spec.Replicas

	// Don't rebalance if there are no desired replicas
	if desiredReplicas == 0 {
		return false
	}

	status := statefulSet.Status

	// Check if StatefulSet is fully ready:
	// 1. All replicas are ready
	// 2. All replicas are current (no ongoing rollout)
	// 3. Updated replicas match desired (rollout complete)
	// StatefulSet doesn't have UnavailableReplicas or AvailableReplicas like Deployment
	isStable := status.ReadyReplicas == desiredReplicas &&
		status.CurrentReplicas == desiredReplicas &&
		status.UpdatedReplicas == desiredReplicas

	return isStable
}

// SetupWithManager sets up the controller with the Manager
func (r *StatefulSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		Complete(r)
}

// SetupWithManagerNamed sets up the controller with the Manager using a custom name
func (r *StatefulSetReconciler) SetupWithManagerNamed(mgr ctrl.Manager, name string) error {
	skipNameValidation := true
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		Named(name).
		WithOptions(controller.Options{
			SkipNameValidation: &skipNameValidation,
		}).
		Complete(r)
}

// statefulSetLabelSelector returns the label selector for a StatefulSet
func statefulSetLabelSelector(statefulSet *appsv1.StatefulSet) (client.MatchingLabels, error) {
	if statefulSet.Spec.Selector == nil {
		return nil, fmt.Errorf("StatefulSet has no selector")
	}

	return client.MatchingLabels(statefulSet.Spec.Selector.MatchLabels), nil
}
