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

	Describe("JSON Pointer Escaping (RFC 6901)", func() {
		Context("jsonPointerEscape", func() {
			It("should escape tilde characters", func() {
				result := jsonPointerEscape("foo~bar")
				Expect(result).To(Equal("foo~0bar"))
			})

			It("should escape forward slashes", func() {
				result := jsonPointerEscape("foo/bar")
				Expect(result).To(Equal("foo~1bar"))
			})

			It("should escape both tilde and slash in correct order", func() {
				result := jsonPointerEscape("~/path")
				Expect(result).To(Equal("~0~1path"))
			})

			It("should escape path with tilde first then slash", func() {
				result := jsonPointerEscape("~foo/bar")
				Expect(result).To(Equal("~0foo~1bar"))
			})

			It("should handle multiple tildes", func() {
				result := jsonPointerEscape("foo~bar~baz")
				Expect(result).To(Equal("foo~0bar~0baz"))
			})

			It("should handle multiple slashes", func() {
				result := jsonPointerEscape("foo/bar/baz")
				Expect(result).To(Equal("foo~1bar~1baz"))
			})

			It("should handle empty string", func() {
				result := jsonPointerEscape("")
				Expect(result).To(Equal(""))
			})

			It("should handle string with no special characters", func() {
				result := jsonPointerEscape("normal-key")
				Expect(result).To(Equal("normal-key"))
			})

			It("should handle complex Kubernetes label key", func() {
				// Real-world example with organization/team structure
				result := jsonPointerEscape("example.com/team~alpha")
				Expect(result).To(Equal("example.com~1team~0alpha"))
			})
		})

		Context("jsonPointerUnescape", func() {
			It("should unescape tilde characters", func() {
				result := jsonPointerUnescape("foo~0bar")
				Expect(result).To(Equal("foo~bar"))
			})

			It("should unescape forward slashes", func() {
				result := jsonPointerUnescape("foo~1bar")
				Expect(result).To(Equal("foo/bar"))
			})

			It("should unescape both in correct order", func() {
				result := jsonPointerUnescape("~0~1path")
				Expect(result).To(Equal("~/path"))
			})

			It("should handle empty string", func() {
				result := jsonPointerUnescape("")
				Expect(result).To(Equal(""))
			})
		})

		Context("roundtrip escape/unescape", func() {
			It("should roundtrip simple tilde", func() {
				original := "foo~bar"
				escaped := jsonPointerEscape(original)
				unescaped := jsonPointerUnescape(escaped)
				Expect(unescaped).To(Equal(original))
			})

			It("should roundtrip simple slash", func() {
				original := "foo/bar"
				escaped := jsonPointerEscape(original)
				unescaped := jsonPointerUnescape(escaped)
				Expect(unescaped).To(Equal(original))
			})

			It("should roundtrip both characters", func() {
				original := "~/path/to~file"
				escaped := jsonPointerEscape(original)
				unescaped := jsonPointerUnescape(escaped)
				Expect(unescaped).To(Equal(original))
			})

			It("should roundtrip complex Kubernetes label", func() {
				original := "example.com/team~alpha/project~beta"
				escaped := jsonPointerEscape(original)
				unescaped := jsonPointerUnescape(escaped)
				Expect(unescaped).To(Equal(original))
			})
		})

		Context("integration with node selector patches", func() {
			It("should correctly escape node selector keys with slashes", func() {
				pod := &corev1.Pod{
					Spec: corev1.PodSpec{
						NodeSelector: map[string]string{},
					},
				}

				patches := handler.generateNodeSelectorPatches(pod, nil)

				// Find the patch for karpenter.sh/capacity-type
				var foundPatch map[string]interface{}
				for _, patch := range patches {
					if patch["path"] == "/spec/nodeSelector/karpenter.sh~1capacity-type" {
						foundPatch = patch
						break
					}
				}

				Expect(foundPatch).NotTo(BeNil(), "Should find patch with escaped path")
				Expect(foundPatch["op"]).To(Equal("add"))
				Expect(foundPatch["path"]).To(Equal("/spec/nodeSelector/karpenter.sh~1capacity-type"))
			})
		})
	})
})
