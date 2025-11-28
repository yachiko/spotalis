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

func TestBurstScenariosIntegration(t *testing.T) {
	// Set up logger to avoid controller-runtime warning
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Burst Scenarios Integration Suite", Label("integration", "burst"))
}

var _ = Describe("Burst Scenario Integration Tests", func() {
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

		// Wait for Spotalis controller to be ready
		kindHelper.WaitForSpotalisController()

		// Create test namespace with Spotalis enabled
		namespace, err = kindHelper.CreateTestNamespace()
		Expect(err).NotTo(HaveOccurred())

		k8sClient = kindHelper.Client
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

	// Tests for Task 001: Webhook Race Condition Fix & Task 005: Optimistic Concurrency
	Context("Webhook Admission Counter Validation (Task 001 & 005)", func() {
		It("should correctly distribute pods during 10-pod burst creation", func() {
			By("Creating a deployment with 10 replicas and 70% spot target")
			replicas := int32(10)
			spotPercentage := 70
			minOnDemand := 1
			deployment := createBurstTestDeployment("burst-10-70", namespace, replicas, spotPercentage, minOnDemand)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all 10 pods to be created")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "burst-10-70"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 120*time.Second, 3*time.Second).Should(Equal(10))

			By("Waiting for pods to have nodeSelector applied")
			Eventually(func() bool {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "burst-10-70"})
				if err != nil || len(podList.Items) == 0 {
					return false
				}
				for _, pod := range podList.Items {
					if pod.Spec.NodeSelector == nil {
						return false
					}
					if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
						return false
					}
				}
				return true
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying pod distribution is within tolerance")
			// Expected: 10 * 70% = 7 spot, min 1 on-demand → 7 spot, 3 on-demand
			// Allow +/- 1 tolerance for rounding
			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "burst-10-70")
			expectedSpot := calculateExpectedSpot(10, spotPercentage, minOnDemand)

			GinkgoWriter.Printf("Distribution: spot=%d, on-demand=%d (expected spot ~%d)\n",
				spotCount, onDemandCount, expectedSpot)

			// Spot count should be within tolerance
			Expect(spotCount).To(BeNumerically(">=", expectedSpot-1))
			Expect(spotCount).To(BeNumerically("<=", expectedSpot+1))
			// On-demand should meet minimum
			Expect(onDemandCount).To(BeNumerically(">=", minOnDemand))
		})

		It("should correctly handle 20-pod burst with 80% spot", func() {
			By("Creating deployment with 20 replicas and 80% spot target")
			replicas := int32(20)
			spotPercentage := 80
			minOnDemand := 2
			deployment := createBurstTestDeployment("burst-20-80", namespace, replicas, spotPercentage, minOnDemand)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all 20 pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "burst-20-80"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 180*time.Second, 3*time.Second).Should(Equal(20))

			By("Waiting for nodeSelectors to be applied")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "burst-20-80")
			}, 120*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying pod distribution")
			// Expected: 20 * 80% = 16 spot, min 2 on-demand → 16 spot, 4 on-demand
			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "burst-20-80")
			expectedSpot := calculateExpectedSpot(20, spotPercentage, minOnDemand)

			GinkgoWriter.Printf("20-pod burst distribution: spot=%d, on-demand=%d (expected spot ~%d)\n",
				spotCount, onDemandCount, expectedSpot)

			Expect(spotCount).To(BeNumerically(">=", expectedSpot-2)) // Allow +/- 2 for larger deployments
			Expect(spotCount).To(BeNumerically("<=", expectedSpot+2))
			Expect(onDemandCount).To(BeNumerically(">=", minOnDemand))
		})

		It("should handle 50/50 distribution correctly during burst", func() {
			By("Creating deployment with 50% spot target")
			replicas := int32(6)
			spotPercentage := 50
			minOnDemand := 1
			deployment := createBurstTestDeployment("burst-6-50", namespace, replicas, spotPercentage, minOnDemand)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "burst-6-50"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 120*time.Second, 3*time.Second).Should(Equal(6))

			By("Waiting for nodeSelectors")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "burst-6-50")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying 50/50 distribution")
			// Expected: 6 * 50% = 3 spot, 3 on-demand
			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "burst-6-50")
			GinkgoWriter.Printf("50/50 distribution: spot=%d, on-demand=%d\n", spotCount, onDemandCount)

			Expect(spotCount).To(BeNumerically(">=", 2))
			Expect(spotCount).To(BeNumerically("<=", 4))
			Expect(onDemandCount).To(BeNumerically(">=", minOnDemand))
		})
	})

	// Rapid Scaling Tests (Task 001 & 005)
	Context("Rapid Scaling Validation", func() {
		It("should maintain correct distribution during rapid scale-up", func() {
			By("Creating deployment with 1 replica")
			deployment := createBurstTestDeployment("rapid-scale", namespace, 1, 70, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial pod")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "rapid-scale"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 60*time.Second, 2*time.Second).Should(BeNumerically(">=", 1))

			By("Rapidly scaling to 10 replicas")
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "rapid-scale"}, deployment)
			Expect(err).NotTo(HaveOccurred())

			newReplicas := int32(10)
			deployment.Spec.Replicas = &newReplicas
			err = k8sClient.Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all 10 pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "rapid-scale"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 120*time.Second, 3*time.Second).Should(Equal(10))

			By("Waiting for all pods to have nodeSelector")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "rapid-scale")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			By("Verifying distribution after rapid scale")
			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "rapid-scale")
			GinkgoWriter.Printf("After rapid scale: spot=%d, on-demand=%d\n", spotCount, onDemandCount)

			// Should have ~7 spot (70% of 10), at least 1 on-demand
			Expect(spotCount).To(BeNumerically(">=", 5))
			Expect(spotCount).To(BeNumerically("<=", 8))
			Expect(onDemandCount).To(BeNumerically(">=", 1))
		})

		It("should handle rapid scale-up followed by immediate scale-down", func() {
			By("Creating deployment with 5 replicas")
			deployment := createBurstTestDeployment("rapid-up-down", namespace, 5, 60, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "rapid-up-down"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 90*time.Second, 2*time.Second).Should(Equal(5))

			By("Scaling up to 15")
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "rapid-up-down"}, deployment)
			Expect(err).NotTo(HaveOccurred())

			scaleUp := int32(15)
			deployment.Spec.Replicas = &scaleUp
			err = k8sClient.Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Immediately scaling back down to 8")
			time.Sleep(5 * time.Second) // Brief pause to allow some pods to be created

			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "rapid-up-down"}, deployment)
			Expect(err).NotTo(HaveOccurred())

			scaleDown := int32(8)
			deployment.Spec.Replicas = &scaleDown
			err = k8sClient.Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for final pod count to stabilize at 8")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "rapid-up-down"})
				if err != nil {
					return 0
				}
				// Count only non-terminating pods
				count := 0
				for _, pod := range podList.Items {
					if pod.DeletionTimestamp == nil {
						count++
					}
				}
				return count
			}, 180*time.Second, 3*time.Second).Should(Equal(8))

			By("Verifying distribution after stabilization")
			Eventually(func() bool {
				return allPodsHaveNodeSelector(ctx, k8sClient, namespace, "rapid-up-down")
			}, 90*time.Second, 3*time.Second).Should(BeTrue())

			spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "rapid-up-down")
			GinkgoWriter.Printf("Final distribution: spot=%d, on-demand=%d\n", spotCount, onDemandCount)

			// 60% of 8 = ~5 spot, at least 1 on-demand
			Expect(spotCount).To(BeNumerically(">=", 3))
			Expect(spotCount).To(BeNumerically("<=", 6))
			Expect(onDemandCount).To(BeNumerically(">=", 1))
		})
	})

	// Controller Convergence Tests
	Context("Controller Convergence Validation", func() {
		It("should converge to stable distribution after burst creation", func() {
			By("Creating burst deployment")
			deployment := createBurstTestDeployment("convergence-test", namespace, 10, 70, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "convergence-test"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 120*time.Second, 3*time.Second).Should(Equal(10))

			By("Waiting for distribution to stabilize")
			var lastSpot, lastOnDemand int
			stableCount := 0
			requiredStable := 3 // Distribution must be stable for 3 consecutive checks

			Eventually(func() bool {
				spotCount, onDemandCount := countPodDistribution(ctx, k8sClient, namespace, "convergence-test")

				if spotCount == lastSpot && onDemandCount == lastOnDemand {
					stableCount++
				} else {
					stableCount = 0
				}

				lastSpot = spotCount
				lastOnDemand = onDemandCount

				GinkgoWriter.Printf("Convergence check: spot=%d, on-demand=%d, stable=%d/%d\n",
					spotCount, onDemandCount, stableCount, requiredStable)

				return stableCount >= requiredStable
			}, 180*time.Second, 10*time.Second).Should(BeTrue(), "Distribution should converge to stable state")

			By("Verifying final converged distribution")
			GinkgoWriter.Printf("✓ Distribution converged: spot=%d, on-demand=%d\n", lastSpot, lastOnDemand)

			// Final distribution should be close to target
			expectedSpot := calculateExpectedSpot(10, 70, 1)
			Expect(lastSpot).To(BeNumerically(">=", expectedSpot-1))
			Expect(lastSpot).To(BeNumerically("<=", expectedSpot+1))
			Expect(lastOnDemand).To(BeNumerically(">=", 1))
		})

		It("should re-converge after annotation change", func() {
			By("Creating deployment with initial 50% spot")
			deployment := createBurstTestDeployment("reconverge-test", namespace, 8, 50, 1)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial pods")
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList,
					client.InNamespace(namespace),
					client.MatchingLabels{"app": "reconverge-test"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, 120*time.Second, 3*time.Second).Should(Equal(8))

			By("Waiting for initial distribution to stabilize")
			time.Sleep(30 * time.Second) // Give time for initial stabilization

			By("Changing spot percentage to 80%")
			err = k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "reconverge-test"}, deployment)
			Expect(err).NotTo(HaveOccurred())

			deployment.Annotations["spotalis.io/spot-percentage"] = "80"
			err = k8sClient.Update(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for distribution to re-converge to 80%")
			// With 80% spot on 8 pods: ceil(8 * 0.8) = 7 spot (with min 1 on-demand = 7 spot, 1 on-demand)
			Eventually(func() int {
				spotCount, _ := countPodDistribution(ctx, k8sClient, namespace, "reconverge-test")
				GinkgoWriter.Printf("Re-convergence check: spot=%d\n", spotCount)
				return spotCount
			}, 300*time.Second, 10*time.Second).Should(BeNumerically(">=", 5)) // At least 5 spot (80% target with tolerance)
		})
	})
})

// Helper functions for burst tests

// createBurstTestDeployment creates a deployment with Spotalis annotations
func createBurstTestDeployment(name, namespace string, replicas int32, spotPercentage, minOnDemand int) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":                 name,
				"spotalis.io/enabled": "true",
			},
			Annotations: map[string]string{
				"spotalis.io/spot-percentage": fmt.Sprintf("%d", spotPercentage),
				"spotalis.io/min-on-demand":   fmt.Sprintf("%d", minOnDemand),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: int64Ptr(0), // Fast termination for tests
					Tolerations: []corev1.Toleration{
						{
							Key:    "node-role.kubernetes.io/control-plane",
							Effect: corev1.TaintEffectNoSchedule,
						},
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
}

// countPodDistribution counts spot vs on-demand pods based on nodeSelector
func countPodDistribution(ctx context.Context, c client.Client, namespace, appLabel string) (spot, onDemand int) {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app": appLabel}); err != nil {
		return 0, 0
	}

	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue // Skip terminating pods
		}
		if pod.Spec.NodeSelector == nil {
			continue
		}
		capacityType := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]
		switch capacityType {
		case "spot":
			spot++
		case "on-demand":
			onDemand++
		}
	}

	return
}

// allPodsHaveNodeSelector checks if all pods have the capacity-type nodeSelector
func allPodsHaveNodeSelector(ctx context.Context, c client.Client, namespace, appLabel string) bool {
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app": appLabel}); err != nil {
		return false
	}

	if len(podList.Items) == 0 {
		return false
	}

	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue // Skip terminating pods
		}
		if pod.Spec.NodeSelector == nil {
			return false
		}
		if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
			return false
		}
	}
	return true
}

// calculateExpectedSpot calculates expected spot count using Spotalis algorithm
// Formula: min(ceil(total * spotPct / 100), total - minOnDemand)
func calculateExpectedSpot(total, spotPct, minOD int) int {
	// Ceiling division for target spot
	targetSpot := (total*spotPct + 99) / 100
	maxSpot := total - minOD
	if targetSpot > maxSpot {
		return maxSpot
	}
	return targetSpot
}
