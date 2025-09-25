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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("MutationHandler", func() {
	var (
		handler *MutationHandler
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Create a scheme with the necessary types
		testScheme := runtime.NewScheme()
		err := scheme.AddToScheme(testScheme)
		Expect(err).NotTo(HaveOccurred())
		handler = NewMutationHandler(nil, testScheme)
	})

	Describe("Handle", func() {
		Context("with unsupported resource kinds", func() {
			It("should allow ConfigMaps", func() {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "ConfigMap",
						},
						Object: runtime.RawExtension{
							Raw: []byte(`{"metadata":{"name":"test-cm"}}`),
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since it's not managed by Spotalis
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
			})

			It("should allow Services", func() {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "Service",
						},
						Object: runtime.RawExtension{
							Raw: []byte(`{"metadata":{"name":"test-svc"}}`),
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since it's not managed by Spotalis
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
			})

			It("should allow Deployments", func() {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "Deployment",
						},
						Object: runtime.RawExtension{
							Raw: []byte(`{"metadata":{"name":"test-deployment"}}`),
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since we only handle pods
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
			})

			It("should allow StatefulSets", func() {
				req := admission.Request{
					AdmissionRequest: admissionv1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Kind: "StatefulSet",
						},
						Object: runtime.RawExtension{
							Raw: []byte(`{"metadata":{"name":"test-statefulset"}}`),
						},
					},
				}

				response := handler.Handle(ctx, req)
				// Should be allowed since we only handle pods
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
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
				Expect(response.Result.Message).To(ContainSubstring("not managed by Spotalis"))
			})
		})
	})

	Describe("Error handling", func() {
		It("should handle malformed JSON for unsupported types", func() {
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
			// Should be allowed since we only handle pods (deployment with malformed JSON is ignored)
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
		})

		It("should handle empty object for unsupported types", func() {
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Kind: "Service",
					},
					Object: runtime.RawExtension{
						Raw: []byte("{}"),
					},
				},
			}

			response := handler.Handle(ctx, req)
			Expect(response.Allowed).To(BeTrue())
			Expect(response.Result.Message).To(ContainSubstring("unsupported resource kind"))
		})
	})
})
