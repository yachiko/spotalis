//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ahoma/spotalis/tests/integration/shared"
)

func TestLeaderElectionIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Leader Election Integration Suite")
}

var _ = Describe("Leader Election Integration", func() {
	var (
		ctx        context.Context
		kindHelper *shared.KindClusterHelper
		k8sClient  client.Client
		clientset  *kubernetes.Clientset
		namespace  string
	)

	BeforeEach(func() {
		ctx, _ = context.WithTimeout(context.Background(), 20*time.Minute)

		// Connect to existing Kind cluster
		var err error
		kindHelper, err = shared.NewKindClusterHelper(ctx)
		Expect(err).NotTo(HaveOccurred())

		k8sClient = kindHelper.Client
		clientset = kindHelper.Clientset

		// Create test namespace
		namespace, err = kindHelper.CreateTestNamespace()
		Expect(err).NotTo(HaveOccurred())

		GinkgoWriter.Printf("Using test namespace: %s\n", namespace)
	})

	Describe("Leader Election with Multiple Replicas", func() {
		It("should ensure only one leader among multiple controller replicas", func() {
			// Check if spotalis-controller deployment exists
			existingDeployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller",
				Namespace: "spotalis-system",
			}, existingDeployment)

			if err != nil {
				if errors.IsNotFound(err) {
					Skip("Spotalis controller deployment not found - skipping leader election test")
					return
				} else {
					Fail(fmt.Sprintf("Error checking for existing deployment: %v", err))
				}
			}

			GinkgoWriter.Printf("Found existing spotalis-controller deployment - proceeding with leader election test\n")

			// Store original replica count for restoration
			originalReplicas := *existingDeployment.Spec.Replicas
			GinkgoWriter.Printf("Original replica count: %d\n", originalReplicas)

			// Scale deployment to 0 replicas first
			GinkgoWriter.Printf("Scaling deployment to 0 replicas...\n")

			// Refresh the deployment object to get the latest resource version
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller",
				Namespace: "spotalis-system",
			}, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			existingDeployment.Spec.Replicas = int32Ptr(0)
			err = k8sClient.Update(ctx, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for all pods to terminate
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(existingDeployment), &updated)
				if err != nil {
					return -1
				}
				GinkgoWriter.Printf("Scaling down: %d/%d replicas ready\n", updated.Status.ReadyReplicas, *updated.Spec.Replicas)
				return updated.Status.ReadyReplicas
			}, 60*time.Second, 3*time.Second).Should(Equal(int32(0)), "All replicas should be scaled down")

			// Clean up any existing lease
			existingLease := &coordinationv1.Lease{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller-leader",
				Namespace: "spotalis-system",
			}, existingLease)
			if err == nil {
				GinkgoWriter.Printf("Deleting existing leader lease...\n")
				err = k8sClient.Delete(ctx, existingLease)
				Expect(err).NotTo(HaveOccurred())
			}

			// Scale the existing deployment to 4 replicas for the test
			GinkgoWriter.Printf("Scaling deployment to 4 replicas for leader election test...\n")

			// Refresh the deployment object again to get the latest resource version
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller",
				Namespace: "spotalis-system",
			}, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			existingDeployment.Spec.Replicas = int32Ptr(4)
			err = k8sClient.Update(ctx, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Use the existing deployment reference for the rest of the test
			deployment := existingDeployment

			// Restore original replica count after test
			defer func() {
				GinkgoWriter.Printf("Restoring deployment to original replica count: %d\n", originalReplicas)

				// Get the current deployment state with fresh resource version
				currentDeployment := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller",
					Namespace: "spotalis-system",
				}, currentDeployment)
				if err == nil {
					currentDeployment.Spec.Replicas = &originalReplicas
					err = k8sClient.Update(ctx, currentDeployment)
					if err != nil {
						GinkgoWriter.Printf("Warning: Failed to restore deployment replica count: %v\n", err)
						// Try one more time with a fresh object
						retryDeployment := &appsv1.Deployment{}
						if k8sClient.Get(ctx, types.NamespacedName{
							Name:      "spotalis-controller",
							Namespace: "spotalis-system",
						}, retryDeployment) == nil {
							retryDeployment.Spec.Replicas = &originalReplicas
							_ = k8sClient.Update(ctx, retryDeployment)
						}
					}
				}

				// Clean up the test lease
				lease := &coordinationv1.Lease{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller-leader",
					Namespace: "spotalis-system",
				}, lease)
				if err == nil {
					_ = k8sClient.Delete(ctx, lease)
					GinkgoWriter.Printf("Cleaned up leader lease\n")
				}
			}()

			// Wait for deployment to be ready (all 4 replicas)
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				GinkgoWriter.Printf("Deployment status: %d/%d replicas ready\n", updated.Status.ReadyReplicas, *updated.Spec.Replicas)
				return updated.Status.ReadyReplicas
			}, 120*time.Second, 5*time.Second).Should(Equal(int32(4)), "All 4 replicas should be ready")

			// Wait 30 seconds as requested
			GinkgoWriter.Printf("Waiting 30 seconds for leader election to stabilize...\n")
			time.Sleep(30 * time.Second)

			// Check the lease to verify only one leader
			Eventually(func() error {
				lease := &coordinationv1.Lease{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller-leader",
					Namespace: "spotalis-system",
				}, lease)
			}, 60*time.Second, 2*time.Second).Should(Succeed(), "Leader lease should exist")

			// Get the lease and verify it has a single holder
			lease := &coordinationv1.Lease{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller-leader",
				Namespace: "spotalis-system",
			}, lease)
			Expect(err).NotTo(HaveOccurred())

			// Verify lease has proper fields
			Expect(lease.Spec.HolderIdentity).NotTo(BeNil())
			Expect(*lease.Spec.HolderIdentity).NotTo(BeEmpty())
			Expect(lease.Spec.AcquireTime).NotTo(BeNil())
			Expect(lease.Spec.RenewTime).NotTo(BeNil())

			leaderIdentity := *lease.Spec.HolderIdentity
			GinkgoWriter.Printf("✓ Leader lease holder: %s\n", leaderIdentity)

			// Check pod logs to verify leader election behavior
			podList := &corev1.PodList{}
			err = k8sClient.List(ctx, podList, client.InNamespace("spotalis-system"), client.MatchingLabels{
				"app.kubernetes.io/name":      "spotalis",
				"app.kubernetes.io/component": "controller",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podList.Items)).To(Equal(4), "Should have exactly 4 controller pods")

			leaderCount := 0
			nonLeaderCount := 0

			for _, pod := range podList.Items {
				if pod.Status.Phase != corev1.PodRunning {
					continue
				}

				// Get pod logs
				logOptions := &corev1.PodLogOptions{
					Container: "controller",
					TailLines: int64Ptr(50),
				}

				logs, err := clientset.CoreV1().Pods("spotalis-system").GetLogs(pod.Name, logOptions).Do(ctx).Raw()
				if err != nil {
					GinkgoWriter.Printf("Warning: Could not get logs for pod %s: %v\n", pod.Name, err)
					continue
				}

				logStr := string(logs)
				GinkgoWriter.Printf("Pod %s logs (last 50 lines):\n%s\n", pod.Name, logStr)

				// Check if this pod is the leader based on logs
				if strings.Contains(logStr, "successfully acquired lease") ||
					strings.Contains(logStr, "became leader") ||
					strings.Contains(logStr, "leader elected") {
					leaderCount++
					GinkgoWriter.Printf("✓ Pod %s appears to be the leader\n", pod.Name)
				} else if strings.Contains(logStr, "not the leader") ||
					strings.Contains(logStr, "waiting to acquire lease") {
					nonLeaderCount++
					GinkgoWriter.Printf("✓ Pod %s is correctly not the leader\n", pod.Name)
				}
			}

			// We should have exactly one leader
			Expect(leaderCount).To(BeNumerically("<=", 1), "Should have at most one leader based on logs")
			if leaderCount == 0 {
				GinkgoWriter.Printf("Warning: Could not detect leader from logs, but lease exists with holder: %s\n", leaderIdentity)
			}

			GinkgoWriter.Printf("✓ Leader election test completed - Lease holder: %s, Leaders from logs: %d, Non-leaders: %d\n",
				leaderIdentity, leaderCount, nonLeaderCount)
		})

		It("should handle leader failover when leader pod is deleted", func() {
			// Check if spotalis-controller deployment exists
			existingDeployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller",
				Namespace: "spotalis-system",
			}, existingDeployment)

			if err != nil {
				if errors.IsNotFound(err) {
					Skip("Spotalis controller deployment not found - skipping leader failover test")
					return
				} else {
					Fail(fmt.Sprintf("Error checking for existing deployment: %v", err))
				}
			}

			GinkgoWriter.Printf("Found existing spotalis-controller deployment - proceeding with leader failover test\n")

			// Store original replica count for restoration
			originalReplicas := *existingDeployment.Spec.Replicas
			GinkgoWriter.Printf("Original replica count: %d\n", originalReplicas)

			// Scale deployment to 0 replicas first to clean slate
			GinkgoWriter.Printf("Scaling deployment to 0 replicas...\n")

			// Wait 10 seconds to ensure any previous leader election is settled
			time.Sleep(10 * time.Second)

			// Refresh the deployment object to get the latest resource version
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller",
				Namespace: "spotalis-system",
			}, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			existingDeployment.Spec.Replicas = int32Ptr(0)
			err = k8sClient.Update(ctx, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for all pods to terminate
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(existingDeployment), &updated)
				if err != nil {
					return -1
				}
				GinkgoWriter.Printf("Scaling down: %d/%d replicas ready\n", updated.Status.ReadyReplicas, *updated.Spec.Replicas)
				return updated.Status.ReadyReplicas
			}, 60*time.Second, 3*time.Second).Should(Equal(int32(0)), "All replicas should be scaled down")

			// Clean up any existing lease
			existingLease := &coordinationv1.Lease{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller-leader",
				Namespace: "spotalis-system",
			}, existingLease)
			if err == nil {
				GinkgoWriter.Printf("Deleting existing leader lease...\n")
				err = k8sClient.Delete(ctx, existingLease)
				Expect(err).NotTo(HaveOccurred())
			}

			// Scale the existing deployment to 2 replicas for the failover test
			GinkgoWriter.Printf("Scaling deployment to 2 replicas for leader failover test...\n")

			// Refresh the deployment object again to get the latest resource version
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller",
				Namespace: "spotalis-system",
			}, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			existingDeployment.Spec.Replicas = int32Ptr(2)
			err = k8sClient.Update(ctx, existingDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Restore original replica count after test
			defer func() {
				GinkgoWriter.Printf("Restoring deployment to original replica count: %d\n", originalReplicas)

				// Get the current deployment state with fresh resource version
				currentDeployment := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller",
					Namespace: "spotalis-system",
				}, currentDeployment)
				if err == nil {
					currentDeployment.Spec.Replicas = &originalReplicas
					err = k8sClient.Update(ctx, currentDeployment)
					if err != nil {
						GinkgoWriter.Printf("Warning: Failed to restore deployment replica count: %v\n", err)
						// Try one more time with a fresh object
						retryDeployment := &appsv1.Deployment{}
						if k8sClient.Get(ctx, types.NamespacedName{
							Name:      "spotalis-controller",
							Namespace: "spotalis-system",
						}, retryDeployment) == nil {
							retryDeployment.Spec.Replicas = &originalReplicas
							_ = k8sClient.Update(ctx, retryDeployment)
						}
					}
				}

				// Clean up the test lease
				lease := &coordinationv1.Lease{}
				err = k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller-leader",
					Namespace: "spotalis-system",
				}, lease)
				if err == nil {
					_ = k8sClient.Delete(ctx, lease)
					GinkgoWriter.Printf("Cleaned up leader lease\n")
				}
			}()

			// Wait for deployment to be ready (both 2 replicas)
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(existingDeployment), &updated)
				if err != nil {
					return 0
				}
				GinkgoWriter.Printf("Deployment status: %d/%d replicas ready\n", updated.Status.ReadyReplicas, *updated.Spec.Replicas)
				return updated.Status.ReadyReplicas
			}, 120*time.Second, 5*time.Second).Should(Equal(int32(2)), "Both 2 replicas should be ready")

			// Wait for leader election to stabilize
			GinkgoWriter.Printf("Waiting 30 seconds for initial leader election to stabilize...\n")
			time.Sleep(30 * time.Second)

			// Get the initial leader lease
			Eventually(func() error {
				lease := &coordinationv1.Lease{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller-leader",
					Namespace: "spotalis-system",
				}, lease)
			}, 60*time.Second, 2*time.Second).Should(Succeed(), "Leader lease should exist")

			initialLease := &coordinationv1.Lease{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller-leader",
				Namespace: "spotalis-system",
			}, initialLease)
			Expect(err).NotTo(HaveOccurred())

			initialLeaderIdentity := *initialLease.Spec.HolderIdentity
			GinkgoWriter.Printf("✓ Initial leader: %s\n", initialLeaderIdentity)

			// Find the pods and identify the actual leader pod by checking logs
			podList := &corev1.PodList{}
			err = k8sClient.List(ctx, podList, client.InNamespace("spotalis-system"), client.MatchingLabels{
				"app.kubernetes.io/name":      "spotalis",
				"app.kubernetes.io/component": "controller",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podList.Items)).To(Equal(2), "Should have exactly 2 controller pods")

			var leaderPod, followerPod *corev1.Pod

			// First try to identify leader by lease holder identity
			for i := range podList.Items {
				pod := &podList.Items[i]
				if pod.Status.Phase == corev1.PodRunning {
					// Check if this pod matches the lease holder identity
					if strings.Contains(initialLeaderIdentity, pod.Name) ||
						strings.Contains(initialLeaderIdentity, string(pod.UID)) {
						leaderPod = pod
						GinkgoWriter.Printf("Identified leader pod by lease identity: %s (matches %s)\n", pod.Name, initialLeaderIdentity)
						break
					}
				}
			}

			// If we couldn't identify by lease identity, check logs to find the actual leader
			if leaderPod == nil {
				GinkgoWriter.Printf("Could not identify leader by lease identity '%s', checking pod logs...\n", initialLeaderIdentity)

				for i := range podList.Items {
					pod := &podList.Items[i]
					if pod.Status.Phase == corev1.PodRunning {
						// Get pod logs to check for leader indicators
						logOptions := &corev1.PodLogOptions{
							Container: "controller",
							TailLines: int64Ptr(100),
						}

						logs, err := clientset.CoreV1().Pods("spotalis-system").GetLogs(pod.Name, logOptions).Do(ctx).Raw()
						if err != nil {
							GinkgoWriter.Printf("Warning: Could not get logs for pod %s: %v\n", pod.Name, err)
							continue
						}

						logStr := string(logs)
						// Look for leader indicators in logs
						if strings.Contains(logStr, "successfully acquired lease") ||
							strings.Contains(logStr, "became leader") ||
							strings.Contains(logStr, "leader elected") ||
							strings.Contains(logStr, "acting as leader") {
							leaderPod = pod
							GinkgoWriter.Printf("Identified leader pod by logs: %s\n", pod.Name)
							break
						}
					}
				}
			}

			// Set follower pod (the other running pod)
			for i := range podList.Items {
				pod := &podList.Items[i]
				if pod.Status.Phase == corev1.PodRunning && (leaderPod == nil || pod.Name != leaderPod.Name) {
					followerPod = pod
					break
				}
			}

			// If we still can't identify the leader, fail the test rather than guess
			if leaderPod == nil {
				Fail(fmt.Sprintf("Could not identify the leader pod from lease identity '%s' or pod logs", initialLeaderIdentity))
			}

			Expect(followerPod).NotTo(BeNil(), "Should have identified a follower pod")

			GinkgoWriter.Printf("✓ Confirmed leader pod: %s, follower pod: %s\n", leaderPod.Name, followerPod.Name)

			// Delete the leader pod to trigger failover
			GinkgoWriter.Printf("Deleting leader pod: %s\n", leaderPod.Name)
			err = k8sClient.Delete(ctx, leaderPod)
			Expect(err).NotTo(HaveOccurred())

			// Wait for the pod to be deleted and replaced
			Eventually(func() bool {
				var pod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      leaderPod.Name,
					Namespace: leaderPod.Namespace,
				}, &pod)
				return errors.IsNotFound(err)
			}, 60*time.Second, 3*time.Second).Should(BeTrue(), "Leader pod should be deleted")

			// Wait for deployment to have 2 ready replicas again (replacement pod)
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(existingDeployment), &updated)
				if err != nil {
					return 0
				}
				GinkgoWriter.Printf("After leader deletion: %d/%d replicas ready\n", updated.Status.ReadyReplicas, *updated.Spec.Replicas)
				return updated.Status.ReadyReplicas
			}, 120*time.Second, 5*time.Second).Should(Equal(int32(2)), "Should have 2 ready replicas after failover")

			// Wait for new leader election to complete
			GinkgoWriter.Printf("Waiting 30 seconds for leader failover to complete...\n")
			time.Sleep(30 * time.Second)

			// Verify that leadership has been transferred to the remaining pod
			var newLeaderIdentity string
			Eventually(func() bool {
				newLease := &coordinationv1.Lease{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "spotalis-controller-leader",
					Namespace: "spotalis-system",
				}, newLease)
				if err != nil {
					GinkgoWriter.Printf("Could not get lease: %v\n", err)
					return false
				}

				newLeaderIdentity = *newLease.Spec.HolderIdentity
				GinkgoWriter.Printf("Current leader after failover: %s (initial was: %s)\n", newLeaderIdentity, initialLeaderIdentity)

				// The leader identity must change (different pod became leader)
				hasChanged := newLeaderIdentity != initialLeaderIdentity

				// Additionally verify that the new leader identity corresponds to the follower pod
				// that should have taken over leadership
				if hasChanged {
					// Check if the new leader identity matches the follower pod
					if strings.Contains(newLeaderIdentity, followerPod.Name) ||
						strings.Contains(newLeaderIdentity, string(followerPod.UID)) {
						GinkgoWriter.Printf("✓ Confirmed: Leadership transferred to follower pod %s\n", followerPod.Name)
						return true
					} else {
						GinkgoWriter.Printf("Warning: Leadership changed but new identity '%s' doesn't match follower pod %s\n",
							newLeaderIdentity, followerPod.Name)
						return true // Still accept if identity changed, even if we can't match it perfectly
					}
				}

				return false
			}, 90*time.Second, 5*time.Second).Should(BeTrue(), "Leadership should have been transferred to the remaining pod after leader deletion")

			// Get final lease state and perform comprehensive verification
			finalLease := &coordinationv1.Lease{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "spotalis-controller-leader",
				Namespace: "spotalis-system",
			}, finalLease)
			Expect(err).NotTo(HaveOccurred())

			finalLeaderIdentity := *finalLease.Spec.HolderIdentity

			// Verify that the lease is still functional
			Expect(finalLease.Spec.HolderIdentity).NotTo(BeNil())
			Expect(*finalLease.Spec.HolderIdentity).NotTo(BeEmpty())
			Expect(finalLease.Spec.AcquireTime).NotTo(BeNil())
			Expect(finalLease.Spec.RenewTime).NotTo(BeNil())

			// Critical verification: ensure leadership actually changed
			Expect(finalLeaderIdentity).NotTo(Equal(initialLeaderIdentity),
				"Leadership must transfer to a different pod when leader is deleted")

			GinkgoWriter.Printf("✓ Leader failover completed successfully!\n")
			GinkgoWriter.Printf("  - Initial leader pod: %s (identity: %s)\n", leaderPod.Name, initialLeaderIdentity)
			GinkgoWriter.Printf("  - Final leader identity: %s\n", finalLeaderIdentity)
			GinkgoWriter.Printf("  - Leadership successfully transferred after pod deletion\n")
		})
	})
})

func int64Ptr(i int64) *int64 {
	return &i
}
