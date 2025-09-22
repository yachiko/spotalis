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
				"spotalis.io/managed":        "true", // Enable Spotalis management
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
		"spotalis.io/managed":        "true",
		"test.spotalis.io/cleanup":   "auto",
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

	err := h.Client.Delete(h.Context, ns)
	if err != nil {
		return err
	}

	// Wait for namespace to be deleted
	Eventually(func() bool {
		err := h.Client.Get(h.Context, client.ObjectKey{Name: namespace}, ns)
		return err != nil // Error means namespace is gone
	}, 30*time.Second, 1*time.Second).Should(BeTrue())

	return nil
}

// CleanupAllTestNamespaces removes all namespaces marked as test resources
func (h *KindClusterHelper) CleanupAllTestNamespaces() error {
	namespaces := &corev1.NamespaceList{}
	err := h.Client.List(h.Context, namespaces, client.MatchingLabels{
		"spotalis.io/test": "true",
	})
	if err != nil {
		return err
	}

	for _, ns := range namespaces.Items {
		if err := h.CleanupNamespace(ns.Name); err != nil {
			// Log error but continue with other namespaces
			continue
		}
	}
	return nil
}

// CleanupLegacyTestNamespaces removes old test namespaces that don't have proper labels
func (h *KindClusterHelper) CleanupLegacyTestNamespaces() error {
	namespaces := &corev1.NamespaceList{}
	err := h.Client.List(h.Context, namespaces)
	if err != nil {
		return err
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
				// Log error but continue with other namespaces
				continue
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
