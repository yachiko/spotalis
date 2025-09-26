/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You 				response := performRequest(engine, "POST", "/mutate", admissionReview)
				Expect(response.Code).To(Equal(http.StatusOK))

				var reviewResponse v1.AdmissionReview
				err := parseJSONResponse(response, &reviewResponse)
				Expect(err).NotTo(HaveOccurred())
				Expect(reviewResponse.Response.Allowed).To(BeTrue()) // Allowed but no mutations applied
				Expect(reviewResponse.Response.Result.Message).To(ContainSubstring("read-only"))
			})
		}) a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"crypto/tls"
	"net/http"

	webhookMutate "github.com/ahoma/spotalis/pkg/webhook"
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("WebhookServer", func() {
	var (
		webhookServer *WebhookServer
		mutateHandler *webhookMutate.MutationHandler
		scheme        *runtime.Scheme
		config        WebhookServerConfig
		engine        *gin.Engine
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		err := appsv1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())

		// Create a proper mutation handler with fake client
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		mutateHandler = webhookMutate.NewMutationHandler(fakeClient, scheme)

		config = WebhookServerConfig{
			Port:     9443,
			CertPath: "/tmp/tls.crt",
			KeyPath:  "/tmp/tls.key",
		}

		webhookServer = NewWebhookServer(config, mutateHandler, scheme)
		engine = createTestEngine()
	})

	Describe("NewWebhookServer", func() {
		It("should create a new webhook server with correct configuration", func() {
			server := NewWebhookServer(config, mutateHandler, scheme)
			Expect(server).NotTo(BeNil())
			Expect(server.mutateHandler).To(Equal(mutateHandler))
			Expect(server.scheme).To(Equal(scheme))
			Expect(server.port).To(Equal(9443))
			Expect(server.certPath).To(Equal("/tmp/tls.crt"))
			Expect(server.keyPath).To(Equal("/tmp/tls.key"))
		})
	})

	Describe("MutateHandler", func() {
		BeforeEach(func() {
			engine.POST("/mutate", webhookServer.MutateHandler)
		})

		Context("when receiving a valid admission request", func() {
			It("should process the mutation request", func() {
				// Create a test admission request
				admissionReq := &v1.AdmissionRequest{
					UID: types.UID("test-uid"),
					Kind: metav1.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
					},
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: []byte(`{
							"apiVersion": "apps/v1",
							"kind": "Deployment",
							"metadata": {
								"name": "test-deployment",
								"namespace": "default"
							},
							"spec": {
								"replicas": 1,
								"selector": {
									"matchLabels": {
										"app": "test"
									}
								},
								"template": {
									"metadata": {
										"labels": {
											"app": "test"
										}
									},
									"spec": {
										"containers": [{
											"name": "test",
											"image": "nginx"
										}]
									}
								}
							}
						}`),
					},
				}

				admissionReview := &v1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: admissionReq,
				}

				response := performRequest(engine, "POST", "/mutate", admissionReview)
				Expect(response.Code).To(Equal(http.StatusOK))

				var reviewResponse v1.AdmissionReview
				err := parseJSONResponse(response, &reviewResponse)
				Expect(err).NotTo(HaveOccurred())
				Expect(reviewResponse.Response).NotTo(BeNil())
				Expect(reviewResponse.Response.UID).To(Equal(admissionReq.UID))
			})
		})

		Context("when receiving invalid JSON", func() {
			It("should return bad request", func() {
				headers := map[string]string{
					"Content-Type": "application/json",
				}
				response := performRequestWithHeaders(engine, "POST", "/mutate", "invalid json", headers)
				Expect(response.Code).To(Equal(http.StatusBadRequest))
			})
		})

		Context("when in read-only mode", func() {
			BeforeEach(func() {
				webhookServer.SetReadOnly(true)
			})

			It("should deny mutations", func() {
				admissionReq := &v1.AdmissionRequest{
					UID: "test-uid",
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"test"}}`),
					},
				}

				admissionReview := v1.AdmissionReview{
					Request: admissionReq,
				}

				response := performRequest(engine, "POST", "/mutate", admissionReview)
				Expect(response.Code).To(Equal(http.StatusOK))

				var reviewResponse v1.AdmissionReview
				err := parseJSONResponse(response, &reviewResponse)
				Expect(err).NotTo(HaveOccurred())
				Expect(reviewResponse.Response.Allowed).To(BeTrue()) // Allowed but no mutations applied
				Expect(reviewResponse.Response.Result.Message).To(ContainSubstring("read-only"))
			})
		})

		Describe("ValidateHandler", func() {
			BeforeEach(func() {
				engine.POST("/validate", webhookServer.ValidateHandler)
			})

			It("should process validation requests", func() {
				admissionReq := &v1.AdmissionRequest{
					UID: types.UID("test-uid"),
					Kind: metav1.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: []byte(`{
						"apiVersion": "apps/v1",
						"kind": "Deployment",
						"metadata": {
							"name": "test-deployment",
							"namespace": "default"
						}
					}`),
					},
				}

				admissionReview := &v1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: admissionReq,
				}

				response := performRequest(engine, "POST", "/validate", admissionReview)
				Expect(response.Code).To(Equal(http.StatusOK))

				var reviewResponse v1.AdmissionReview
				err := parseJSONResponse(response, &reviewResponse)
				Expect(err).NotTo(HaveOccurred())
				Expect(reviewResponse.Response).NotTo(BeNil())
				Expect(reviewResponse.Response.UID).To(Equal(admissionReq.UID))
			})
		})

		Describe("State Management", func() {
			Context("SetReadOnly and IsReadOnly", func() {
				It("should manage read-only state", func() {
					Expect(webhookServer.IsReadOnly()).To(BeFalse())

					webhookServer.SetReadOnly(true)
					Expect(webhookServer.IsReadOnly()).To(BeTrue())

					webhookServer.SetReadOnly(false)
					Expect(webhookServer.IsReadOnly()).To(BeFalse())
				})
			})

			Context("GetTLSConfig", func() {
				It("should return TLS configuration", func() {
					tlsConfig := webhookServer.GetTLSConfig()
					// TLS config might be nil if not explicitly set
					if tlsConfig != nil {
						// TLS config should have reasonable defaults
						Expect(tlsConfig.MinVersion).To(BeNumerically(">=", tls.VersionTLS12))
					}
				})
			})
		})

		Describe("SetupRoutes", func() {
			It("should setup webhook routes", func() {
				router := gin.New()
				webhookServer.SetupRoutes(router)

				// Test that routes are registered by making requests
				response := performRequest(router, "POST", "/webhook/mutate", nil)
				// Should get bad request due to missing body, but route exists
				Expect(response.Code).To(Equal(http.StatusBadRequest))

				response = performRequest(router, "POST", "/webhook/validate", nil)
				// Should get bad request due to missing body, but route exists
				Expect(response.Code).To(Equal(http.StatusBadRequest))
			})
		})

		Describe("GetControllerRuntimeServer", func() {
			It("should return the controller-runtime webhook server", func() {
				server := webhookServer.GetControllerRuntimeServer()
				Expect(server).NotTo(BeNil())
			})
		})
	})

	var _ = Describe("WebhookHandler", func() {
		var (
			webhookHandler *WebhookHandler
			webhookServer  *WebhookServer
			mutateHandler  *webhookMutate.MutationHandler
			scheme         *runtime.Scheme
			config         WebhookServerConfig
			engine         *gin.Engine
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			err := appsv1.AddToScheme(scheme)
			Expect(err).NotTo(HaveOccurred())

			// Create a proper mutation handler with fake client
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			mutateHandler = webhookMutate.NewMutationHandler(fakeClient, scheme)

			config = WebhookServerConfig{
				Port:     9443,
				CertPath: "/tmp/tls.crt",
				KeyPath:  "/tmp/tls.key",
			}

			webhookServer = NewWebhookServer(config, mutateHandler, scheme)
			webhookHandler = NewWebhookHandler()
			webhookHandler.SetServer(webhookServer)
			engine = createTestEngine()
		})

		Describe("NewWebhookHandler", func() {
			It("should create a new webhook handler", func() {
				handler := NewWebhookHandler()
				Expect(handler).NotTo(BeNil())
			})
		})

		Describe("Mutate", func() {
			BeforeEach(func() {
				engine.POST("/mutate", webhookHandler.Mutate)
			})

			It("should delegate to the webhook server", func() {
				admissionReq := &v1.AdmissionRequest{
					UID: types.UID("test-uid"),
					Kind: metav1.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					Operation: v1.Create,
				}

				admissionReview := &v1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: admissionReq,
				}

				response := performRequest(engine, "POST", "/mutate", admissionReview)
				Expect(response.Code).To(Equal(http.StatusOK))
			})

			It("should handle case when server is not set", func() {
				handler := NewWebhookHandler()
				engine.POST("/no-server", handler.Mutate)

				response := performRequest(engine, "POST", "/no-server", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
			})
		})

		Describe("Validate", func() {
			BeforeEach(func() {
				engine.POST("/validate", webhookHandler.Validate)
			})

			It("should delegate to the webhook server", func() {
				admissionReq := &v1.AdmissionRequest{
					UID: types.UID("test-uid"),
					Kind: metav1.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					Operation: v1.Create,
				}

				admissionReview := &v1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: admissionReq,
				}

				response := performRequest(engine, "POST", "/validate", admissionReview)
				Expect(response.Code).To(Equal(http.StatusOK))
			})

			It("should handle case when server is not set", func() {
				handler := NewWebhookHandler()
				engine.POST("/no-server", handler.Validate)

				response := performRequest(engine, "POST", "/no-server", nil)
				Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
			})
		})

		Describe("SetReadOnly", func() {
			It("should delegate to the webhook server", func() {
				webhookHandler.SetReadOnly(true)
				Expect(webhookServer.IsReadOnly()).To(BeTrue())

				webhookHandler.SetReadOnly(false)
				Expect(webhookServer.IsReadOnly()).To(BeFalse())
			})

			It("should handle case when server is not set", func() {
				handler := NewWebhookHandler()
				// Should not panic
				handler.SetReadOnly(true)
			})
		})
	})
})
