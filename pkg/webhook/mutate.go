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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yachiko/spotalis/internal/annotations"
	"github.com/yachiko/spotalis/internal/config"
	"github.com/yachiko/spotalis/pkg/apis"
	pkgconfig "github.com/yachiko/spotalis/pkg/config"
	"github.com/yachiko/spotalis/pkg/controllers"
	"github.com/yachiko/spotalis/pkg/metrics"
	"github.com/yachiko/spotalis/pkg/observation"
	"gomodules.xyz/jsonpatch/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// Workload types
	workloadTypeReplicaSet  = "ReplicaSet"
	workloadTypeStatefulSet = "StatefulSet"
	workloadTypeDeployment  = "Deployment"

	// Capacity types
	capacityTypeOnDemand = "on-demand"
	capacityTypeSpot     = "spot"
)

// MutationHandler handles admission webhook requests for pod and workload mutation
type MutationHandler struct {
	Client               client.Client
	AnnotationParser     *annotations.AnnotationParser
	NodeClassifier       *config.NodeClassifierService
	NodeClassifierConfig *pkgconfig.NodeClassifierConfig
	MetricsCollector     *metrics.Collector
	AdmissionTracker     *AdmissionStateTracker
	Observer             *observation.Service
	decoder              admission.Decoder
}

// NewMutationHandler creates a new mutation handler
func NewMutationHandler(client client.Client, scheme *runtime.Scheme) *MutationHandler {
	return &MutationHandler{
		Client:           client,
		AnnotationParser: annotations.NewAnnotationParser(),
		decoder:          admission.NewDecoder(scheme),
	}
}

// SetNodeClassifier sets the node classifier service
func (m *MutationHandler) SetNodeClassifier(classifier *config.NodeClassifierService) {
	m.NodeClassifier = classifier
}

// SetMetricsCollector sets the metrics collector for recording webhook metrics
func (m *MutationHandler) SetMetricsCollector(collector *metrics.Collector) {
	m.MetricsCollector = collector
}

// SetNodeClassifierConfig sets the node classifier configuration for label extraction
func (m *MutationHandler) SetNodeClassifierConfig(cfg *pkgconfig.NodeClassifierConfig) {
	m.NodeClassifierConfig = cfg
}

// Handle processes admission webhook requests
func (m *MutationHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Create structured logger for this webhook request
	logger := controllers.NewWebhookLogger(ctx, req)
	logger.V(1).Info("Processing admission request")

	var response admission.Response
	switch req.Kind.Kind {
	case "Pod":
		response = m.mutatePod(ctx, req, logger)
	default:
		logger.V(1).Info("Unsupported resource kind, allowing")
		response = admission.Allowed("unsupported resource kind")
	}

	// Record webhook metrics
	if m.MetricsCollector != nil {
		result := "allowed"
		if !response.Allowed {
			result = "denied"
		}
		m.MetricsCollector.RecordWebhookRequest(string(req.Operation), req.Kind.Kind, result)
	}

	return response
}

// mutatePod handles pod mutation for node affinity and tolerations
func (m *MutationHandler) mutatePod(ctx context.Context, req admission.Request, logger *controllers.WebhookLogger) admission.Response {
	var pod corev1.Pod
	if err := m.decoder.Decode(req, &pod); err != nil {
		logger.Error(err, "Failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if this pod belongs to a workload with Spotalis annotations
	workloadConfig, workloadKind, workloadName, err := m.getWorkloadConfigForPod(ctx, &pod)
	if err != nil {
		logger.Error(err, "Failed to get workload configuration")
		return admission.Allowed("failed to get workload config")
	}

	if workloadConfig == nil {
		logger.V(1).Info("Pod not managed by Spotalis")
		return admission.Allowed("not managed by Spotalis")
	}

	// Add workload and config context to logger
	logger = logger.WithWorkload(workloadKind, workloadName).
		WithConfig(int(workloadConfig.SpotPercentage), workloadConfig.MinOnDemand)

	logger.V(1).Info("Mutating pod for Spotalis workload")

	// Apply mutations based on workload configuration
	patches, mutationTypes := m.generatePodPatches(ctx, &pod, workloadConfig)

	if len(patches) == 0 {
		logger.MutationSkipped("no mutations needed")
		return admission.Allowed("no mutations needed")
	}

	logger.MutationApplied("Applied pod mutations", len(patches), mutationTypes)

	// Record mutation metrics
	if m.MetricsCollector != nil {
		for _, mutationType := range mutationTypes {
			m.MetricsCollector.RecordWebhookMutation("Pod", mutationType)
		}
	}

	// Convert patches to JSON patch operations
	var jsonPatches []jsonpatch.Operation
	for _, patch := range patches {
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			logger.Error(err, "Failed to marshal patch")
			return admission.Errored(http.StatusInternalServerError, err)
		}

		var operation jsonpatch.Operation
		if err := json.Unmarshal(patchBytes, &operation); err != nil {
			logger.Error(err, "Failed to unmarshal patch operation")
			return admission.Errored(http.StatusInternalServerError, err)
		}
		jsonPatches = append(jsonPatches, operation)
	}

	return admission.Patched("applied Spotalis mutations", jsonPatches...)
}

// getWorkloadConfigForPod retrieves workload configuration for a pod
func (m *MutationHandler) getWorkloadConfigForPod(ctx context.Context, pod *corev1.Pod) (*apis.WorkloadConfiguration, string, string, error) {
	// Check if pod has owner references to a Deployment or StatefulSet
	for _, ownerRef := range pod.OwnerReferences {
		switch ownerRef.Kind {
		case workloadTypeReplicaSet:
			// For deployments, we need to get the ReplicaSet's owner (Deployment)
			if config, kind, name, err := m.getConfigFromReplicaSet(ctx, pod.Namespace, ownerRef.Name); err == nil && config != nil {
				return config, kind, name, nil
			}
		case workloadTypeStatefulSet:
			if config, err := m.getConfigFromStatefulSet(ctx, pod.Namespace, ownerRef.Name); err == nil && config != nil {
				return config, workloadTypeStatefulSet, ownerRef.Name, nil
			}
		case workloadTypeDeployment:
			if config, err := m.getConfigFromDeployment(ctx, pod.Namespace, ownerRef.Name); err == nil && config != nil {
				return config, workloadTypeDeployment, ownerRef.Name, nil
			}
		}
	}

	return nil, "", "", nil
}

// getConfigFromReplicaSet gets configuration from a ReplicaSet's owner Deployment
func (m *MutationHandler) getConfigFromReplicaSet(ctx context.Context, namespace, name string) (*apis.WorkloadConfiguration, string, string, error) {
	var rs appsv1.ReplicaSet
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &rs); err != nil {
		return nil, "", "", err
	}

	// Get the Deployment that owns this ReplicaSet
	for _, ownerRef := range rs.OwnerReferences {
		if ownerRef.Kind == workloadTypeDeployment {
			config, err := m.getConfigFromDeployment(ctx, namespace, ownerRef.Name)
			if err != nil {
				return nil, "", "", err
			}
			return config, workloadTypeDeployment, ownerRef.Name, nil
		}
	}

	return nil, "", "", nil
}

// getConfigFromDeployment gets configuration from a Deployment
func (m *MutationHandler) getConfigFromDeployment(ctx context.Context, namespace, name string) (*apis.WorkloadConfiguration, error) {
	var deployment appsv1.Deployment
	return m.getConfigFromWorkload(ctx, namespace, name, &deployment)
}

// getConfigFromStatefulSet gets configuration from a StatefulSet
func (m *MutationHandler) getConfigFromStatefulSet(ctx context.Context, namespace, name string) (*apis.WorkloadConfiguration, error) {
	var statefulSet appsv1.StatefulSet
	return m.getConfigFromWorkload(ctx, namespace, name, &statefulSet)
}

// getConfigFromWorkload is a generic helper to get configuration from any workload type
func (m *MutationHandler) getConfigFromWorkload(ctx context.Context, namespace, name string, obj client.Object) (*apis.WorkloadConfiguration, error) {
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj); err != nil {
		return nil, err
	}

	if !m.AnnotationParser.HasSpotalisAnnotations(obj) {
		return nil, nil
	}

	return m.AnnotationParser.ParseWorkloadConfiguration(obj)
}

// generatePodPatches generates JSON patches for pod mutation
func (m *MutationHandler) generatePodPatches(ctx context.Context, pod *corev1.Pod, config *apis.WorkloadConfiguration) ([]map[string]interface{}, []string) {
	var patches []map[string]interface{}
	var mutationTypes []string

	// Always add/override nodeSelector to ensure correct capacity type
	// This handles both spot and on-demand scenarios
	nodeSelectorPatches := m.generateNodeSelectorPatches(ctx, pod, config)
	patches = append(patches, nodeSelectorPatches...)
	if len(nodeSelectorPatches) > 0 {
		mutationTypes = append(mutationTypes, "nodeSelector")
	}

	return patches, mutationTypes
}

// jsonPointerEscape escapes a string for use in JSON Pointer according to RFC 6901
// Order is critical: ~ must be escaped before / to avoid double-escaping
func jsonPointerEscape(s string) string {
	// Replace ~ with ~0 first (to avoid double-escaping)
	s = strings.ReplaceAll(s, "~", "~0")
	// Then replace / with ~1
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

// jsonPointerUnescape reverses the JSON Pointer escaping
// Order is critical: ~1 must be unescaped before ~0 to avoid incorrect results
func jsonPointerUnescape(s string) string {
	// Replace ~1 with / first
	s = strings.ReplaceAll(s, "~1", "/")
	// Then replace ~0 with ~
	s = strings.ReplaceAll(s, "~0", "~")
	return s
}

// getCapacityTypeLabelConfig extracts the label key and values for spot/on-demand
// from the NodeClassifierConfig. Falls back to Karpenter defaults if not configured.
func (m *MutationHandler) getCapacityTypeLabelConfig() (labelKey, spotValue, onDemandValue string) {
	// Defaults (Karpenter)
	labelKey = apis.CapacityTypeLabel
	spotValue = capacityTypeSpot
	onDemandValue = capacityTypeOnDemand

	if m.NodeClassifierConfig == nil {
		return labelKey, spotValue, onDemandValue
	}

	// Extract spot label config
	if len(m.NodeClassifierConfig.SpotLabels) > 0 {
		selector := m.NodeClassifierConfig.SpotLabels[0]
		if len(selector.MatchLabels) > 0 {
			// Use first key-value pair found
			for k, v := range selector.MatchLabels {
				labelKey = k
				spotValue = v
				break
			}
		}
	}

	// Extract on-demand value (same key expected)
	if len(m.NodeClassifierConfig.OnDemandLabels) > 0 {
		selector := m.NodeClassifierConfig.OnDemandLabels[0]
		if v, exists := selector.MatchLabels[labelKey]; exists {
			onDemandValue = v
		}
	}

	return labelKey, spotValue, onDemandValue
}

// generateNodeSelectorPatches generates patches for node selector based on current pod distribution
func (m *MutationHandler) generateNodeSelectorPatches(ctx context.Context, pod *corev1.Pod, config *apis.WorkloadConfiguration) []map[string]interface{} {
	var patches []map[string]interface{}

	// Determine the target capacity type based on current state
	capacityType, err := m.determineTargetCapacityType(ctx, pod, config)
	if err != nil {
		// If we can't determine the state, default to on-demand for safety
		capacityType = capacityTypeOnDemand
	}

	// Get label configuration
	labelKey, spotValue, onDemandValue := m.getCapacityTypeLabelConfig()

	// Map capacity type to actual label value
	labelValue := onDemandValue
	if capacityType == capacityTypeSpot {
		labelValue = spotValue
	}

	nodeSelector := map[string]string{
		labelKey: labelValue,
	}

	// Add nodeSelector if it doesn't exist
	if pod.Spec.NodeSelector == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/spec/nodeSelector",
			"value": nodeSelector,
		})
	} else {
		// Add or update individual nodeSelector entries
		for key, value := range nodeSelector {
			patches = append(patches, map[string]interface{}{
				"op":    "add",
				"path":  fmt.Sprintf("/spec/nodeSelector/%s", jsonPointerEscape(key)),
				"value": value,
			})
		}
	}

	return patches
}

// determineTargetCapacityType determines whether to schedule on spot or on-demand based on current state
func (m *MutationHandler) determineTargetCapacityType(ctx context.Context, pod *corev1.Pod, config *apis.WorkloadConfiguration) (string, error) {
	if m.Observer != nil {
		workload, err := m.workloadForPod(ctx, pod)
		if err != nil {
			return capacityTypeOnDemand, err
		}
		snapshot, err := m.Observer.Observe(ctx, workload)
		if err != nil {
			return capacityTypeOnDemand, err
		}
		if !snapshot.IsFresh(time.Now(), DefaultPodListMaxAge) || snapshot.ClassificationError != nil {
			return capacityTypeOnDemand, nil
		}
		desired, err := workloadDesiredReplicas(workload)
		if err != nil {
			return capacityTypeOnDemand, err
		}
		allocation, err := apis.AllocateReplicaDistribution(desired, config.AllocationPolicy())
		if err != nil {
			return capacityTypeOnDemand, fmt.Errorf("calculate replica allocation: %w", err)
		}
		spot, onDemand := snapshot.ActualCounts()
		if m.AdmissionTracker != nil {
			key := m.AdmissionTracker.WorkloadKeyForUID(snapshot.WorkloadRef.UID)
			m.AdmissionTracker.UpdatePodListMetadata(key, snapshot.PodResourceVersion, workload.GetGeneration())
			return m.AdmissionTracker.SelectAndReserve(key, workload.GetGeneration(), int(spot), int(onDemand), int(allocation.TargetSpot), int(allocation.TargetOnDemand)), nil
		}
		if onDemand < allocation.TargetOnDemand {
			return capacityTypeOnDemand, nil
		}
		if spot < allocation.TargetSpot {
			return capacityTypeSpot, nil
		}
		return capacityTypeOnDemand, nil
	}

	// Get the workload that owns this pod
	workloadName, workloadKind, err := m.getWorkloadInfo(pod)
	if err != nil {
		return capacityTypeOnDemand, err
	}

	// Get workload generation for staleness detection
	generation, desiredReplicas, err := m.getWorkloadGeneration(ctx, pod.Namespace, workloadName, workloadKind)
	if err != nil {
		return capacityTypeOnDemand, err
	}

	// Check staleness before using cached state
	var key string
	if m.AdmissionTracker != nil {
		key = m.AdmissionTracker.WorkloadKey(pod.Namespace, workloadKind, workloadName)
		staleness := m.AdmissionTracker.CheckStaleness(key, generation)
		if staleness.IsStale {
			m.logStaleness(staleness, workloadName, workloadKind, pod.Namespace)
			// Reset state on staleness
			m.AdmissionTracker.ResetPending(key, generation)
		}
	}

	// List pods and capture metadata
	spotCount, onDemandCount, listRV, err := m.countCurrentPodsWithRV(ctx, pod.Namespace, workloadName, workloadKind, pod.Name)
	if err != nil {
		return capacityTypeOnDemand, err
	}

	// Update pod list metadata for future staleness checks
	if m.AdmissionTracker != nil {
		m.AdmissionTracker.UpdatePodListMetadata(key, listRV, generation)
	}

	// Add pending admissions to effective count if tracker is available
	effectiveSpot := spotCount
	effectiveOnDemand := onDemandCount
	if m.AdmissionTracker != nil {
		pendingSpot, pendingOnDemand := m.AdmissionTracker.GetPendingCounts(key, generation)
		effectiveSpot = spotCount + int(pendingSpot)
		effectiveOnDemand = onDemandCount + int(pendingOnDemand)
	}

	// The allocation target is defined by the workload's desired replicas, not
	// by the current burst size. Observed Pods and pending admissions only
	// determine where this Pod fits relative to that stable target.
	allocation, err := apis.AllocateReplicaDistribution(desiredReplicas, config.AllocationPolicy())
	if err != nil {
		return capacityTypeOnDemand, fmt.Errorf("calculate replica allocation: %w", err)
	}
	targetOnDemandCount := int(allocation.TargetOnDemand)
	targetSpotCount := int(allocation.TargetSpot)

	// Floor-first: the on-demand target includes the effective floor. If both
	// targets are met, the final fallback below is the deterministic tie-break.
	if effectiveOnDemand < targetOnDemandCount {
		if m.AdmissionTracker != nil {
			m.AdmissionTracker.IncrementPending(key, capacityTypeOnDemand, generation)
		}
		return capacityTypeOnDemand, nil
	}

	// If we have enough on-demand pods and need more spot pods, schedule on spot
	if effectiveSpot < targetSpotCount {
		if m.AdmissionTracker != nil {
			m.AdmissionTracker.IncrementPending(key, capacityTypeSpot, generation)
		}
		return capacityTypeSpot, nil
	}

	// Default to on-demand for safety
	if m.AdmissionTracker != nil {
		m.AdmissionTracker.IncrementPending(key, capacityTypeOnDemand, generation)
	}
	return capacityTypeOnDemand, nil
}

// getWorkloadGeneration returns the workload generation and desired replica
// count used by the admission allocation target.
func (m *MutationHandler) getWorkloadGeneration(ctx context.Context, namespace, name, kind string) (int64, int32, error) {
	switch kind {
	case workloadTypeDeployment:
		var deploy appsv1.Deployment
		if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &deploy); err != nil {
			return 0, 0, err
		}
		return deploy.Generation, desiredReplicas(deploy.Spec.Replicas), nil
	case workloadTypeStatefulSet:
		var sts appsv1.StatefulSet
		if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &sts); err != nil {
			return 0, 0, err
		}
		return sts.Generation, desiredReplicas(sts.Spec.Replicas), nil
	}
	return 0, 0, fmt.Errorf("unsupported workload kind %q", kind)
}

// desiredReplicas applies the Kubernetes default for an omitted replicas field.
func desiredReplicas(replicas *int32) int32 {
	if replicas == nil {
		return 1
	}
	return *replicas
}

func workloadDesiredReplicas(workload client.Object) (int32, error) {
	switch typed := workload.(type) {
	case *appsv1.Deployment:
		return desiredReplicas(typed.Spec.Replicas), nil
	case *appsv1.StatefulSet:
		return desiredReplicas(typed.Spec.Replicas), nil
	default:
		return 0, fmt.Errorf("unsupported workload type %T", workload)
	}
}

// workloadForPod resolves the direct controller chain and verifies UID identity.
func (m *MutationHandler) workloadForPod(ctx context.Context, pod *corev1.Pod) (client.Object, error) {
	for _, owner := range pod.OwnerReferences {
		switch owner.Kind {
		case workloadTypeReplicaSet:
			var rs appsv1.ReplicaSet
			if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: owner.Name}, &rs); err != nil {
				return nil, err
			}
			if rs.UID != owner.UID {
				return nil, fmt.Errorf("ReplicaSet owner UID does not match pod owner reference")
			}
			for _, rsOwner := range rs.OwnerReferences {
				if rsOwner.Kind != workloadTypeDeployment || rsOwner.Controller == nil || !*rsOwner.Controller {
					continue
				}
				var deployment appsv1.Deployment
				if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: rsOwner.Name}, &deployment); err != nil {
					return nil, err
				}
				if deployment.UID != rsOwner.UID {
					return nil, fmt.Errorf("deployment owner UID does not match ReplicaSet owner reference")
				}
				return &deployment, nil
			}
		case workloadTypeStatefulSet:
			var sts appsv1.StatefulSet
			if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: owner.Name}, &sts); err != nil {
				return nil, err
			}
			if sts.UID != owner.UID {
				return nil, fmt.Errorf("StatefulSet owner UID does not match pod owner reference")
			}
			return &sts, nil
		}
	}
	return nil, fmt.Errorf("no supported workload owner found")
}

// getWorkloadInfo extracts workload information from pod owner references
func (m *MutationHandler) getWorkloadInfo(pod *corev1.Pod) (workloadType, workloadName string, err error) {
	for _, ownerRef := range pod.OwnerReferences {
		switch ownerRef.Kind {
		case workloadTypeReplicaSet:
			// For deployments, we need to get the ReplicaSet's owner (Deployment)
			ctx := context.Background()
			var rs appsv1.ReplicaSet
			if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: ownerRef.Name}, &rs); err != nil {
				continue
			}

			for _, rsOwnerRef := range rs.OwnerReferences {
				if rsOwnerRef.Kind == workloadTypeDeployment {
					return rsOwnerRef.Name, workloadTypeDeployment, nil
				}
			}
		case workloadTypeStatefulSet:
			return ownerRef.Name, workloadTypeStatefulSet, nil
		case workloadTypeDeployment:
			return ownerRef.Name, workloadTypeDeployment, nil
		}
	}

	return "", "", fmt.Errorf("no supported workload owner found")
}

// countCurrentPodsWithRV lists pods and returns the list's resourceVersion
func (m *MutationHandler) countCurrentPodsWithRV(
	ctx context.Context,
	namespace, workloadName, workloadKind, excludePodName string,
) (spotCount, onDemandCount int, resourceVersion string, err error) {
	var podList corev1.PodList
	if err := m.Client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return 0, 0, "", err
	}

	resourceVersion = podList.ResourceVersion

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Name == excludePodName {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}
		if !m.podBelongsToWorkload(pod, workloadName, workloadKind) {
			continue
		}

		capacityType := m.getPodCapacityType(pod)
		if capacityType == capacityTypeSpot {
			spotCount++
		} else {
			onDemandCount++
		}
	}

	return spotCount, onDemandCount, resourceVersion, nil
}

// logStaleness logs when stale state is detected
func (m *MutationHandler) logStaleness(s StalenessCheck, name, kind, ns string) {
	log.FromContext(context.Background()).V(1).Info(
		"Detected stale admission state, resetting",
		"workload", name,
		"kind", kind,
		"namespace", ns,
		"reason", s.Reason,
		"details", s.Details,
	)
}

// podBelongsToWorkload checks if a pod belongs to the specified workload
func (m *MutationHandler) podBelongsToWorkload(pod *corev1.Pod, workloadName, workloadKind string) bool {
	for _, ownerRef := range pod.OwnerReferences {
		switch workloadKind {
		case workloadTypeDeployment:
			if ownerRef.Kind == workloadTypeReplicaSet {
				// Check if the ReplicaSet belongs to our deployment
				ctx := context.Background()
				var rs appsv1.ReplicaSet
				if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: ownerRef.Name}, &rs); err != nil {
					continue
				}

				for _, rsOwnerRef := range rs.OwnerReferences {
					if rsOwnerRef.Kind == workloadTypeDeployment && rsOwnerRef.Name == workloadName {
						return true
					}
				}
			}
		case workloadTypeStatefulSet:
			if ownerRef.Kind == workloadTypeStatefulSet && ownerRef.Name == workloadName {
				return true
			}
		}
	}

	return false
}

// getPodCapacityType determines the capacity type of an existing pod
func (m *MutationHandler) getPodCapacityType(pod *corev1.Pod) string {
	// Check nodeSelector first
	if capacityType := m.getCapacityTypeFromNodeSelector(pod); capacityType != "" {
		return capacityType
	}

	// Check node affinity
	if capacityType := m.getCapacityTypeFromNodeAffinity(pod); capacityType != "" {
		return capacityType
	}

	// If we can't determine, assume on-demand (safer default)
	return capacityTypeOnDemand
}

// getCapacityTypeFromNodeSelector extracts capacity type from pod's nodeSelector
func (m *MutationHandler) getCapacityTypeFromNodeSelector(pod *corev1.Pod) string {
	if pod.Spec.NodeSelector == nil {
		return ""
	}

	labelKey, spotValue, _ := m.getCapacityTypeLabelConfig()

	if capacityType, exists := pod.Spec.NodeSelector[labelKey]; exists {
		if capacityType == spotValue {
			return capacityTypeSpot
		}
		return capacityTypeOnDemand
	}
	return ""
}

// getCapacityTypeFromNodeAffinity extracts capacity type from pod's node affinity
func (m *MutationHandler) getCapacityTypeFromNodeAffinity(pod *corev1.Pod) string {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		return ""
	}

	nodeAffinity := pod.Spec.Affinity.NodeAffinity

	// Check required affinity first (highest priority)
	if capacityType := m.getCapacityTypeFromRequiredAffinity(nodeAffinity); capacityType != "" {
		return capacityType
	}

	// Check preferred affinity
	return m.getCapacityTypeFromPreferredAffinity(nodeAffinity)
}

// getCapacityTypeFromRequiredAffinity extracts capacity type from required node affinity
func (m *MutationHandler) getCapacityTypeFromRequiredAffinity(nodeAffinity *corev1.NodeAffinity) string {
	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return ""
	}

	labelKey, spotValue, _ := m.getCapacityTypeLabelConfig()

	for _, term := range nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Key == labelKey && len(expr.Values) > 0 {
				if expr.Values[0] == spotValue {
					return capacityTypeSpot
				}
				return capacityTypeOnDemand
			}
		}
	}
	return ""
}

// getCapacityTypeFromPreferredAffinity extracts capacity type from preferred node affinity (highest weight wins)
func (m *MutationHandler) getCapacityTypeFromPreferredAffinity(nodeAffinity *corev1.NodeAffinity) string {
	if nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		return ""
	}

	labelKey, spotValue, _ := m.getCapacityTypeLabelConfig()

	highestWeight := int32(0)
	preferredType := ""

	for _, term := range nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		if term.Weight > highestWeight {
			for _, expr := range term.Preference.MatchExpressions {
				if expr.Key == labelKey && len(expr.Values) > 0 {
					highestWeight = term.Weight
					if expr.Values[0] == spotValue {
						preferredType = capacityTypeSpot
					} else {
						preferredType = capacityTypeOnDemand
					}
				}
			}
		}
	}

	return preferredType
}

// InjectDecoder injects the decoder into the handler
func (m *MutationHandler) InjectDecoder(d admission.Decoder) error {
	m.decoder = d
	return nil
}

// MutatingHandler provides legacy support for existing interface compatibility
type MutatingHandler = MutationHandler

// NewMutatingHandler creates a new mutating webhook handler (legacy)
func NewMutatingHandler() *MutatingHandler {
	// Return a basic handler for backward compatibility
	return &MutatingHandler{
		AnnotationParser: annotations.NewAnnotationParser(),
	}
}
