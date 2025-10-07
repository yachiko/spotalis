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
	"time"

	"github.com/ahoma/spotalis/internal/server"
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GET /healthz endpoint", func() {
	var (
		router        *gin.Engine
		recorder      *httptest.ResponseRecorder
		healthHandler *server.HealthHandler
		healthChecker *server.HealthChecker
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// Create a health checker with nil manager/client for testing
		healthChecker = server.NewHealthChecker(nil, nil, "test-namespace")
		healthHandler = server.NewHealthHandler()
		healthHandler.SetChecker(healthChecker)
		router.GET("/healthz", healthHandler.Healthz)
	})

	Context("when the controller is healthy", func() {
		BeforeEach(func() {
			// Reset recorder for each test
			recorder = httptest.NewRecorder()
		})

		It("should return 200 OK status", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should return JSON content type", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("application/json"))
		})

		It("should return health status in response body", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"healthy"`))
		})

		It("should include uptime in response", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"uptime"`))
		})

		It("should respond within 100ms", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
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
			// Reset router with fresh recorder
			recorder = httptest.NewRecorder()

			// Configure health handler to simulate unhealthy state
			healthHandler.SetUnhealthy("test error")
		})

		AfterEach(func() {
			// Clean up unhealthy state for next context
			healthChecker.ClearUnhealthy()
		})

		It("should return 503 Service Unavailable when unhealthy", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable))
		})

		It("should include error details in unhealthy response", func() {
			req, err := http.NewRequest("GET", "/healthz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"unhealthy"`))
			Expect(recorder.Body.String()).To(ContainSubstring(`"reason"`))
		})
	})
})
