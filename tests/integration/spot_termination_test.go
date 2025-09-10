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

func TestSpotTerminationIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spot Termination Integration Suite")
}

var _ = Describe("Spot node termination scenario", func() {
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

		namespace = "spotalis-spot-test-" + randString(6)
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

		spotalisOp = operator.New(cfg)
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

	Context("when a spot instance receives termination notice", func() {
		var (
			deployment *appsv1.Deployment
			spotNode   *corev1.Node
		)

		BeforeEach(func() {
			// Create spot node
			spotNode = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spot-node-" + randString(6),
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "m5.large",
						"karpenter.sh/capacity-type":       "spot",
					},
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws:///us-west-2a/i-" + randString(17),
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			err := k8sClient.Create(ctx, spotNode)
			Expect(err).NotTo(HaveOccurred())

			// Create deployment that will be scheduled on spot nodes
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "spot-workload",
					Namespace: namespace,
					Annotations: map[string]string{
						"spotalis.io/replica-strategy":     "spot-optimized",
						"spotalis.io/disruption-tolerance": "high",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "spot-app",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "spot-app",
							},
						},
						Spec: corev1.PodSpec{
							NodeSelector: map[string]string{
								"karpenter.sh/capacity-type": "spot",
							},
							Containers: []corev1.Container{
								{
									Name:  "app-container",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			err = k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should detect spot interruption signal", func() {
			// Simulate spot interruption by adding label
			spotNode.Labels["spotalis.io/spot-interruption"] = "2-minute-warning"
			err := k8sClient.Update(ctx, spotNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify Spotalis detects the interruption
			Eventually(func() bool {
				metrics := spotalisOp.GetMetrics()
				return metrics.SpotInterruptions > 0
			}, "30s", "2s").Should(BeTrue())
		})

		It("should proactively drain workloads from terminating nodes", func() {
			// Simulate spot interruption
			spotNode.Labels["spotalis.io/spot-interruption"] = "2-minute-warning"
			err := k8sClient.Update(ctx, spotNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify pods are rescheduled
			Eventually(func() int {
				var pods corev1.PodList
				err := k8sClient.List(ctx, &pods, client.InNamespace(namespace))
				if err != nil {
					return 0
				}

				runningPods := 0
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning && pod.Spec.NodeName != spotNode.Name {
						runningPods++
					}
				}
				return runningPods
			}, "60s", "5s").Should(BeNumerically(">=", 3))
		})

		It("should maintain minimum replica count during termination", func() {
			// Simulate spot interruption
			spotNode.Labels["spotalis.io/spot-interruption"] = "2-minute-warning"
			err := k8sClient.Update(ctx, spotNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify replica count is maintained
			Consistently(func() int32 {
				var updated appsv1.Deployment
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), &updated)
				if err != nil {
					return 0
				}
				return updated.Status.ReadyReplicas
			}, "30s", "2s").Should(BeNumerically(">=", 3))
		})

		It("should update disruption metrics", func() {
			// Record initial metrics
			initialMetrics := spotalisOp.GetMetrics()

			// Simulate spot interruption
			spotNode.Labels["spotalis.io/spot-interruption"] = "2-minute-warning"
			err := k8sClient.Update(ctx, spotNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify metrics are updated
			Eventually(func() bool {
				currentMetrics := spotalisOp.GetMetrics()
				return currentMetrics.SpotInterruptions > initialMetrics.SpotInterruptions
			}, "30s", "2s").Should(BeTrue())
		})

		It("should respect disruption budgets during evacuation", func() {
			// Create PodDisruptionBudget
			pdb := &metav1.PartialObjectMetadata{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "policy/v1",
					Kind:       "PodDisruptionBudget",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "spot-app-pdb",
					Namespace: namespace,
				},
			}
			pdb.Object = map[string]interface{}{
				"spec": map[string]interface{}{
					"minAvailable": 2,
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app": "spot-app",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, pdb)
			Expect(err).NotTo(HaveOccurred())

			// Simulate spot interruption
			spotNode.Labels["spotalis.io/spot-interruption"] = "2-minute-warning"
			err = k8sClient.Update(ctx, spotNode)
			Expect(err).NotTo(HaveOccurred())

			// Verify minimum pods remain available
			Consistently(func() int {
				var pods corev1.PodList
				err := k8sClient.List(ctx, &pods, client.InNamespace(namespace))
				if err != nil {
					return 0
				}

				availablePods := 0
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning {
						availablePods++
					}
				}
				return availablePods
			}, "45s", "3s").Should(BeNumerically(">=", 2))
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
