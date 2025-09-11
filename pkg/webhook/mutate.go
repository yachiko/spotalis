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
	"strconv"

	"gomodules.xyz/jsonpatch/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	logger.V(1).Info("Processing admission webhook request")

	switch req.Kind.Kind {
	case "Pod":
		return m.mutatePod(ctx, req)
	case "Deployment":
		return m.mutateDeployment(ctx, req)
	case "StatefulSet":
		return m.mutateStatefulSet(ctx, req)
	default:
		logger.V(1).Info("Unsupported resource kind, allowing")
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
		logger.V(1).Info("Pod does not belong to a Spotalis-managed workload")
		return admission.Allowed("not managed by Spotalis")
	}

	logger.Info("Mutating pod for Spotalis workload management")

	// Apply mutations based on workload configuration
	patches := m.generatePodPatches(&pod, workloadConfig)

	if len(patches) == 0 {
		return admission.Allowed("no mutations needed")
	}

	// Convert patches to JSON
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		logger.Error(err, "Failed to marshal patches")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info("Applied pod mutations", "patches", len(patches))

	podBytes, err := pod.Marshal()
	if err != nil {
		logger.Error(err, "Failed to marshal pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	patchOperations, err := jsonpatch.CreatePatch(podBytes, patchBytes)

	if err != nil {
		logger.Error(err, "Failed to create patch operations")
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.Patched("applied Spotalis mutations", patchOperations...)
}

// mutateDeployment handles deployment mutation for Spotalis annotations
func (m *MutationHandler) mutateDeployment(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	var deployment appsv1.Deployment
	if err := m.decoder.Decode(req, &deployment); err != nil {
		logger.Error(err, "Failed to decode deployment")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if this deployment has Spotalis annotations
	if !m.AnnotationParser.HasSpotalisAnnotations(&deployment) {
		logger.V(1).Info("Deployment has no Spotalis annotations")
		return admission.Allowed("not managed by Spotalis")
	}

	logger.Info("Mutating deployment for Spotalis management")

	// Validate annotations
	if errors := m.AnnotationParser.ValidateAnnotations(&deployment); len(errors) > 0 {
		logger.Error(fmt.Errorf("validation errors: %v", errors), "Invalid Spotalis annotations")
		return admission.Denied(fmt.Sprintf("Invalid Spotalis annotations: %v", errors))
	}

	// Generate patches for deployment
	patches := m.generateDeploymentPatches(&deployment)

	if len(patches) == 0 {
		return admission.Allowed("no mutations needed")
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		logger.Error(err, "Failed to marshal patches")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info("Applied deployment mutations", "patches", len(patches))

	deploymentBytes, err := json.Marshal(deployment)
	if err != nil {
		logger.Error(err, "Failed to marshal deployment")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	patchOperations, err := jsonpatch.CreatePatch(deploymentBytes, patchBytes)
	if err != nil {
		logger.Error(err, "Failed to create patch operations")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.Patched("applied Spotalis mutations", patchOperations...)
}

// mutateStatefulSet handles StatefulSet mutation for Spotalis annotations
func (m *MutationHandler) mutateStatefulSet(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx)

	var statefulSet appsv1.StatefulSet
	if err := m.decoder.Decode(req, &statefulSet); err != nil {
		logger.Error(err, "Failed to decode StatefulSet")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Check if this StatefulSet has Spotalis annotations
	if !m.AnnotationParser.HasSpotalisAnnotations(&statefulSet) {
		logger.V(1).Info("StatefulSet has no Spotalis annotations")
		return admission.Allowed("not managed by Spotalis")
	}

	logger.Info("Mutating StatefulSet for Spotalis management")

	// Validate annotations
	if errors := m.AnnotationParser.ValidateAnnotations(&statefulSet); len(errors) > 0 {
		logger.Error(fmt.Errorf("validation errors: %v", errors), "Invalid Spotalis annotations")
		return admission.Denied(fmt.Sprintf("Invalid Spotalis annotations: %v", errors))
	}

	// Generate patches for StatefulSet
	patches := m.generateStatefulSetPatches(&statefulSet)

	if len(patches) == 0 {
		return admission.Allowed("no mutations needed")
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		logger.Error(err, "Failed to marshal patches")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logger.Info("Applied StatefulSet mutations", "patches", len(patches))

	statefulSetBytes, err := json.Marshal(statefulSet)
	if err != nil {
		logger.Error(err, "Failed to marshal statefulSet")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	patchOperations, err := jsonpatch.CreatePatch(statefulSetBytes, patchBytes)
	if err != nil {
		logger.Error(err, "Failed to create patch operations")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.Patched("applied Spotalis mutations", patchOperations...)
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
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &deployment); err != nil {
		return nil, err
	}

	if !m.AnnotationParser.HasSpotalisAnnotations(&deployment) {
		return nil, nil
	}

	return m.AnnotationParser.ParseWorkloadConfiguration(&deployment)
}

// getConfigFromStatefulSet gets configuration from a StatefulSet
func (m *MutationHandler) getConfigFromStatefulSet(ctx context.Context, namespace, name string) (*apis.WorkloadConfiguration, error) {
	var statefulSet appsv1.StatefulSet
	if err := m.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &statefulSet); err != nil {
		return nil, err
	}

	if !m.AnnotationParser.HasSpotalisAnnotations(&statefulSet) {
		return nil, nil
	}

	return m.AnnotationParser.ParseWorkloadConfiguration(&statefulSet)
}

// generatePodPatches generates JSON patches for pod mutation
func (m *MutationHandler) generatePodPatches(pod *corev1.Pod, config *apis.WorkloadConfiguration) []map[string]interface{} {
	var patches []map[string]interface{}

	// Add spot instance tolerations if spot percentage > 0
	if config.SpotPercentage > 0 {
		tolerationPatches := m.generateTolerationPatches(pod)
		patches = append(patches, tolerationPatches...)
	}

	// Add node affinity for better scheduling
	affinityPatches := m.generateNodeAffinityPatches(pod, config)
	patches = append(patches, affinityPatches...)

	// Add scheduling annotations
	annotationPatches := m.generateSchedulingAnnotationPatches(pod, config)
	patches = append(patches, annotationPatches...)

	return patches
}

// generateDeploymentPatches generates JSON patches for deployment mutation
func (m *MutationHandler) generateDeploymentPatches(deployment *appsv1.Deployment) []map[string]interface{} {
	var patches []map[string]interface{}

	// Add default annotations if missing
	if deployment.Annotations == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations",
			"value": map[string]string{},
		})
	}

	// Add Spotalis management annotation
	patches = append(patches, map[string]interface{}{
		"op":    "add",
		"path":  "/metadata/annotations/spotalis.io~1managed",
		"value": "true",
	})

	return patches
}

// generateStatefulSetPatches generates JSON patches for StatefulSet mutation
func (m *MutationHandler) generateStatefulSetPatches(statefulSet *appsv1.StatefulSet) []map[string]interface{} {
	var patches []map[string]interface{}

	// Add default annotations if missing
	if statefulSet.Annotations == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations",
			"value": map[string]string{},
		})
	}

	// Add Spotalis management annotation
	patches = append(patches, map[string]interface{}{
		"op":    "add",
		"path":  "/metadata/annotations/spotalis.io~1managed",
		"value": "true",
	})

	return patches
}

// generateTolerationPatches generates patches for spot instance tolerations
func (m *MutationHandler) generateTolerationPatches(pod *corev1.Pod) []map[string]interface{} {
	var patches []map[string]interface{}

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
		{
			Key:      "cloud.google.com/gke-preemptible",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "kubernetes.azure.com/scalesetpriority",
			Value:    "spot",
			Operator: corev1.TolerationOpEqual,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}

	// Add tolerations if spec.tolerations doesn't exist
	if pod.Spec.Tolerations == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/spec/tolerations",
			"value": spotTolerations,
		})
	} else {
		// Add individual tolerations
		for i, toleration := range spotTolerations {
			patches = append(patches, map[string]interface{}{
				"op":    "add",
				"path":  fmt.Sprintf("/spec/tolerations/%d", len(pod.Spec.Tolerations)+i),
				"value": toleration,
			})
		}
	}

	return patches
}

// generateNodeAffinityPatches generates patches for node affinity
func (m *MutationHandler) generateNodeAffinityPatches(pod *corev1.Pod, config *apis.WorkloadConfiguration) []map[string]interface{} {
	var patches []map[string]interface{}

	// Create node affinity for spot instances if spot percentage is configured
	if config.SpotPercentage > 0 {
		nodeAffinity := &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: int32(config.SpotPercentage),
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "karpenter.sh/capacity-type",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"spot"},
							},
						},
					},
				},
			},
		}

		if pod.Spec.Affinity == nil {
			patches = append(patches, map[string]interface{}{
				"op":   "add",
				"path": "/spec/affinity",
				"value": &corev1.Affinity{
					NodeAffinity: nodeAffinity,
				},
			})
		} else if pod.Spec.Affinity.NodeAffinity == nil {
			patches = append(patches, map[string]interface{}{
				"op":    "add",
				"path":  "/spec/affinity/nodeAffinity",
				"value": nodeAffinity,
			})
		}
	}

	return patches
}

// generateSchedulingAnnotationPatches generates patches for scheduling annotations
func (m *MutationHandler) generateSchedulingAnnotationPatches(pod *corev1.Pod, config *apis.WorkloadConfiguration) []map[string]interface{} {
	var patches []map[string]interface{}

	schedulingAnnotations := map[string]string{
		"spotalis.io/spot-percentage":     strconv.Itoa(int(config.SpotPercentage)),
		"spotalis.io/min-on-demand":       strconv.Itoa(int(config.MinOnDemand)),
		"spotalis.io/scheduled-timestamp": metav1.Now().Format("2006-01-02T15:04:05Z"),
	}

	// Add annotations if they don't exist
	if pod.Annotations == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations",
			"value": schedulingAnnotations,
		})
	} else {
		// Add individual annotations
		for key, value := range schedulingAnnotations {
			escapedKey := jsonPointerEscape(key)
			patches = append(patches, map[string]interface{}{
				"op":    "add",
				"path":  fmt.Sprintf("/metadata/annotations/%s", escapedKey),
				"value": value,
			})
		}
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
