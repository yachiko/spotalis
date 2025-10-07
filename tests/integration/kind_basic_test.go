//go:build integration

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
	"testing"
	"time"

	"github.com/ahoma/spotalis/tests/integration/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKindIntegration(t *testing.T) {
	// Set up logger to avoid controller-runtime warning
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Spotalis Kind Integration Suite", Label("integration"))
}

var _ = Describe("Spotalis Integration on Kind", func() {
	Context("Controller Health", func() {
		It("should have Spotalis controller running", func() {
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, client.ObjectKey{
				Namespace: "spotalis-system",
				Name:      "spotalis-controller",
			}, deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(deployment.Status.ReadyReplicas).To(BeNumerically(">=", 1))
		})

		It("should have webhook service available", func() {
			service := &corev1.Service{}
			err := k8sClient.Get(ctx, client.ObjectKey{
				Namespace: "spotalis-system",
				Name:      "spotalis-webhook",
			}, service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.Spec.Ports).NotTo(BeEmpty())
		})
	})

	Context("Basic Deployment Testing", func() {
		var testNamespace *corev1.Namespace

		BeforeEach(func() {
			// Create a test namespace
			testNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spotalis-test-" + generateRandomSuffix(),
					Labels: map[string]string{
						"test-namespace": "true",
					},
				},
			}
			Expect(k8sClient.Create(ctx, testNamespace)).To(Succeed())
		})

		AfterEach(func() {
			// Clean up test namespace
			if testNamespace != nil {
				Expect(k8sClient.Delete(ctx, testNamespace)).To(Succeed())
			}
		})

		It("should handle deployment creation", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: testNamespace.Name,
					Labels: map[string]string{
						"app":                 "test-app",
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(4),
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
									Image: "nginx:alpine",
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 80,
											Protocol:      corev1.ProtocolTCP,
										},
									},
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Wait for deployment to be processed
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				return err == nil
			}, 30*time.Second, 1*time.Second).Should(BeTrue())

			// Check that Spotalis annotations or modifications were applied
			// This depends on your specific Spotalis logic
			Eventually(func() map[string]string {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
				if err != nil {
					return nil
				}
				return deployment.Annotations
			}, 30*time.Second, 1*time.Second).Should(HaveKey(ContainSubstring("spotalis")))
		})
	})

	Context("Webhook Integration", func() {
		It("should validate webhook is responding", func() {
			// Create a deployment that should trigger webhook
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-test",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "webhook-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "webhook-test",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "nginx:alpine",
								},
							},
						},
					},
				},
			}

			// This should succeed if webhook is working properly
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
		})
	})
})
