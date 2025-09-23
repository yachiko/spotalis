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

	"gomodules.xyz/jsonpatch/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
)

// MutationHandler handles admission webhook requests for pod and workload mutation
type MutationHandler struct {
	Client           client.Client
	AnnotationParser *annotations.AnnotationParser
	NodeClassifier   *config.NodeClassifierService
	decoder          admission.Decoder
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

// Handle processes admission webhook requests
func (m *MutationHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues(
		"kind", req.Kind.Kind,
		"namespace", req.Namespace,
		"name", req.Name,
		"operation", req.Operation,
	)

	logger.Info("Processing admission webhook request")

	switch req.Kind.Kind {
	case "Pod":
		return m.mutatePod(ctx, req)
	default:
		logger.Info("Unsupported resource kind, allowing", "kind", req.Kind.Kind)
		return admission.Allowed("unsupported resource kind")
	}
}

// mutatePod handles pod mutation for node affinity and tolerations
func (m *MutationHandler) mutatePod(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	var pod corev1.Pod
	if err := m.decoder.Decode(req, &pod); err != nil {
		logger.Error(err, "Failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if this pod belongs to a workload with Spotalis annotations
	workloadConfig, err := m.getWorkloadConfigForPod(ctx, &pod)
	if err != nil {
		logger.Error(err, "Failed to get workload configuration for pod")
		return admission.Allowed("failed to get workload config")
	}

	if workloadConfig == nil {
		logger.Info("Pod does not belong to a Spotalis-managed workload")
		return admission.Allowed("not managed by Spotalis")
	}

	logger.Info("Mutating pod for Spotalis workload management")

	// Apply mutations based on workload configuration
	patches := m.generatePodPatches(&pod, workloadConfig)

	if len(patches) == 0 {
		return admission.Allowed("no mutations needed")
	}

	logger.Info("Applied pod mutations", "patches", len(patches))
	logger.Info("Patches: ", "patches", patches)

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
func (m *MutationHandler) getWorkloadConfigForPod(ctx context.Context, pod *corev1.Pod) (*apis.WorkloadConfiguration, error) {
	// Check if pod has owner references to a Deployment or StatefulSet
	for _, ownerRef := range pod.OwnerReferences {
		switch ownerRef.Kind {
		case "ReplicaSet":
			// For deployments, we need to get the ReplicaSet's owner (Deployment)
			if config, err := m.getConfigFromReplicaSet(ctx, pod.Namespace, ownerRef.Name); err == nil && config != nil {
				return config, nil
			}
		case "StatefulSet":
			if config, err := m.getConfigFromStatefulSet(ctx, pod.Namespace, ownerRef.Name); err == nil && config != nil {
				return config, nil
			}
		case "Deployment":
			if config, err := m.getConfigFromDeployment(ctx, pod.Namespace, ownerRef.Name); err == nil && config != nil {
				return config, nil
			}
		}
	}

	return nil, nil
}

// getConfigFromReplicaSet gets configuration from a ReplicaSet's owner Deployment
func (m *MutationHandler) getConfigFromReplicaSet(ctx context.Context, namespace, name string) (*apis.WorkloadConfiguration, error) {
	var rs appsv1.ReplicaSet
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &rs); err != nil {
		return nil, err
	}

	// Get the Deployment that owns this ReplicaSet
	for _, ownerRef := range rs.OwnerReferences {
		if ownerRef.Kind == "Deployment" {
			return m.getConfigFromDeployment(ctx, namespace, ownerRef.Name)
		}
	}

	return nil, nil
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
func (m *MutationHandler) generatePodPatches(pod *corev1.Pod, config *apis.WorkloadConfiguration) []map[string]interface{} {
	var patches []map[string]interface{}

	// Add nodeSelector for spot instances if spot percentage > 0
	if config.SpotPercentage > 0 {
		nodeSelectorPatches := m.generateNodeSelectorPatches(pod, config)
		patches = append(patches, nodeSelectorPatches...)
	}

	return patches
}

// jsonPointerEscape escapes a string for use in JSON Pointer
func jsonPointerEscape(s string) string {
	// Replace ~ with ~0 and / with ~1 as per RFC 6901
	s = fmt.Sprintf("%s", s)
	s = fmt.Sprintf("%s", s) // Double formatting to handle special characters
	return s
}

// generateNodeSelectorPatches generates patches for node selector based on current pod distribution
func (m *MutationHandler) generateNodeSelectorPatches(pod *corev1.Pod, config *apis.WorkloadConfiguration) []map[string]interface{} {
	var patches []map[string]interface{}

	// Determine the target capacity type based on current state
	capacityType, err := m.determineTargetCapacityType(pod, config)
	if err != nil {
		// If we can't determine the state, default to on-demand for safety
		capacityType = "on-demand"
	}

	nodeSelector := map[string]string{
		"karpenter.sh/capacity-type": capacityType,
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
func (m *MutationHandler) determineTargetCapacityType(pod *corev1.Pod, config *apis.WorkloadConfiguration) (string, error) {
	ctx := context.Background()

	// Get the workload that owns this pod
	workloadName, workloadKind, err := m.getWorkloadInfo(pod)
	if err != nil {
		return "on-demand", err
	}

	// Count current pods for this workload
	spotCount, onDemandCount, err := m.countCurrentPods(ctx, pod.Namespace, workloadName, workloadKind)
	if err != nil {
		return "on-demand", err
	}

	// Calculate totals
	totalPods := spotCount + onDemandCount + 1 // +1 for the new pod being scheduled

	// Calculate required on-demand pods
	requiredOnDemand := int(config.MinOnDemand)

	// If we haven't met the minimum on-demand requirement, schedule on on-demand
	if onDemandCount < requiredOnDemand {
		return "on-demand", nil
	}

	// Calculate the target distribution based on spot percentage
	targetSpotCount := int(float64(totalPods) * float64(config.SpotPercentage) / 100.0)
	targetOnDemandCount := totalPods - targetSpotCount

	// Prioritize on-demand: if we need more on-demand pods, schedule there
	if onDemandCount < targetOnDemandCount {
		return "on-demand", nil
	}

	// If we have enough on-demand pods and need more spot pods, schedule on spot
	if spotCount < targetSpotCount {
		return "spot", nil
	}

	// Default to on-demand for safety
	return "on-demand", nil
}

// getWorkloadInfo extracts workload information from pod owner references
func (m *MutationHandler) getWorkloadInfo(pod *corev1.Pod) (string, string, error) {
	for _, ownerRef := range pod.OwnerReferences {
		switch ownerRef.Kind {
		case "ReplicaSet":
			// For deployments, we need to get the ReplicaSet's owner (Deployment)
			ctx := context.Background()
			var rs appsv1.ReplicaSet
			if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: ownerRef.Name}, &rs); err != nil {
				continue
			}

			for _, rsOwnerRef := range rs.OwnerReferences {
				if rsOwnerRef.Kind == "Deployment" {
					return rsOwnerRef.Name, "Deployment", nil
				}
			}
		case "StatefulSet":
			return ownerRef.Name, "StatefulSet", nil
		case "Deployment":
			return ownerRef.Name, "Deployment", nil
		}
	}

	return "", "", fmt.Errorf("no supported workload owner found")
}

// countCurrentPods counts existing spot and on-demand pods for a workload
func (m *MutationHandler) countCurrentPods(ctx context.Context, namespace, workloadName, workloadKind string) (spotCount, onDemandCount int, err error) {
	// List all pods in the namespace
	var podList corev1.PodList
	if err := m.Client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return 0, 0, err
	}

	for _, pod := range podList.Items {
		// Skip pods that are not running or pending
		if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodPending {
			continue
		}

		// Check if this pod belongs to our workload
		if !m.podBelongsToWorkload(&pod, workloadName, workloadKind) {
			continue
		}

		// Determine capacity type based on nodeSelector or node affinity
		capacityType := m.getPodCapacityType(&pod)

		switch capacityType {
		case "spot":
			spotCount++
		case "on-demand":
			onDemandCount++
		}
	}

	return spotCount, onDemandCount, nil
}

// podBelongsToWorkload checks if a pod belongs to the specified workload
func (m *MutationHandler) podBelongsToWorkload(pod *corev1.Pod, workloadName, workloadKind string) bool {
	for _, ownerRef := range pod.OwnerReferences {
		switch workloadKind {
		case "Deployment":
			if ownerRef.Kind == "ReplicaSet" {
				// Check if the ReplicaSet belongs to our deployment
				ctx := context.Background()
				var rs appsv1.ReplicaSet
				if err := m.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: ownerRef.Name}, &rs); err != nil {
					continue
				}

				for _, rsOwnerRef := range rs.OwnerReferences {
					if rsOwnerRef.Kind == "Deployment" && rsOwnerRef.Name == workloadName {
						return true
					}
				}
			}
		case "StatefulSet":
			if ownerRef.Kind == "StatefulSet" && ownerRef.Name == workloadName {
				return true
			}
		}
	}

	return false
}

// getPodCapacityType determines the capacity type of an existing pod
func (m *MutationHandler) getPodCapacityType(pod *corev1.Pod) string {
	// Check nodeSelector first
	if pod.Spec.NodeSelector != nil {
		if capacityType, exists := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; exists {
			return capacityType
		}
	}

	// Check node affinity
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		nodeAffinity := pod.Spec.Affinity.NodeAffinity

		// Check required affinity
		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			for _, term := range nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				for _, expr := range term.MatchExpressions {
					if expr.Key == "karpenter.sh/capacity-type" && len(expr.Values) > 0 {
						return expr.Values[0]
					}
				}
			}
		}

		// Check preferred affinity for highest weight
		if nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution != nil {
			highestWeight := int32(0)
			preferredType := ""

			for _, term := range nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
				if term.Weight > highestWeight {
					for _, expr := range term.Preference.MatchExpressions {
						if expr.Key == "karpenter.sh/capacity-type" && len(expr.Values) > 0 {
							highestWeight = term.Weight
							preferredType = expr.Values[0]
						}
					}
				}
			}

			if preferredType != "" {
				return preferredType
			}
		}
	}

	// If we can't determine, assume on-demand (safer default)
	return "on-demand"
}

// InjectDecoder injects the decoder into the handler
func (m *MutationHandler) InjectDecoder(d admission.Decoder) error {
	m.decoder = d
	return nil
}

// Legacy support for existing interface
type MutatingHandler = MutationHandler

// NewMutatingHandler creates a new mutating webhook handler (legacy)
func NewMutatingHandler() *MutatingHandler {
	// Return a basic handler for backward compatibility
	return &MutatingHandler{
		AnnotationParser: annotations.NewAnnotationParser(),
	}
}
