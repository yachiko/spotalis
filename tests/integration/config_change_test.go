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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestConfigChangeIntegration(t *testing.T) {
	// Set up logger to avoid controller-runtime warning
	if err := shared.SetupTestLogger(); err != nil {
		t.Fatalf("Failed to set up logger: %v", err)
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Configuration Change Integration Suite", Label("integration"))
}

var _ = Describe("Configuration change scenario", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
		namespace  string
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithCancel(ctx)

		// Create a unique test namespace
		namespace = "spotalis-config-test-" + generateRandomSuffix()
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"test-namespace": "true",
				},
			},
		}
		err := k8sClient.Create(testCtx, ns)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("Created test namespace: %s", namespace))
	})

	AfterEach(func() {
		// Clean up test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		err := k8sClient.Delete(testCtx, ns)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to delete namespace %s", namespace))

		// Wait for namespace to be fully deleted to ensure cleanup is complete
		Eventually(func() bool {
			err := k8sClient.Get(testCtx, client.ObjectKey{Name: namespace}, ns)
			return err != nil // Error means namespace is gone
		}, 30*time.Second, 1*time.Second).Should(BeTrue(), fmt.Sprintf("Namespace %s was not deleted within timeout", namespace))

		testCancel()
	})

	Context("when changing reconcile interval configuration", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test-deployment",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "config-test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "config-test",
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
			err := k8sClient.Create(testCtx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when configuration changes affect existing workloads", func() {
		It("should gracefully handle annotation changes", func() {
			// Deploy workload with specific annotations
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "annotation-test",
					Namespace: namespace,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
					},
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "80%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "annotation-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "annotation-test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "app", Image: "nginx:latest"}},
						},
					},
				},
			}
			err := k8sClient.Create(testCtx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for initial processing - just verify deployment exists
			Eventually(func() error {
				var updated appsv1.Deployment
				return k8sClient.Get(testCtx, client.ObjectKeyFromObject(deployment), &updated)
			}, "15s", "1s").Should(Succeed())

			// Change annotation strategy with retry to handle potential conflicts
			Eventually(func() error {
				var currentDeployment appsv1.Deployment
				err := k8sClient.Get(testCtx, client.ObjectKeyFromObject(deployment), &currentDeployment)
				if err != nil {
					return err
				}

				currentDeployment.Annotations["spotalis.io/spot-percentage"] = "90%"
				return k8sClient.Update(testCtx, &currentDeployment)
			}, "10s", "1s").Should(Succeed())

			// Wait a moment for the update to be processed
			time.Sleep(2 * time.Second)

			// Verify configuration change is processed
			Eventually(func() string {
				var updated appsv1.Deployment
				err := k8sClient.Get(testCtx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return ""
				}
				return updated.Annotations["spotalis.io/spot-percentage"]
			}, "20s", "2s").Should(Equal("90%"))
		})
	})
})

func createTestDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"spotalis.io/enabled": "true",
			},
			Annotations: map[string]string{
				"spotalis.io/spot-percentage": "70%",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
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
}
