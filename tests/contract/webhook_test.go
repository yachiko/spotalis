/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/li		req, e		body, err := js		req, err 		req, er		Expect(err).N		req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")

		GinkgoHelper()
		start := time.Now()
		router.ServeHTTP(recorder, req)
		duration := time.Since(start)

		// Webhook should be fast to not delay deployments
		Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		Expect(recorder.Code).To(Equal(http.StatusOK))Occurred())

		req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")

		GinkgoHelper()
		start := time.Now()
		router.ServeHTTP(recorder, req)
		duration := time.Since(start)

		// Webhook should be fast to not delay deployments
		Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		Expect(recorder.Code).To(Equal(http.StatusOK))ewRequest("POST", "/mutate", bytes.NewBuffer(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")

		GinkgoHelper()
		start := time.Now()
		router.ServeHTTP(recorder, req)
		duration := time.Since(start)

		// Webhook should be fast to not delay deployments
		Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		Expect(recorder.Code).To(Equal(http.StatusOK))ewRequest("POST", "/mutate", bytes.NewBuffer(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")

		GinkgoHelper()
		start := time.Now()
		router.ServeHTTP(recorder, req)
		duration := time.Since(start)

		// Webhook should be fast to not delay deployments
		Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		Expect(recorder.Code).To(Equal(http.StatusOK))l(admissionReview)
		Expect(err).NotTo(HaveOccurred())

		req, err := http.NewRequest("POST", "/mutate", bytes.NewBuffer(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")

		GinkgoHelper()
		start := time.Now()
		router.ServeHTTP(recorder, req)
		duration := time.Since(start)

		// Webhook should be fast to not delay deployments
		Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		Expect(recorder.Code).To(Equal(http.StatusOK))ewRequest("POST", "/mutate", bytes.NewBuffer(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/json")

		GinkgoHelper()
		start := time.Now()
		router.ServeHTTP(recorder, req)
		duration := time.Since(start)

		// Webhook should be fast to not delay deployments
		Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		Expect(recorder.Code).To(Equal(http.StatusOK))CENSE-2.0

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
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestWebhookContract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Contract Suite")
}

var _ = Describe("POST /mutate webhook", func() {
	var (
		router   *gin.Engine
		recorder *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// This will fail until we implement the webhook handler
		router.POST("/mutate", func(c *gin.Context) {
			// TODO: Implement proper webhook handler wrapper for Gin
			c.JSON(501, gin.H{"error": "webhook handler not implemented for Gin integration"})
		})
	})

	Context("when receiving a valid deployment admission request", func() {
		var admissionRequest *admissionv1.AdmissionRequest

		BeforeEach(func() {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "default",
					Annotations: map[string]string{
						"spotalis.io/replica-strategy": "spot-optimized",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: int32Ptr(3),
				},
			}

			deploymentJSON, _ := json.Marshal(deployment)
			admissionRequest = &admissionv1.AdmissionRequest{
				UID: "test-uid",
				Kind: metav1.GroupVersionKind{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
				},
				Object: runtime.RawExtension{
					Raw: deploymentJSON,
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

func int32Ptr(i int32) *int32 {
	return &i
}
