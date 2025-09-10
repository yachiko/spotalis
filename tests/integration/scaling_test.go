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

func TestScalingIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scaling Integration Suite")
}

var _ = Describe("Scale up/down scenario", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		testEnv    *envtest.Environment
		k8sClient  client.Client
		clientset  *kubernetes.Clientset
		spotalisOp *operator.Operator
		namespace  string
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

		namespace = "spotalis-scaling-test-" + randString(6)
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

		spotalisOp = operator.New(cfg)
		go func() {
			defer GinkgoRecover()
			err := spotalisOp.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
		}()

		Eventually(func() bool {
			return spotalisOp.IsReady()
		}, "30s", "1s").Should(BeTrue())
	})

	AfterEach(func() {
		cancel()
		if testEnv != nil {
			err := testEnv.Stop()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("when scaling up workloads", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scaling-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy":     "spot-optimized",
						"spotalis.io/cost-optimization":    "enabled",
						"spotalis.io/disruption-tolerance": "medium",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2), // Start with 2 replicas
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "scaling-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "scaling-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
									Image: "nginx:latest",
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    "100m",
											corev1.ResourceMemory: "128Mi",
										},
									},
								},
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should manage replica distribution during scale up", func() {
			// Wait for initial deployment to be managed
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/managed"))

			// Scale up to 10 replicas
			var current appsv1.Deployment
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(10)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify Spotalis handles the scaling
			Eventually(func() string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return ""
				}
				return updated.Annotations["spotalis.io/scaling-strategy"]
			}, "30s", "2s").Should(Equal("progressive"))

			// Verify replica count is achieved
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "45s", "3s").Should(Equal(int32(10)))
		})

		It("should optimize node selection during scale up", func() {
			// Scale up deployment
			var current appsv1.Deployment
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(6)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify node selectors are optimized for cost
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Spec.Template.Spec.NodeSelector
			}, "30s", "2s").Should(HaveKey("karpenter.sh/capacity-type"))

			// Verify affinity rules are set for distribution
			Eventually(func() bool {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return false
				}
				return updated.Spec.Template.Spec.Affinity != nil
			}, "30s", "2s").Should(BeTrue())
		})

		It("should update metrics during scale events", func() {
			initialMetrics := spotalisOp.GetMetrics()

			// Scale up
			var current appsv1.Deployment
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(8)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify scaling metrics are updated
			Eventually(func() int {
				metrics := spotalisOp.GetMetrics()
				return metrics.ScalingEvents
			}, "30s", "2s").Should(BeNumerically(">", initialMetrics.ScalingEvents))

			Eventually(func() int {
				metrics := spotalisOp.GetMetrics()
				return metrics.ReplicasManaged
			}, "30s", "2s").Should(BeNumerically(">", initialMetrics.ReplicasManaged))
		})
	})

	Context("when scaling down workloads", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scale-down-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy":     "spot-optimized",
						"spotalis.io/disruption-tolerance": "high",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(10), // Start with more replicas
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "scale-down-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "scale-down-app",
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
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should gracefully handle scale down operations", func() {
			// Wait for deployment to be managed
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/managed"))

			// Scale down to 3 replicas
			var current appsv1.Deployment
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(3)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify graceful scale down
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "60s", "3s").Should(Equal(int32(3)))

			// Verify pods are terminated gracefully
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "15s", "2s").Should(Equal(int32(3)))
		})

		It("should respect PodDisruptionBudget during scale down", func() {
			// Create PodDisruptionBudget
			pdb := &metav1.PartialObjectMetadata{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "policy/v1",
					Kind:       "PodDisruptionBudget",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scale-down-pdb",
					Namespace: namespace,
				},
			}
			pdb.Object = map[string]interface{}{
				"spec": map[string]interface{}{
					"minAvailable": 5,
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app": "scale-down-app",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			// Scale down to 2 replicas (below PDB minimum)
			var current appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(2)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify PDB is respected (should stay at 5 minimum)
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "30s", "2s").Should(BeNumerically(">=", 5))
		})

		It("should optimize cost during scale down", func() {
			// Scale down
			var current appsv1.Deployment
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
			Expect(err).NotTo(HaveOccurred())

			current.Spec.Replicas = int32Ptr(4)
			err = k8sClient.Update(ctx, &current)
			Expect(err).NotTo(HaveOccurred())

			// Verify cost optimization annotations are updated
			Eventually(func() string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return ""
				}
				return updated.Annotations["spotalis.io/cost-savings"]
			}, "30s", "2s").ShouldNot(BeEmpty())
		})
	})

	Context("when handling rapid scaling events", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rapid-scaling-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "spot-optimized",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "rapid-scaling-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "rapid-scaling-app",
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
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle rapid scaling changes without conflicts", func() {
			// Wait for initial state
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/managed"))

			// Perform rapid scaling operations
			scaleTargets := []int32{2, 8, 1, 6, 3}

			for _, target := range scaleTargets {
				var current appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
				Expect(err).NotTo(HaveOccurred())

				current.Spec.Replicas = int32Ptr(target)
				err = k8sClient.Update(ctx, &current)
				Expect(err).NotTo(HaveOccurred())

				// Brief pause between operations
				time.Sleep(2 * time.Second)
			}

			// Verify final state is reached
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "60s", "3s").Should(Equal(int32(3)))

			// Verify no conflicts occurred
			Eventually(func() string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return ""
				}
				return updated.Annotations["spotalis.io/status"]
			}, "20s", "2s").Should(Equal("stable"))
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
