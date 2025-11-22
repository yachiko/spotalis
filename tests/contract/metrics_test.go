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
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/internal/server"
	"github.com/yachiko/spotalis/pkg/metrics"
)

var _ = Describe("GET /metrics endpoint", func() {
	var (
		router         *gin.Engine
		recorder       *httptest.ResponseRecorder
		metricsHandler *server.MetricsHandler
		metricsServer  *server.MetricsServer
		collector      *metrics.Collector
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		router = gin.New()
		recorder = httptest.NewRecorder()

		// Create a real collector to register metrics
		collector = metrics.NewCollector()

		// Create metrics server with the collector
		metricsServer = server.NewMetricsServer(collector)

		// Create metrics handler and wire it to the server
		metricsHandler = server.NewMetricsHandler()
		metricsHandler.SetServer(metricsServer)

		// Register the metrics endpoint
		router.GET("/metrics", metricsHandler.Metrics)
	})

	Context("when metrics are available", func() {
		It("should return 200 OK status", func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should return Prometheus metrics format", func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Header().Get("Content-Type")).To(ContainSubstring("text/plain"))
		})

		It("should return valid Prometheus exposition format", func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			// Verify Prometheus exposition format basics
			Expect(body).To(ContainSubstring("# HELP"))
			Expect(body).To(ContainSubstring("# TYPE"))
			// Should contain at least promhttp internal metrics
			Expect(body).To(ContainSubstring("promhttp_metric_handler"))
		})
	})

	Context("when metrics collection has errors", func() {
		BeforeEach(func() {
			// Reset recorder and configure error state
			recorder = httptest.NewRecorder()
			metricsHandler.SetCollectionError("prometheus registry error")
		})

		It("should still return 200 even with collection errors", func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			Expect(recorder.Code).To(Equal(http.StatusOK))
			// Metrics endpoint should be resilient and still serve available metrics
		})
	})

	Context("performance requirements", func() {
		It("should respond within 1 second", func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			GinkgoHelper()
			start := time.Now()
			router.ServeHTTP(recorder, req)
			duration := time.Since(start)

			// Metrics endpoint should be responsive
			Expect(duration).To(BeNumerically("<", 1*time.Second))
			Expect(recorder.Code).To(Equal(http.StatusOK))
		})

		It("should not include sensitive data", func() {
			req, err := http.NewRequest("GET", "/metrics", http.NoBody)
			Expect(err).NotTo(HaveOccurred())

			router.ServeHTTP(recorder, req)

			body := strings.ToLower(recorder.Body.String())
			Expect(body).NotTo(ContainSubstring("password"))
			Expect(body).NotTo(ContainSubstring("secret"))
			Expect(body).NotTo(ContainSubstring("token"))
		})
	})
})
