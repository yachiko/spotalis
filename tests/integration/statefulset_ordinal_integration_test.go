//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/yachiko/spotalis/tests/integration/shared"
)

var _ = Describe("StatefulSet Ordinal-Aware Deletion Integration Tests", func() {
	var (
		ctx           context.Context
		testNamespace string
		helper        *shared.KindClusterHelper
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		helper, err = shared.NewKindClusterHelper()
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kind cluster helper")

		testNamespace = fmt.Sprintf("ordinal-test-%d", time.Now().Unix())
		err = helper.CreateTestNamespace(ctx, testNamespace)
		Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")

		By(fmt.Sprintf("Created test namespace: %s", testNamespace))
	})

	AfterEach(func() {
		if testNamespace != "" {
			By(fmt.Sprintf("Cleaning up test namespace: %s", testNamespace))
			_ = helper.CleanupNamespace(ctx, testNamespace)
		}
	})

	Context("Ordinal-Aware Deletion Validation", func() {
		It("should create StatefulSet pods with sequential ordinals", func() {
			By("Creating StatefulSet with 5 replicas")
			sts := createOrdinalTestStatefulSet("ordinal-seq", 5, 60, 1, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "ordinal-seq")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(5))

			By("Verifying sequential ordinals 0-4")
			pods, err := getStatefulSetPods(ctx, helper, testNamespace, "ordinal-seq")
			Expect(err).NotTo(HaveOccurred())

			ordinals := extractOrdinals(pods)
			sort.Ints(ordinals)
			Expect(ordinals).To(Equal([]int{0, 1, 2, 3, 4}), "StatefulSet should create pods with ordinals 0-4")
		})

		It("should respect OrderedReady pod management policy during rebalancing", func() {
			By("Creating StatefulSet with OrderedReady policy")
			sts := createOrdinalTestStatefulSet("ordinal-ordered", 6, 50, 2, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be ready")
			Eventually(func() bool {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "ordinal-ordered")
				return count == 6
			}, 180*time.Second, 5*time.Second).Should(BeTrue())

			By("Waiting for node selectors to be applied")
			Eventually(func() bool {
				pods, _ := getStatefulSetPods(ctx, helper, testNamespace, "ordinal-ordered")
				return allStatefulSetPodsHaveNodeSelector(pods)
			}, 120*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying ordinal integrity after rebalancing")
			pods, err := getStatefulSetPods(ctx, helper, testNamespace, "ordinal-ordered")
			Expect(err).NotTo(HaveOccurred())

			ordinals := extractOrdinals(pods)
			sort.Ints(ordinals)
			Expect(ordinals).To(Equal([]int{0, 1, 2, 3, 4, 5}), "Ordinals should remain sequential after rebalancing")
		})

		It("should handle Parallel pod management policy correctly", func() {
			By("Creating StatefulSet with Parallel policy")
			sts := createOrdinalTestStatefulSet("ordinal-parallel", 4, 75, 1, appsv1.ParallelPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created (parallel)")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "ordinal-parallel")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(4))

			By("Verifying ordinals are correct despite parallel creation")
			pods, err := getStatefulSetPods(ctx, helper, testNamespace, "ordinal-parallel")
			Expect(err).NotTo(HaveOccurred())

			ordinals := extractOrdinals(pods)
			sort.Ints(ordinals)
			Expect(ordinals).To(Equal([]int{0, 1, 2, 3}), "Parallel creation should still maintain ordinal integrity")
		})

		It("should maintain ordinal integrity during spot/on-demand rebalancing", func() {
			By("Creating StatefulSet with 8 replicas and 75% spot")
			sts := createOrdinalTestStatefulSet("ordinal-rebalance", 8, 75, 2, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "ordinal-rebalance")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(8))

			By("Waiting for node selectors to be applied")
			Eventually(func() bool {
				pods, _ := getStatefulSetPods(ctx, helper, testNamespace, "ordinal-rebalance")
				return allStatefulSetPodsHaveNodeSelector(pods)
			}, 120*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying no gaps in ordinal sequence")
			pods, err := getStatefulSetPods(ctx, helper, testNamespace, "ordinal-rebalance")
			Expect(err).NotTo(HaveOccurred())

			ordinals := extractOrdinals(pods)
			sort.Ints(ordinals)
			Expect(ordinals).To(Equal([]int{0, 1, 2, 3, 4, 5, 6, 7}), "No ordinal gaps should exist after rebalancing")
		})
	})

	Context("StatefulSet Distribution Tests", func() {
		It("should achieve 75% spot distribution on 8-replica StatefulSet", func() {
			By("Creating StatefulSet with 8 replicas and 75% spot target")
			sts := createOrdinalTestStatefulSet("dist-75pct", 8, 75, 1, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "dist-75pct")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(8))

			By("Waiting for distribution to stabilize")
			Eventually(func() bool {
				pods, _ := getStatefulSetPods(ctx, helper, testNamespace, "dist-75pct")
				return allStatefulSetPodsHaveNodeSelector(pods)
			}, 120*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying spot/on-demand distribution")
			spotCount, onDemandCount := countStatefulSetDistribution(ctx, helper, testNamespace, "dist-75pct")

			// Expected: 75% of 8 = 6 spot, 2 on-demand (ceiling(8 * 0.75) = 6)
			// Allow +/- 1 pod tolerance for rounding
			Expect(spotCount).To(BeNumerically("~", 6, 1), "Should have ~6 spot pods")
			Expect(onDemandCount).To(BeNumerically(">=", 1), "Should have at least min-on-demand pods")
			Expect(spotCount+onDemandCount).To(Equal(8), "Total should be 8 pods")
		})

		It("should enforce min-on-demand constraint", func() {
			By("Creating StatefulSet with min-on-demand=2")
			sts := createOrdinalTestStatefulSet("dist-min-od", 5, 80, 2, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all pods to be created")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "dist-min-od")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(5))

			By("Waiting for distribution to stabilize")
			Eventually(func() bool {
				pods, _ := getStatefulSetPods(ctx, helper, testNamespace, "dist-min-od")
				return allStatefulSetPodsHaveNodeSelector(pods)
			}, 120*time.Second, 5*time.Second).Should(BeTrue())

			By("Verifying min-on-demand constraint is enforced")
			spotCount, onDemandCount := countStatefulSetDistribution(ctx, helper, testNamespace, "dist-min-od")

			// With 5 replicas, 80% spot, min-on-demand=2:
			// Target spot = ceil(5 * 0.8) = 4
			// Max spot = 5 - 2 = 3 (constrained by min-on-demand)
			// So: 3 spot, 2 on-demand
			Expect(onDemandCount).To(BeNumerically(">=", 2), "Should enforce min-on-demand=2")
			Expect(spotCount).To(BeNumerically("<=", 3), "Should not exceed max spot due to min-on-demand constraint")
			Expect(spotCount+onDemandCount).To(Equal(5), "Total should be 5 pods")
		})
	})

	Context("StatefulSet Scaling Tests", func() {
		It("should maintain ordinal assignment when scaling up", func() {
			By("Creating StatefulSet with 3 replicas")
			sts := createOrdinalTestStatefulSet("scale-up", 3, 60, 1, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial 3 pods")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "scale-up")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(3))

			By("Scaling up to 6 replicas")
			err = helper.ScaleStatefulSet(ctx, testNamespace, "scale-up", 6)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all 6 pods")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "scale-up")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(6))

			By("Verifying ordinals 0-5 exist")
			pods, err := getStatefulSetPods(ctx, helper, testNamespace, "scale-up")
			Expect(err).NotTo(HaveOccurred())

			ordinals := extractOrdinals(pods)
			sort.Ints(ordinals)
			Expect(ordinals).To(Equal([]int{0, 1, 2, 3, 4, 5}), "Should have ordinals 0-5 after scale-up")
		})

		It("should remove highest ordinals when scaling down", func() {
			By("Creating StatefulSet with 5 replicas")
			sts := createOrdinalTestStatefulSet("scale-down", 5, 60, 1, appsv1.OrderedReadyPodManagement)
			err := helper.CreateStatefulSet(ctx, testNamespace, sts)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all 5 pods")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "scale-down")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(5))

			By("Scaling down to 3 replicas")
			err = helper.ScaleStatefulSet(ctx, testNamespace, "scale-down", 3)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for scale-down to complete")
			Eventually(func() int {
				count, _ := countStatefulSetPods(ctx, helper, testNamespace, "scale-down")
				return count
			}, 180*time.Second, 5*time.Second).Should(Equal(3))

			By("Verifying only ordinals 0-2 remain")
			pods, err := getStatefulSetPods(ctx, helper, testNamespace, "scale-down")
			Expect(err).NotTo(HaveOccurred())

			ordinals := extractOrdinals(pods)
			sort.Ints(ordinals)
			Expect(ordinals).To(Equal([]int{0, 1, 2}), "Should keep lowest ordinals 0-2 after scale-down")
		})
	})
})

// Helper functions

func createOrdinalTestStatefulSet(name string, replicas int32, spotPercentage, minOnDemand int, podManagementPolicy appsv1.PodManagementPolicyType) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"spotalis.io/enabled":         "true",
				"spotalis.io/spot-percentage": fmt.Sprintf("%d", spotPercentage),
				"spotalis.io/min-on-demand":   fmt.Sprintf("%d", minOnDemand),
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			ServiceName:         name,
			PodManagementPolicy: podManagementPolicy,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.25",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Name: "web"},
							},
						},
					},
				},
			},
		},
	}
}

func countStatefulSetPods(ctx context.Context, helper *shared.KindClusterHelper, namespace, stsName string) (int, error) {
	pods, err := helper.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", stsName),
	})
	if err != nil {
		return 0, err
	}
	return len(pods.Items), nil
}

func getStatefulSetPods(ctx context.Context, helper *shared.KindClusterHelper, namespace, stsName string) ([]corev1.Pod, error) {
	podList, err := helper.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", stsName),
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func extractOrdinals(pods []corev1.Pod) []int {
	ordinals := make([]int, 0, len(pods))
	for _, pod := range pods {
		ordinal := extractPodOrdinal(pod.Name)
		if ordinal >= 0 {
			ordinals = append(ordinals, ordinal)
		}
	}
	return ordinals
}

func extractPodOrdinal(podName string) int {
	// Pod name format: <statefulset-name>-<ordinal>
	parts := strings.Split(podName, "-")
	if len(parts) < 2 {
		return -1
	}
	ordinalStr := parts[len(parts)-1]
	ordinal, err := strconv.Atoi(ordinalStr)
	if err != nil {
		return -1
	}
	return ordinal
}

func allStatefulSetPodsHaveNodeSelector(pods []corev1.Pod) bool {
	if len(pods) == 0 {
		return false
	}
	for _, pod := range pods {
		if pod.Spec.NodeSelector == nil {
			return false
		}
		if _, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; !ok {
			return false
		}
	}
	return true
}

func countStatefulSetDistribution(ctx context.Context, helper *shared.KindClusterHelper, namespace, stsName string) (spot, onDemand int) {
	pods, err := getStatefulSetPods(ctx, helper, namespace, stsName)
	if err != nil {
		return 0, 0
	}

	for _, pod := range pods {
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
	return spot, onDemand
}
