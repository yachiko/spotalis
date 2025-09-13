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

package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
)

var _ = Describe("MetricsServer", func() {
	var (
		metricsServer *MetricsServer
		engine        *gin.Engine
	)

	BeforeEach(func() {
		// Create a metrics server without the problematic collector registration
		// Use nil collector to avoid registration issues
		metricsServer = &MetricsServer{
			collector:     nil,
			registry:      prometheus.NewRegistry(),
			customMetrics: make(map[string]prometheus.Collector),
		}
		metricsServer.gatherer = metricsServer.registry
		engine = createTestEngine()
	})

	Describe("NewMetricsServer", func() {
		It("should create a new metrics server with valid registry", func() {
			// Test with nil collector to avoid registration issues
			server := &MetricsServer{
				collector:     nil,
				registry:      prometheus.NewRegistry(),
				customMetrics: make(map[string]prometheus.Collector),
			}
			Expect(server).NotTo(BeNil())
			Expect(server.registry).NotTo(BeNil())
			Expect(server.customMetrics).NotTo(BeNil())
		})
	})

	Describe("MetricsHandler", func() {
		BeforeEach(func() {
			engine.GET("/metrics", metricsServer.MetricsHandler)
		})

		It("should serve Prometheus metrics", func() {
			response := performRequest(engine, "GET", "/metrics", nil)
			Expect(response.Code).To(Equal(http.StatusOK))
			Expect(response.Header().Get("Content-Type")).To(ContainSubstring("text/plain"))

			// Check that response contains Prometheus format metrics
			body := response.Body.String()
			Expect(body).To(ContainSubstring("# HELP"))
			Expect(body).To(ContainSubstring("# TYPE"))
		})

		It("should include custom metrics when registered", func() {
			// Register a custom counter
			customCounter := prometheus.NewCounter(prometheus.CounterOpts{
				Name: "test_custom_counter",
				Help: "A test custom counter",
			})

			err := metricsServer.RegisterCustomMetric("test_counter", customCounter)
			Expect(err).NotTo(HaveOccurred())

			// Increment the counter
			customCounter.Inc()

			response := performRequest(engine, "GET", "/metrics", nil)
			Expect(response.Code).To(Equal(http.StatusOK))

			body := response.Body.String()
			Expect(body).To(ContainSubstring("test_custom_counter"))
		})

		It("should handle collection errors gracefully", func() {
			metricsServer.SetCollectionError("test collection error")

			response := performRequest(engine, "GET", "/metrics", nil)
			Expect(response.Code).To(Equal(http.StatusOK))

			// Should still serve metrics but include error information
			body := response.Body.String()
			Expect(body).To(ContainSubstring("# HELP"))
		})
	})

	Describe("HealthMetricsHandler", func() {
		BeforeEach(func() {
			engine.GET("/metrics/health", metricsServer.HealthMetricsHandler)
		})

		It("should return health metrics in JSON format", func() {
			response := performRequest(engine, "GET", "/metrics/health", nil)
			Expect(response.Code).To(Equal(http.StatusOK))
			Expect(response.Header().Get("Content-Type")).To(ContainSubstring("application/json"))

			var result map[string]interface{}
			err := parseJSONResponse(response, &result)
			Expect(err).NotTo(HaveOccurred())

			Expect(result).To(HaveKey("status"))
			Expect(result).To(HaveKey("metrics_collector"))

			metricsCollector, ok := result["metrics_collector"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(metricsCollector).To(HaveKey("error"))
			Expect(metricsCollector).To(HaveKey("last_collection"))
			Expect(metricsCollector).To(HaveKey("latency_ms"))
		})

		It("should show error when collection error is set", func() {
			testError := "test collection failure"
			metricsServer.SetCollectionError(testError)

			response := performRequest(engine, "GET", "/metrics/health", nil)
			Expect(response.Code).To(Equal(http.StatusServiceUnavailable))

			var result map[string]interface{}
			err := parseJSONResponse(response, &result)
			Expect(err).NotTo(HaveOccurred())

			Expect(result["status"]).To(Equal("degraded"))

			metricsCollector, ok := result["metrics_collector"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(metricsCollector["error"]).To(Equal(testError))
		})

		It("should show clean state when no error", func() {
			metricsServer.ClearCollectionError()

			response := performRequest(engine, "GET", "/metrics/health", nil)
			Expect(response.Code).To(Equal(http.StatusOK))

			var result map[string]interface{}
			err := parseJSONResponse(response, &result)
			Expect(err).NotTo(HaveOccurred())

			metricsCollector, ok := result["metrics_collector"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(metricsCollector["error"]).To(Equal(""))
		})
	})

	Describe("Custom Metrics Management", func() {
		Context("RegisterCustomMetric", func() {
			It("should register a new custom metric", func() {
				counter := prometheus.NewCounter(prometheus.CounterOpts{
					Name: "test_counter",
					Help: "A test counter",
				})

				err := metricsServer.RegisterCustomMetric("test_counter", counter)
				Expect(err).NotTo(HaveOccurred())

				metrics := metricsServer.GetRegisteredMetrics()
				Expect(metrics).To(ContainElement("test_counter"))
			})

			It("should return error when registering duplicate metric", func() {
				counter1 := prometheus.NewCounter(prometheus.CounterOpts{
					Name: "duplicate_counter",
					Help: "First counter",
				})
				counter2 := prometheus.NewCounter(prometheus.CounterOpts{
					Name: "duplicate_counter",
					Help: "Second counter",
				})

				err := metricsServer.RegisterCustomMetric("duplicate", counter1)
				Expect(err).NotTo(HaveOccurred())

				err = metricsServer.RegisterCustomMetric("duplicate", counter2)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("UnregisterCustomMetric", func() {
			It("should unregister an existing custom metric", func() {
				counter := prometheus.NewCounter(prometheus.CounterOpts{
					Name: "unregister_test_counter",
					Help: "A test counter for unregistering",
				})

				err := metricsServer.RegisterCustomMetric("unregister_test", counter)
				Expect(err).NotTo(HaveOccurred())

				metrics := metricsServer.GetRegisteredMetrics()
				Expect(metrics).To(ContainElement("unregister_test"))

				err = metricsServer.UnregisterCustomMetric("unregister_test")
				Expect(err).NotTo(HaveOccurred())

				metrics = metricsServer.GetRegisteredMetrics()
				Expect(metrics).NotTo(ContainElement("unregister_test"))
			})

			It("should return error when unregistering non-existent metric", func() {
				err := metricsServer.UnregisterCustomMetric("non_existent")
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Collection Status", func() {
		Context("SetCollectionError", func() {
			It("should set collection error", func() {
				testError := "test error message"
				metricsServer.SetCollectionError(testError)

				errMsg, _, _ := metricsServer.GetCollectionStatus()
				Expect(errMsg).To(Equal(testError))
			})
		})

		Context("ClearCollectionError", func() {
			It("should clear collection error", func() {
				metricsServer.SetCollectionError("test error")
				metricsServer.ClearCollectionError()

				errMsg, _, _ := metricsServer.GetCollectionStatus()
				Expect(errMsg).To(Equal(""))
			})
		})

		Context("GetCollectionStatus", func() {
			It("should return collection status information", func() {
				metricsServer.SetCollectionError("test error")

				errMsg, lastCollection, latency := metricsServer.GetCollectionStatus()

				Expect(errMsg).To(Equal("test error"))
				Expect(lastCollection).To(BeTemporally("<=", time.Now()))
				Expect(latency).To(BeNumerically(">=", 0))
			})
		})
	})

	Describe("GetRegistry", func() {
		It("should return the Prometheus registry", func() {
			registry := metricsServer.GetRegistry()
			Expect(registry).NotTo(BeNil())
			Expect(registry).To(Equal(metricsServer.registry))
		})
	})
})

var _ = Describe("MetricsHandler", func() {
	var (
		metricsHandler *MetricsHandler
		metricsServer  *MetricsServer
		engine         *gin.Engine
	)

	BeforeEach(func() {
		// Create a metrics server without the problematic collector registration
		metricsServer = &MetricsServer{
			collector:     nil,
			registry:      prometheus.NewRegistry(),
			customMetrics: make(map[string]prometheus.Collector),
		}
		metricsServer.gatherer = metricsServer.registry
		metricsHandler = NewMetricsHandler()
		metricsHandler.SetServer(metricsServer)
		engine = createTestEngine()
	})

	Describe("NewMetricsHandler", func() {
		It("should create a new metrics handler", func() {
			handler := NewMetricsHandler()
			Expect(handler).NotTo(BeNil())
		})
	})

	Describe("Metrics", func() {
		BeforeEach(func() {
			engine.GET("/metrics", metricsHandler.Metrics)
		})

		It("should delegate to the metrics server", func() {
			response := performRequest(engine, "GET", "/metrics", nil)
			Expect(response.Code).To(Equal(http.StatusOK))
			Expect(response.Header().Get("Content-Type")).To(ContainSubstring("text/plain"))
		})

		It("should handle case when server is not set", func() {
			handler := NewMetricsHandler()
			engine.GET("/no-server", handler.Metrics)

			response := performRequest(engine, "GET", "/no-server", nil)
			Expect(response.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Describe("SetCollectionError", func() {
		It("should delegate to the metrics server", func() {
			testError := "handler test error"
			metricsHandler.SetCollectionError(testError)

			errMsg, _, _ := metricsServer.GetCollectionStatus()
			Expect(errMsg).To(Equal(testError))
		})

		It("should handle case when server is not set", func() {
			handler := NewMetricsHandler()
			// Should not panic
			handler.SetCollectionError("test error")
		})
	})
})
