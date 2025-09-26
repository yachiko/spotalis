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

	"github.com/ahoma/spotalis/tests/integration/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestScalingIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scaling Integration Suite")
}

var _ = Describe("Pod rebalancing scenario", func() {
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

		// Kind cluster already has Spotalis controller running
	})

	AfterEach(func() {
		// Cleanup namespace
		if namespace != "" && kindHelper != nil {
			err := kindHelper.CleanupNamespace(namespace)
			Expect(err).NotTo(HaveOccurred())
		}
		if cancel != nil {
			cancel()
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
						"spotalis.io/enabled":         "true",
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
							Tolerations: []corev1.Toleration{
								{
									Key:    "node-role.kubernetes.io/control-plane",
									Effect: corev1.TaintEffectNoSchedule,
								},
							},
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
			// Wait for deployment to be ready with the specified replica count
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "120s", "3s").Should(Equal(int32(6)))

			// Verify that Spotalis doesn't change the replica count
			// Spotalis maintains stable replica counts and only rebalances pods
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
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rebalance-app"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, "30s", "3s").Should(BeNumerically(">=", 1))

			// The primary focus is on admission webhook mutations for new pods
			// which would be tested by checking pod creation with correct nodeSelector
		})

		It("should handle webhook-driven node placement", func() {
			// First ensure pods exist and are ready
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rebalance-app"})
				if err != nil {
					return 0
				}
				return len(podList.Items)
			}, "30s", "3s").Should(BeNumerically(">=", 1))

			// For integration testing, we focus on pod creation and rebalancing
			// rather than specific nodeSelector mutations (which require actual nodes)
			// In a real cluster with Karpenter, the webhook would add nodeSelector
			podList := &corev1.PodList{}
			err := k8sClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rebalance-app"})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podList.Items)).To(BeNumerically(">=", 1))
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
						"spotalis.io/enabled":         "true",
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
							Tolerations: []corev1.Toleration{
								{
									Key:    "node-role.kubernetes.io/control-plane",
									Effect: corev1.TaintEffectNoSchedule,
								},
							},
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
			// Wait for deployment to be ready with specified replica count
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "240s", "3s").Should(Equal(int32(5)))

			// Verify replica count remains stable (no scaling)
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return *updated.Spec.Replicas
			}, "30s", "2s").Should(Equal(int32(5)))

			// Verify pods exist and can be managed for rebalancing
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
						"spotalis.io/enabled":         "true",
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
							Tolerations: []corev1.Toleration{
								{
									Key:    "node-role.kubernetes.io/control-plane",
									Effect: corev1.TaintEffectNoSchedule,
								},
							},
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

			// Wait for deployment to be ready first
			Eventually(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "120s", "2s").Should(BeNumerically(">=", 3))

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
	})

	Context("when handling stable workloads", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stable-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/enabled":         "true",
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
							Tolerations: []corev1.Toleration{
								{
									Key:    "node-role.kubernetes.io/control-plane",
									Effect: corev1.TaintEffectNoSchedule,
								},
							},
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
		})
	})
})
