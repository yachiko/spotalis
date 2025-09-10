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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ahoma/spotalis/internal/server"
)

func TestHealthzContract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Healthz Contract Suite")
}

var _ = Describe("GET /healthz endpoint", func() {
	var (
		router   *gin.Engine
		recorder *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// This will fail until we implement the health handler
		healthHandler := server.NewHealthHandler()
		router.GET("/healthz", healthHandler.Healthz)
	})

	Context("when the controller is healthy", func() {
		It("should return 200 OK status", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should return JSON content type", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("application/json"))
		})

		It("should return health status in response body", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"healthy"`))
		})

		It("should include timestamp in response", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"timestamp"`))
		})

		It("should respond within 100ms", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			GinkgoHelper()
			start := time.Now()
			router.ServeHTTP(recorder, req)
			duration := time.Since(start)

			// Health checks should be fast
			Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})
	})

	Context("when the controller has issues", func() {
		BeforeEach(func() {
			// Configure health handler to simulate unhealthy state
			healthHandler := server.NewHealthHandler()
			healthHandler.SetUnhealthy("test error")
			router.GET("/healthz", healthHandler.Healthz)
		})

		It("should return 503 Service Unavailable when unhealthy", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable))
		})

		It("should include error details in unhealthy response", func() {
			req, err := http.NewRequest("GET", "/healthz", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"unhealthy"`))
			Expect(recorder.Body.String()).To(ContainSubstring(`"error"`))
		})
	})
})
