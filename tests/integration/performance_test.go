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
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

		// Create operator with performance tuning configuration
		config := operator.Config{
			ReconcileInterval:       5 * time.Second,  // High frequency reconciliation
			MaxConcurrentReconciles: 10,               // High concurrency
			WorkerPoolSize:          20,               // Large worker pool
			CacheResyncPeriod:       30 * time.Second, // Frequent cache resync
			MetricsInterval:         2 * time.Second,  // High-frequency metrics
		}
		spotalisOp = operator.NewWithConfig(cfg, config)
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
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

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
			Expect(metrics.AverageReconcileTime).To(BeNumerically("<", 100*time.Millisecond))
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

			initialMemory := spotalisOp.GetMemoryUsage()

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

			// Force garbage collection
			spotalisOp.ForceGarbageCollection()

			// Verify memory usage hasn't grown excessively
			finalMemory := spotalisOp.GetMemoryUsage()
			memoryGrowth := finalMemory - initialMemory

			// Allow for some growth but detect memory leaks
			Expect(memoryGrowth).To(BeNumerically("<", 50*1024*1024)) // Less than 50MB growth
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
			metrics := spotalisOp.GetMetrics()
			Expect(metrics.BatchOperations).To(BeNumerically(">", 0))
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

			initialCacheHits := spotalisOp.GetMetrics().CacheHits

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

			// Verify cache utilization improved
			Eventually(func() int64 {
				return spotalisOp.GetMetrics().CacheHits
			}, "20s", "1s").Should(BeNumerically(">", initialCacheHits))

			// Verify cache hit ratio is reasonable
			metrics := spotalisOp.GetMetrics()
			cacheHitRatio := float64(metrics.CacheHits) / float64(metrics.CacheHits+metrics.CacheMisses)
			Expect(cacheHitRatio).To(BeNumerically(">", 0.7)) // At least 70% hit ratio
		})

		It("should handle concurrent reconciliation efficiently", func() {
			// Start operator with high concurrency
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Create many deployments simultaneously
			deploymentCount := 30
			var deployments []*appsv1.Deployment

			for i := 0; i < deploymentCount; i++ {
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("concurrent-test-deployment-%d", i),
						Namespace: namespace,
						Annotations: map[string]string{
							"spotalis.io/replica-strategy": "spot-optimized",
							"spotalis.io/processing-delay": "2s", // Simulate processing time
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(2),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": fmt.Sprintf("concurrent-test-%d", i),
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": fmt.Sprintf("concurrent-test-%d", i),
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

				deployments = append(deployments, deployment)
				err := k8sClient.Create(ctx, deployment)
				Expect(err).NotTo(HaveOccurred())
			}

			startTime := time.Now()

			// Verify all deployments are processed
			Eventually(func() int {
				processedCount := 0
				for _, deployment := range deployments {
					var current appsv1.Deployment
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &current)
					if err == nil && current.Annotations["spotalis.io/managed"] == "true" {
						processedCount++
					}
				}
				return processedCount
			}, "60s", "2s").Should(Equal(deploymentCount))

			processingTime := time.Since(startTime)

			// Verify concurrent processing was more efficient than sequential
			// With 10 concurrent reconcilers and 2s processing delay each,
			// 30 deployments should complete much faster than 60s (30 * 2s sequential)
			Expect(processingTime).To(BeNumerically("<", 30*time.Second))

			// Verify concurrency metrics
			metrics := spotalisOp.GetMetrics()
			Expect(metrics.ConcurrentReconciliations).To(BeNumerically(">", 1))
			Expect(metrics.MaxConcurrentReconciliations).To(BeNumerically("<=", 10)) // Respect limit
		})
	})

	Context("when monitoring performance metrics", func() {
		It("should track detailed performance metrics", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Create and process some workloads
			for i := 0; i < 5; i++ {
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("metrics-test-deployment-%d", i),
						Namespace: namespace,
						Annotations: map[string]string{
							"spotalis.io/replica-strategy": "spot-optimized",
						},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(2),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": fmt.Sprintf("metrics-test-%d", i),
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": fmt.Sprintf("metrics-test-%d", i),
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
			Eventually(func() int {
				return int(spotalisOp.GetMetrics().WorkloadsManaged)
			}, "30s", "2s").Should(Equal(5))

			// Verify comprehensive metrics are available
			metrics := spotalisOp.GetMetrics()

			Expect(metrics.TotalReconciliations).To(BeNumerically(">", 0))
			Expect(metrics.AverageReconcileTime).To(BeNumerically(">", 0))
			Expect(metrics.ReconcileErrors).To(BeNumerically(">=", 0))
			Expect(metrics.APICallsPerSecond).To(BeNumerically(">", 0))
			Expect(metrics.CacheHitRatio).To(BeNumerically(">=", 0))
			Expect(metrics.WorkerQueueDepth).To(BeNumerically(">=", 0))
			Expect(metrics.MemoryUsageBytes).To(BeNumerically(">", 0))
			Expect(metrics.CPUUsagePercent).To(BeNumerically(">=", 0))
		})

		It("should export performance metrics for monitoring", func() {
			// Start operator
			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())

			// Verify metrics endpoint provides performance data
			metricsResponse := spotalisOp.GetMetricsEndpointResponse()

			// Check for key performance metrics in Prometheus format
			Expect(metricsResponse).To(ContainSubstring("spotalis_reconcile_duration_seconds"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_reconcile_total"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_workloads_managed_total"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_api_calls_per_second"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_cache_hit_ratio"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_worker_queue_depth"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_memory_usage_bytes"))
			Expect(metricsResponse).To(ContainSubstring("spotalis_concurrent_reconciliations"))
		})
	})
})
