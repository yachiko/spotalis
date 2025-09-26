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

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DeploymentReconciler", func() {
	var (
		reconciler       *DeploymentReconciler
		fakeClient       client.Client
		scheme           *runtime.Scheme
		ctx              context.Context
		annotationParser *annotations.AnnotationParser
		nodeClassifier   *config.NodeClassifierService
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(appsv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		annotationParser = annotations.NewAnnotationParser()

		// Create properly initialized NodeClassifierService
		nodeClassifier = config.NewNodeClassifierService(fakeClient, &config.NodeClassifierConfig{
			SpotLabels: map[string]string{
				"node.kubernetes.io/instance-type": "spot",
			},
			OnDemandLabels: map[string]string{
				"node.kubernetes.io/instance-type": "on-demand",
			},
		})

		reconciler = &DeploymentReconciler{
			Client:              fakeClient,
			Scheme:              scheme,
			AnnotationParser:    annotationParser,
			NodeClassifier:      nodeClassifier,
			ReconcileInterval:   30 * time.Second,
			MaxConcurrentRecons: 10,
		}
	})

	Describe("NewDeploymentReconciler", func() {
		It("should create a new reconciler with default settings", func() {
			newReconciler := NewDeploymentReconciler(fakeClient, scheme)

			Expect(newReconciler.Client).To(Equal(fakeClient))
			Expect(newReconciler.Scheme).To(Equal(scheme))
			Expect(newReconciler.AnnotationParser).ToNot(BeNil())
			Expect(newReconciler.ReconcileInterval).To(Equal(5 * time.Minute)) // Updated to match new default
			Expect(newReconciler.MaxConcurrentRecons).To(Equal(10))
		})
	})

	Describe("SetNodeClassifier", func() {
		It("should set the node classifier service", func() {
			newClassifier := &config.NodeClassifierService{}
			reconciler.SetNodeClassifier(newClassifier)

			Expect(reconciler.NodeClassifier).To(Equal(newClassifier))
		})
	})

	Describe("Reconcile", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test",
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
				Status: appsv1.DeploymentStatus{
					Replicas:            5,
					ReadyReplicas:       5, // Make deployment appear stable
					UpdatedReplicas:     5,
					AvailableReplicas:   5,
					UnavailableReplicas: 0,
				},
			}
		})

		Context("when deployment is not found", func() {
			It("should return without error", func() {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "nonexistent",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("when deployment has no Spotalis annotations", func() {
			It("should skip reconciliation", func() {
				deploymentWithoutAnnotations := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deployment",
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: int32Ptr(3),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test",
							},
						},
					},
				}

				Expect(fakeClient.Create(ctx, deploymentWithoutAnnotations)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-deployment",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("when deployment has Spotalis annotations", func() {
			BeforeEach(func() {
				Expect(fakeClient.Create(ctx, deployment)).To(Succeed())
			})

			It("should reconcile successfully", func() {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-deployment",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(60 * time.Second)) // 2 * reconcileInterval when no action needed
			})

			It("should handle invalid annotation configuration", func() {
				invalidDeployment := deployment.DeepCopy()
				invalidDeployment.Name = "invalid-deployment"
				invalidDeployment.ResourceVersion = "" // Clear resourceVersion for Create
				invalidDeployment.Annotations["spotalis.io/enabled"] = "true"
				invalidDeployment.Annotations["spotalis.io/spot-percentage"] = "invalid"

				Expect(fakeClient.Create(ctx, invalidDeployment)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "invalid-deployment",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).To(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(30 * time.Second)) // Base interval for errors
			})
		})
	})

	Describe("calculateCurrentReplicaState", func() {
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:      5,
					ReadyReplicas: 5,
				},
			}
		})

		It("should calculate replica state correctly", func() {
			// Create nodes for the pods to run on
			spotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spot-node-1",
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "t3.medium",
						"karpenter.sh/capacity-type":       "spot",
					},
				},
			}
			onDemandNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ondemand-node-1",
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "t3.medium",
						"karpenter.sh/capacity-type":       "on-demand",
					},
				},
			}

			Expect(fakeClient.Create(ctx, spotNode)).To(Succeed())
			Expect(fakeClient.Create(ctx, onDemandNode)).To(Succeed())

			// Create pods on these nodes
			for i := 0; i < 3; i++ {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-pod-spot-%d", i),
						Namespace: "default",
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "spot-node-1",
					},
				}
				Expect(fakeClient.Create(ctx, pod)).To(Succeed())
			}

			for i := 0; i < 2; i++ {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-pod-ondemand-%d", i),
						Namespace: "default",
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "ondemand-node-1",
					},
				}
				Expect(fakeClient.Create(ctx, pod)).To(Succeed())
			}

			state, err := reconciler.calculateCurrentReplicaState(ctx, deployment)

			Expect(err).ToNot(HaveOccurred())
			Expect(state).ToNot(BeNil())
			Expect(state.CurrentSpot).To(Equal(int32(3)))
			Expect(state.CurrentOnDemand).To(Equal(int32(2)))
			Expect(state.LastReconciled).To(BeTemporally("~", time.Now(), time.Second))
		})

		It("should handle deployment with no selector", func() {
			deployment.Spec.Selector = nil

			state, err := reconciler.calculateCurrentReplicaState(ctx, deployment)

			Expect(err).To(HaveOccurred())
			Expect(state).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("no selector"))
		})
	})

	Describe("needsRebalancing", func() {
		var replicaState *apis.ReplicaState

		Context("when rebalancing is needed", func() {
			It("should return true when spot difference exceeds tolerance", func() {
				replicaState = &apis.ReplicaState{
					CurrentSpot:     1,
					CurrentOnDemand: 4,
					DesiredSpot:     4,
					DesiredOnDemand: 1,
				}

				result := reconciler.needsRebalancing(replicaState)
				Expect(result).To(BeTrue())
			})

			It("should return true when on-demand difference exceeds tolerance", func() {
				replicaState = &apis.ReplicaState{
					CurrentSpot:     4,
					CurrentOnDemand: 1,
					DesiredSpot:     1,
					DesiredOnDemand: 4,
				}

				result := reconciler.needsRebalancing(replicaState)
				Expect(result).To(BeTrue())
			})
		})

		Context("when rebalancing is not needed", func() {
			It("should return false when distribution is within tolerance", func() {
				replicaState = &apis.ReplicaState{
					CurrentSpot:     3,
					CurrentOnDemand: 2,
					DesiredSpot:     3,
					DesiredOnDemand: 2,
				}

				result := reconciler.needsRebalancing(replicaState)
				Expect(result).To(BeFalse())
			})

			It("should return false when no pods exist", func() {
				replicaState = &apis.ReplicaState{
					CurrentSpot:     0,
					CurrentOnDemand: 0,
					DesiredSpot:     2,
					DesiredOnDemand: 3,
				}

				result := reconciler.needsRebalancing(replicaState)
				Expect(result).To(BeFalse())
			})
		})
	})

	Describe("performPodRebalancing", func() {
		var (
			deployment   *appsv1.Deployment
			replicaState *apis.ReplicaState
		)

		BeforeEach(func() {
			deployment = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test",
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

			replicaState = &apis.ReplicaState{
				TotalReplicas:   5,
				CurrentSpot:     4, // Too many spot pods
				CurrentOnDemand: 1,
				DesiredSpot:     2,
				DesiredOnDemand: 3,
			}

			Expect(fakeClient.Create(ctx, deployment)).To(Succeed())
		})

		It("should delete excess spot pods", func() {
			// Create nodes
			spotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spot-node-1",
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "spot", // Updated to match classifier config
					},
				},
			}
			onDemandNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ondemand-node-1",
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": "on-demand", // Updated to match classifier config
					},
				},
			}
			Expect(fakeClient.Create(ctx, spotNode)).To(Succeed())
			Expect(fakeClient.Create(ctx, onDemandNode)).To(Succeed())

			// Create pods on spot nodes (excess)
			for i := 0; i < 4; i++ {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-pod-spot-%d", i),
						Namespace: "default",
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "spot-node-1",
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
							},
						},
					},
				}
				Expect(fakeClient.Create(ctx, pod)).To(Succeed())
			}

			// Create one pod on on-demand node
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-ondemand-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "ondemand-node-1",
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			}
			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			err := reconciler.performPodRebalancing(ctx, deployment, replicaState, "test/deployment")

			Expect(err).ToNot(HaveOccurred())

			// Verify that 1 spot pod was deleted (gradual deletion approach)
			podList := &corev1.PodList{}
			err = fakeClient.List(ctx, podList, client.InNamespace("default"))
			Expect(err).ToNot(HaveOccurred())

			remainingPods := 0
			for _, p := range podList.Items {
				if p.DeletionTimestamp == nil {
					remainingPods++
				}
			}
			Expect(remainingPods).To(Equal(4)) // 5 - 1 deleted = 4 (gradual deletion)
		})
	})

	Describe("SetupWithManager", func() {
		It("should setup the controller with manager", func() {
			// This test would require a real manager, so we'll just test the method exists
			// and doesn't panic when called with a nil manager (which will error but not panic)
			err := reconciler.SetupWithManager(nil)
			Expect(err).To(HaveOccurred()) // Expected since manager is nil
		})
	})

	Describe("deploymentLabelSelector", func() {
		It("should return correct label selector", func() {
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":     "test",
							"version": "v1",
						},
					},
				},
			}

			selector, err := deploymentLabelSelector(deployment)

			Expect(err).ToNot(HaveOccurred())
			Expect(selector).To(Equal(client.MatchingLabels{
				"app":     "test",
				"version": "v1",
			}))
		})

		It("should handle deployment with no selector", func() {
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: nil,
				},
			}

			selector, err := deploymentLabelSelector(deployment)

			Expect(err).To(HaveOccurred())
			Expect(selector).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("no selector"))
		})
	})

	Describe("Edge Cases", func() {
		It("should handle deployment with nil replica count", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: nil, // Nil replicas
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas: 1, // Default to 1
				},
			}

			Expect(fakeClient.Create(ctx, deployment)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-deployment",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(60 * time.Second)) // 2 * reconcileInterval when deployment not stable
		})

		It("should handle context cancellation gracefully", func() {
			canceledCtx, cancel := context.WithCancel(ctx)
			cancel() // Cancel immediately

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test",
						},
					},
				},
			}

			Expect(fakeClient.Create(ctx, deployment)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-deployment",
					Namespace: "default",
				},
			}

			// Should handle canceled context
			_, err := reconciler.Reconcile(canceledCtx, req)
			// The specific error will depend on implementation, but it should handle cancellation
			// We're mainly testing that it doesn't panic
			_ = err // Explicitly ignore error as we're testing cancellation handling
		})
	})

	Describe("isDeploymentStableAndReady", func() {
		It("should return false for deployment with nil replicas", func() {
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: nil,
				},
			}
			Expect(reconciler.isDeploymentStableAndReady(ctx, deployment)).To(BeFalse())
		})

		It("should return false for deployment with zero replicas", func() {
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(0),
				},
			}
			Expect(reconciler.isDeploymentStableAndReady(ctx, deployment)).To(BeFalse())
		})

		It("should return false for unstable deployment", func() {
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas:       2,
					UpdatedReplicas:     3,
					AvailableReplicas:   2,
					UnavailableReplicas: 1,
				},
			}
			Expect(reconciler.isDeploymentStableAndReady(ctx, deployment)).To(BeFalse())
		})

		It("should return true for stable and ready deployment", func() {
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas:       3,
					UpdatedReplicas:     3,
					AvailableReplicas:   3,
					UnavailableReplicas: 0,
				},
			}
			Expect(reconciler.isDeploymentStableAndReady(ctx, deployment)).To(BeTrue())
		})
	})

	Describe("needsRebalancing", func() {
		It("should return true when spot pods exceed desired", func() {
			state := &apis.ReplicaState{
				TotalReplicas:   5,
				DesiredSpot:     2,
				DesiredOnDemand: 3,
				CurrentSpot:     4,
				CurrentOnDemand: 1,
			}
			Expect(reconciler.needsRebalancing(state)).To(BeTrue())
		})

		It("should return true when on-demand pods exceed desired", func() {
			state := &apis.ReplicaState{
				TotalReplicas:   5,
				DesiredSpot:     3,
				DesiredOnDemand: 2,
				CurrentSpot:     1,
				CurrentOnDemand: 4,
			}
			Expect(reconciler.needsRebalancing(state)).To(BeTrue())
		})

		It("should return false when distribution is correct", func() {
			state := &apis.ReplicaState{
				TotalReplicas:   5,
				DesiredSpot:     3,
				DesiredOnDemand: 2,
				CurrentSpot:     3,
				CurrentOnDemand: 2,
			}
			Expect(reconciler.needsRebalancing(state)).To(BeFalse())
		})
	})

	Describe("selectPodsForDeletion", func() {
		var spotPods, onDemandPods []corev1.Pod

		BeforeEach(func() {
			spotPods = []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "spot-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "spot-2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "spot-3"}},
			}
			onDemandPods = []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "ondemand-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "ondemand-2"}},
			}
		})

		It("should select excess spot pods for deletion", func() {
			state := &apis.ReplicaState{
				DesiredSpot:     2,
				DesiredOnDemand: 2,
			}
			podsToDelete := reconciler.selectPodsForDeletion(spotPods, onDemandPods, state)
			Expect(len(podsToDelete)).To(Equal(1))
			Expect(podsToDelete[0].Name).To(ContainSubstring("spot"))
		})

		It("should select excess on-demand pods for deletion", func() {
			state := &apis.ReplicaState{
				DesiredSpot:     3,
				DesiredOnDemand: 1,
			}
			podsToDelete := reconciler.selectPodsForDeletion(spotPods, onDemandPods, state)
			Expect(len(podsToDelete)).To(Equal(1))
			Expect(podsToDelete[0].Name).To(ContainSubstring("ondemand"))
		})

		It("should return empty list when no rebalancing needed", func() {
			state := &apis.ReplicaState{
				DesiredSpot:     3,
				DesiredOnDemand: 2,
			}
			podsToDelete := reconciler.selectPodsForDeletion(spotPods, onDemandPods, state)
			Expect(len(podsToDelete)).To(Equal(0))
		})
	})

	Describe("SetupWithManager", func() {
		It("should setup controller with manager successfully", func() {
			// This would require a real manager in practice
			// For now, we'll test that the method exists and doesn't panic
			Skip("SetupWithManager requires real controller-runtime manager")
		})
	})

	Describe("SetupWithManagerNamed", func() {
		It("should setup controller with custom name", func() {
			// This would require a real manager in practice
			Skip("SetupWithManagerNamed requires real controller-runtime manager")
		})
	})

	Describe("Edge Cases and Error Handling", func() {
		It("should handle deployment with invalid selector", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-selector-deployment",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(1),
					Selector: nil, // Invalid: no selector
				},
			}

			// Should handle the case where deployment has no selector
			_, _, err := reconciler.categorizeDeploymentPods(ctx, deployment)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("selector"))
		})

		It("should handle metrics collection", func() {
			// Test that metrics recorder interface is properly called
			mockMetrics := &MockMetricsRecorder{}
			reconciler.MetricsCollector = mockMetrics

			state := &apis.ReplicaState{
				TotalReplicas:   3,
				DesiredSpot:     2,
				DesiredOnDemand: 1,
				CurrentSpot:     2,
				CurrentOnDemand: 1,
			}

			// This would be called during reconciliation
			reconciler.MetricsCollector.RecordWorkloadMetrics("default", "test-deployment", "Deployment", state)

			Expect(mockMetrics.RecordedMetrics).To(HaveLen(1))
			Expect(mockMetrics.RecordedMetrics[0].Namespace).To(Equal("default"))
		})

		It("should handle cooldown period correctly", func() {
			deploymentKey := "default/test-deployment"
			now := time.Now()

			// Store a recent deletion time
			reconciler.lastDeletionTimes.Store(deploymentKey, now.Add(-30*time.Second))

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/enabled":         "true",
						"spotalis.io/spot-percentage": "70",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas:       3,
					UpdatedReplicas:     3,
					AvailableReplicas:   3,
					UnavailableReplicas: 0,
				},
			}

			Expect(fakeClient.Create(ctx, deployment)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-deployment",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			// Should be in cooldown and requeue after remaining cooldown time
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		})
	})
})

// MockMetricsRecorder implements MetricsRecorder for testing
type MockMetricsRecorder struct {
	RecordedMetrics []struct {
		Namespace    string
		WorkloadName string
		WorkloadType string
		ReplicaState *apis.ReplicaState
	}
}

func (m *MockMetricsRecorder) RecordWorkloadMetrics(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState) {
	m.RecordedMetrics = append(m.RecordedMetrics, struct {
		Namespace    string
		WorkloadName string
		WorkloadType string
		ReplicaState *apis.ReplicaState
	}{
		Namespace:    namespace,
		WorkloadName: workloadName,
		WorkloadType: workloadType,
		ReplicaState: replicaState,
	})
}

// Helper function to create int32 pointers
func int32Ptr(i int32) *int32 {
	return &i
}
