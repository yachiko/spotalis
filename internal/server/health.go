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

// Package server provides HTTP server components for health checks, metrics,
// and webhook endpoints used by the Spotalis controller.
package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// HealthChecker provides health checking functionality for the Spotalis controller
type HealthChecker struct {
	manager    manager.Manager
	kubeClient kubernetes.Interface
	startTime  time.Time
	namespace  string

	mu              sync.RWMutex
	unhealthyReason string
	notReadyReason  string
	kubernetesDown  bool
}

// NewHealthChecker creates a new health checker instance
func NewHealthChecker(mgr manager.Manager, kubeClient kubernetes.Interface, namespace string) *HealthChecker {
	return &HealthChecker{
		manager:    mgr,
		kubeClient: kubeClient,
		startTime:  time.Now(),
		namespace:  namespace,
	}
}

// HealthzHandler implements the /healthz endpoint
// Returns 200 OK if the controller is running and basic components are healthy
func (h *HealthChecker) HealthzHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h.mu.RLock()
	unhealthyReason := h.unhealthyReason
	kubernetesDown := h.kubernetesDown
	h.mu.RUnlock()

	// Check if manually set to unhealthy
	if unhealthyReason != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"reason": unhealthyReason,
			"uptime": time.Since(h.startTime).String(),
		})
		return
	}

	// Check if Kubernetes is manually set as down
	if kubernetesDown {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    "unhealthy",
			"component": "kubernetes-api",
			"error":     "kubernetes API marked as unavailable",
			"uptime":    time.Since(h.startTime).String(),
		})
		return
	}

	// Basic liveness check - controller is running
	uptime := time.Since(h.startTime)

	// Check if we can reach the Kubernetes API server
	if err := h.checkKubernetesAPI(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":    "unhealthy",
			"component": "kubernetes-api",
			"error":     err.Error(),
			"uptime":    uptime.String(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"uptime": uptime.String(),
		"checks": gin.H{
			"kubernetes-api": "ok",
			"controller":     "running",
		},
	})
}

// ReadyzHandler implements the /readyz endpoint
// Returns 200 OK only if the controller is ready to handle requests
func (h *HealthChecker) ReadyzHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.mu.RLock()
	notReadyReason := h.notReadyReason
	kubernetesDown := h.kubernetesDown
	h.mu.RUnlock()

	checks := make(map[string]string)
	healthy := true

	// Check if manually set to not ready
	if notReadyReason != "" {
		checks["manual-check"] = fmt.Sprintf("not ready: %s", notReadyReason)
		healthy = false
	}

	// Check if Kubernetes is manually set as down
	if kubernetesDown {
		checks["kubernetes-api"] = "manually marked as unavailable"
		healthy = false
	} else {
		// Check Kubernetes API connectivity
		if err := h.checkKubernetesAPI(ctx); err != nil {
			checks["kubernetes-api"] = fmt.Sprintf("failed: %v", err)
			healthy = false
		} else {
			checks["kubernetes-api"] = "ok"
		}
	}

	// Check if manager is running
	if h.manager == nil {
		checks["manager"] = "not initialized (test mode)"
		// In test environments, we don't require a manager to be ready
	} else {
		checks["manager"] = "ok"
	}

	// Check if we can access our deployment namespace
	if !kubernetesDown {
		if err := h.checkNamespaceAccess(ctx); err != nil {
			checks["namespace-access"] = fmt.Sprintf("failed: %v", err)
			healthy = false
		} else {
			checks["namespace-access"] = "ok"
		}
	}

	// Check controller manager readiness
	if err := h.checkControllerReadiness(ctx); err != nil {
		checks["controller-readiness"] = fmt.Sprintf("failed: %v", err)
		healthy = false
	} else {
		checks["controller-readiness"] = "ok"
	}

	status := "ready"
	statusCode := http.StatusOK
	if !healthy {
		status = "not ready"
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, gin.H{
		"status": status,
		"checks": checks,
		"uptime": time.Since(h.startTime).String(),
	})
}

// SetUnhealthy sets the health handler to unhealthy state
func (h *HealthChecker) SetUnhealthy(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unhealthyReason = reason
}

// SetNotReady sets the health handler to not ready state
func (h *HealthChecker) SetNotReady(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notReadyReason = reason
}

// SetKubernetesUnavailable sets Kubernetes as unavailable
func (h *HealthChecker) SetKubernetesUnavailable() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kubernetesDown = true
}

// ClearUnhealthy clears the unhealthy state
func (h *HealthChecker) ClearUnhealthy() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unhealthyReason = ""
}

// ClearNotReady clears the not ready state
func (h *HealthChecker) ClearNotReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notReadyReason = ""
}

// ClearKubernetesUnavailable clears the Kubernetes unavailable state
func (h *HealthChecker) ClearKubernetesUnavailable() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kubernetesDown = false
}

// checkKubernetesAPI verifies we can communicate with the Kubernetes API server
func (h *HealthChecker) checkKubernetesAPI(_ context.Context) error {
	if h.kubeClient == nil {
		return fmt.Errorf("kubernetes client not initialized")
	}

	// Try to get server version - lightweight API call
	_, err := h.kubeClient.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to connect to kubernetes API: %w", err)
	}

	return nil
}

// checkNamespaceAccess verifies we can access our deployment namespace
func (h *HealthChecker) checkNamespaceAccess(ctx context.Context) error {
	if h.namespace == "" {
		return fmt.Errorf("namespace not configured")
	}

	// Try to get the namespace
	_, err := h.kubeClient.CoreV1().Namespaces().Get(ctx, h.namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to access namespace %s: %w", h.namespace, err)
	}

	return nil
}

// checkControllerReadiness verifies the controller manager is ready
func (h *HealthChecker) checkControllerReadiness(ctx context.Context) error {
	if h.manager == nil {
		// In test environments or when manager is not initialized,
		// we don't require controller readiness
		return nil
	}

	// Check if the manager has been started
	select {
	case <-h.manager.Elected():
		// Manager is elected and ready
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for controller readiness")
	default:
		// Check if we're in a non-leader state but still healthy
		return nil
	}
}

// GetHealthzChecker returns a controller-runtime health checker for integration
func (h *HealthChecker) GetHealthzChecker() healthz.Checker {
	return func(req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()

		h.mu.RLock()
		unhealthyReason := h.unhealthyReason
		kubernetesDown := h.kubernetesDown
		h.mu.RUnlock()

		if unhealthyReason != "" {
			return fmt.Errorf("manually set unhealthy: %s", unhealthyReason)
		}

		if kubernetesDown {
			return fmt.Errorf("kubernetes API marked as unavailable")
		}

		if err := h.checkKubernetesAPI(ctx); err != nil {
			return fmt.Errorf("kubernetes API check failed: %w", err)
		}

		return nil
	}
}

// GetReadyzChecker returns a controller-runtime readiness checker for integration
func (h *HealthChecker) GetReadyzChecker() healthz.Checker {
	return func(req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()

		h.mu.RLock()
		notReadyReason := h.notReadyReason
		kubernetesDown := h.kubernetesDown
		h.mu.RUnlock()

		if notReadyReason != "" {
			return fmt.Errorf("manually set not ready: %s", notReadyReason)
		}

		if kubernetesDown {
			return fmt.Errorf("kubernetes API marked as unavailable")
		}

		if err := h.checkKubernetesAPI(ctx); err != nil {
			return fmt.Errorf("kubernetes API check failed: %w", err)
		}

		if err := h.checkNamespaceAccess(ctx); err != nil {
			return fmt.Errorf("namespace access check failed: %w", err)
		}

		if err := h.checkControllerReadiness(ctx); err != nil {
			return fmt.Errorf("controller readiness check failed: %w", err)
		}

		return nil
	}
}

// HealthHandler provides backward compatibility with contract tests
type HealthHandler struct {
	checker *HealthChecker
}

// NewHealthHandler creates a new health handler (for compatibility)
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// SetChecker sets the underlying health checker
func (h *HealthHandler) SetChecker(checker *HealthChecker) {
	h.checker = checker
}

// Healthz handles the /healthz endpoint (compatibility wrapper)
func (h *HealthHandler) Healthz(c *gin.Context) {
	if h.checker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "health checker not initialized",
		})
		return
	}
	h.checker.HealthzHandler(c)
}

// Readyz handles the /readyz endpoint (compatibility wrapper)
func (h *HealthHandler) Readyz(c *gin.Context) {
	if h.checker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not ready",
			"error":  "health checker not initialized",
		})
		return
	}
	h.checker.ReadyzHandler(c)
}

// SetUnhealthy sets the health handler to unhealthy state (compatibility wrapper)
func (h *HealthHandler) SetUnhealthy(reason string) {
	if h.checker != nil {
		h.checker.SetUnhealthy(reason)
	}
}

// SetNotReady sets the health handler to not ready state (compatibility wrapper)
func (h *HealthHandler) SetNotReady(reason string) {
	if h.checker != nil {
		h.checker.SetNotReady(reason)
	}
}

// SetKubernetesUnavailable sets Kubernetes as unavailable (compatibility wrapper)
func (h *HealthHandler) SetKubernetesUnavailable() {
	if h.checker != nil {
		h.checker.SetKubernetesUnavailable()
	}
}
