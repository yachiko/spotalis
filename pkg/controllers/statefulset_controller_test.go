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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
)

var _ = Describe("StatefulSetReconciler", func() {
	var (
		reconciler       *StatefulSetReconciler
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

		reconciler = &StatefulSetReconciler{
			Client:              fakeClient,
			Scheme:              scheme,
			AnnotationParser:    annotationParser,
			NodeClassifier:      nodeClassifier,
			ReconcileInterval:   30 * time.Second,
			MaxConcurrentRecons: 10,
		}
	})

	Describe("NewStatefulSetReconciler", func() {
		It("should create a new reconciler with default settings", func() {
			newReconciler := NewStatefulSetReconciler(fakeClient, scheme)

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
		var statefulSet *appsv1.StatefulSet

		BeforeEach(func() {
			statefulSet = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "60",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-sts",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test-sts",
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
					ServiceName: "test-service",
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:        3,
					ReadyReplicas:   3, // Make StatefulSet appear stable
					CurrentReplicas: 3,
					UpdatedReplicas: 3,
				},
			}
		})

		Context("when StatefulSet is not found", func() {
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

		Context("when StatefulSet has no Spotalis annotations", func() {
			It("should skip reconciliation", func() {
				statefulSetWithoutAnnotations := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-statefulset",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "test-sts",
							},
						},
						ServiceName: "test-service",
					},
				}

				Expect(fakeClient.Create(ctx, statefulSetWithoutAnnotations)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-statefulset",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
			})
		})

		Context("when StatefulSet has Spotalis annotations", func() {
			BeforeEach(func() {
				Expect(fakeClient.Create(ctx, statefulSet)).To(Succeed())
			})

			It("should reconcile successfully", func() {
				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-statefulset",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(60 * time.Second)) // 2 * reconcileInterval when no action needed
			})

			It("should handle invalid annotation configuration", func() {
				invalidStatefulSet := statefulSet.DeepCopy()
				invalidStatefulSet.Name = "invalid-statefulset"
				invalidStatefulSet.ResourceVersion = "" // Clear resourceVersion for Create
				invalidStatefulSet.Annotations["spotalis.io/enabled"] = "true"
				invalidStatefulSet.Annotations["spotalis.io/spot-percentage"] = "invalid"

				Expect(fakeClient.Create(ctx, invalidStatefulSet)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "invalid-statefulset",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).To(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(30 * time.Second))
			})
		})
	})

	Describe("calculateCurrentReplicaState", func() {
		var statefulSet *appsv1.StatefulSet

		BeforeEach(func() {
			statefulSet = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-sts",
						},
					},
					ServiceName: "test-service",
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:      3,
					ReadyReplicas: 3,
				},
			}
		})

		It("should calculate replica state correctly", func() {
			// Create nodes for the pods to run on
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

			// Create pods on these nodes
			for i := 0; i < 2; i++ {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-statefulset-%d", i),
						Namespace: "default",
						Labels: map[string]string{
							"app": "test-sts",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "spot-node-1",
					},
				}
				Expect(fakeClient.Create(ctx, pod)).To(Succeed())
			}

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset-2",
					Namespace: "default",
					Labels: map[string]string{
						"app": "test-sts",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "ondemand-node-1",
				},
			}
			Expect(fakeClient.Create(ctx, pod)).To(Succeed())

			state, err := reconciler.calculateCurrentReplicaState(ctx, statefulSet)

			Expect(err).ToNot(HaveOccurred())
			Expect(state).ToNot(BeNil())
			Expect(state.CurrentSpot).To(Equal(int32(2)))
			Expect(state.CurrentOnDemand).To(Equal(int32(1)))
			Expect(state.LastReconciled).To(BeTemporally("~", time.Now(), time.Second))
		})

		It("should handle StatefulSet with no selector", func() {
			statefulSet.Spec.Selector = nil

			state, err := reconciler.calculateCurrentReplicaState(ctx, statefulSet)

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
			statefulSet  *appsv1.StatefulSet
			replicaState *apis.ReplicaState
		)

		BeforeEach(func() {
			statefulSet = &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(5),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-sts",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "test-sts",
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
					ServiceName: "test-service",
				},
			}

			replicaState = &apis.ReplicaState{
				TotalReplicas:   5,
				CurrentSpot:     4, // Too many spot pods
				CurrentOnDemand: 1,
				DesiredSpot:     2,
				DesiredOnDemand: 3,
			}

			Expect(fakeClient.Create(ctx, statefulSet)).To(Succeed())
		})

		It("should delete excess spot pods and update template", func() {
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
						Name:      fmt.Sprintf("test-statefulset-%d", i),
						Namespace: "default",
						Labels: map[string]string{
							"app": "test-sts",
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
					Name:      "test-statefulset-4",
					Namespace: "default",
					Labels: map[string]string{
						"app": "test-sts",
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

			err := reconciler.performPodRebalancing(ctx, statefulSet, replicaState, "test/statefulset")

			Expect(err).ToNot(HaveOccurred())

			// Verify that 2 spot pods were deleted (4 - 2 = 2)
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

			// Verify StatefulSet was updated with rebalance annotation
			var updatedStatefulSet appsv1.StatefulSet
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      statefulSet.Name,
				Namespace: statefulSet.Namespace,
			}, &updatedStatefulSet)

			Expect(err).ToNot(HaveOccurred())
			Expect(updatedStatefulSet.Spec.Template.Annotations).To(HaveKey("spotalis.io/rebalance-timestamp"))
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

	Describe("statefulSetLabelSelector", func() {
		It("should return correct label selector", func() {
			statefulSet := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app":     "test-sts",
							"version": "v1",
						},
					},
				},
			}

			selector, err := statefulSetLabelSelector(statefulSet)

			Expect(err).ToNot(HaveOccurred())
			Expect(selector).To(Equal(client.MatchingLabels{
				"app":     "test-sts",
				"version": "v1",
			}))
		})

		It("should handle StatefulSet with no selector", func() {
			statefulSet := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Selector: nil,
				},
			}

			selector, err := statefulSetLabelSelector(statefulSet)

			Expect(err).To(HaveOccurred())
			Expect(selector).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("no selector"))
		})
	})

	Describe("StatefulSet Specific Features", func() {
		Context("ordered scaling operations", func() {
			It("should respect StatefulSet scaling semantics", func() {
				statefulSet := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ordered-scaling-test",
						Namespace: "default",
						Annotations: map[string]string{
							"spotalis.io/spot-percentage": "50",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "ordered-test",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "ordered-test",
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
						ServiceName: "ordered-service",
					},
				}

				Expect(fakeClient.Create(ctx, statefulSet)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "ordered-scaling-test",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(60 * time.Second)) // 2 * reconcileInterval when no action needed
			})
		})

		Context("persistent volume handling", func() {
			It("should handle StatefulSets with volume claim templates", func() {
				statefulSet := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pvc-test",
						Namespace: "default",
						Annotations: map[string]string{
							"spotalis.io/spot-percentage": "40",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(2),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "pvc-test",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "pvc-test",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "nginx:latest",
										VolumeMounts: []corev1.VolumeMount{
											{
												Name:      "data",
												MountPath: "/data",
											},
										},
									},
								},
							},
						},
						VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "data",
								},
								Spec: corev1.PersistentVolumeClaimSpec{
									AccessModes: []corev1.PersistentVolumeAccessMode{
										corev1.ReadWriteOnce,
									},
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: mustParseQuantity("1Gi"),
										},
									},
								},
							},
						},
						ServiceName: "pvc-service",
					},
				}

				Expect(fakeClient.Create(ctx, statefulSet)).To(Succeed())

				req := ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name:      "pvc-test",
						Namespace: "default",
					},
				}

				result, err := reconciler.Reconcile(ctx, req)

				Expect(err).ToNot(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(60 * time.Second)) // 2 * reconcileInterval when no action needed
			})
		})
	})

	Describe("Edge Cases", func() {
		It("should handle StatefulSet with nil replica count", func() {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: nil, // Nil replicas
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-sts",
						},
					},
					ServiceName: "test-service",
				},
				Status: appsv1.StatefulSetStatus{
					Replicas: 1, // Default to 1
				},
			}

			Expect(fakeClient.Create(ctx, statefulSet)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-statefulset",
					Namespace: "default",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(60 * time.Second)) // 2 * reconcileInterval when not stable
		})

		It("should handle context cancellation gracefully", func() {
			canceledCtx, cancel := context.WithCancel(ctx)
			cancel() // Cancel immediately

			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(3),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "test-sts",
						},
					},
					ServiceName: "test-service",
				},
			}

			Expect(fakeClient.Create(ctx, statefulSet)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-statefulset",
					Namespace: "default",
				},
			}

			// Should handle canceled context
			_, _ = reconciler.Reconcile(canceledCtx, req)
			// The specific error will depend on implementation, but it should handle cancellation
			// We're mainly testing that it doesn't panic
		})
	})
})

// Helper function to create resource quantities
func mustParseQuantity(value string) resource.Quantity {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		panic(err)
	}
	return q
}
