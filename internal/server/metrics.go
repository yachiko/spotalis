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
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	metricsCollector "github.com/spotalis/spotalis/pkg/metrics"
)

// MetricsServer provides metrics collection and serving functionality
type MetricsServer struct {
	collector *metricsCollector.Collector
	registry  *prometheus.Registry
	gatherer  prometheus.Gatherer

	mu                sync.RWMutex
	collectionError   string
	customMetrics     map[string]prometheus.Collector
	lastCollection    time.Time
	collectionLatency time.Duration
}

// NewMetricsServer creates a new metrics server instance
func NewMetricsServer(collector *metricsCollector.Collector) *MetricsServer {
	registry := prometheus.NewRegistry()

	// Register the collector with the custom registry
	if collector != nil {
		registry.MustRegister(collector)
	}

	// Also register with controller-runtime's default registry
	if collector != nil {
		metrics.Registry.MustRegister(collector)
	}

	return &MetricsServer{
		collector:     collector,
		registry:      registry,
		gatherer:      registry,
		customMetrics: make(map[string]prometheus.Collector),
	}
}

// MetricsHandler implements the /metrics endpoint
// Returns Prometheus format metrics for the Spotalis controller
func (m *MetricsServer) MetricsHandler(c *gin.Context) {
	start := time.Now()
	defer func() {
		m.mu.Lock()
		m.lastCollection = time.Now()
		m.collectionLatency = time.Since(start)
		m.mu.Unlock()
	}()

	// Check if there's a collection error
	m.mu.RLock()
	collectionError := m.collectionError
	m.mu.RUnlock()

	if collectionError != "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "metrics collection failed",
			"reason": collectionError,
			"code":   "METRICS_COLLECTION_ERROR",
		})
		return
	}

	// Update metrics before serving
	if m.collector != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		if err := m.collector.UpdateMetrics(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "failed to update metrics",
				"reason": err.Error(),
				"code":   "METRICS_UPDATE_ERROR",
			})
			return
		}
	}

	// Use Prometheus HTTP handler to serve metrics
	handler := promhttp.HandlerFor(m.gatherer, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		Registry:      m.registry,
		Timeout:       30 * time.Second,
	})

	// Wrap the Prometheus handler for Gin
	gin.WrapH(handler)(c)
}

// HealthMetricsHandler returns health information about metrics collection
func (m *MetricsServer) HealthMetricsHandler(c *gin.Context) {
	m.mu.RLock()
	collectionError := m.collectionError
	lastCollection := m.lastCollection
	latency := m.collectionLatency
	m.mu.RUnlock()

	status := "healthy"
	statusCode := http.StatusOK

	health := gin.H{
		"status": status,
		"metrics_collector": gin.H{
			"last_collection": lastCollection.Format(time.RFC3339),
			"latency_ms":      latency.Milliseconds(),
			"error":           collectionError,
		},
	}

	// Check if metrics collection is failing
	if collectionError != "" {
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
		health["status"] = status
	}

	// Check if collection is stale (more than 5 minutes old)
	if !lastCollection.IsZero() && time.Since(lastCollection) > 5*time.Minute {
		status = "stale"
		statusCode = http.StatusServiceUnavailable
		health["status"] = status
		health["warning"] = "metrics collection is stale"
	}

	c.JSON(statusCode, health)
}

// SetCollectionError sets a metrics collection error
func (m *MetricsServer) SetCollectionError(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectionError = err
}

// ClearCollectionError clears the metrics collection error
func (m *MetricsServer) ClearCollectionError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectionError = ""
}

// RegisterCustomMetric registers a custom Prometheus metric
func (m *MetricsServer) RegisterCustomMetric(name string, metric prometheus.Collector) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.customMetrics[name]; exists {
		return fmt.Errorf("metric %s already registered", name)
	}

	if err := m.registry.Register(metric); err != nil {
		return fmt.Errorf("failed to register metric %s: %w", name, err)
	}

	m.customMetrics[name] = metric
	return nil
}

// UnregisterCustomMetric unregisters a custom Prometheus metric
func (m *MetricsServer) UnregisterCustomMetric(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	metric, exists := m.customMetrics[name]
	if !exists {
		return fmt.Errorf("metric %s not found", name)
	}

	if !m.registry.Unregister(metric) {
		return fmt.Errorf("failed to unregister metric %s", name)
	}

	delete(m.customMetrics, name)
	return nil
}

// GetCollectionStatus returns the current collection status
func (m *MetricsServer) GetCollectionStatus() (string, time.Time, time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.collectionError, m.lastCollection, m.collectionLatency
}

// GetRegisteredMetrics returns a list of registered custom metrics
func (m *MetricsServer) GetRegisteredMetrics() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.customMetrics))
	for name := range m.customMetrics {
		names = append(names, name)
	}
	return names
}

// GetRegistry returns the Prometheus registry for advanced usage
func (m *MetricsServer) GetRegistry() *prometheus.Registry {
	return m.registry
}

// MetricsHandler provides backward compatibility with contract tests
type MetricsHandler struct {
	server *MetricsServer
}

// NewMetricsHandler creates a new metrics handler (for compatibility)
func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

// SetServer sets the underlying metrics server
func (m *MetricsHandler) SetServer(server *MetricsServer) {
	m.server = server
}

// Metrics handles the /metrics endpoint (compatibility wrapper)
func (m *MetricsHandler) Metrics(c *gin.Context) {
	if m.server == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "metrics server not initialized",
			"code":  "METRICS_SERVER_NOT_INITIALIZED",
		})
		return
	}
	m.server.MetricsHandler(c)
}

// SetCollectionError sets a metrics collection error (compatibility wrapper)
func (m *MetricsHandler) SetCollectionError(err string) {
	if m.server != nil {
		m.server.SetCollectionError(err)
	}
}
