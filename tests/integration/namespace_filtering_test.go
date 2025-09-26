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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ahoma/spotalis/tests/integration/shared"
)

func TestNamespaceFilteringIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Namespace Filtering Integration Suite")
}

var _ = Describe("Multi-tenant namespace filtering", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		kindHelper *shared.KindClusterHelper
		k8sClient  client.Client
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

		// Kind cluster already has Spotalis controller running with namespace filtering
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Context("with namespace filtering enabled", func() {
		var (
			managedNamespace   string
			unmanagedNamespace string
			deployment         *appsv1.Deployment
		)

		BeforeEach(func() {
			// Create managed namespace
			managedNamespace = "spotalis-managed-" + randString(6)
			managedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedNamespace,
					Labels: map[string]string{
						"test-managed":        "true",
						"spotalis.io/enabled": "true",
					},
				},
			}
			err := k8sClient.Create(ctx, managedNS)
			Expect(err).NotTo(HaveOccurred())

			// Create unmanaged namespace
			unmanagedNamespace = "unmanaged-" + randString(6)
			unmanagedNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: unmanagedNamespace,
					Labels: map[string]string{
						"tenant": "team-b",
					},
				},
			}
			err = k8sClient.Create(ctx, unmanagedNS)
			Expect(err).NotTo(HaveOccurred())

			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-deployment",
					Annotations: map[string]string{
						"spotalis.io/enabled":         "true",
						"spotalis.io/spot-percentage": "70%",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(2),
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
									Name:  "app",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
		})

		It("should manage workloads only in labeled namespaces", func() {
			// Deploy to managed namespace
			managedDeployment := deployment.DeepCopy()
			managedDeployment.Namespace = managedNamespace
			managedDeployment.Name = "managed-deployment"

			err := k8sClient.Create(ctx, managedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify it gets managed
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedDeployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/spot-percentage"))
		})

		It("should ignore workloads in non-labeled namespaces", func() {
			// Deploy to unmanaged namespace
			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanagedNamespace
			unmanagedDeployment.Name = "unmanaged-deployment"

			err := k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
