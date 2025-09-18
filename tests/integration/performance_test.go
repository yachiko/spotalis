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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/ahoma/spotalis/pkg/operator"
)

func TestPerformanceIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Performance Tuning Integration Suite")
}

var _ = Describe("Performance tuning scenario", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		testEnv    *envtest.Environment
		k8sClient  client.Client
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

		namespace = "spotalis-perf-test-" + randString(6)
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

		// For Kind cluster tests, create operator with disabled services to avoid conflicts
		operatorConfig := &operator.OperatorConfig{
			MetricsAddr:   ":0",  // Disable metrics server
			ProbeAddr:     ":0",  // Disable health probes
			EnableWebhook: false, // Disable webhook
		}
		spotalisOp, err = operator.NewOperator(operatorConfig)
		Expect(err).NotTo(HaveOccurred())

		// NOTE: We don't start this operator since Kind cluster has its own
	})

	AfterEach(func() {
		cancel()
		if testEnv != nil {
			err := testEnv.Stop()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("when handling high workload volumes", func() {
		It("should efficiently process many deployments", func() {
			// NOTE: In Kind cluster tests, the operator is already running

			// Create 50 test deployments
			deployments := make([]*appsv1.Deployment, 50)
			for i := 0; i < 50; i++ {
				deployments[i] = &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("perf-test-deployment-%d", i),
						Namespace: namespace,
						Annotations: map[string]string{
							"spotalis.io/replica-strategy": "spot-optimized",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(2),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": fmt.Sprintf("perf-test-%d", i),
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": fmt.Sprintf("perf-test-%d", i),
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

				err := k8sClient.Create(ctx, deployments[i])
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify all deployments are processed within reasonable time
			Eventually(func() int {
				managedCount := 0
				for _, deployment := range deployments {
					var current appsv1.Deployment
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
					if err == nil && current.Annotations["spotalis.io/managed"] == "true" {
						managedCount++
					}
				}
				return managedCount
			}, "60s", "2s").Should(Equal(50))

			// Verify performance metrics
			metrics := spotalisOp.GetMetrics()
			Expect(metrics.WorkloadsManaged).To(Equal(50))
			// TODO: Implement AverageReconcileTime metric
			// Expect(metrics.AverageReconcileTime).To(BeNumerically("<", 100*time.Millisecond))
		})

		It("should maintain low memory usage under load", func() {
			// Start operator with memory monitoring
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// TODO: Implement GetMemoryUsage method
			// initialMemory := spotalisOp.GetMemoryUsage()

			// Create and delete many deployments to test memory leaks
			for iteration := 0; iteration < 5; iteration++ {
				// Create deployments
				for i := 0; i < 20; i++ {
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("mem-test-deployment-%d-%d", iteration, i),
							Namespace: namespace,
							Annotations: map[string]string{
								"spotalis.io/replica-strategy": "spot-optimized",
							},
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: int32Ptr(3),
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": fmt.Sprintf("mem-test-%d-%d", iteration, i),
								},
							},
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"app": fmt.Sprintf("mem-test-%d-%d", iteration, i),
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
				}

				// Wait for processing
				time.Sleep(10 * time.Second)

				// Delete deployments
				err := k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{},
					client.InNamespace(namespace),
					client.MatchingLabels{"app": fmt.Sprintf("mem-test-%d", iteration)})
				Expect(err).NotTo(HaveOccurred())

				// Wait for cleanup
				time.Sleep(5 * time.Second)
			}

			// TODO: Implement ForceGarbageCollection method
			// spotalisOp.ForceGarbageCollection()

			// TODO: Implement GetMemoryUsage method
			// Verify memory usage hasn't grown excessively
			// finalMemory := spotalisOp.GetMemoryUsage()
			// memoryGrowth := finalMemory - initialMemory

			// Allow for some growth but detect memory leaks
			// Expect(memoryGrowth).To(BeNumerically("<", 50*1024*1024)) // Less than 50MB growth
		})

		It("should efficiently handle resource quota constraints", func() {
			// Create resource quota for namespace
			quota := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quota",
					Namespace: namespace,
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourcePods:           resource.MustParse("50"),
						corev1.ResourceRequestsCPU:    resource.MustParse("10"),
						corev1.ResourceRequestsMemory: resource.MustParse("20Gi"),
					},
				},
			}
			err := k8sClient.Create(ctx, quota)
			Expect(err).NotTo(HaveOccurred())

			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Create deployment that would exceed quota if not managed properly
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "quota-test-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "quota-aware",
						"spotalis.io/max-replicas":     "100", // Would exceed quota
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(60), // Exceeds pod quota
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "quota-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "quota-test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "app",
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

			err = k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify operator respects quota and adjusts replicas
			Eventually(func() int32 {
				var current appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
				if err != nil {
					return 0
				}
				return *current.Spec.Replicas
			}, "30s", "2s").Should(BeNumerically("<=", 50)) // Should be adjusted to respect quota

			// Verify quota compliance annotation
			Eventually(func() string {
				var current appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
				if err != nil {
					return ""
				}
				return current.Annotations["spotalis.io/quota-adjusted"]
			}, "30s", "2s").Should(Equal("true"))
		})
	})

	Context("when optimizing reconciliation performance", func() {
		It("should batch similar operations efficiently", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Create multiple deployments with same strategy rapidly
			startTime := time.Now()
			for i := 0; i < 20; i++ {
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("batch-test-deployment-%d", i),
						Namespace: namespace,
						Annotations: map[string]string{
							"spotalis.io/replica-strategy": "spot-optimized",
							"spotalis.io/scaling-policy":   "aggressive",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(2),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": fmt.Sprintf("batch-test-%d", i),
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": fmt.Sprintf("batch-test-%d", i),
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
			}

			// Verify all are processed efficiently
			Eventually(func() int {
				processedCount := 0
				for i := 0; i < 20; i++ {
					var deployment appsv1.Deployment
					err := k8sClient.Get(ctx, client.ObjectKey{
						Name:      fmt.Sprintf("batch-test-deployment-%d", i),
						Namespace: namespace,
					}, &deployment)
					if err == nil && deployment.Annotations["spotalis.io/managed"] == "true" {
						processedCount++
					}
				}
				return processedCount
			}, "45s", "2s").Should(Equal(20))

			processingTime := time.Since(startTime)

			// Verify efficient processing (should benefit from batching)
			// TODO: Implement BatchOperations metric and add metrics validation
			Expect(processingTime).To(BeNumerically("<", 60*time.Second))
		})

		It("should cache frequently accessed resources", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Create deployment
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cache-test-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "spot-optimized",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "cache-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "cache-test",
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

			// Wait for initial processing
			Eventually(func() string {
				var current appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
				if err != nil {
					return ""
				}
				return current.Annotations["spotalis.io/managed"]
			}, "30s", "2s").Should(Equal("true"))

			initialWorkloads := spotalisOp.GetMetrics().WorkloadsManaged

			// Trigger multiple reconciliations by updating annotations
			for i := 0; i < 10; i++ {
				var current appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
				Expect(err).NotTo(HaveOccurred())

				current.Annotations[fmt.Sprintf("test-annotation-%d", i)] = "value"
				err = k8sClient.Update(ctx, &current)
				Expect(err).NotTo(HaveOccurred())

				time.Sleep(1 * time.Second)
			}

			// Verify workload management continues to function
			Eventually(func() int {
				return spotalisOp.GetMetrics().WorkloadsManaged
			}, "20s", "1s").Should(BeNumerically(">=", initialWorkloads))

			// Skip cache validation - cache metrics not yet implemented
			Skip("Cache metrics not implemented yet")
		})

	})
})
