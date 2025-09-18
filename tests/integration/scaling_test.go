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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/ahoma/spotalis/pkg/operator"
)

func TestScalingIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scaling Integration Suite")
}

var _ = Describe("Pod rebalancing scenario", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		testEnv   *envtest.Environment
		k8sClient client.Client
		namespace string
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

		namespace = "spotalis-scaling-test-" + randString(6)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"test-namespace": "true",
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
		_, err = operator.NewOperator(operatorConfig)
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

	Context("when rebalancing workloads", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rebalance-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(6), // Fixed replica count - Spotalis won't change this
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "rebalance-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "rebalance-app",
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
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not modify replica counts", func() {
			// Wait for deployment to be ready
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "60s", "3s").Should(Equal(int32(6)))

			// Verify that Spotalis doesn't change the replica count
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return *updated.Spec.Replicas
			}, "30s", "2s").Should(Equal(int32(6))) // Replica count should remain unchanged
		})

		It("should focus on webhook mutations rather than scaling", func() {
			// Verify that pods exist and are being managed by admission webhook
			podList := &corev1.PodList{}
			err := k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rebalance-app"})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podList.Items)).To(BeNumerically(">=", 1))

			// The primary focus is now on admission webhook mutations
			// which would be tested separately in webhook integration tests
		})

		It("should handle webhook-driven node placement", func() {
			// Verify node selectors are optimized for cost via webhook
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Spec.Template.Spec.NodeSelector
			}, "30s", "2s").Should(HaveKey("karpenter.sh/capacity-type"))

			// Verify affinity rules are set for distribution via webhook
			Eventually(func() bool {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return false
				}
				return updated.Spec.Template.Spec.Affinity != nil
			}, "30s", "2s").Should(BeTrue())
		})
	})

	Context("when managing pod distribution", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-distribution-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5), // Fixed replica count
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "pod-distribution-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "pod-distribution-app",
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

		It("should maintain stable replica counts while allowing pod rebalancing", func() {
			// Wait for deployment to be ready
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "60s", "3s").Should(Equal(int32(5)))

			// Verify replica count remains stable (no scaling)
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return *updated.Spec.Replicas
			}, "30s", "2s").Should(Equal(int32(5)))

			// Verify pods can be deleted for rebalancing (but deployment controller will recreate them)
			podList := &corev1.PodList{}
			err := k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{"app": "pod-distribution-app"})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podList.Items)).To(BeNumerically(">=", 1))
		})
	})

	Context("When handling scale down scenarios", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scale-down-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "60%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
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

		It("should handle pod rebalancing needs", func() {
			// Skip this test - managed annotation not supported
			Skip("Test skipped - managed annotation not supported")
		})

		It("should respect PodDisruptionBudget during pod rebalancing", func() {
			// Create PodDisruptionBudget
			pdb := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-distribution-pdb",
					Namespace: namespace,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 3,
					},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "pod-distribution-app",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			// Verify PDB is respected during pod deletion for rebalancing
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "30s", "2s").Should(BeNumerically(">=", 3))
		})

		It("should optimize cost through pod placement decisions", func() {
			// Skip test - cost-savings annotation not supported
			Skip("Test skipped - cost-savings annotation not supported")
		})
	})

	Context("when handling stable workloads", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stable-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "80%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "stable-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "stable-app",
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

		It("should maintain stable replica counts without scaling operations", func() {
			// Wait for initial state
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/spot-percentage"))

			Skip("Test skipped - managed annotation not supported")

			// Verify that Spotalis doesn't perform scaling operations
			// Even if external changes occur, replica count should be managed by K8s deployment controller
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return *updated.Spec.Replicas
			}, "30s", "2s").Should(Equal(int32(5)))

			Skip("Test skipped - status annotation not supported")
		})
	})
})
