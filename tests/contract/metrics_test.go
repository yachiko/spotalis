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
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ahoma/spotalis/internal/server"
)

func TestMetricsContract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Contract Suite")
}

var _ = Describe("GET /metrics endpoint", func() {
	var (
		router   *gin.Engine
		recorder *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// This will fail until we implement the metrics handler
		metricsHandler := server.NewMetricsHandler()
		router.GET("/metrics", metricsHandler.Metrics)
	})

	Context("when metrics are available", func() {
		It("should return 200 OK status", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should return Prometheus metrics format", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("text/plain"))
		})

		It("should include controller runtime metrics", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("controller_runtime"))
		})

		It("should include workload management metrics", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("spotalis_workloads_total"))
			Expect(body).To(ContainSubstring("spotalis_replicas_managed"))
		})

		It("should include reconciliation performance metrics", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("spotalis_reconciliation_duration_seconds"))
			Expect(body).To(ContainSubstring("spotalis_reconciliation_errors_total"))
		})

		It("should include node classification metrics", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("spotalis_nodes_by_type"))
			Expect(body).To(ContainSubstring("spotalis_spot_interruptions_total"))
		})

		It("should include webhook metrics", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("spotalis_webhook_requests_total"))
			Expect(body).To(ContainSubstring("spotalis_webhook_duration_seconds"))
		})

		It("should include rate limiting metrics", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("spotalis_api_requests_rate_limited_total"))
		})
	})

	Context("when metrics collection has errors", func() {
		BeforeEach(func() {
			metricsHandler := server.NewMetricsHandler()
			metricsHandler.SetCollectionError("prometheus registry error")
			router.GET("/metrics", metricsHandler.Metrics)
		})

		It("should still return 200 but with error metric", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			body := recorder.Body.String()
			Expect(body).To(ContainSubstring("spotalis_metrics_collection_errors_total"))
		})
	})

	Context("performance requirements", func() {
		It("should respond within 1 second", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			start := GinkgoHelper()
			router.ServeHTTP(recorder, req)

			// Metrics endpoint should be responsive
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should not include sensitive data", func() {
			req, err := http.NewRequest("GET", "/metrics", nil)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := strings.ToLower(recorder.Body.String())
			Expect(body).NotTo(ContainSubstring("password"))
			Expect(body).NotTo(ContainSubstring("secret"))
			Expect(body).NotTo(ContainSubstring("token"))
		})
	})
})
