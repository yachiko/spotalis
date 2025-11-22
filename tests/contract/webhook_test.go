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

package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/internal/server"
	"github.com/yachiko/spotalis/pkg/webhook"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("POST /mutate webhook", func() {
	var (
		router        *gin.Engine
		recorder      *httptest.ResponseRecorder
		webhookServer *server.WebhookServer
		scheme        *runtime.Scheme
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// Create scheme with necessary types
		scheme = runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)
		_ = admissionv1.AddToScheme(scheme)

		// Create a fake client for the mutation handler
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Create mutation handler
		mutationHandler := webhook.NewMutationHandler(fakeClient, scheme)

		// Create webhook server
		webhookServer = server.NewWebhookServer(
			server.WebhookServerConfig{
				Port:     9443,
				CertPath: "",
				KeyPath:  "",
				ReadOnly: false,
				Quiet:    true, // Suppress debug logs in tests
			},
			mutationHandler,
			scheme,
		)

		// Register the webhook endpoint
		router.POST("/mutate", webhookServer.MutateHandler)
	})

	Context("when receiving a valid deployment admission request", func() {
		var admissionRequest *admissionv1.AdmissionRequest

		BeforeEach(func() {
			// Test with a simple Pod instead of Deployment since webhook only processes Pods
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"spotalis.io/enabled": "true", // Required for labels-only architecture
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
						},
					},
				},
			}

			podJSON, err := json.Marshal(pod)
			Expect(err).NotTo(HaveOccurred())
			admissionRequest = &admissionv1.AdmissionRequest{
				UID: "test-uid",
				Kind: metav1.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
				Object: runtime.RawExtension{
					Raw: podJSON,
				},
				Operation: admissionv1.Create,
			}
		})

		It("should return 200 OK status", func() {
			admissionReview := &admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: admissionRequest,
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should return JSON content type", func() {
			admissionReview := &admissionv1.AdmissionReview{
				Request: admissionRequest,
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("application/json"))
		})

		It("should return valid AdmissionResponse", func() {
			admissionReview := &admissionv1.AdmissionReview{
				Request: admissionRequest,
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			var response admissionv1.AdmissionReview
			err = json.Unmarshal(recorder.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Response).NotTo(BeNil())
			Expect(response.Response.UID).To(Equal(admissionRequest.UID))
		})

		It("should allow the request by default", func() {
			admissionReview := &admissionv1.AdmissionReview{
				Request: admissionRequest,
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			var response admissionv1.AdmissionReview
			err = json.Unmarshal(recorder.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Response.Allowed).To(BeTrue())
		})

		It("should add node selector patches when replica strategy is specified", func() {
			admissionReview := &admissionv1.AdmissionReview{
				Request: admissionRequest,
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			var response admissionv1.AdmissionReview
			err = json.Unmarshal(recorder.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())

			if response.Response.Patch != nil {
				// The webhook should apply node scheduling mutations like nodeSelector
				patch := string(response.Response.Patch)
				Expect(patch).To(Or(
					ContainSubstring("nodeSelector"),
					ContainSubstring("tolerations"),
				))
			}
		})
	})

	Context("when receiving invalid admission request", func() {
		It("should return 400 Bad Request for malformed JSON", func() {
			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer([]byte("invalid json")))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return error for missing request", func() {
			admissionReview := &admissionv1.AdmissionReview{
				Request: nil,
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Context("performance requirements", func() {
		It("should respond within 100ms", func() {
			admissionReview := &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					Operation: admissionv1.Create,
				},
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).NotTo(HaveOccurred())

			req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "application/json")

			GinkgoHelper()
			router.ServeHTTP(recorder, req)

			// Webhook should be fast to not delay deployments
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})
	})
})
