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

package integration_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/ahoma/spotalis/pkg/operator"
)

func TestLeaderElectionIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Leader Election Integration Suite")
}

var _ = Describe("Leader election scenario", func() {
	var (
		ctx         context.Context
		cancel      context.CancelFunc
		testEnv     *envtest.Environment
		k8sClient   client.Client
		clientset   *kubernetes.Clientset
		spotalisOp1 *operator.Operator
		spotalisOp2 *operator.Operator
		namespace   string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{"../../configs/crd/bases"},
		}

		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())

		k8sClient, err = client.New(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())

		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		namespace = "spotalis-leader-test-" + randString(6)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"spotalis.io/enabled": "true",
				},
			},
		}
		err = k8sClient.Create(ctx, ns)
		Expect(err).NotTo(HaveOccurred())

		// Create first operator instance
		spotalisOp1 = operator.NewWithID(cfg, "spotalis-1")

		// Create second operator instance
		spotalisOp2 = operator.NewWithID(cfg, "spotalis-2")
	})

	AfterEach(func() {
		cancel()
		if testEnv != nil {
			err := testEnv.Stop()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("when starting multiple operator instances", func() {
		It("should elect exactly one leader", func() {
			// Start first operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Wait for first operator to become ready
			Eventually(func() bool {
				return spotalisOp1.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Verify first operator is leader
			Eventually(func() bool {
				return spotalisOp1.IsLeader()
			}, "10s", "1s").Should(BeTrue())

			// Start second operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp2.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Wait for second operator to become ready
			Eventually(func() bool {
				return spotalisOp2.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Verify only one is leader
			Eventually(func() bool {
				op1IsLeader := spotalisOp1.IsLeader()
				op2IsLeader := spotalisOp2.IsLeader()
				return (op1IsLeader && !op2IsLeader) || (!op1IsLeader && op2IsLeader)
			}, "20s", "1s").Should(BeTrue())

			// Verify non-leader is in follower mode
			if spotalisOp1.IsLeader() {
				Expect(spotalisOp2.IsFollower()).To(BeTrue())
			} else {
				Expect(spotalisOp1.IsFollower()).To(BeTrue())
			}
		})

		It("should create leader election ConfigMap", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp1.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Verify leader election ConfigMap exists
			Eventually(func() error {
				_, err := clientset.CoreV1().ConfigMaps("kube-system").Get(
					ctx, "spotalis-leader-election", metav1.GetOptions{})
				return err
			}, "15s", "1s").Should(Not(HaveOccurred()))
		})

		It("should handle leader failover", func() {
			// Start both operators
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			go func() {
				defer GinkgoRecover()
				err := spotalisOp2.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Wait for both to be ready
			Eventually(func() bool {
				return spotalisOp1.IsReady() && spotalisOp2.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Identify the leader
			var leader, follower *operator.Operator
			Eventually(func() bool {
				if spotalisOp1.IsLeader() {
					leader = spotalisOp1
					follower = spotalisOp2
					return true
				} else if spotalisOp2.IsLeader() {
					leader = spotalisOp2
					follower = spotalisOp1
					return true
				}
				return false
			}, "20s", "1s").Should(BeTrue())

			// Stop the leader
			err := leader.Stop()
			Expect(err).NotTo(HaveOccurred())

			// Verify follower becomes leader
			Eventually(func() bool {
				return follower.IsLeader()
			}, "30s", "1s").Should(BeTrue())
		})
	})

	Context("when leader performs operations", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "leader-test-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "spot-optimized",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "leader-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "leader-test",
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

		It("should only process workloads on leader instance", func() {
			// Start both operators
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			go func() {
				defer GinkgoRecover()
				err := spotalisOp2.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Wait for leadership election
			Eventually(func() bool {
				return (spotalisOp1.IsLeader() && spotalisOp2.IsFollower()) ||
					(spotalisOp2.IsLeader() && spotalisOp1.IsFollower())
			}, "30s", "1s").Should(BeTrue())

			// Deploy workload
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify only leader processes it
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "20s", "1s").Should(HaveKey("spotalis.io/managed"))

			// Verify metrics are only updated on leader
			if spotalisOp1.IsLeader() {
				Expect(spotalisOp1.GetMetrics().WorkloadsManaged).To(BeNumerically(">", 0))
				Expect(spotalisOp2.GetMetrics().WorkloadsManaged).To(Equal(0))
			} else {
				Expect(spotalisOp2.GetMetrics().WorkloadsManaged).To(BeNumerically(">", 0))
				Expect(spotalisOp1.GetMetrics().WorkloadsManaged).To(Equal(0))
			}
		})

		It("should continue operations after leader change", func() {
			// Start both operators
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			go func() {
				defer GinkgoRecover()
				err := spotalisOp2.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Wait for leadership
			Eventually(func() bool {
				return (spotalisOp1.IsLeader() && spotalisOp2.IsFollower()) ||
					(spotalisOp2.IsLeader() && spotalisOp1.IsFollower())
			}, "30s", "1s").Should(BeTrue())

			// Deploy workload
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for initial processing
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "20s", "1s").Should(HaveKey("spotalis.io/managed"))

			// Force leader change by stopping current leader
			var originalLeader, newLeader *operator.Operator
			if spotalisOp1.IsLeader() {
				originalLeader = spotalisOp1
				newLeader = spotalisOp2
			} else {
				originalLeader = spotalisOp2
				newLeader = spotalisOp1
			}

			err = originalLeader.Stop()
			Expect(err).NotTo(HaveOccurred())

			// Verify new leader takes over
			Eventually(func() bool {
				return newLeader.IsLeader()
			}, "30s", "1s").Should(BeTrue())

			// Update deployment to verify new leader processes changes
			var current appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(5)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify new leader processes the update
			Eventually(func() string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return ""
				}
				return updated.Annotations["spotalis.io/last-processed-by"]
			}, "25s", "2s").Should(Equal(newLeader.GetID()))
		})
	})

	Context("when handling leader election edge cases", func() {
		It("should handle network partitions gracefully", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp1.IsLeader()
			}, "30s", "1s").Should(BeTrue())

			// Simulate network partition by stopping API access temporarily
			err := spotalisOp1.SimulateNetworkPartition(5 * time.Second)
			Expect(err).NotTo(HaveOccurred())

			// Start second operator during partition
			go func() {
				defer GinkgoRecover()
				err := spotalisOp2.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// After partition heals, verify stable leadership
			Eventually(func() bool {
				return (spotalisOp1.IsLeader() && !spotalisOp2.IsLeader()) ||
					(!spotalisOp1.IsLeader() && spotalisOp2.IsLeader())
			}, "45s", "2s").Should(BeTrue())
		})

		It("should handle lease renewal failures", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp1.IsLeader()
			}, "30s", "1s").Should(BeTrue())

			// Simulate lease renewal failure
			err := spotalisOp1.SimulateLeaseRenewalFailure()
			Expect(err).NotTo(HaveOccurred())

			// Verify operator steps down and enters follower mode
			Eventually(func() bool {
				return spotalisOp1.IsFollower()
			}, "30s", "1s").Should(BeTrue())
		})
	})

	Context("when monitoring leader election health", func() {
		It("should expose leader election metrics", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp1.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Verify leader election metrics
			Eventually(func() bool {
				metrics := spotalisOp1.GetMetrics()
				return metrics.LeaderElectionChanges >= 0
			}, "10s", "1s").Should(BeTrue())

			Eventually(func() time.Duration {
				metrics := spotalisOp1.GetMetrics()
				return metrics.LeadershipDuration
			}, "10s", "1s").Should(BeNumerically(">", 0))
		})

		It("should update health status based on leadership", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp1.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp1.IsReady() && spotalisOp1.IsLeader()
			}, "30s", "1s").Should(BeTrue())

			// Verify health endpoint reflects leadership
			health := spotalisOp1.GetHealthStatus()
			Expect(health.Leadership).To(Equal("leader"))
			Expect(health.Status).To(Equal("healthy"))
		})
	})
})

func int32Ptr(i int32) *int32 {
	return &i
}

func randString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
