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

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var _ = Describe("Manual Testing Scenarios", func() {

	var (
		ctx       context.Context
		k8sClient client.Client
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "spotalis-test-scenarios"

		cfg, err := config.GetConfig()
		if err != nil {
			Skip("No kubeconfig available")
		}

		k8sClient, err = client.New(cfg, client.Options{})
		Expect(err).ToNot(HaveOccurred())

		// Create test namespace with spotalis enabled
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"spotalis.io/enabled": "true",
				},
			},
		}
		err = k8sClient.Create(ctx, ns)
		if err != nil {
			// Namespace might already exist
			fmt.Printf("Namespace creation result: %v\n", err)
		}
	})

	AfterEach(func() {
		// Cleanup test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		err := k8sClient.Delete(ctx, ns)
		if err != nil {
			fmt.Printf("Namespace cleanup warning: %v\n", err)
		}
	})

	Describe("Scenario 1: Basic Workload Deployment", func() {
		It("should deploy workload with annotation-based configuration", func() {
			By("Creating a deployment with Spotalis annotations")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage":   "70",
						"spotalis.io/min-on-demand":     "2",
						"spotalis.io/max-replicas":      "20",
						"spotalis.io/disruption-policy": `{"maxUnavailable": "25%", "gracefulShutdownTimeout": "30s"}`,
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(10),
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
									Name:  "nginx",
									Image: "nginx:latest",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("128Mi"),
										},
									},
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying deployment is created successfully")
			var createdDeployment appsv1.Deployment
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &createdDeployment)
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Checking that deployment has correct annotations")
			Expect(createdDeployment.Annotations).To(HaveKey("spotalis.io/spot-percentage"))
			Expect(createdDeployment.Annotations["spotalis.io/spot-percentage"]).To(Equal("70"))

			By("Waiting for pods to be scheduled")
			Eventually(func() int {
				podList := &corev1.PodList{}
				selector, _ := labels.Parse("app=test-app")
				err := k8sClient.List(ctx, podList, &client.ListOptions{
					Namespace:     namespace,
					LabelSelector: selector,
				})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 2*time.Minute, 10*time.Second).Should(BeNumerically(">=", 5))

			By("Validating pod distribution (manual inspection)")
			podList := &corev1.PodList{}
			selector, _ := labels.Parse("app=test-app")
			err = k8sClient.List(ctx, podList, &client.ListOptions{
				Namespace:     namespace,
				LabelSelector: selector,
			})
			Expect(err).ToNot(HaveOccurred())

			spotPods := 0
			onDemandPods := 0
			for _, pod := range podList.Items {
				if pod.Spec.NodeName != "" {
					// Get node to check its labels
					node := &corev1.Node{}
					err := k8sClient.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node)
					if err == nil {
						nodeLabels := node.GetLabels()
						if lifecycle, exists := nodeLabels["node.kubernetes.io/lifecycle"]; exists {
							if lifecycle == "spot" {
								spotPods++
							} else if lifecycle == "normal" {
								onDemandPods++
							}
						} else if capacity, exists := nodeLabels["karpenter.sh/capacity-type"]; exists {
							if capacity == "spot" {
								spotPods++
							} else if capacity == "on-demand" {
								onDemandPods++
							}
						}
					}
				}
			}

			fmt.Printf("Pod distribution: %d spot, %d on-demand (total: %d)\n",
				spotPods, onDemandPods, len(podList.Items))

			// Manual validation points
			By("Manual validation points")
			fmt.Println("\n=== MANUAL VALIDATION REQUIRED ===")
			fmt.Printf("1. Verify %d on-demand pods (should be >= 2 per min-on-demand annotation)\n", onDemandPods)
			fmt.Printf("2. Verify spot percentage is approximately 70%% (actual: %.1f%%)\n",
				float64(spotPods)/float64(spotPods+onDemandPods)*100)
			fmt.Println("3. Check that pods are scheduled on appropriate node types")
			fmt.Println("4. Verify webhook is functioning (pods should have nodeSelector/affinity)")
			fmt.Println("===================================")
		})
	})

	Describe("Scenario 2: Configuration Changes", func() {
		It("should handle annotation updates correctly", func() {
			By("Creating initial deployment")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test-app",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "80",
						"spotalis.io/min-on-demand":   "1",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(6),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "config-test-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "config-test-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for initial deployment to be ready")
			Eventually(func() bool {
				var dep appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &dep)
				return err == nil && dep.Status.ReadyReplicas >= 3
			}, 2*time.Minute, 10*time.Second).Should(BeTrue())

			By("Updating spot percentage annotation")
			var dep appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &dep)
			Expect(err).ToNot(HaveOccurred())

			dep.Annotations["spotalis.io/spot-percentage"] = "50"
			err = k8sClient.Update(ctx, &dep)
			Expect(err).ToNot(HaveOccurred())

			By("Manual validation for configuration change")
			fmt.Println("\n=== CONFIGURATION CHANGE VALIDATION ===")
			fmt.Println("1. Updated spot percentage from 80% to 50%")
			fmt.Println("2. Monitor controller logs for reconciliation events")
			fmt.Println("3. Verify that replica distribution adjusts over time")
			fmt.Println("4. Check that disruption policy is respected during changes")
			fmt.Println("==========================================")
		})
	})

	Describe("Scenario 3: Scaling Operations", func() {
		It("should maintain distribution during scale up and down", func() {
			By("Creating base deployment")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scale-test-app",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "60",
						"spotalis.io/min-on-demand":   "2",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "scale-test-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "scale-test-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())

			By("Scaling up to 15 replicas")
			var dep appsv1.Deployment
			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &dep)
				if err != nil {
					return err
				}
				dep.Spec.Replicas = int32Ptr(15)
				return k8sClient.Update(ctx, &dep)
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Waiting for scale up to complete")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &dep)
				return err == nil && dep.Status.ReadyReplicas >= 10
			}, 3*time.Minute, 15*time.Second).Should(BeTrue())

			By("Scaling down to 3 replicas")
			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &dep)
				if err != nil {
					return err
				}
				dep.Spec.Replicas = int32Ptr(3)
				return k8sClient.Update(ctx, &dep)
			}, 30*time.Second, 5*time.Second).Should(Succeed())

			By("Manual validation for scaling operations")
			fmt.Println("\n=== SCALING OPERATIONS VALIDATION ===")
			fmt.Println("1. Verify distribution maintained during scale-up (60% spot, 2+ on-demand)")
			fmt.Println("2. Check that new pods follow the same distribution rules")
			fmt.Println("3. During scale-down, verify graceful termination")
			fmt.Println("4. Ensure minimum on-demand replicas (2) are preserved")
			fmt.Println("5. Monitor for any admission webhook failures during scaling")
			fmt.Println("=====================================")
		})
	})

	Describe("Scenario 4: StatefulSet Support", func() {
		It("should handle StatefulSet workloads correctly", func() {
			By("Creating StatefulSet with Spotalis annotations")
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stateful-test-app",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage":   "40",
						"spotalis.io/min-on-demand":     "1",
						"spotalis.io/disruption-policy": `{"maxUnavailable": "1", "gracefulShutdownTimeout": "60s"}`,
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "stateful-test-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "stateful-test-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for StatefulSet to be ready")
			Eventually(func() bool {
				var sts appsv1.StatefulSet
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), &sts)
				return err == nil && sts.Status.ReadyReplicas >= 3
			}, 3*time.Minute, 15*time.Second).Should(BeTrue())

			By("Manual validation for StatefulSet")
			fmt.Println("\n=== STATEFULSET VALIDATION ===")
			fmt.Println("1. Verify StatefulSet pods are created with proper ordering")
			fmt.Println("2. Check that spot percentage (40%) is respected")
			fmt.Println("3. Ensure minimum on-demand replica (1) is maintained")
			fmt.Println("4. Verify ordered scaling behavior is preserved")
			fmt.Println("5. Check that persistent volumes are properly handled")
			fmt.Println("===============================")
		})
	})

	Describe("Scenario 5: Multi-Namespace Validation", func() {
		It("should respect namespace filtering", func() {
			By("Creating namespace without Spotalis enabled")
			disabledNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spotalis-disabled",
					// No spotalis.io/enabled label
				},
			}
			err := k8sClient.Create(ctx, disabledNs)
			Expect(err).ToNot(HaveOccurred())

			defer func() {
				k8sClient.Delete(ctx, disabledNs)
			}()

			By("Creating deployment in disabled namespace")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ignored-app",
					Namespace: "spotalis-disabled",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "ignored-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "ignored-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}

			err = k8sClient.Create(ctx, deployment)
			Expect(err).ToNot(HaveOccurred())

			By("Manual validation for namespace filtering")
			fmt.Println("\n=== NAMESPACE FILTERING VALIDATION ===")
			fmt.Println("1. Verify that ignored-app deployment is NOT managed by Spotalis")
			fmt.Println("2. Check that pods in 'spotalis-disabled' namespace have NO nodeSelector")
			fmt.Println("3. Confirm webhook does NOT intercept pods in disabled namespace")
			fmt.Println("4. Compare with enabled namespace behavior")
			fmt.Println("=====================================")
		})
	})
})

// Helper function to create int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
