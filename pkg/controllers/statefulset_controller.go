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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
)

// StatefulSetReconciler reconciles StatefulSet objects for spot/on-demand replica management
type StatefulSetReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	AnnotationParser    *annotations.AnnotationParser
	NodeClassifier      *config.NodeClassifierService
	ReconcileInterval   time.Duration
	MaxConcurrentRecons int
}

// NewStatefulSetReconciler creates a new StatefulSetReconciler
func NewStatefulSetReconciler(client client.Client, scheme *runtime.Scheme) *StatefulSetReconciler {
	return &StatefulSetReconciler{
		Client:              client,
		Scheme:              scheme,
		AnnotationParser:    annotations.NewAnnotationParser(),
		ReconcileInterval:   30 * time.Second,
		MaxConcurrentRecons: 10,
	}
}

// SetNodeClassifier sets the node classifier service
func (r *StatefulSetReconciler) SetNodeClassifier(classifier *config.NodeClassifierService) {
	r.NodeClassifier = classifier
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile handles the reconciliation of StatefulSet objects
func (r *StatefulSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("statefulset", req.NamespacedName)

	// Fetch the StatefulSet
	var statefulSet appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &statefulSet); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("StatefulSet not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get StatefulSet")
		return ctrl.Result{}, err
	}

	// Check if this StatefulSet has Spotalis annotations
	if !r.AnnotationParser.HasSpotalisAnnotations(&statefulSet) {
		logger.V(1).Info("StatefulSet has no Spotalis annotations, skipping")
		return ctrl.Result{}, nil
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

	desiredState := replicaState.CalculateDesiredDistribution(workloadConfig, desiredReplicas)

	// Determine if reconciliation is needed
	action := replicaState.GetNextAction(desiredState)

	switch action {
	case apis.ReplicaActionNone:
		logger.V(1).Info("No reconciliation action needed")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, nil

	case apis.ReplicaActionScaleUp:
		logger.Info("Scaling up StatefulSet",
			"currentSpot", replicaState.CurrentSpotReplicas,
			"currentOnDemand", replicaState.CurrentOnDemandReplicas,
			"desiredSpot", desiredState.DesiredSpotReplicas,
			"desiredOnDemand", desiredState.DesiredOnDemandReplicas)

		if err := r.performScaleUp(ctx, &statefulSet, desiredState); err != nil {
			logger.Error(err, "Failed to scale up StatefulSet")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

	case apis.ReplicaActionScaleDown:
		logger.Info("Scaling down StatefulSet",
			"currentSpot", replicaState.CurrentSpotReplicas,
			"currentOnDemand", replicaState.CurrentOnDemandReplicas,
			"desiredSpot", desiredState.DesiredSpotReplicas,
			"desiredOnDemand", desiredState.DesiredOnDemandReplicas)

		if err := r.performScaleDown(ctx, &statefulSet, desiredState); err != nil {
			logger.Error(err, "Failed to scale down StatefulSet")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

	case apis.ReplicaActionRebalance:
		logger.Info("Rebalancing StatefulSet",
			"currentSpot", replicaState.CurrentSpotReplicas,
			"currentOnDemand", replicaState.CurrentOnDemandReplicas,
			"desiredSpot", desiredState.DesiredSpotReplicas,
			"desiredOnDemand", desiredState.DesiredOnDemandReplicas)

		if err := r.performRebalance(ctx, &statefulSet, desiredState); err != nil {
			logger.Error(err, "Failed to rebalance StatefulSet")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}
	}

	logger.Info("StatefulSet reconciliation completed successfully")
	return ctrl.Result{RequeueAfter: r.ReconcileInterval}, nil
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
		Namespace:     statefulSet.Namespace,
		LabelSelector: selector,
	}); err != nil {
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
		CurrentSpotReplicas:     spotReplicas,
		CurrentOnDemandReplicas: onDemandReplicas,
		LastUpdated:             time.Now(),
	}, nil
}

// performScaleUp handles scaling up the StatefulSet with proper spot/on-demand distribution
func (r *StatefulSetReconciler) performScaleUp(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
	// For StatefulSets, we can control replica placement more precisely
	// by updating individual pod specs as they are created

	newReplicaCount := desiredState.DesiredSpotReplicas + desiredState.DesiredOnDemandReplicas

	// Update the replica count
	statefulSet.Spec.Replicas = &newReplicaCount

	// Ensure proper node affinity and tolerations are set for spot instances
	if err := r.updatePodTemplateForNodePlacement(statefulSet, desiredState); err != nil {
		return fmt.Errorf("failed to update pod template: %w", err)
	}

	if err := r.Update(ctx, statefulSet); err != nil {
		return fmt.Errorf("failed to update StatefulSet replicas: %w", err)
	}

	return nil
}

// performScaleDown handles scaling down the StatefulSet while maintaining distribution
func (r *StatefulSetReconciler) performScaleDown(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
	currentTotal := desiredState.CurrentSpotReplicas + desiredState.CurrentOnDemandReplicas
	desiredTotal := desiredState.DesiredSpotReplicas + desiredState.DesiredOnDemandReplicas

	if currentTotal <= desiredTotal {
		return nil // Nothing to scale down
	}

	logger := log.FromContext(ctx)

	// For StatefulSets, we might want to delete specific pods rather than just reducing replica count
	// This allows us to control which type of instances are removed first
	if err := r.deleteExcessPods(ctx, statefulSet, desiredState); err != nil {
		logger.Error(err, "Failed to delete excess pods, falling back to replica count update")
	}

	// Update the replica count
	statefulSet.Spec.Replicas = &desiredTotal

	if err := r.Update(ctx, statefulSet); err != nil {
		return fmt.Errorf("failed to update StatefulSet replicas: %w", err)
	}

	return nil
}

// performRebalance handles rebalancing pods between spot and on-demand nodes
func (r *StatefulSetReconciler) performRebalance(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
	logger := log.FromContext(ctx)

	// For StatefulSets, we can be more surgical about rebalancing
	// by deleting specific pods that are on the "wrong" node type

	// Get current pods and their node assignments
	podList := &corev1.PodList{}
	selector, err := statefulSetLabelSelector(statefulSet)
	if err != nil {
		return fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace:     statefulSet.Namespace,
		LabelSelector: selector,
	}); err != nil {
		return fmt.Errorf("failed to list pods for rebalancing: %w", err)
	}

	// Identify pods that need to be moved
	var podsToDelete []corev1.Pod

	for _, pod := range podList.Items {
		if pod.Spec.NodeName == "" {
			continue // Skip pending pods
		}

		// Get the node for this pod
		var node corev1.Node
		if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
			continue // Skip if we can't get the node
		}

		nodeType := r.NodeClassifier.ClassifyNode(&node)

		// Determine if this pod should be moved based on desired distribution
		shouldMove := r.shouldMovePod(&pod, nodeType, desiredState)
		if shouldMove {
			podsToDelete = append(podsToDelete, pod)
		}
	}

	// Delete pods that need to be rebalanced (StatefulSet controller will recreate them)
	for _, pod := range podsToDelete {
		logger.Info("Deleting pod for rebalancing", "pod", pod.Name, "node", pod.Spec.NodeName)
		if err := r.Delete(ctx, &pod); err != nil {
			logger.Error(err, "Failed to delete pod for rebalancing", "pod", pod.Name)
		}
	}

	// Update pod template to ensure proper placement for new pods
	if err := r.updatePodTemplateForNodePlacement(statefulSet, desiredState); err != nil {
		return fmt.Errorf("failed to update pod template: %w", err)
	}

	// Trigger a rolling update if needed
	if len(podsToDelete) > 0 {
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

// deleteExcessPods deletes specific pods to achieve desired distribution
func (r *StatefulSetReconciler) deleteExcessPods(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
	// Get current pods and prioritize which ones to delete
	podList := &corev1.PodList{}
	selector, err := statefulSetLabelSelector(statefulSet)
	if err != nil {
		return fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace:     statefulSet.Namespace,
		LabelSelector: selector,
	}); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	currentSpot := desiredState.CurrentSpotReplicas
	currentOnDemand := desiredState.CurrentOnDemandReplicas
	desiredSpot := desiredState.DesiredSpotReplicas
	desiredOnDemand := desiredState.DesiredOnDemandReplicas

	excessSpot := currentSpot - desiredSpot
	excessOnDemand := currentOnDemand - desiredOnDemand

	logger := log.FromContext(ctx)

	// Delete excess spot pods first (if any)
	if excessSpot > 0 {
		deleted := r.deletePodsByNodeType(ctx, podList.Items, apis.NodeTypeSpot, int(excessSpot))
		logger.Info("Deleted excess spot pods", "count", deleted)
	}

	// Delete excess on-demand pods (if any)
	if excessOnDemand > 0 {
		deleted := r.deletePodsByNodeType(ctx, podList.Items, apis.NodeTypeOnDemand, int(excessOnDemand))
		logger.Info("Deleted excess on-demand pods", "count", deleted)
	}

	return nil
}

// deletePodsByNodeType deletes a specific number of pods running on the specified node type
func (r *StatefulSetReconciler) deletePodsByNodeType(ctx context.Context, pods []corev1.Pod, nodeType apis.NodeType, count int) int {
	deleted := 0

	for _, pod := range pods {
		if deleted >= count {
			break
		}

		if pod.Spec.NodeName == "" {
			continue
		}

		// Get the node for this pod
		var node corev1.Node
		if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
			continue
		}

		if r.NodeClassifier.ClassifyNode(&node) == nodeType {
			if err := r.Delete(ctx, &pod); err == nil {
				deleted++
			}
		}
	}

	return deleted
}

// shouldMovePod determines if a pod should be moved to achieve desired distribution
func (r *StatefulSetReconciler) shouldMovePod(pod *corev1.Pod, currentNodeType apis.NodeType, desiredState *apis.ReplicaState) bool {
	// Simple heuristic: if we have too many of this type, consider moving some

	currentSpot := desiredState.CurrentSpotReplicas
	currentOnDemand := desiredState.CurrentOnDemandReplicas
	desiredSpot := desiredState.DesiredSpotReplicas
	desiredOnDemand := desiredState.DesiredOnDemandReplicas

	if currentNodeType == apis.NodeTypeSpot && currentSpot > desiredSpot {
		return true
	}

	if currentNodeType == apis.NodeTypeOnDemand && currentOnDemand > desiredOnDemand {
		return true
	}

	return false
}

// updatePodTemplateForNodePlacement updates pod template with appropriate node placement
func (r *StatefulSetReconciler) updatePodTemplateForNodePlacement(statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
	template := &statefulSet.Spec.Template

	// Add spot instance tolerations if needed
	if desiredState.DesiredSpotReplicas > 0 {
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

		template.Spec.Tolerations = append(template.Spec.Tolerations, spotTolerations...)
	}

	// Add scheduling annotations for the desired distribution
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}

	template.Annotations["spotalis.io/desired-spot-replicas"] = strconv.Itoa(int(desiredState.DesiredSpotReplicas))
	template.Annotations["spotalis.io/desired-ondemand-replicas"] = strconv.Itoa(int(desiredState.DesiredOnDemandReplicas))

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *StatefulSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		Complete(r)
}

// statefulSetLabelSelector returns the label selector for a StatefulSet
func statefulSetLabelSelector(statefulSet *appsv1.StatefulSet) (client.MatchingLabels, error) {
	if statefulSet.Spec.Selector == nil {
		return nil, fmt.Errorf("StatefulSet has no selector")
	}

	return client.MatchingLabels(statefulSet.Spec.Selector.MatchLabels), nil
}
