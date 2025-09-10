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
	"github.com/gin-gonic/gin"
)

// HealthHandler handles health check endpoints
type HealthHandler struct {
	// Implementation will be added in Phase 3.3
}

// NewHealthHandler creates a new health handler
func NewHealthHandler() *HealthHandler {
	// This will fail until we implement it in T030
	panic("NewHealthHandler not implemented - test should fail")
}

// Healthz handles the /healthz endpoint
func (h *HealthHandler) Healthz(c *gin.Context) {
	// This will fail until we implement it in T030
	panic("Healthz not implemented - test should fail")
}

// Readyz handles the /readyz endpoint
func (h *HealthHandler) Readyz(c *gin.Context) {
	// This will fail until we implement it in T030
	panic("Readyz not implemented - test should fail")
}

// SetUnhealthy sets the health handler to unhealthy state
func (h *HealthHandler) SetUnhealthy(reason string) {
	// This will fail until we implement it in T030
	panic("SetUnhealthy not implemented - test should fail")
}

// SetNotReady sets the health handler to not ready state
func (h *HealthHandler) SetNotReady(reason string) {
	// This will fail until we implement it in T030
	panic("SetNotReady not implemented - test should fail")
}

// SetKubernetesUnavailable sets Kubernetes as unavailable
func (h *HealthHandler) SetKubernetesUnavailable() {
	// This will fail until we implement it in T030
	panic("SetKubernetesUnavailable not implemented - test should fail")
}

// MetricsHandler handles metrics endpoints
type MetricsHandler struct {
	// Implementation will be added in Phase 3.3
}

// NewMetricsHandler creates a new metrics handler
func NewMetricsHandler() *MetricsHandler {
	// This will fail until we implement it in T031
	panic("NewMetricsHandler not implemented - test should fail")
}

// Metrics handles the /metrics endpoint
func (h *MetricsHandler) Metrics(c *gin.Context) {
	// This will fail until we implement it in T031
	panic("Metrics not implemented - test should fail")
}

// SetCollectionError sets a metrics collection error
func (h *MetricsHandler) SetCollectionError(err string) {
	// This will fail until we implement it in T031
	panic("SetCollectionError not implemented - test should fail")
}
