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

// DeploymentReconciler reconciles Deployment objects for spot/on-demand replica management
type DeploymentReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	AnnotationParser    *annotations.AnnotationParser
	NodeClassifier      *config.NodeClassifierService
	ReconcileInterval   time.Duration
	MaxConcurrentRecons int
}

// NewDeploymentReconciler creates a new DeploymentReconciler
func NewDeploymentReconciler(client client.Client, scheme *runtime.Scheme) *DeploymentReconciler {
	return &DeploymentReconciler{
		Client:              client,
		Scheme:              scheme,
		AnnotationParser:    annotations.NewAnnotationParser(),
		ReconcileInterval:   30 * time.Second,
		MaxConcurrentRecons: 10,
	}
}

// SetNodeClassifier sets the node classifier service
func (r *DeploymentReconciler) SetNodeClassifier(classifier *config.NodeClassifierService) {
	r.NodeClassifier = classifier
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile handles the reconciliation of Deployment objects
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("deployment", req.NamespacedName)

	// Fetch the Deployment
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Deployment not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// Check if this deployment has Spotalis annotations
	if !r.AnnotationParser.HasSpotalisAnnotations(&deployment) {
		logger.V(1).Info("Deployment has no Spotalis annotations, skipping")
		return ctrl.Result{}, nil
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

	// Determine if reconciliation is needed
	action := replicaState.GetNextAction()

	switch action {
	case apis.ReplicaActionNone:
		logger.V(1).Info("No reconciliation action needed")
		return ctrl.Result{RequeueAfter: r.ReconcileInterval}, nil

	case apis.ReplicaActionScaleUpOnDemand, apis.ReplicaActionScaleUpSpot:
		logger.Info("Scaling up deployment",
			"currentSpot", replicaState.CurrentSpot,
			"currentOnDemand", replicaState.CurrentOnDemand,
			"desiredSpot", replicaState.DesiredSpot,
			"desiredOnDemand", replicaState.DesiredOnDemand)

		if err := r.performScaleUp(ctx, &deployment, replicaState); err != nil {
			logger.Error(err, "Failed to scale up deployment")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

	case apis.ReplicaActionScaleDownOnDemand, apis.ReplicaActionScaleDownSpot:
		logger.Info("Scaling down deployment",
			"currentSpot", replicaState.CurrentSpot,
			"currentOnDemand", replicaState.CurrentOnDemand,
			"desiredSpot", replicaState.DesiredSpot,
			"desiredOnDemand", replicaState.DesiredOnDemand)

		if err := r.performScaleDown(ctx, &deployment, replicaState); err != nil {
			logger.Error(err, "Failed to scale down deployment")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}

	case apis.ReplicaActionMigrateToSpot, apis.ReplicaActionMigrateToOnDemand:
		logger.Info("Rebalancing deployment",
			"currentSpot", replicaState.CurrentSpot,
			"currentOnDemand", replicaState.CurrentOnDemand,
			"desiredSpot", replicaState.DesiredSpot,
			"desiredOnDemand", replicaState.DesiredOnDemand)

		if err := r.performRebalance(ctx, &deployment, replicaState); err != nil {
			logger.Error(err, "Failed to rebalance deployment")
			return ctrl.Result{RequeueAfter: r.ReconcileInterval}, err
		}
	}

	logger.Info("Deployment reconciliation completed successfully")
	return ctrl.Result{RequeueAfter: r.ReconcileInterval}, nil
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

// performScaleUp handles scaling up the deployment with proper spot/on-demand distribution
func (r *DeploymentReconciler) performScaleUp(ctx context.Context, deployment *appsv1.Deployment, desiredState *apis.ReplicaState) error {
	// For deployments, we rely on the scheduler to place pods on appropriate nodes
	// We update the replica count and let node affinity/tolerations do the work

	newReplicaCount := desiredState.DesiredSpot + desiredState.DesiredOnDemand

	deployment.Spec.Replicas = &newReplicaCount

	if err := r.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to update deployment replicas: %w", err)
	}

	return nil
}

// performScaleDown handles scaling down the deployment while maintaining distribution
func (r *DeploymentReconciler) performScaleDown(ctx context.Context, deployment *appsv1.Deployment, desiredState *apis.ReplicaState) error {
	// Calculate how many replicas to remove and from which node types
	currentTotal := desiredState.CurrentSpot + desiredState.CurrentOnDemand
	desiredTotal := desiredState.DesiredSpot + desiredState.DesiredOnDemand

	if currentTotal <= desiredTotal {
		return nil // Nothing to scale down
	}

	// For deployments, we can only control total replica count
	// The deployment controller will handle which pods to remove
	deployment.Spec.Replicas = &desiredTotal

	if err := r.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to update deployment replicas: %w", err)
	}

	return nil
}

// performRebalance handles rebalancing pods between spot and on-demand nodes
func (r *DeploymentReconciler) performRebalance(ctx context.Context, deployment *appsv1.Deployment, desiredState *apis.ReplicaState) error {
	// For deployments, rebalancing is achieved by triggering a rolling update
	// This can be done by updating an annotation to force pod recreation

	logger := log.FromContext(ctx)
	logger.Info("Triggering rolling update for rebalancing")

	// Add or update a rebalance annotation to trigger rolling update
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}

	deployment.Spec.Template.Annotations["spotalis.io/rebalance-timestamp"] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to trigger rebalancing rolling update: %w", err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}

// deploymentLabelSelector returns the label selector for a deployment
func deploymentLabelSelector(deployment *appsv1.Deployment) (client.MatchingLabels, error) {
	if deployment.Spec.Selector == nil {
		return nil, fmt.Errorf("deployment has no selector")
	}

	return client.MatchingLabels(deployment.Spec.Selector.MatchLabels), nil
}
