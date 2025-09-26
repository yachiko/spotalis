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
	"fmt"
	"testing"
	"time"

	"github.com/ahoma/spotalis/tests/integration/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNamespaceFilteringIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Namespace Filtering Integration Suite")
}

var _ = Describe("Multi-tenant namespace filtering", func() {
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

		// Kind cluster already has Spotalis controller running with namespace filtering
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Context("with namespace filtering enabled", func() {
		var (
			managedNamespace   string
			unmanagedNamespace string
			deployment         *appsv1.Deployment
		)

		BeforeEach(func() {
			// Create managed namespace
			managedNamespace = "spotalis-managed-" + randString(6)
			managedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedNamespace,
					Labels: map[string]string{
						"test-managed":        "true",
						"spotalis.io/enabled": "true",
					},
				},
			}
			err := k8sClient.Create(ctx, managedNS)
			Expect(err).NotTo(HaveOccurred())

			// Create unmanaged namespace
			unmanagedNamespace = "unmanaged-" + randString(6)
			unmanagedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: unmanagedNamespace,
					Labels: map[string]string{
						"tenant": "team-b",
					},
				},
			}
			err = k8sClient.Create(ctx, unmanagedNS)
			Expect(err).NotTo(HaveOccurred())

			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-deployment",
					Annotations: map[string]string{
						"spotalis.io/enabled":         "true",
						"spotalis.io/spot-percentage": "70%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
		})

		It("should manage workloads only in labeled namespaces", func() {
			// Deploy to managed namespace
			managedDeployment := deployment.DeepCopy()
			managedDeployment.Namespace = managedNamespace
			managedDeployment.Name = "managed-deployment"

			err := k8sClient.Create(ctx, managedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait 20 seconds for pods to be created and processed by Spotalis webhook
			time.Sleep(20 * time.Second)

			// Check that pods in the managed namespace DO have Spotalis-specific nodeSelector
			podList := &corev1.PodList{}
			err = k8sClient.List(ctx, podList, client.InNamespace(managedNamespace))
			Expect(err).NotTo(HaveOccurred())

			// Verify that pods have been processed by Spotalis webhook
			Expect(len(podList.Items)).To(BeNumerically(">", 0), "Expected pods to be created by K8s deployment controller")

			// At least one pod should have Spotalis-specific node selectors
			hasSpotalisNodeSelector := false
			for _, pod := range podList.Items {
				fmt.Printf("Managed pod %s nodeSelector: %v\n", pod.Name, pod.Spec.NodeSelector)

				if pod.Spec.NodeSelector != nil {
					// Check for Spotalis-specific node selectors
					if _, hasCapacityType := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; hasCapacityType {
						hasSpotalisNodeSelector = true
						break
					}
				}
			}

			Expect(hasSpotalisNodeSelector).To(BeTrue(), "Expected at least one pod to have Spotalis-specific nodeSelector in managed namespace")
		})

		It("should ignore workloads in non-labeled namespaces", func() {
			// Deploy to unmanaged namespace
			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanagedNamespace
			unmanagedDeployment.Name = "unmanaged-deployment"

			err := k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait 20 seconds for pods to be created and stabilize
			time.Sleep(20 * time.Second)

			// Check that pods in the unmanaged namespace don't have Spotalis-specific nodeSelector
			podList := &corev1.PodList{}
			err = k8sClient.List(ctx, podList, client.InNamespace(unmanagedNamespace))
			Expect(err).NotTo(HaveOccurred())

			// Verify that no pods have Spotalis-specific node selectors
			for _, pod := range podList.Items {
				fmt.Printf("Pod %s nodeSelector: %v\n", pod.Name, pod.Spec.NodeSelector)

				if pod.Spec.NodeSelector != nil {
					// Pods should NOT have Spotalis-specific node selectors
					Expect(pod.Spec.NodeSelector).NotTo(HaveKey("karpenter.sh/capacity-type"))
					Expect(pod.Spec.NodeSelector).NotTo(HaveKey("spotalis.io/node-type"))
					Expect(pod.Spec.NodeSelector).NotTo(HaveKey("kubernetes.io/arch")) // Common Spotalis selector
				}
			}

			// Also verify that the pods list is not empty (deployment controller should create pods)
			Expect(len(podList.Items)).To(BeNumerically(">", 0), "Expected pods to be created by K8s deployment controller")
		})

		It("should not manage deployment with spotalis annotations in namespace without spotalis.io/enabled", func() {
			// This test verifies that the Spotalis controller respects namespace-level permissions.
			// Even if a deployment has valid Spotalis annotations, it should NOT be managed
			// if the namespace doesn't have the required "spotalis.io/enabled": "true" annotation.

			// Create a namespace without the required spotalis.io/enabled annotation
			restrictedNamespace := "restricted-" + randString(6)
			restrictedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: restrictedNamespace,
					Labels: map[string]string{
						"environment": "test",
						// Deliberately omitting "spotalis.io/enabled": "true"
					},
				},
			}
			err := k8sClient.Create(ctx, restrictedNS)
			Expect(err).NotTo(HaveOccurred())

			// Create a deployment with Spotalis annotations in the restricted namespace
			restrictedDeployment := deployment.DeepCopy()
			restrictedDeployment.Namespace = restrictedNamespace
			restrictedDeployment.Name = "restricted-deployment"
			restrictedDeployment.Annotations = map[string]string{
				"spotalis.io/enabled":         "true",
				"spotalis.io/spot-percentage": "80",
				"spotalis.io/min-on-demand":   "1",
			}

			err = k8sClient.Create(ctx, restrictedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify that Spotalis controller does NOT manage this deployment
			Consistently(func() bool {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(restrictedDeployment), &updated)
				if err != nil {
					return false
				}

				// Deployment should remain unprocessed by Spotalis:
				// 1. No Spotalis finalizers should be added
				// 2. Status should remain minimal (no managed replicas)
				// 3. No pods should be actively managed for spot/on-demand distribution
				hasSpotalisFinalizers := false
				for _, finalizer := range updated.Finalizers {
					if finalizer == "spotalis.io/finalizer" {
						hasSpotalisFinalizers = true
						break
					}
				}

				return !hasSpotalisFinalizers &&
					updated.Status.ObservedGeneration <= 1 // Should not be actively reconciled
			}, "15s", "2s").Should(BeTrue())

			// Also verify that pods (if any get created by K8s deployment controller)
			// don't get mutated with Spotalis-specific node selectors
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList, client.InNamespace(restrictedNamespace))
				if err != nil {
					return -1
				}

				// Count pods that have Spotalis-specific node selectors
				spotalisModifiedPods := 0
				for _, pod := range podList.Items {
					if pod.Spec.NodeSelector != nil {
						if _, hasCapacityType := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; hasCapacityType {
							spotalisModifiedPods++
						}
						if _, hasInstanceCategory := pod.Spec.NodeSelector["node.kubernetes.io/instance-type"]; hasInstanceCategory {
							spotalisModifiedPods++
						}
					}
				}
				return spotalisModifiedPods
			}, "10s", "2s").Should(Equal(0)) // No pods should be modified by Spotalis webhook
		})
	})
})
