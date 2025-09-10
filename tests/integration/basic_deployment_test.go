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
		clientset  *kubernetes.Clientset
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

		clientset, err = kubernetes.NewForConfig(cfg)
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
						"spotalis.io/replica-strategy":     "spot-optimized",
						"spotalis.io/cost-optimization":    "enabled",
						"spotalis.io/disruption-tolerance": "medium",
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

		It("should add Spotalis management annotations", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for webhook to process
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "10s", "1s").Should(HaveKey("spotalis.io/managed"))
		})

		It("should set appropriate node selectors for spot instances", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for controller to process
			Eventually(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Spec.Template.Spec.NodeSelector
			}, "30s", "2s").Should(HaveKey("node.kubernetes.io/instance-type"))
		})

		It("should configure pod disruption budget", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for controller to create PodDisruptionBudget
			Eventually(func() bool {
				pdbList := &metav1.PartialObjectMetadataList{}
				pdbList.SetGroupVersionKind(metav1.GroupVersionKind{
					Group:   "policy",
					Version: "v1",
					Kind:    "PodDisruptionBudget",
				})
				err := k8sClient.List(ctx, pdbList, client.InNamespace(namespace))
				return err == nil && len(pdbList.Items) > 0
			}, "30s", "2s").Should(BeTrue())
		})

		It("should track workload in controller metrics", func() {
			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Wait for metrics to be updated
			Eventually(func() bool {
				metrics := spotalisOp.GetMetrics()
				return metrics.WorkloadsManaged > 0
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

			// Verify it doesn't get Spotalis annotations
			Consistently(func() map[string]string {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(unmanagedDeployment), &updated)
				if err != nil {
					return nil
				}
				return updated.Annotations
			}, "10s", "1s").ShouldNot(HaveKey("spotalis.io/managed"))
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
			originalAnnotations := make(map[string]string)
			for k, v := range deployment.Annotations {
				originalAnnotations[k] = v
			}

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())

			// Verify no Spotalis annotations are added
			Consistently(func() bool {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return false
				}
				for key := range updated.Annotations {
					if key == "spotalis.io/managed" {
						return false
					}
				}
				return true
			}, "10s", "1s").Should(BeTrue())
		})
	})
})

func int32Ptr(i int32) *int32 {
	return &i
}

func randString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
