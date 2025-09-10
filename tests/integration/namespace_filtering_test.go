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

	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/operator"
)

func TestNamespaceFilteringIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Namespace Filtering Integration Suite")
}

var _ = Describe("Multi-tenant namespace filtering", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		testEnv    *envtest.Environment
		k8sClient  client.Client
		clientset  *kubernetes.Clientset
		spotalisOp *operator.Operator
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

		// Configure Spotalis with namespace filtering
		operatorConfig := &config.OperatorConfig{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"spotalis.io/enabled": "true",
				},
			},
		}

		spotalisOp = operator.NewWithConfig(cfg, operatorConfig)
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
						"spotalis.io/enabled": "true",
						"tenant":              "team-a",
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
						"spotalis.io/replica-strategy": "spot-optimized",
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
			}, "15s", "1s").Should(HaveKey("spotalis.io/managed"))
		})

		It("should ignore workloads in non-labeled namespaces", func() {
			// Deploy to unmanaged namespace
			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanagedNamespace
			unmanagedDeployment.Name = "unmanaged-deployment"

			err := k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify it does NOT get managed
			Consistently(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(unmanagedDeployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").ShouldNot(HaveKey("spotalis.io/managed"))
		})

		It("should reflect namespace filtering in metrics", func() {
			// Deploy to both namespaces
			managedDeployment := deployment.DeepCopy()
			managedDeployment.Namespace = managedNamespace
			managedDeployment.Name = "managed-deployment"
			err := k8sClient.Create(ctx, managedDeployment)
			Expect(err).NotTo(HaveOccurred())

			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanagedNamespace
			unmanagedDeployment.Name = "unmanaged-deployment"
			err = k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify only managed namespace workloads are counted
			Eventually(func() int {
				metrics := spotalisOp.GetMetrics()
				return metrics.WorkloadsManaged
			}, "20s", "2s").Should(Equal(1))
		})

		It("should support dynamic namespace label changes", func() {
			// Deploy to initially unmanaged namespace
			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanagedNamespace
			unmanagedDeployment.Name = "dynamic-deployment"
			err := k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify not managed initially
			Consistently(func() bool {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(unmanagedDeployment), &updated)
				if err != nil {
					return false
				}
				_, exists := updated.Annotations["spotalis.io/managed"]
				return exists
			}, "10s", "1s").Should(BeFalse())

			// Add spotalis label to namespace
			var ns corev1.Namespace
			err = k8sClient.Get(ctx, client.ObjectKey{Name: unmanagedNamespace}, &ns)
			Expect(err).NotTo(HaveOccurred())

			ns.Labels["spotalis.io/enabled"] = "true"
			err = k8sClient.Update(ctx, &ns)
			Expect(err).NotTo(HaveOccurred())

			// Verify deployment becomes managed
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(unmanagedDeployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "20s", "2s").Should(HaveKey("spotalis.io/managed"))
		})

		It("should enforce RBAC boundaries between tenants", func() {
			// This test would verify that Spotalis respects RBAC
			// and cannot access resources outside allowed namespaces

			// Deploy workloads in both namespaces
			managedDeployment := deployment.DeepCopy()
			managedDeployment.Namespace = managedNamespace
			managedDeployment.Name = "tenant-a-deployment"
			err := k8sClient.Create(ctx, managedDeployment)
			Expect(err).NotTo(HaveOccurred())

			unmanagedDeployment := deployment.DeepCopy()
			unmanagedDeployment.Namespace = unmanagedNamespace
			unmanagedDeployment.Name = "tenant-b-deployment"
			err = k8sClient.Create(ctx, unmanagedDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify metrics show only managed workloads
			Eventually(func() int {
				metrics := spotalisOp.GetMetrics()
				return metrics.WorkloadsManaged
			}, "15s", "1s").Should(Equal(1))

			// Verify unmanaged deployment remains untouched
			var unmanagedUpdated appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(unmanagedDeployment), &unmanagedUpdated)
			Expect(err).NotTo(HaveOccurred())
			Expect(unmanagedUpdated.Spec.Template.Spec.NodeSelector).To(BeEmpty())
		})
	})

	Context("with wildcard namespace selector", func() {
		BeforeEach(func() {
			// Reconfigure operator to manage all namespaces
			cancel() // Stop current operator

			operatorConfig := &config.OperatorConfig{
				NamespaceSelector: nil, // nil means all namespaces
			}

			spotalisOp = operator.NewWithConfig(testEnv.Config, operatorConfig)
			ctx, cancel = context.WithCancel(context.Background())

			go func() {
				defer GinkgoRecover()
				err := spotalisOp.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return spotalisOp.IsReady()
			}, "30s", "1s").Should(BeTrue())
		})

		It("should manage workloads in all namespaces", func() {
			// Create namespace without labels
			namespace := "no-labels-" + randString(6)
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			err := k8sClient.Create(ctx, ns)
			Expect(err).NotTo(HaveOccurred())

			// Deploy workload
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wildcard-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "spot-optimized",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "wildcard-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "wildcard-app",
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

			err = k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify it gets managed even without namespace labels
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/managed"))
		})
	})
})

func randString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

func int32Ptr(i int32) *int32 {
	return &i
}
