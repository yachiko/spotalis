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

package webhook

import (
	"context"
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("MutationHandler", func() {

	var (
		handler *MutationHandler
		scheme  *runtime.Scheme
		ctx     context.Context
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		err := corev1.AddToScheme(scheme)
		Expect(err).To(BeNil())
		err = appsv1.AddToScheme(scheme)
		Expect(err).To(BeNil())

		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		handler = NewMutationHandler(client, scheme)
		ctx = context.Background()
	})

	Describe("Handle", func() {
		Context("with unsupported resource kinds", func() {
			It("should allow ConfigMaps", func() {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "ConfigMap",
						},
					},
				}

				response := handler.Handle(ctx, req)
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
			})

			It("should allow Services", func() {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "Service",
						},
					},
				}

				response := handler.Handle(ctx, req)
				Expect(response.Allowed).To(BeTrue())
			})
		})

		Context("with supported resource kinds", func() {
			It("should process Pod requests", func() {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx:latest",
							},
						},
					},
				}

				podBytes, err := json.Marshal(pod)
				Expect(err).To(BeNil())

				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "Pod",
						},
						Object: runtime.RawExtension{
							Raw: podBytes,
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since it's not managed by Spotalis
				Expect(response.Allowed).To(BeTrue())
			})

			It("should process Deployment requests", func() {
				deployment := &appsv1.Deployment{
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

				deploymentBytes, err := json.Marshal(deployment)
				Expect(err).To(BeNil())

				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "Deployment",
						},
						Object: runtime.RawExtension{
							Raw: deploymentBytes,
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since no Spotalis annotations
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("not managed by Spotalis"))
			})

			It("should process StatefulSet requests", func() {
				statefulSet := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-statefulset",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: int32Ptr(3),
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

				statefulSetBytes, err := json.Marshal(statefulSet)
				Expect(err).To(BeNil())

				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "StatefulSet",
						},
						Object: runtime.RawExtension{
							Raw: statefulSetBytes,
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since no Spotalis annotations
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("not managed by Spotalis"))
			})
		})
	})

	Describe("Deployment mutation with Spotalis annotations", func() {
		It("should deny deployment with invalid annotations", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "150", // Invalid: > 100
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
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

			deploymentBytes, err := json.Marshal(deployment)
			Expect(err).To(BeNil())

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "Deployment",
					},
					Object: runtime.RawExtension{
						Raw: deploymentBytes,
					},
				},
			}

			response := handler.Handle(ctx, req)
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring("Invalid Spotalis annotations"))
		})

		It("should allow deployment with valid annotations", func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "70",
						"spotalis.io/min-on-demand":   "1",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
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

			deploymentBytes, err := json.Marshal(deployment)
			Expect(err).To(BeNil())

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "Deployment",
					},
					Object: runtime.RawExtension{
						Raw: deploymentBytes,
					},
				},
			}

			response := handler.Handle(ctx, req)
			// Should be allowed or patched (depending on implementation)
			Expect(response.Allowed || len(response.Patches) > 0).To(BeTrue())
		})
	})

	Describe("StatefulSet mutation with Spotalis annotations", func() {
		It("should deny StatefulSet with invalid annotations", func() {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/min-on-demand": "-1", // Invalid: negative
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: int32Ptr(3),
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

			statefulSetBytes, err := json.Marshal(statefulSet)
			Expect(err).To(BeNil())

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "StatefulSet",
					},
					Object: runtime.RawExtension{
						Raw: statefulSetBytes,
					},
				},
			}

			response := handler.Handle(ctx, req)
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Message).To(ContainSubstring("Invalid Spotalis annotations"))
		})

		It("should allow StatefulSet with valid annotations", func() {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
						"spotalis.io/min-on-demand":   "2",
					},
				},
				Spec: appsv1.StatefulSetSpec{
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

			statefulSetBytes, err := json.Marshal(statefulSet)
			Expect(err).To(BeNil())

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "StatefulSet",
					},
					Object: runtime.RawExtension{
						Raw: statefulSetBytes,
					},
				},
			}

			response := handler.Handle(ctx, req)
			// Should be allowed or patched (depending on implementation)
			Expect(response.Allowed || len(response.Patches) > 0).To(BeTrue())
		})
	})

	Describe("Error handling", func() {
		It("should handle malformed JSON", func() {
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "Deployment",
					},
					Object: runtime.RawExtension{
						Raw: []byte("invalid json"),
					},
				},
			}

			response := handler.Handle(ctx, req)
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
		})

		It("should handle empty object", func() {
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "Pod",
					},
					Object: runtime.RawExtension{
						Raw: []byte("{}"),
					},
				},
			}

			response := handler.Handle(ctx, req)
			// Should handle gracefully
			Expect(response.Allowed).To(BeTrue())
		})
	})
})

// Helper function for int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
