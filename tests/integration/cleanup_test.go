//go:build integration
// +build integration

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

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/ahoma/spotalis/tests/integration/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCleanupIntegration(t *testing.T) {
	// Set up logger to avoid controller-runtime warning
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Cleanup Integration Suite", Label("integration"))
}

var _ = Describe("Resource cleanup mechanisms", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		kindHelper *shared.KindClusterHelper
		k8sClient  client.Client
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		// Connect to existing Kind cluster
		var err error
		kindHelper, err = shared.NewKindClusterHelper(ctx)
		Expect(err).NotTo(HaveOccurred())

		k8sClient = kindHelper.Client

		// Wait for Spotalis controller to be ready
		kindHelper.WaitForSpotalisController()
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Context("when cleaning up test resources", func() {
		var (
			testNamespaces  []string
			testDeployments []string
		)

		BeforeEach(func() {
			// Create test resources to clean up
			testNamespaces = make([]string, 0, 3)
			testDeployments = make([]string, 0, 3)

			// Create test namespaces
			for i := 0; i < 3; i++ {
				namespace, err := kindHelper.CreateTestNamespace()
				Expect(err).NotTo(HaveOccurred())
				testNamespaces = append(testNamespaces, namespace)

				// Create test deployment in namespace
				deployment, err := kindHelper.CreateTestDeployment(
					namespace,
					"cleanup-test-deployment",
					1,
					map[string]string{
						"spotalis.io/enabled":         "true",
						"spotalis.io/spot-percentage": "50",
					},
				)
				Expect(err).NotTo(HaveOccurred())
				testDeployments = append(testDeployments, deployment.Name)
			}
		})

		It("should clean up individual test namespaces", func() {
			// Verify resources exist
			for _, namespace := range testNamespaces {
				ns := &corev1.Namespace{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns)
				Expect(err).NotTo(HaveOccurred())
			}

			// Clean up each namespace individually
			for _, namespace := range testNamespaces {
				err := kindHelper.CleanupNamespace(namespace)
				Expect(err).NotTo(HaveOccurred())

				// Verify namespace is deleted
				Eventually(func() bool {
					ns := &corev1.Namespace{}
					err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, ns)
					return err != nil // Error means namespace is gone
				}, 30*time.Second, 1*time.Second).Should(BeTrue())
			}

			// Clear the list since we've cleaned them up
			testNamespaces = []string{}
		})

		It("should clean up all test namespaces at once", func() {
			// Add additional test namespaces with different patterns
			legacyManagedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-" + randString(6),
					Labels: map[string]string{
						"spotalis.io/test": "true",
					},
				},
			}
			err := k8sClient.Create(ctx, legacyManagedNS)
			Expect(err).NotTo(HaveOccurred())

			legacyUnmanagedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unmanaged-" + randString(6),
					Labels: map[string]string{
						"spotalis.io/test": "true",
					},
				},
			}
			err = k8sClient.Create(ctx, legacyUnmanagedNS)
			Expect(err).NotTo(HaveOccurred())

			// Verify all test namespaces exist
			namespaces := &corev1.NamespaceList{}
			err = k8sClient.List(ctx, namespaces, client.MatchingLabels{
				"spotalis.io/test": "true",
			})
			Expect(err).NotTo(HaveOccurred())
			initialCount := len(namespaces.Items)
			Expect(initialCount).To(BeNumerically(">=", 5)) // At least our 5 test namespaces

			// Clean up all test namespaces
			err = kindHelper.CleanupAllTestNamespaces()
			Expect(err).NotTo(HaveOccurred())

			// Verify all test namespaces are deleted
			Eventually(func() int {
				namespaces := &corev1.NamespaceList{}
				err := k8sClient.List(ctx, namespaces, client.MatchingLabels{
					"spotalis.io/test": "true",
				})
				if err != nil {
					return -1
				}
				return len(namespaces.Items)
			}, 60*time.Second, 2*time.Second).Should(Equal(0))

			// Clear the list since we've cleaned them up
			testNamespaces = []string{}
		})

		It("should clean up legacy test namespaces", func() {
			// Create legacy test namespaces without proper labels
			legacyNamespaces := []string{
				"managed-" + randString(6),
				"unmanaged-" + randString(6),
				"spotalis-managed-" + randString(6),
			}

			for _, name := range legacyNamespaces {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
						// Intentionally no test labels to simulate legacy namespaces
					},
				}
				err := k8sClient.Create(ctx, ns)
				Expect(err).NotTo(HaveOccurred())
			}

			// Clean up legacy test namespaces
			err := kindHelper.CleanupLegacyTestNamespaces()
			Expect(err).NotTo(HaveOccurred())

			// Verify legacy namespaces are deleted
			for _, name := range legacyNamespaces {
				Eventually(func() bool {
					ns := &corev1.Namespace{}
					err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
					return err != nil // Error means namespace is gone
				}, 30*time.Second, 1*time.Second).Should(BeTrue())
			}
		})

		It("should clean up all test resources comprehensively", func() {
			// Create additional resources outside of namespaces
			// (Note: In practice, deployments must be in namespaces, but we test the pattern)

			// Perform comprehensive cleanup
			err := kindHelper.CleanupAllTestResources()
			Expect(err).NotTo(HaveOccurred())

			// Verify all test resources are cleaned up
			Eventually(func() int {
				namespaces := &corev1.NamespaceList{}
				err := k8sClient.List(ctx, namespaces, client.MatchingLabels{
					"spotalis.io/test": "true",
				})
				if err != nil {
					return -1
				}
				return len(namespaces.Items)
			}, 60*time.Second, 2*time.Second).Should(Equal(0))

			// Clear the lists since we've cleaned them up
			testNamespaces = []string{}
			testDeployments = []string{}
		})

		AfterEach(func() {
			// Emergency cleanup - remove any remaining test resources
			for _, namespace := range testNamespaces {
				if namespace != "" {
					err := kindHelper.CleanupNamespaceIfExists(namespace)
					if err != nil {
						GinkgoWriter.Printf("Warning: Emergency cleanup failed for namespace %s: %v\n", namespace, err)
						// Don't fail the test here as this is emergency cleanup
					}
				}
			}
		})
	})

	Context("when handling cleanup edge cases", func() {
		It("should handle cleanup of non-existent namespaces gracefully", func() {
			// Try to clean up a namespace that doesn't exist
			err := kindHelper.CleanupNamespaceIfExists("non-existent-namespace-" + randString(8))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle cleanup when no test resources exist", func() {
			// Clean up when there are no test resources
			err := kindHelper.CleanupAllTestNamespaces()
			Expect(err).NotTo(HaveOccurred())

			err = kindHelper.CleanupLegacyTestNamespaces()
			Expect(err).NotTo(HaveOccurred())

			err = kindHelper.CleanupAllTestResources()
			Expect(err).NotTo(HaveOccurred())
		})

		It("should preserve system namespaces during cleanup", func() {
			systemNamespaces := []string{
				"default",
				"kube-system",
				"kube-public",
				"kube-node-lease",
				"spotalis-system",
			}

			// Verify system namespaces exist before cleanup
			for _, name := range systemNamespaces {
				ns := &corev1.Namespace{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
				Expect(err).NotTo(HaveOccurred())
			}

			// Perform cleanup
			err := kindHelper.CleanupAllTestResources()
			Expect(err).NotTo(HaveOccurred())

			// Verify system namespaces still exist after cleanup
			for _, name := range systemNamespaces {
				ns := &corev1.Namespace{}
				err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})
})
