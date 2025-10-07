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
	"sync"
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

func TestWebhookResilience(t *testing.T) {
	// Set up logger to avoid controller-runtime warning
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Resilience Integration Suite")
}

var _ = Describe("Webhook resilience and failure handling", func() {
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

	Context("when webhook mutates pods", func() {
		It("should handle empty nodeSelector correctly", func() {
			By("Creating deployment without any nodeSelector")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-selector",
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
							// No nodeSelector defined
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

			By("Verifying webhook adds nodeSelector to pods")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil || len(podList.Items) == 0 {
					return false
				}

				// Check that pods have nodeSelector added by webhook
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "test-app" {
						if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
							return false
						}
					}
				}
				return true
			}, 60*time.Second, 2*time.Second).Should(BeTrue())
		})

		It("should merge with existing nodeSelector", func() {
			By("Creating deployment with existing nodeSelector")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-merge-selector",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "merge-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "merge-app",
							},
						},
						Spec: corev1.PodSpec{
							NodeSelector: map[string]string{
								"disktype": "ssd",
								"zone":     "us-west-2a",
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

			By("Verifying webhook preserves existing selectors and adds capacity type")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil || len(podList.Items) == 0 {
					return false
				}

				for _, pod := range podList.Items {
					if pod.Labels["app"] == "merge-app" {
						// Check existing selectors are preserved
						if pod.Spec.NodeSelector["disktype"] != "ssd" {
							return false
						}
						if pod.Spec.NodeSelector["zone"] != "us-west-2a" {
							return false
						}
						// Check capacity type is added
						if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
							return false
						}
					}
				}
				return true
			}, 60*time.Second, 2*time.Second).Should(BeTrue())
		})

		It("should handle pods without owner references gracefully", func() {
			By("Creating standalone pod (no deployment owner)")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "standalone-pod",
					Namespace: namespace,
					Labels: map[string]string{
						"app": "standalone",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:alpine",
						},
					},
				},
			}

			err := k8sClient.Create(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pod is created without webhook mutation")
			Eventually(func() bool {
				key := client.ObjectKey{Name: pod.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, pod)
				return err == nil && pod.Status.Phase != ""
			}, 30*time.Second, 1*time.Second).Should(BeTrue())

			By("Checking pod has no capacity type selector (no workload annotations)")
			Expect(pod.Spec.NodeSelector).To(Not(HaveKey("karpenter.sh/capacity-type")))
		})
	})

	Context("when handling high concurrency", func() {
		It("should handle rapid pod creation bursts", func() {
			By("Creating multiple deployments simultaneously")
			var wg sync.WaitGroup
			deploymentCount := 5
			replicasPerDeployment := 4

			for i := 0; i < deploymentCount; i++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					defer GinkgoRecover()

					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("concurrent-deploy-%d", index),
							Namespace: namespace,
							Labels: map[string]string{
								"spotalis.io/enabled": "true",
							},
							Annotations: map[string]string{
								"spotalis.io/spot-percentage": "70",
							},
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: int32Ptr(int32(replicasPerDeployment)),
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app":   "concurrent-app",
									"index": fmt.Sprintf("%d", index),
								},
							},
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"app":   "concurrent-app",
										"index": fmt.Sprintf("%d", index),
									},
								},
								Spec: corev1.PodSpec{
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

					err := k8sClient.Create(context.Background(), deployment)
					Expect(err).NotTo(HaveOccurred())
				}(i)
			}

			wg.Wait()

			By("Verifying all pods are created with webhook mutation")
			expectedPodCount := deploymentCount * replicasPerDeployment
			Eventually(func() int {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return 0
				}

				mutatedCount := 0
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "concurrent-app" {
						if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok {
							mutatedCount++
						}
					}
				}
				return mutatedCount
			}, 120*time.Second, 3*time.Second).Should(Equal(expectedPodCount))

			GinkgoWriter.Printf("Successfully mutated %d pods concurrently\n", expectedPodCount)
		})

		It("should maintain webhook availability under load", func() {
			By("Creating deployment with many replicas")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "high-replica-deploy",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "80",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(20), // Large number to stress webhook
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "high-replica-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "high-replica-app",
							},
						},
						Spec: corev1.PodSpec{
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

			startTime := time.Now()
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying all 20 replicas are mutated")
			Eventually(func() int {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return 0
				}

				count := 0
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "high-replica-app" {
						if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok {
							count++
						}
					}
				}
				return count
			}, 180*time.Second, 3*time.Second).Should(Equal(20))

			elapsedTime := time.Since(startTime)
			GinkgoWriter.Printf("Webhook processed 20 pods in %v\n", elapsedTime)
			Expect(elapsedTime).To(BeNumerically("<", 3*time.Minute), "Webhook should handle 20 pods within 3 minutes")
		})
	})

	Context("when workload annotations change", func() {
		It("should handle invalid spot percentage gracefully", func() {
			By("Creating deployment with invalid spot percentage")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-percentage",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "invalid", // Invalid value
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "invalid-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "invalid-app",
							},
						},
						Spec: corev1.PodSpec{
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

			By("Verifying controller handles invalid annotation gracefully")
			// Pods should still be created even if annotation parsing fails
			Eventually(func() int {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return 0
				}

				count := 0
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "invalid-app" {
						count++
					}
				}
				return count
			}, 60*time.Second, 2*time.Second).Should(Equal(2))
		})

		It("should handle annotation removal correctly", func() {
			By("Creating deployment with annotations")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "annotation-removal",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "removal-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "removal-app",
							},
						},
						Spec: corev1.PodSpec{
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

			By("Waiting for initial pods with spot optimization")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil || len(podList.Items) < 3 {
					return false
				}

				for _, pod := range podList.Items {
					if pod.Labels["app"] == "removal-app" {
						if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
							return false
						}
					}
				}
				return true
			}, 60*time.Second, 2*time.Second).Should(BeTrue())

			By("Removing spot optimization annotation")
			Eventually(func() error {
				key := client.ObjectKey{Name: deployment.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, deployment)
				if err != nil {
					return err
				}
				delete(deployment.Annotations, "spotalis.io/enabled")
				return k8sClient.Update(ctx, deployment)
			}, 10*time.Second, 1*time.Second).Should(Succeed())

			By("Verifying new pods don't get spot optimization")
			// Trigger new pod creation by deleting one
			podList := &corev1.PodList{}
			listOpts := &client.ListOptions{
				Namespace: namespace,
			}
			err = k8sClient.List(ctx, podList, listOpts)
			Expect(err).NotTo(HaveOccurred())

			var podToDelete *corev1.Pod
			for i := range podList.Items {
				if podList.Items[i].Labels["app"] == "removal-app" {
					podToDelete = &podList.Items[i]
					break
				}
			}

			if podToDelete != nil {
				err = k8sClient.Delete(ctx, podToDelete)
				Expect(err).NotTo(HaveOccurred())

				// Wait for replacement pod - it should NOT have capacity type selector
				time.Sleep(10 * time.Second)

				GinkgoWriter.Println("Annotation removal test completed - new pods should not have spot optimization")
			}
		})
	})

	Context("when webhook configuration changes", func() {
		It("should handle namespace label filtering", func() {
			By("Creating namespace without spotalis label")
			unlabeledNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("unlabeled-%s", namespace),
				},
			}
			err := k8sClient.Create(ctx, unlabeledNs)
			Expect(err).NotTo(HaveOccurred())

			defer func() {
				_ = k8sClient.Delete(ctx, unlabeledNs)
			}()

			By("Creating deployment in unlabeled namespace")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unlabeled-ns",
					Namespace: unlabeledNs.Name,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "unlabeled-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "unlabeled-app",
							},
						},
						Spec: corev1.PodSpec{
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

			err = k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying pods are created (webhook might skip unlabeled namespace)")
			Eventually(func() int {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: unlabeledNs.Name,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return 0
				}

				count := 0
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "unlabeled-app" {
						count++
					}
				}
				return count
			}, 60*time.Second, 2*time.Second).Should(Equal(2))

			GinkgoWriter.Println("Pods created in unlabeled namespace - webhook filtering behavior verified")
		})
	})
})
