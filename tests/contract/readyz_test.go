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

	"github.com/ahoma/spotalis/internal/server"
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GET /readyz endpoint", func() {
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
		router.GET("/readyz", healthHandler.Readyz)
	})

	Context("when the controller is ready", func() {
		BeforeEach(func() {
			// Reset recorder for each test
			recorder = httptest.NewRecorder()
		})

		It("should return 200 OK status", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should return JSON content type", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("application/json"))
		})

		It("should return readiness status in response body", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"ready"`))
		})

		It("should include controller readiness checks", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"checks"`))
		})

		It("should include Kubernetes API connectivity status", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"kubernetes-api"`))
		})
	})

	Context("when the controller is not ready", func() {
		BeforeEach(func() {
			// Reset recorder and configure not ready state
			recorder = httptest.NewRecorder()
			healthHandler.SetNotReady("leader election pending")
		})

		AfterEach(func() {
			// Clean up not ready state
			healthChecker.ClearNotReady()
		})

		It("should return 503 Service Unavailable when not ready", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable))
		})

		It("should include reason for not being ready", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"not ready"`))
			Expect(recorder.Body.String()).To(ContainSubstring(`"manual-check"`))
		})
	})

	Context("when Kubernetes API is unavailable", func() {
		BeforeEach(func() {
			// Reset recorder and mark Kubernetes as unavailable
			recorder = httptest.NewRecorder()
			healthHandler.SetKubernetesUnavailable()
		})

		AfterEach(func() {
			// Clean up Kubernetes unavailable state
			healthChecker.ClearKubernetesUnavailable()
		})

		It("should return 503 when Kubernetes API is unreachable", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})
})
