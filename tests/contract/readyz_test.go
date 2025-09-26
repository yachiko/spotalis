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

	"github.com/ahoma/spotalis/internal/server"
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReadyzContract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Readyz Contract Suite")
}

var _ = Describe("GET /readyz endpoint", func() {
	var (
		router   *gin.Engine
		recorder *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// This will fail until we implement the readiness handler
		healthHandler := server.NewHealthHandler()
		router.GET("/readyz", healthHandler.Readyz)
	})

	Context("when the controller is ready", func() {
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

		It("should include leader election status", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"leader"`))
		})

		It("should include Kubernetes API connectivity status", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Body.String()).To(ContainSubstring(`"kubernetes"`))
		})
	})

	Context("when the controller is not ready", func() {
		BeforeEach(func() {
			// Configure health handler to simulate not ready state
			healthHandler := server.NewHealthHandler()
			healthHandler.SetNotReady("leader election pending")
			router.GET("/readyz", healthHandler.Readyz)
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

			Expect(recorder.Body.String()).To(ContainSubstring(`"status":"not_ready"`))
			Expect(recorder.Body.String()).To(ContainSubstring(`"reason"`))
		})
	})

	Context("when Kubernetes API is unavailable", func() {
		BeforeEach(func() {
			healthHandler := server.NewHealthHandler()
			healthHandler.SetKubernetesUnavailable()
			router.GET("/readyz", healthHandler.Readyz)
		})

		It("should return 503 when Kubernetes API is unreachable", func() {
			req, err := http.NewRequest("GET", "/readyz", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})
})
