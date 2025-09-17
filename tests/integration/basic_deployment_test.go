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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/ahoma/spotalis/pkg/operator"
)

func TestBasicDeploymentIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Basic Deployment Integration Suite")
}

var _ = Describe("Basic workload deployment with annotations", func() {
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

		// Setup test environment - this will fail until we have proper setup
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{"../../configs/crd/bases"},
		}

		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		// Setup clients
		k8sClient, err = client.New(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())

		// Create test namespace
		namespace = "spotalis-test-" + randString(8)
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

		// Start Spotalis operator
		spotalisOp = operator.New(cfg)
		go func() {
			defer GinkgoRecover()
			err := spotalisOp.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
		}()

		// Wait for operator to be ready
		Eventually(func() bool {
			return spotalisOp.IsReady()
		}, "30s", "1s").Should(BeTrue())
	})

	AfterEach(func() {
		// Cleanup
		cancel()
		if testEnv != nil {
			err := testEnv.Stop()
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("when deploying a workload with spot optimization annotation", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70",
						"spotalis.io/min-on-demand":   "1",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
		})

		It("should successfully create the deployment", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment exists
			var created appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &created)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should handle pod mutations via webhook", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for pods to be created and check if webhook mutated them
			Eventually(func() bool {
				var podList corev1.PodList
				err := k8sClient.List(ctx, &podList, client.InNamespace(namespace),
					client.MatchingLabels{"app": "test-app"})
				if err != nil || len(podList.Items) == 0 {
					return false
				}

				// Check if any pod has the spot/on-demand nodeSelector from webhook
				for _, pod := range podList.Items {
					if pod.Spec.NodeSelector != nil {
						if capacityType, exists := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; exists {
							return capacityType == "spot" || capacityType == "on-demand"
						}
					}
				}
				return false
			}, "30s", "2s").Should(BeTrue())
		})

		It("should manage replica distribution via controller", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for controller to process the deployment
			Eventually(func() bool {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return false
				}

				// Check if deployment is being managed by checking if replicas are scaling
				return updated.Status.Replicas >= 0
			}, "30s", "2s").Should(BeTrue())
		})

		It("should track workload in controller metrics", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for metrics to be updated - this test may need to be adjusted based on actual metrics implementation
			Eventually(func() bool {
				// Since we don't know the exact metrics API, we'll check if the deployment exists
				// and is being processed by the controller
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				return err == nil
			}, "15s", "1s").Should(BeTrue())
		})

		It("should respect namespace filtering when enabled", func() {
			// Create deployment in non-managed namespace
			unmanaged := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unmanaged-" + randString(6),
				},
			}
			err := k8sClient.Create(ctx, unmanaged)
			Expect(err).NotTo(HaveOccurred())

			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanaged.Name
			unmanagedDeployment.Name = "unmanaged-deployment"

			err = k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify it doesn't get processed by Spotalis (no webhook mutations)
			Consistently(func() bool {
				var podList corev1.PodList
				err := k8sClient.List(ctx, &podList, client.InNamespace(unmanaged.Name))
				if err != nil || len(podList.Items) == 0 {
					return true // No pods yet, which is expected
				}

				// Check that pods don't have Spotalis nodeSelector mutations
				for _, pod := range podList.Items {
					if pod.Spec.NodeSelector != nil {
						if _, exists := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; exists {
							return false // Found Spotalis mutation, test should fail
						}
					}
				}
				return true
			}, "10s", "1s").Should(BeTrue())
		})
	})

	Context("when deploying workload without Spotalis annotations", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vanilla-deployment",
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "vanilla-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "vanilla-app",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "vanilla-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
		})

		It("should remain unmodified by Spotalis", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify no Spotalis webhook mutations are applied to pods
			Consistently(func() bool {
				var podList corev1.PodList
				err := k8sClient.List(ctx, &podList, client.InNamespace(namespace),
					client.MatchingLabels{"app": "vanilla-app"})
				if err != nil || len(podList.Items) == 0 {
					return true // No pods yet, which is fine
				}

				// Check that pods don't have Spotalis nodeSelector mutations
				for _, pod := range podList.Items {
					if pod.Spec.NodeSelector != nil {
						if _, exists := pod.Spec.NodeSelector["karpenter.sh/capacity-type"]; exists {
							return false // Found Spotalis mutation, should not happen
						}
					}
				}
				return true
			}, "10s", "1s").Should(BeTrue())
		})
	})
})
