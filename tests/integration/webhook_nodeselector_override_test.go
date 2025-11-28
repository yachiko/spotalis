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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/tests/integration/shared"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestWebhookNodeSelectorOverride(t *testing.T) {
	// Set up logger to avoid controller-runtime warning
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook NodeSelector Override Integration Suite", Label("integration"))
}

var _ = Describe("Webhook NodeSelector Override Behavior", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		kindHelper *shared.KindClusterHelper
		k8sClient  client.Client
		namespace  string
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

		// Create test namespace
		namespace, err = kindHelper.CreateTestNamespace()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Cleanup namespace
		if namespace != "" && kindHelper != nil {
			err := kindHelper.CleanupNamespace(namespace)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to cleanup test namespace %s", namespace))
		}
		if cancel != nil {
			cancel()
		}
	})

	Context("when pod has existing karpenter.sh/capacity-type nodeSelector", func() {
		It("should override on-demand with spot when spot percentage requires it", func() {
			By("Creating deployment with 100% spot requirement but on-demand nodeSelector")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-override-ondemand-to-spot",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "100", // 100% spot requirement
						"spotalis.io/min-on-demand":   "0",   // No minimum on-demand
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "override-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "override-test",
							},
						},
						Spec: corev1.PodSpec{
							// Pod comes with on-demand, but should be overridden to spot
							NodeSelector: map[string]string{
								"karpenter.sh/capacity-type": "on-demand",
							},
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:alpine",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook overrides nodeSelector to spot")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "override-test"})
				if err != nil || len(podList.Items) == 0 {
					GinkgoWriter.Printf("Waiting for pods... err=%v, count=%d\n", err, len(podList.Items))
					return false
				}

				// All pods should have spot nodeSelector (100% spot requirement)
				spotCount := 0
				for _, pod := range podList.Items {
					if capacityType, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok {
						if capacityType == "spot" {
							spotCount++
						} else {
							GinkgoWriter.Printf("Pod %s has unexpected capacity type: %s (expected spot)\n", pod.Name, capacityType)
							return false
						}
					} else {
						GinkgoWriter.Printf("Pod %s missing karpenter.sh/capacity-type nodeSelector\n", pod.Name)
						return false
					}
				}

				GinkgoWriter.Printf("Found %d spot pods out of %d total\n", spotCount, len(podList.Items))
				return spotCount == 3 // All 3 replicas should be spot
			}, 90*time.Second, 3*time.Second).Should(BeTrue(), "All pods should have nodeSelector overridden to spot")
		})

		It("should override spot with on-demand when spot percentage is 0", func() {
			By("Creating deployment with 0% spot requirement but spot nodeSelector")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-override-spot-to-ondemand",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "0", // 0% spot requirement (all on-demand)
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "override-spot-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "override-spot-test",
							},
						},
						Spec: corev1.PodSpec{
							// Pod comes with spot, but should be overridden to on-demand
							NodeSelector: map[string]string{
								"karpenter.sh/capacity-type": "spot",
							},
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:alpine",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook overrides nodeSelector to on-demand")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "override-spot-test"})
				if err != nil || len(podList.Items) == 0 {
					GinkgoWriter.Printf("Waiting for pods... err=%v, count=%d\n", err, len(podList.Items))
					return false
				}

				// All pods should have on-demand nodeSelector (0% spot requirement)
				onDemandCount := 0
				for _, pod := range podList.Items {
					if capacityType, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok {
						if capacityType == "on-demand" {
							onDemandCount++
						} else {
							GinkgoWriter.Printf("Pod %s has unexpected capacity type: %s (expected on-demand)\n", pod.Name, capacityType)
							return false
						}
					} else {
						GinkgoWriter.Printf("Pod %s missing karpenter.sh/capacity-type nodeSelector\n", pod.Name)
						return false
					}
				}

				GinkgoWriter.Printf("Found %d on-demand pods out of %d total\n", onDemandCount, len(podList.Items))
				return onDemandCount == 2 // All 2 replicas should be on-demand
			}, 90*time.Second, 3*time.Second).Should(BeTrue(), "All pods should have nodeSelector overridden to on-demand")
		})

		It("should preserve other nodeSelector labels while overriding capacity type", func() {
			By("Creating deployment with multiple nodeSelector labels")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-preserve-other-labels",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "100", // Force spot
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "preserve-labels-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "preserve-labels-test",
							},
						},
						Spec: corev1.PodSpec{
							// Multiple nodeSelector labels
							NodeSelector: map[string]string{
								"karpenter.sh/capacity-type": "on-demand", // Should be overridden to spot
								"disktype":                   "ssd",       // Should be preserved
								"zone":                       "us-west",   // Should be preserved
								"custom-label":               "custom-value",
							},
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:alpine",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook overrides capacity-type but preserves other labels")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "preserve-labels-test"})
				if err != nil || len(podList.Items) == 0 {
					GinkgoWriter.Printf("Waiting for pods... err=%v, count=%d\n", err, len(podList.Items))
					return false
				}

				// Check all pods
				for _, pod := range podList.Items {
					// Should have spot (overridden)
					if capacityType, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok || capacityType != "spot" {
						GinkgoWriter.Printf("Pod %s missing spot capacity type: %v\n", pod.Name, pod.Spec.NodeSelector)
						return false
					}

					// Should preserve other labels
					if disktype, ok := pod.Spec.NodeSelector["disktype"]; !ok || disktype != "ssd" {
						GinkgoWriter.Printf("Pod %s missing disktype label\n", pod.Name)
						return false
					}
					if zone, ok := pod.Spec.NodeSelector["zone"]; !ok || zone != "us-west" {
						GinkgoWriter.Printf("Pod %s missing zone label\n", pod.Name)
						return false
					}
					if custom, ok := pod.Spec.NodeSelector["custom-label"]; !ok || custom != "custom-value" {
						GinkgoWriter.Printf("Pod %s missing custom-label\n", pod.Name)
						return false
					}
				}

				return len(podList.Items) == 2 // Both replicas should pass checks
			}, 90*time.Second, 3*time.Second).Should(BeTrue(), "All pods should have capacity type overridden while preserving other labels")
		})
	})

	Context("when pod has no existing nodeSelector", func() {
		It("should add nodeSelector with appropriate capacity type", func() {
			By("Creating deployment without nodeSelector")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-add-nodeselector",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "add-selector-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "add-selector-test",
							},
						},
						Spec: corev1.PodSpec{
							// No nodeSelector at all
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:alpine",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying webhook adds nodeSelector")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "add-selector-test"})
				if err != nil || len(podList.Items) == 0 {
					return false
				}

				// All pods should have nodeSelector added
				for _, pod := range podList.Items {
					if pod.Spec.NodeSelector == nil {
						return false
					}
					if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
						return false
					}
				}

				return len(podList.Items) == 3
			}, 90*time.Second, 3*time.Second).Should(BeTrue(), "All pods should have nodeSelector added")
		})
	})

	Context("dynamic rebalancing with nodeSelector override", func() {
		It("should override nodeSelector when rebalancing from on-demand to spot", func() {
			By("Creating deployment with initial on-demand pods")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rebalance-override",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "0", // Start with 0% spot
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "rebalance-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "rebalance-test",
							},
						},
						Spec: corev1.PodSpec{
							TerminationGracePeriodSeconds: int64Ptr(0), // Fast eviction for testing
							Tolerations: []corev1.Toleration{
								{
									Key:    "node-role.kubernetes.io/control-plane",
									Effect: corev1.TaintEffectNoSchedule,
								},
							},
							NodeSelector: map[string]string{
								"karpenter.sh/capacity-type": "on-demand",
							},
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:alpine",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all on-demand pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "rebalance-test"}); err != nil {
					return 0
				}
				return len(podList.Items)
			}, 90*time.Second, 3*time.Second).Should(Equal(3))

			By("Updating to 100% spot requirement")
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "test-rebalance-override"}, deployment)
			Expect(err).NotTo(HaveOccurred())

			deployment.Annotations["spotalis.io/spot-percentage"] = "100"
			err = k8sClient.Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying new pods have spot nodeSelector after rebalancing")
			// Note: This test verifies that when controller triggers pod replacements,
			// the webhook will correctly override the template's on-demand nodeSelector to spot
			Eventually(func() int {
				podList := &corev1.PodList{}
				if err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "rebalance-test"}); err != nil {
					return 0
				}

				spotCount := 0
				for _, pod := range podList.Items {
					if capacityType, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok && capacityType == "spot" {
						spotCount++
					}
				}
				GinkgoWriter.Printf("Spot pods during rebalancing: %d/%d\n", spotCount, len(podList.Items))
				return spotCount
			}, 180*time.Second, 5*time.Second).Should(Equal(3), "All pods should eventually be on spot after rebalancing")
		})
	})
})
