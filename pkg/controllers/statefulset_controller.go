/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License") you may not use this file except in compliance with the License.
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yachiko/spotalis/internal/annotations"
	"github.com/yachiko/spotalis/internal/config"
	"github.com/yachiko/spotalis/pkg/apis"
	pkgconfig "github.com/yachiko/spotalis/pkg/config"
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

// StatefulSetReconciler reconciles StatefulSet objects for spot/on-demand pod distribution management
type StatefulSetReconciler struct {
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

	// Track last pod deletion time per statefulset to implement cooldown
	lastDeletionTimes sync.Map // map[string]time.Time

	// Metrics tracking
	reconcileCount atomic.Int64
	errorCount     atomic.Int64

	// Error tracking
	lastError     *ControllerError
	lastErrorLock sync.RWMutex
}

// NewStatefulSetReconciler creates a new StatefulSetReconciler
func NewStatefulSetReconciler(client client.Client, scheme *runtime.Scheme) *StatefulSetReconciler {
	defaults := defaultWorkloadTimingConfig()
	return &StatefulSetReconciler{
		Client:                       client,
		Scheme:                       scheme,
		AnnotationParser:             annotations.NewAnnotationParser(),
		ReconcileInterval:            5 * time.Minute, // Increased from 30s - pod rebalancing is less time-sensitive
		MaxConcurrentRecons:          10,
		CooldownPeriod:               defaults.CooldownPeriod,
		DisruptionRetryInterval:      defaults.DisruptionRetryInterval,
		DisruptionWindowPollInterval: defaults.DisruptionWindowPollInterval,
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
	// Increment reconcile counter
	r.reconcileCount.Add(1)

	// Create structured logger for this reconciliation
	logger := NewControllerLogger(ctx, "statefulset-controller", req, "StatefulSet")
	logger.V(1).Info("Starting reconciliation")

	// Fetch the StatefulSet
	var statefulSet appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &statefulSet); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("StatefulSet not found, cleaning up")
			// Clean up tracking data for deleted statefulset
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
		logger.ReconcileFailed(err, "Failed to get StatefulSet")
		return ctrl.Result{}, err
	}

	// Add workload context to logger
	var replicas int32
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}
	logger = logger.WithWorkload("statefulset", replicas)

	// Check if Spotalis is explicitly enabled for this StatefulSet
	if !r.AnnotationParser.IsSpotalisEnabled(&statefulSet) {
		logger.V(1).Info("Spotalis not enabled, skipping")
		return ctrl.Result{}, nil
	}

	// Check if the namespace is allowed for Spotalis management
	if r.NamespaceFilter != nil {
		logger.V(1).Info("Checking namespace permissions")
		result, err := r.NamespaceFilter.IsNamespaceAllowed(ctx, statefulSet.Namespace)
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

	// Implement cooldown period after pod deletion to avoid constant rescheduling
	statefulsetKey := req.String()
	if lastDeletionInterface, exists := r.lastDeletionTimes.Load(statefulsetKey); exists {
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

	// Check if StatefulSet is stable and ready for rebalancing
	if !r.isStatefulSetStableAndReady(&statefulSet) {
		logger.V(1).Info("StatefulSet not stable, skipping rebalancing",
			"replicas", statefulSet.Status.Replicas,
			"ready", statefulSet.Status.ReadyReplicas,
			"current", statefulSet.Status.CurrentReplicas,
			"updated", statefulSet.Status.UpdatedReplicas)

		// Requeue with longer interval when StatefulSet is not stable
		return ctrl.Result{RequeueAfter: 2 * r.ReconcileInterval}, nil
	}

	logger.V(1).Info("Processing Spotalis-enabled StatefulSet")

	// Parse the workload configuration from annotations
	workloadConfig, err := r.AnnotationParser.ParseWorkloadConfiguration(&statefulSet)
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
	replicaState, err := r.calculateCurrentReplicaState(ctx, &statefulSet)
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
	desiredReplicas := statefulSet.Status.Replicas
	if statefulSet.Spec.Replicas != nil {
		desiredReplicas = *statefulSet.Spec.Replicas
	}

	replicaState.TotalReplicas = desiredReplicas
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
		disruptionWindow, err := r.resolveDisruptionWindow(ctx, &statefulSet)
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

		logger.Info("Rebalancing StatefulSet pods",
			"current_spot", replicaState.CurrentSpot,
			"current_on_demand", replicaState.CurrentOnDemand,
			"target_spot", replicaState.DesiredSpot,
			"target_on_demand", replicaState.DesiredOnDemand)

		// Track rebalancing metrics
		var rebalanceErr error

		if err := r.performPodRebalancing(ctx, &statefulSet, replicaState, statefulsetKey); err != nil {
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
				"statefulset",
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
	logger.V(1).Info("No rebalancing needed")

	// Mark as recovered since reconcile completed successfully
	r.markRecovered()

	logger.V(1).Info("Reconciliation completed successfully")

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
			"statefulset", statefulSet.Name,
			"namespace", statefulSet.Namespace,
		).Error(err, "Failed to classify nodes for StatefulSet pods")
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
			// Unknown node type - treat as on-demand for safety
			onDemandReplicas++
			// Note: pod.Name node.Name has unknown type, treating as on-demand
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

// resolveDisruptionWindow resolves the disruption window from the configuration hierarchy:
// 1. Workload-level annotations (highest priority)
// 2. Namespace-level annotations
// 3. Global configuration
// 4. No window (always allowed)
func (r *StatefulSetReconciler) resolveDisruptionWindow(
	ctx context.Context,
	statefulSet *appsv1.StatefulSet,
) (*annotations.DisruptionWindow, error) {
	logger := log.FromContext(ctx).WithValues(
		"statefulset", statefulSet.Name,
		"namespace", statefulSet.Namespace,
	)

	// Priority 1: Workload-level annotations
	if window, err := annotations.ParseDisruptionWindow(statefulSet.Annotations); err != nil {
		logger.Error(err, "Invalid disruption window annotations on StatefulSet")
		return nil, err
	} else if window != nil {
		logger.V(1).Info("Using StatefulSet-level disruption window",
			"schedule", statefulSet.Annotations[annotations.DisruptionScheduleAnnotation],
			"duration", statefulSet.Annotations[annotations.DisruptionDurationAnnotation])
		return window, nil
	}

	// Priority 2: Namespace-level annotations (using labels)
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: statefulSet.Namespace}, ns); err != nil {
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
	logger.V(1).Info("No disruption window configured, disruptions always allowed")
	return nil, nil
}

// performPodRebalancing deletes pods that are on wrong node types to achieve target distribution
func (r *StatefulSetReconciler) performPodRebalancing(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState, statefulsetKey string) error {
	// Get pods categorized by node type
	spotPods, onDemandPods, err := r.categorizePodsByNodeType(ctx, statefulSet)
	if err != nil {
		return err
	}

	// Determine which pods need to be deleted for rebalancing
	podsToDelete := r.selectPodsForDeletion(spotPods, onDemandPods, desiredState)

	// Perform gradual pod deletion and updates
	return r.executeGradualRebalancing(ctx, statefulSet, desiredState, statefulsetKey, podsToDelete)
}

// categorizePodsByNodeType gets all pods for a StatefulSet and categorizes them by node type
func (r *StatefulSetReconciler) categorizePodsByNodeType(ctx context.Context, statefulSet *appsv1.StatefulSet) (spotPods, onDemandPods []corev1.Pod, err error) {
	// Get all pods for this StatefulSet
	podList := &corev1.PodList{}
	selector, err := statefulSetLabelSelector(statefulSet)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get selector: %w", err)
	}

	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace: statefulSet.Namespace,
	}, selector); err != nil {
		return nil, nil, fmt.Errorf("failed to list pods: %w", err)
	}

	filteredPods := make([]*corev1.Pod, 0, len(podList.Items))
	nodeNameSet := make(map[string]struct{})
	// Categorize pods by current node type
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
			"statefulset", statefulSet.Name,
			"namespace", statefulSet.Namespace,
		).Error(err, "Failed to classify nodes while categorizing StatefulSet pods")
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

// selectPodsForDeletion determines which pods should be deleted based on desired state
func (r *StatefulSetReconciler) selectPodsForDeletion(spotPods, onDemandPods []corev1.Pod, desiredState *apis.ReplicaState) []corev1.Pod {
	var podsToDelete []corev1.Pod

	// Bounds check before int32 conversion to prevent overflow
	spotPodsLen := len(spotPods)
	if spotPodsLen > int(^uint32(0)) {
		spotPodsLen = int(^uint32(0)) // Cap at reasonable maximum
	}
	spotPodsCount := int32(spotPodsLen) // #nosec G115 - Bounds checked above

	// If we have too many spot pods, delete the excess
	if spotPodsCount > desiredState.DesiredSpot {
		excess := spotPodsCount - desiredState.DesiredSpot
		for i := int32(0); i < excess && i < spotPodsCount; i++ {
			podsToDelete = append(podsToDelete, spotPods[i])
		}
	}

	// Bounds check for on-demand pods before int32 conversion
	onDemandPodsLen := len(onDemandPods)
	if onDemandPodsLen > int(^uint32(0)) {
		onDemandPodsLen = int(^uint32(0)) // Cap at reasonable maximum
	}
	onDemandPodsCount := int32(onDemandPodsLen) // #nosec G115 - Bounds checked above

	// If we have too many on-demand pods, delete the excess
	if onDemandPodsCount > desiredState.DesiredOnDemand {
		excess := onDemandPodsCount - desiredState.DesiredOnDemand
		for i := int32(0); i < excess && i < onDemandPodsCount; i++ {
			podsToDelete = append(podsToDelete, onDemandPods[i])
		}
	}

	return podsToDelete
}

// executeGradualRebalancing performs gradual pod deletion and StatefulSet updates
func (r *StatefulSetReconciler) executeGradualRebalancing(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState, statefulsetKey string, podsToDelete []corev1.Pod) error {
	if len(podsToDelete) == 0 {
		return nil
	}

	logger := log.FromContext(ctx)

	// Determine pod management policy
	isOrdered := isOrderedReadyPolicy(statefulSet)

	// Sort pods by ordinal before deletion
	// For OrderedReady: delete highest ordinals first (descending)
	// For Parallel: still sort for consistency (descending)
	podsToDelete = sortPodsByOrdinal(podsToDelete, statefulSet.Name, true)
	logger.V(1).Info("Sorted pods for deletion by ordinal",
		"count", len(podsToDelete),
		"isOrderedReady", isOrdered)

	// Only delete the first pod (highest ordinal) to avoid overwhelming the system
	pod := podsToDelete[0]
	ordinal, _ := extractPodOrdinal(pod.Name, statefulSet.Name)

	// Pre-check PDB status before attempting eviction
	pdbStatus, err := CheckPDBStatus(ctx, r.Client, &pod)
	if err != nil {
		logger.Error(err, "Failed to check PDB status", "pod", pod.Name)
		// Continue anyway - eviction API will enforce PDB
	} else if !pdbStatus.CanDisrupt {
		// PDB blocks eviction - log and skip
		logger.Info("Rebalancing blocked by PDB",
			"pod", pod.Name,
			"pdb", pdbStatus.PDBName,
			"disruptions_allowed", pdbStatus.DisruptionsAllowed,
			"current_healthy", pdbStatus.CurrentHealthy,
			"desired_healthy", pdbStatus.DesiredHealthy,
			"reason", pdbStatus.BlockReason)

		// Record metric if collector is available
		if r.MetricsCollector != nil {
			r.MetricsCollector.RecordPDBBlock(pod.Namespace, statefulSet.Name, "statefulset", pdbStatus.PDBName)
		}

		// Return nil - not an error, just can't proceed now
		// Controller will requeue and try again later
		return nil
	}

	// Log PDB info if present
	if pdbStatus != nil && pdbStatus.HasPDB {
		logger.V(1).Info("PDB allows eviction",
			"pod", pod.Name,
			"pdb", pdbStatus.PDBName,
			"disruptions_allowed", pdbStatus.DisruptionsAllowed)
	}

	logger.Info("Evicting pod for rebalancing",
		"pod", pod.Name,
		"ordinal", ordinal,
		"node", pod.Spec.NodeName,
		"remaining", len(podsToDelete)-1)

	// Use Eviction API to respect PodDisruptionBudgets
	result, err := EvictPod(ctx, r.Client, &pod)
	if err != nil {
		if result == EvictionResultPDBBlocked {
			// Log PDB blocking but don't treat as error - will retry next reconcile
			logger.Info("Pod eviction blocked by PodDisruptionBudget, will retry later",
				"pod", pod.Name,
				"node", pod.Spec.NodeName)
			// Return nil to avoid exponential backoff - PDB blocking is expected
			return nil
		}
		logger.Error(err, "Failed to evict pod", "pod", pod.Name)
		return err
	}

	if result == EvictionResultAlreadyGone {
		logger.V(1).Info("Pod already deleted, continuing", "pod", pod.Name)
	} else {
		logger.V(1).Info("Pod evicted successfully", "pod", pod.Name, "result", result)
	}

	// Record the deletion time for cooldown tracking
	r.lastDeletionTimes.Store(statefulsetKey, time.Now())
	logger.V(1).Info("Pod deleted successfully, will continue in next reconcile", "pod", pod.Name)

	// Update pod template and trigger rolling update
	return r.updateStatefulSetForRebalancing(ctx, statefulSet, desiredState)
}

// updateStatefulSetForRebalancing updates the StatefulSet template and triggers rolling update
func (r *StatefulSetReconciler) updateStatefulSetForRebalancing(ctx context.Context, statefulSet *appsv1.StatefulSet, desiredState *apis.ReplicaState) error {
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

// extractPodOrdinal extracts the ordinal from a StatefulSet pod name
// Pod names are formatted as: <statefulset-name>-<ordinal>
func extractPodOrdinal(podName string, statefulSetName string) (int, error) {
	// Validate prefix
	prefix := statefulSetName + "-"
	if !strings.HasPrefix(podName, prefix) {
		return -1, fmt.Errorf("pod name %q does not match StatefulSet %q pattern", podName, statefulSetName)
	}

	ordinalStr := podName[len(prefix):]
	ordinal, err := strconv.Atoi(ordinalStr)
	if err != nil {
		return -1, fmt.Errorf("failed to parse ordinal from pod %q: %w", podName, err)
	}

	return ordinal, nil
}

// podWithOrdinal pairs a pod with its parsed ordinal for sorting
type podWithOrdinal struct {
	pod     corev1.Pod
	ordinal int
}

// sortPodsByOrdinal sorts pods by their ordinal
// For OrderedReady: highest ordinal first (descending)
// For Parallel: original order (but we still sort for consistency)
func sortPodsByOrdinal(pods []corev1.Pod, statefulSetName string, descending bool) []corev1.Pod {
	if len(pods) == 0 {
		return pods
	}

	podsWithOrdinals := make([]podWithOrdinal, 0, len(pods))

	for i := range pods {
		pod := pods[i]
		ordinal, err := extractPodOrdinal(pod.Name, statefulSetName)
		if err != nil {
			// If we can't parse ordinal, put at end with ordinal -1
			ordinal = -1
		}
		podsWithOrdinals = append(podsWithOrdinals, podWithOrdinal{
			pod:     pod,
			ordinal: ordinal,
		})
	}

	if descending {
		// Highest ordinal first
		sort.Slice(podsWithOrdinals, func(i, j int) bool {
			return podsWithOrdinals[i].ordinal > podsWithOrdinals[j].ordinal
		})
	} else {
		// Lowest ordinal first
		sort.Slice(podsWithOrdinals, func(i, j int) bool {
			return podsWithOrdinals[i].ordinal < podsWithOrdinals[j].ordinal
		})
	}

	result := make([]corev1.Pod, len(podsWithOrdinals))
	for i, pwo := range podsWithOrdinals {
		result[i] = pwo.pod
	}
	return result
}

// getPodManagementPolicy returns the pod management policy, defaulting to OrderedReady
func getPodManagementPolicy(sts *appsv1.StatefulSet) appsv1.PodManagementPolicyType {
	if sts.Spec.PodManagementPolicy == "" {
		return appsv1.OrderedReadyPodManagement
	}
	return sts.Spec.PodManagementPolicy
}

// isOrderedReadyPolicy returns true if StatefulSet uses OrderedReady policy
func isOrderedReadyPolicy(sts *appsv1.StatefulSet) bool {
	policy := getPodManagementPolicy(sts)
	return policy == appsv1.OrderedReadyPodManagement
}

// GetReconcileCount returns the total number of reconciliations performed
func (r *StatefulSetReconciler) GetReconcileCount() int64 {
	return r.reconcileCount.Load()
}

// GetErrorCount returns the total number of reconciliation errors
func (r *StatefulSetReconciler) GetErrorCount() int64 {
	return r.errorCount.Load()
}

// setLastError records a controller error with timestamp and context
func (r *StatefulSetReconciler) setLastError(err *ControllerError) {
	r.lastErrorLock.Lock()
	defer r.lastErrorLock.Unlock()
	r.lastError = err
}

// GetLastError returns the most recent controller error, if any
func (r *StatefulSetReconciler) GetLastError() *ControllerError {
	r.lastErrorLock.RLock()
	defer r.lastErrorLock.RUnlock()
	return r.lastError
}

// markRecovered marks the last error as recovered if one existed
func (r *StatefulSetReconciler) markRecovered() {
	r.lastErrorLock.Lock()
	defer r.lastErrorLock.Unlock()
	if r.lastError != nil && !r.lastError.Recovered {
		r.lastError.Recovered = true
	}
}

func (r *StatefulSetReconciler) getCooldownPeriod() time.Duration {
	if r.CooldownPeriod > 0 {
		return r.CooldownPeriod
	}
	return defaultWorkloadTimingConfig().CooldownPeriod
}

func (r *StatefulSetReconciler) getDisruptionRetryInterval() time.Duration {
	if r.DisruptionRetryInterval > 0 {
		return r.DisruptionRetryInterval
	}
	return defaultWorkloadTimingConfig().DisruptionRetryInterval
}

func (r *StatefulSetReconciler) getDisruptionWindowPollInterval() time.Duration {
	if r.DisruptionWindowPollInterval > 0 {
		return r.DisruptionWindowPollInterval
	}
	return defaultWorkloadTimingConfig().DisruptionWindowPollInterval
}
