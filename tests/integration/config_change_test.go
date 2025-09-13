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

func TestConfigChangeIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Configuration Change Integration Suite")
}

var _ = Describe("Configuration change scenario", func() {
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

		namespace = "spotalis-config-test-" + randString(6)
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

		// Start with initial configuration
		operatorConfig := &config.OperatorConfig{
			ReconcileInterval: time.Duration(30) * time.Second,
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

	Context("when changing reconcile interval configuration", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-test-deployment",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "spot-optimized",
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
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should apply new reconcile interval immediately", func() {
			// Verify initial interval is working
			initialMetrics := spotalisOp.GetMetrics()

			// Update configuration to faster reconcile interval
			newConfig := &config.OperatorConfig{
				ReconcileInterval: time.Duration(5) * time.Second, // Minimum allowed
			}

			err := spotalisOp.UpdateConfiguration(newConfig)
			Expect(err).NotTo(HaveOccurred())

			// Verify new interval is applied
			Eventually(func() time.Duration {
				return spotalisOp.GetReconcileInterval()
			}, "10s", "1s").Should(Equal(time.Duration(5) * time.Second))

			// Verify reconciliation happens more frequently
			Eventually(func() int {
				currentMetrics := spotalisOp.GetMetrics()
				return currentMetrics.ReconciliationCount
			}, "15s", "1s").Should(BeNumerically(">", initialMetrics.ReconciliationCount+1))
		})

		It("should reject invalid reconcile intervals", func() {
			// Try to set interval below minimum (5s)
			invalidConfig := &config.OperatorConfig{
				ReconcileInterval: time.Duration(2) * time.Second,
			}

			err := spotalisOp.UpdateConfiguration(invalidConfig)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("minimum reconcile interval is 5s"))

			// Verify original interval is maintained
			Expect(spotalisOp.GetReconcileInterval()).To(Equal(time.Duration(30) * time.Second))
		})

		It("should handle configuration reload from environment", func() {
			// Simulate environment variable change
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "spotalis-config",
					Namespace: namespace,
				},
				Data: map[string]string{
					"reconcile-interval": "10s",
					"namespace-selector": `{"matchLabels":{"spotalis.io/enabled":"true"}}`,
				},
			}
			err := k8sClient.Create(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			// Trigger configuration reload
			err = spotalisOp.ReloadConfiguration()
			Expect(err).NotTo(HaveOccurred())

			// Verify new configuration is applied
			Eventually(func() time.Duration {
				return spotalisOp.GetReconcileInterval()
			}, "10s", "1s").Should(Equal(time.Duration(10) * time.Second))
		})
	})

	Context("when changing namespace selector configuration", func() {
		var (
			managedNS   string
			unmanagedNS string
		)

		BeforeEach(func() {
			// Create additional namespaces for testing
			managedNS = "managed-" + randString(6)
			managedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedNS,
					Labels: map[string]string{
						"spotalis.io/enabled": "true",
						"team":                "alpha",
					},
				},
			}
			err := k8sClient.Create(ctx, managedNamespace)
			Expect(err).NotTo(HaveOccurred())

			unmanagedNS = "unmanaged-" + randString(6)
			unmanagedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: unmanagedNS,
					Labels: map[string]string{
						"team": "beta",
					},
				},
			}
			err = k8sClient.Create(ctx, unmanagedNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update namespace filtering when selector changes", func() {
			// Deploy workloads to both namespaces
			deployment1 := createTestDeployment("team-alpha-app", managedNS)
			err := k8sClient.Create(ctx, deployment1)
			Expect(err).NotTo(HaveOccurred())

			deployment2 := createTestDeployment("team-beta-app", unmanagedNS)
			err = k8sClient.Create(ctx, deployment2)
			Expect(err).NotTo(HaveOccurred())

			// Initially only team-alpha should be managed
			Eventually(func() int {
				metrics := spotalisOp.GetMetrics()
				return metrics.WorkloadsManaged
			}, "15s", "1s").Should(Equal(1))

			// Change selector to include team beta
			newConfig := &config.OperatorConfig{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"team": "beta",
					},
				},
			}

			err = spotalisOp.UpdateConfiguration(newConfig)
			Expect(err).NotTo(HaveOccurred())

			// Now only team-beta should be managed
			Eventually(func() int {
				metrics := spotalisOp.GetMetrics()
				return metrics.WorkloadsManaged
			}, "20s", "1s").Should(Equal(1))

			// Verify team-alpha is no longer managed
			var alphaDeployment appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment1), &alphaDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(alphaDeployment.Annotations).NotTo(HaveKey("spotalis.io/managed"))
		})

		It("should handle wildcard namespace selector", func() {
			// Set selector to nil (manage all namespaces)
			newConfig := &config.OperatorConfig{
				NamespaceSelector: nil,
			}

			err := spotalisOp.UpdateConfiguration(newConfig)
			Expect(err).NotTo(HaveOccurred())

			// Deploy to unmanaged namespace
			deployment := createTestDeployment("wildcard-app", unmanagedNS)
			err = k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Should now be managed even without labels
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

	Context("when configuration changes affect existing workloads", func() {
		It("should gracefully handle annotation changes", func() {
			// Deploy workload with specific annotations
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "annotation-test",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy":     "spot-optimized",
						"spotalis.io/disruption-tolerance": "low",
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
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for initial processing
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "15s", "1s").Should(HaveKey("spotalis.io/managed"))

			// Change annotation strategy
			var currentDeployment appsv1.Deployment
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &currentDeployment)
			Expect(err).NotTo(HaveOccurred())

			currentDeployment.Annotations["spotalis.io/replica-strategy"] = "cost-optimized"
			currentDeployment.Annotations["spotalis.io/disruption-tolerance"] = "high"
			err = k8sClient.Update(ctx, &currentDeployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify configuration change is processed
			Eventually(func() string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return ""
				}
				return updated.Annotations["spotalis.io/current-strategy"]
			}, "20s", "2s").Should(Equal("cost-optimized"))
		})
	})
})

func createTestDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"spotalis.io/replica-strategy": "spot-optimized",
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
