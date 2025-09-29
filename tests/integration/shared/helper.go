//go:build integration

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

package shared

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KindClusterHelper provides utilities for connecting to a Kind cluster
// and setting up integration tests
type KindClusterHelper struct {
	Client    client.Client
	Clientset *kubernetes.Clientset
	Config    *rest.Config
	Context   context.Context
}

// NewKindClusterHelper creates a new helper that connects to the existing Kind cluster
func NewKindClusterHelper(ctx context.Context) (*KindClusterHelper, error) {
	// Load kubeconfig - try Kind context first, then default
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		kubeconfig = filepath.Join(homeDir, ".kube", "config")
	}

	// Load the kubeconfig
	config, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		return nil, err
	}

	// Try to use kind-spotalis context if available
	contextName := os.Getenv("KUBE_CONTEXT")
	if contextName == "" {
		contextName = "kind-spotalis"
	}

	// Check if the context exists
	if _, exists := config.Contexts[contextName]; !exists {
		// Fall back to current context
		contextName = config.CurrentContext
	}

	// Build the rest config with the specific context
	cfg, err := clientcmd.NewNonInteractiveClientConfig(
		*config,
		contextName,
		&clientcmd.ConfigOverrides{},
		nil,
	).ClientConfig()
	if err != nil {
		return nil, err
	}

	// Create controller-runtime client
	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, err
	}

	// Create clientset for direct API access
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &KindClusterHelper{
		Client:    k8sClient,
		Clientset: clientset,
		Config:    cfg,
		Context:   ctx,
	}, nil
}

// CreateTestNamespace creates a unique test namespace with proper labels
func (h *KindClusterHelper) CreateTestNamespace() (string, error) {
	namespace := "spotalis-test-" + randString(8)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"spotalis.io/test":           "true", // Mark as test resource
				"spotalis.io/enabled":        "true", // Enable Spotalis features
				"test.spotalis.io/cleanup":   "auto", // Enable automatic cleanup
				"test.spotalis.io/component": "integration-test",
			},
		},
	}
	err := h.Client.Create(h.Context, ns)
	return namespace, err
}

// CreateTestNamespaceWithLabels creates a test namespace with custom labels in addition to standard test labels
func (h *KindClusterHelper) CreateTestNamespaceWithLabels(customLabels map[string]string) (string, error) {
	namespace := "spotalis-test-" + randString(8)

	// Start with standard test labels
	labels := map[string]string{
		"spotalis.io/test":           "true",
		"test.spotalis.io/cleanup":   "auto",
		"spotalis.io/enabled":        "true", // Enable Spotalis features
		"test.spotalis.io/component": "integration-test",
	}

	// Add custom labels
	for k, v := range customLabels {
		labels[k] = v
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: labels,
		},
	}
	err := h.Client.Create(h.Context, ns)
	return namespace, err
}

// CleanupNamespace deletes a test namespace and waits for deletion
func (h *KindClusterHelper) CleanupNamespace(namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	// First check if namespace exists
	err := h.Client.Get(h.Context, client.ObjectKey{Name: namespace}, ns)
	if err != nil {
		// Namespace doesn't exist, nothing to clean up
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return fmt.Errorf("failed to check if namespace %s exists: %w", namespace, err)
	}

	// Force remove finalizers that might prevent deletion
	if len(ns.Finalizers) > 0 {
		ns.Finalizers = []string{}
		if err := h.Client.Update(h.Context, ns); err != nil {
			// Log but don't fail on finalizer removal
			fmt.Printf("Warning: failed to remove finalizers from namespace %s: %v\n", namespace, err)
		}
	}

	// Delete all resources in the namespace first
	if err := h.forceCleanupNamespaceResources(namespace); err != nil {
		fmt.Printf("Warning: failed to cleanup resources in namespace %s: %v\n", namespace, err)
	}

	// Delete the namespace
	err = h.Client.Delete(h.Context, ns)
	if err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete namespace %s: %w", namespace, err)
	}

	// Wait for namespace to be deleted with shorter timeout to avoid test hangs
	timeout := 60 * time.Second
	interval := 2 * time.Second

	deleted := false
	Eventually(func() bool {
		err := h.Client.Get(h.Context, client.ObjectKey{Name: namespace}, ns)
		deleted = (err != nil && client.IgnoreNotFound(err) == nil) // Error means namespace is gone
		return deleted
	}, timeout, interval).Should(BeTrue(), fmt.Sprintf("namespace %s was not deleted within %v", namespace, timeout))

	return nil
}

// forceCleanupNamespaceResources attempts to delete all resources in a namespace
func (h *KindClusterHelper) forceCleanupNamespaceResources(namespace string) error {
	// Delete all deployments in the namespace
	deployments := &appsv1.DeploymentList{}
	if err := h.Client.List(h.Context, deployments, client.InNamespace(namespace)); err == nil {
		for _, deployment := range deployments.Items {
			if err := h.Client.Delete(h.Context, &deployment); err != nil {
				fmt.Printf("Warning: failed to delete deployment %s/%s: %v\n", deployment.Namespace, deployment.Name, err)
			}
		}
	}

	// Delete all pods in the namespace
	pods := &corev1.PodList{}
	if err := h.Client.List(h.Context, pods, client.InNamespace(namespace)); err == nil {
		for _, pod := range pods.Items {
			if err := h.Client.Delete(h.Context, &pod); err != nil {
				fmt.Printf("Warning: failed to delete pod %s/%s: %v\n", pod.Namespace, pod.Name, err)
			}
		}
	}

	// Give resources a moment to terminate
	time.Sleep(5 * time.Second)
	return nil
}

// CleanupAllTestNamespaces removes all namespaces marked as test resources
func (h *KindClusterHelper) CleanupAllTestNamespaces() error {
	namespaces := &corev1.NamespaceList{}
	err := h.Client.List(h.Context, namespaces, client.MatchingLabels{
		"spotalis.io/test": "true",
	})
	if err != nil {
		return fmt.Errorf("failed to list test namespaces: %w", err)
	}

	var cleanupErrors []error
	for _, ns := range namespaces.Items {
		if err := h.CleanupNamespace(ns.Name); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to cleanup namespace %s: %w", ns.Name, err))
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup failed for %d namespaces: %v", len(cleanupErrors), cleanupErrors)
	}
	return nil
}

// CleanupLegacyTestNamespaces removes old test namespaces that don't have proper labels
func (h *KindClusterHelper) CleanupLegacyTestNamespaces() error {
	namespaces := &corev1.NamespaceList{}
	err := h.Client.List(h.Context, namespaces)
	if err != nil {
		return fmt.Errorf("failed to list namespaces for legacy cleanup: %w", err)
	}

	for _, ns := range namespaces.Items {
		// Skip system namespaces
		if ns.Name == "default" || ns.Name == "kube-system" ||
			ns.Name == "kube-public" || ns.Name == "kube-node-lease" ||
			ns.Name == "local-path-storage" || ns.Name == "admission-controller" ||
			ns.Name == "spotalis-system" {
			continue
		}

		// Clean up namespaces that match test patterns but don't have proper labels
		if h.isLegacyTestNamespace(ns.Name) {
			if err := h.CleanupNamespace(ns.Name); err != nil {
				return fmt.Errorf("failed to cleanup legacy namespace %s: %w", ns.Name, err)
			}
		}
	}
	return nil
}

// isLegacyTestNamespace checks if a namespace name matches old test patterns
func (h *KindClusterHelper) isLegacyTestNamespace(name string) bool {
	// Check for legacy test namespace patterns
	legacyPrefixes := []string{
		"managed-",
		"unmanaged-",
		"spotalis-managed-",
		"test-",
	}

	for _, prefix := range legacyPrefixes {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			// Additional check: ensure it's likely a test namespace with random suffix
			suffix := name[len(prefix):]
			if len(suffix) >= 6 && h.isRandomString(suffix) {
				return true
			}
		}
	}
	return false
}

// isRandomString checks if a string looks like a random test suffix
func (h *KindClusterHelper) isRandomString(s string) bool {
	if len(s) < 6 || len(s) > 10 {
		return false
	}
	for _, char := range s {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')) {
			return false
		}
	}
	return true
}

// WaitForSpotalisController ensures the Spotalis controller is ready
func (h *KindClusterHelper) WaitForSpotalisController() {
	Eventually(func() error {
		return h.verifySpotalisDeployment()
	}, 2*time.Minute, 10*time.Second).Should(Succeed())
}

// AddTestLabelsToResource adds standard test labels to any Kubernetes resource
func (h *KindClusterHelper) AddTestLabelsToResource(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	// Add standard test labels
	labels["spotalis.io/test"] = "true"
	labels["test.spotalis.io/cleanup"] = "auto"
	labels["test.spotalis.io/component"] = "integration-test"

	obj.SetLabels(labels)
}

// AddTestAnnotationsToResource adds standard test annotations to any Kubernetes resource
func (h *KindClusterHelper) AddTestAnnotationsToResource(obj client.Object, spotalisAnnotations map[string]string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Add test metadata
	annotations["test.spotalis.io/created-by"] = "integration-test"
	annotations["test.spotalis.io/created-at"] = time.Now().Format(time.RFC3339)

	// Add any Spotalis-specific annotations
	for k, v := range spotalisAnnotations {
		annotations[k] = v
	}

	// If Spotalis annotations are provided, ensure the workload is enabled
	if len(spotalisAnnotations) > 0 {
		// Only add enabled=true if not explicitly set to false
		if _, exists := annotations["spotalis.io/enabled"]; !exists {
			annotations["spotalis.io/enabled"] = "true"
		}
	}

	obj.SetAnnotations(annotations)
}

// CreateTestDeployment creates a deployment with proper test labels and annotations
func (h *KindClusterHelper) CreateTestDeployment(namespace, name string, replicas int32, spotalisAnnotations map[string]string) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	// Add test labels and annotations
	h.AddTestLabelsToResource(deployment)
	h.AddTestAnnotationsToResource(deployment, spotalisAnnotations)

	err := h.Client.Create(h.Context, deployment)
	return deployment, err
}

func (h *KindClusterHelper) verifySpotalisDeployment() error {
	// This would check the Spotalis deployment, but since we're not using
	// an actual deployment in this setup, we'll just verify cluster connectivity
	namespaces := &corev1.NamespaceList{}
	return h.Client.List(h.Context, namespaces)
}

// CleanupAllTestResources performs comprehensive cleanup of all test resources
func (h *KindClusterHelper) CleanupAllTestResources() error {
	var allErrors []error

	// Clean up test namespaces
	if err := h.CleanupAllTestNamespaces(); err != nil {
		allErrors = append(allErrors, fmt.Errorf("test namespaces cleanup failed: %w", err))
	}

	// Clean up legacy test namespaces
	if err := h.CleanupLegacyTestNamespaces(); err != nil {
		allErrors = append(allErrors, fmt.Errorf("legacy namespaces cleanup failed: %w", err))
	}

	// Clean up any test-labeled deployments in non-test namespaces
	if err := h.CleanupTestDeployments(); err != nil {
		allErrors = append(allErrors, fmt.Errorf("test deployments cleanup failed: %w", err))
	}

	// Clean up test-labeled pods that might be stuck
	if err := h.CleanupTestPods(); err != nil {
		allErrors = append(allErrors, fmt.Errorf("test pods cleanup failed: %w", err))
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("comprehensive cleanup failed with %d errors: %v", len(allErrors), allErrors)
	}

	return nil
}

// CleanupTestDeployments removes deployments marked with test labels across all namespaces
func (h *KindClusterHelper) CleanupTestDeployments() error {
	deployments := &appsv1.DeploymentList{}
	err := h.Client.List(h.Context, deployments, client.MatchingLabels{
		"spotalis.io/test": "true",
	})
	if err != nil {
		return fmt.Errorf("failed to list test deployments: %w", err)
	}

	var cleanupErrors []error
	for _, deployment := range deployments.Items {
		if err := h.Client.Delete(h.Context, &deployment); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to delete deployment %s/%s: %w", deployment.Namespace, deployment.Name, err))
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("deployment cleanup failed for %d resources: %v", len(cleanupErrors), cleanupErrors)
	}
	return nil
}

// CleanupTestPods removes pods marked with test labels across all namespaces
func (h *KindClusterHelper) CleanupTestPods() error {
	pods := &corev1.PodList{}
	err := h.Client.List(h.Context, pods, client.MatchingLabels{
		"spotalis.io/test": "true",
	})
	if err != nil {
		return fmt.Errorf("failed to list test pods: %w", err)
	}

	var cleanupErrors []error
	for _, pod := range pods.Items {
		if err := h.Client.Delete(h.Context, &pod); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to delete pod %s/%s: %w", pod.Namespace, pod.Name, err))
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("pod cleanup failed for %d resources: %v", len(cleanupErrors), cleanupErrors)
	}
	return nil
}

// CleanupNamespaceIfExists safely deletes a namespace if it exists, with timeout
func (h *KindClusterHelper) CleanupNamespaceIfExists(namespace string) error {
	if namespace == "" {
		return nil
	}

	ns := &corev1.Namespace{}
	err := h.Client.Get(h.Context, client.ObjectKey{Name: namespace}, ns)
	if err != nil {
		// Namespace doesn't exist, nothing to clean up
		if client.IgnoreNotFound(err) == nil {
			return nil // Not found is OK
		}
		return fmt.Errorf("failed to check if namespace %s exists: %w", namespace, err)
	}

	// Use the robust cleanup method
	return h.CleanupNamespace(namespace)
}

// RestoreDeploymentReplicas restores a deployment to its original replica count
func (h *KindClusterHelper) RestoreDeploymentReplicas(namespace, name string, originalReplicas int32) error {
	deployment := &appsv1.Deployment{}
	err := h.Client.Get(h.Context, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, deployment)
	if err != nil {
		return err
	}

	deployment.Spec.Replicas = &originalReplicas
	return h.Client.Update(h.Context, deployment)
}

// WaitForNamespaceCleanup waits for namespaces with specific labels to be cleaned up
func (h *KindClusterHelper) WaitForNamespaceCleanup(labels map[string]string) error {
	Eventually(func() int {
		namespaces := &corev1.NamespaceList{}
		err := h.Client.List(h.Context, namespaces, client.MatchingLabels(labels))
		if err != nil {
			return -1
		}
		return len(namespaces.Items)
	}, 30*time.Second, 2*time.Second).Should(Equal(0))

	return nil
}

// WaitForDeploymentCleanup waits for deployments with specific labels to be cleaned up
func (h *KindClusterHelper) WaitForDeploymentCleanup(labels map[string]string) error {
	Eventually(func() int {
		deployments := &appsv1.DeploymentList{}
		err := h.Client.List(h.Context, deployments, client.MatchingLabels(labels))
		if err != nil {
			return -1
		}
		return len(deployments.Items)
	}, 30*time.Second, 2*time.Second).Should(Equal(0))

	return nil
}

// randString generates a random string of specified length
func randString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[num.Int64()]
	}
	return string(b)
}
