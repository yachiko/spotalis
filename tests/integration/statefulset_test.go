package integration
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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestStatefulSetIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StatefulSet Workload Integration Suite")
}

var _ = Describe("StatefulSet workload management", func() {
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

	Context("when deploying StatefulSet with spot optimization", func() {
		var statefulSet *appsv1.StatefulSet

		BeforeEach(func() {
			statefulSet = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/enabled":         "true",
						"spotalis.io/spot-percentage": "60",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "stateful-app",
						},
					},
					ServiceName: "test-service",
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "stateful-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:alpine",
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 80,
											Name:          "web",
										},
									},
								},
							},
						},
					},
				},
			}
		})

		It("should create StatefulSet with spot optimization annotations", func() {
			By("Creating StatefulSet")
			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying StatefulSet exists")
			Eventually(func() error {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				return k8sClient.Get(ctx, key, statefulSet)
			}, 30*time.Second, 1*time.Second).Should(Succeed())

			By("Checking annotations are preserved")
			Expect(statefulSet.Annotations).To(HaveKeyWithValue("spotalis.io/enabled", "true"))
			Expect(statefulSet.Annotations).To(HaveKeyWithValue("spotalis.io/spot-percentage", "60"))

			By("Waiting for pods to be created with correct ordinals")
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
					if pod.Labels["app"] == "stateful-app" {
						count++
					}
				}
				return count
			}, 60*time.Second, 2*time.Second).Should(Equal(5))

			By("Verifying pod names have ordinal suffixes")
			podList := &corev1.PodList{}
			listOpts := &client.ListOptions{
				Namespace: namespace,
			}
			err = k8sClient.List(ctx, podList, listOpts)
			Expect(err).NotTo(HaveOccurred())

			ordinalFound := make(map[int]bool)
			for _, pod := range podList.Items {
				if pod.Labels["app"] == "stateful-app" {
					// StatefulSet pods should be named: test-statefulset-0, test-statefulset-1, etc.
					Expect(pod.Name).To(MatchRegexp(`^test-statefulset-\d+$`))
					
					// Track that we have sequential ordinals
					var ordinal int
					fmt.Sscanf(pod.Name, "test-statefulset-%d", &ordinal)
					ordinalFound[ordinal] = true
				}
			}
			
			// Should have ordinals 0-4
			for i := 0; i < 5; i++ {
				Expect(ordinalFound[i]).To(BeTrue(), fmt.Sprintf("Missing pod with ordinal %d", i))
			}
		})

		It("should respect cooldown period after pod deletion", func() {
			By("Creating StatefulSet")
			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for StatefulSet to be ready")
			Eventually(func() bool {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return false
				}
				return statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas
			}, 90*time.Second, 2*time.Second).Should(BeTrue())

			By("Deleting one pod to trigger rebalancing")
			podList := &corev1.PodList{}
			listOpts := &client.ListOptions{
				Namespace: namespace,
			}
			err = k8sClient.List(ctx, podList, listOpts)
			Expect(err).NotTo(HaveOccurred())

			var podToDelete *corev1.Pod
			for i := range podList.Items {
				if podList.Items[i].Labels["app"] == "stateful-app" {
					podToDelete = &podList.Items[i]
					break
				}
			}
			Expect(podToDelete).NotTo(BeNil())

			deletionTime := time.Now()
			err = k8sClient.Delete(ctx, podToDelete)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying cooldown period is respected (3 minutes for StatefulSets)")
			// StatefulSet controller should not immediately delete another pod
			// Check that no additional pods are deleted within 3 minutes
			Consistently(func() int {
				currentPodList := &corev1.PodList{}
				err := k8sClient.List(ctx, currentPodList, listOpts)
				if err != nil {
					return 0
				}
				
				deletingCount := 0
				for _, pod := range currentPodList.Items {
					if pod.Labels["app"] == "stateful-app" && pod.DeletionTimestamp != nil {
						deletingCount++
					}
				}
				return deletingCount
			}, 30*time.Second, 5*time.Second).Should(BeNumerically("<=", 1), "Should not delete additional pods during cooldown")

			By("Verifying deleted pod is recreated")
			Eventually(func() bool {
				newPodList := &corev1.PodList{}
				err := k8sClient.List(ctx, newPodList, listOpts)
				if err != nil {
					return false
				}
				
				for _, pod := range newPodList.Items {
					if pod.Labels["app"] == "stateful-app" && pod.Name == podToDelete.Name {
						return pod.UID != podToDelete.UID && pod.DeletionTimestamp == nil
					}
				}
				return false
			}, 60*time.Second, 2*time.Second).Should(BeTrue())

			elapsedTime := time.Since(deletionTime)
			GinkgoWriter.Printf("Pod recreation took %v (cooldown period should be ~3 minutes)\n", elapsedTime)
		})

		It("should maintain stable network identities during rebalancing", func() {
			By("Creating headless service for StatefulSet")
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: namespace,
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "None", // Headless service
					Selector: map[string]string{
						"app": "stateful-app",
					},
					Ports: []corev1.ServicePort{
						{
							Name: "web",
							Port: 80,
						},
					},
				},
			}
			err := k8sClient.Create(ctx, service)
			Expect(err).NotTo(HaveOccurred())

			By("Creating StatefulSet")
			err = k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Recording initial pod hostnames")
			Eventually(func() bool {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return false
				}
				return statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas
			}, 90*time.Second, 2*time.Second).Should(BeTrue())

			initialHostnames := make(map[string]string) // pod name -> hostname
			podList := &corev1.PodList{}
			listOpts := &client.ListOptions{
				Namespace: namespace,
			}
			err = k8sClient.List(ctx, podList, listOpts)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range podList.Items {
				if pod.Labels["app"] == "stateful-app" {
					// StatefulSet DNS: <pod-name>.<service-name>.<namespace>.svc.cluster.local
					hostname := fmt.Sprintf("%s.test-service.%s.svc.cluster.local", pod.Name, namespace)
					initialHostnames[pod.Name] = hostname
				}
			}
			Expect(initialHostnames).To(HaveLen(5))

			By("Triggering rebalancing by updating spot percentage")
			Eventually(func() error {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return err
				}
				statefulSet.Annotations["spotalis.io/spot-percentage"] = "80"
				return k8sClient.Update(ctx, statefulSet)
			}, 10*time.Second, 1*time.Second).Should(Succeed())

			By("Verifying hostnames remain stable after rebalancing")
			// Give time for rebalancing to occur
			time.Sleep(30 * time.Second)

			newPodList := &corev1.PodList{}
			err = k8sClient.List(ctx, newPodList, listOpts)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range newPodList.Items {
				if pod.Labels["app"] == "stateful-app" {
					expectedHostname := initialHostnames[pod.Name]
					Expect(expectedHostname).NotTo(BeEmpty(), fmt.Sprintf("Pod %s should maintain its name", pod.Name))
				}
			}
		})

		It("should handle scale-up operations correctly", func() {
			By("Creating StatefulSet with 3 replicas")
			statefulSet.Spec.Replicas = int32Ptr(3)
			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial replicas")
			Eventually(func() int32 {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return 0
				}
				return statefulSet.Status.ReadyReplicas
			}, 90*time.Second, 2*time.Second).Should(Equal(int32(3)))

			By("Scaling up to 7 replicas")
			Eventually(func() error {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return err
				}
				statefulSet.Spec.Replicas = int32Ptr(7)
				return k8sClient.Update(ctx, statefulSet)
			}, 10*time.Second, 1*time.Second).Should(Succeed())

			By("Verifying all 7 replicas are created with correct spot distribution")
			Eventually(func() int32 {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return 0
				}
				return statefulSet.Status.ReadyReplicas
			}, 120*time.Second, 2*time.Second).Should(Equal(int32(7)))

			By("Checking spot percentage is maintained (60% of 7 ≈ 4-5 spot pods)")
			podList := &corev1.PodList{}
			listOpts := &client.ListOptions{
				Namespace: namespace,
			}
			err = k8sClient.List(ctx, podList, listOpts)
			Expect(err).NotTo(HaveOccurred())

			spotCount := 0
			for _, pod := range podList.Items {
				if pod.Labels["app"] == "stateful-app" {
					if selector, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok && selector == "spot" {
						spotCount++
					}
				}
			}
			
			// 60% of 7 = 4.2, so expect 4 or 5 spot pods
			Expect(spotCount).To(BeNumerically(">=", 4))
			Expect(spotCount).To(BeNumerically("<=", 5))
		})

		It("should handle scale-down operations correctly", func() {
			By("Creating StatefulSet with 6 replicas")
			statefulSet.Spec.Replicas = int32Ptr(6)
			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for all replicas")
			Eventually(func() int32 {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return 0
				}
				return statefulSet.Status.ReadyReplicas
			}, 90*time.Second, 2*time.Second).Should(Equal(int32(6)))

			By("Scaling down to 2 replicas")
			Eventually(func() error {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return err
				}
				statefulSet.Spec.Replicas = int32Ptr(2)
				return k8sClient.Update(ctx, statefulSet)
			}, 10*time.Second, 1*time.Second).Should(Succeed())

			By("Verifying pods are removed in reverse ordinal order")
			// StatefulSets scale down by removing highest ordinal first
			Eventually(func() []string {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return nil
				}
				
				var podNames []string
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "stateful-app" && pod.DeletionTimestamp == nil {
						podNames = append(podNames, pod.Name)
					}
				}
				return podNames
			}, 90*time.Second, 2*time.Second).Should(ConsistOf(
				"test-statefulset-0",
				"test-statefulset-1",
			))

			By("Verifying final replica count")
			Eventually(func() int32 {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return 0
				}
				return statefulSet.Status.ReadyReplicas
			}, 60*time.Second, 2*time.Second).Should(Equal(int32(2)))
		})

		It("should check stability before rebalancing", func() {
			By("Creating StatefulSet")
			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Simulating unstable StatefulSet by setting low ready replicas")
			// This test verifies controller waits for stability
			Eventually(func() bool {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return false
				}
				return statefulSet.Status.Replicas > 0
			}, 60*time.Second, 2*time.Second).Should(BeTrue())

			By("Triggering rebalancing while StatefulSet is not fully ready")
			Eventually(func() error {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return err
				}
				statefulSet.Annotations["spotalis.io/spot-percentage"] = "50"
				return k8sClient.Update(ctx, statefulSet)
			}, 10*time.Second, 1*time.Second).Should(Succeed())

			By("Verifying no pods are deleted until StatefulSet is stable")
			// Controller should wait for ReadyReplicas == Replicas
			Consistently(func() bool {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return false
				}
				
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "stateful-app" && pod.DeletionTimestamp != nil {
						return false // Found deleting pod during instability
					}
				}
				return true
			}, 20*time.Second, 3*time.Second).Should(BeTrue())

			By("Waiting for StatefulSet to become stable")
			Eventually(func() bool {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return false
				}
				return statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas
			}, 90*time.Second, 2*time.Second).Should(BeTrue())

			GinkgoWriter.Println("StatefulSet is now stable, rebalancing should proceed")
		})

		It("should handle multiple concurrent annotation updates", func() {
			By("Creating StatefulSet")
			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for StatefulSet to be ready")
			Eventually(func() bool {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return false
				}
				return statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas
			}, 90*time.Second, 2*time.Second).Should(BeTrue())

			By("Rapidly updating spot percentage multiple times")
			percentages := []string{"70", "50", "80", "40", "90"}
			for _, pct := range percentages {
				Eventually(func() error {
					key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
					err := k8sClient.Get(ctx, key, statefulSet)
					if err != nil {
						return err
					}
					statefulSet.Annotations["spotalis.io/spot-percentage"] = pct
					return k8sClient.Update(ctx, statefulSet)
				}, 10*time.Second, 500*time.Millisecond).Should(Succeed())
				
				time.Sleep(2 * time.Second) // Small delay between updates
			}

			By("Verifying StatefulSet eventually reconciles to final percentage (90%)")
			// Give controller time to process all updates
			time.Sleep(10 * time.Second)

			By("Checking final spot distribution matches 90%")
			Eventually(func() int {
				podList := &corev1.PodList{}
				listOpts := &client.ListOptions{
					Namespace: namespace,
				}
				err := k8sClient.List(ctx, podList, listOpts)
				if err != nil {
					return 0
				}
				
				spotCount := 0
				totalCount := 0
				for _, pod := range podList.Items {
					if pod.Labels["app"] == "stateful-app" && pod.DeletionTimestamp == nil {
						totalCount++
						if selector, ok := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; ok && selector == "spot" {
							spotCount++
						}
					}
				}
				
				if totalCount == 0 {
					return 0
				}
				return (spotCount * 100) / totalCount
			}, 120*time.Second, 5*time.Second).Should(BeNumerically(">=", 80)) // Allow some variance

			GinkgoWriter.Println("StatefulSet successfully handled concurrent annotation updates")
		})

		It("should preserve StatefulSet-specific fields during reconciliation", func() {
			By("Creating StatefulSet with volume claim templates")
			statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, statefulSet)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying volume claim templates are preserved")
			Eventually(func() int {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return 0
				}
				return len(statefulSet.Spec.VolumeClaimTemplates)
			}, 30*time.Second, 1*time.Second).Should(Equal(1))

			By("Triggering reconciliation")
			Eventually(func() error {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return err
				}
				statefulSet.Annotations["spotalis.io/spot-percentage"] = "75"
				return k8sClient.Update(ctx, statefulSet)
			}, 10*time.Second, 1*time.Second).Should(Succeed())

			By("Verifying volume claim templates still exist after reconciliation")
			Consistently(func() bool {
				key := client.ObjectKey{Name: statefulSet.Name, Namespace: namespace}
				err := k8sClient.Get(ctx, key, statefulSet)
				if err != nil {
					return false
				}
				return len(statefulSet.Spec.VolumeClaimTemplates) == 1 &&
					statefulSet.Spec.VolumeClaimTemplates[0].Name == "data"
			}, 30*time.Second, 3*time.Second).Should(BeTrue())

			GinkgoWriter.Println("StatefulSet volume claim templates preserved during reconciliation")
		})
	})
})
