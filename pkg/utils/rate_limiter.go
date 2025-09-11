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

package utils

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/client-go/util/workqueue"
)

// RateLimiterConfig contains configuration for rate limiting
type RateLimiterConfig struct {
	// Basic rate limiting
	QPS   float64
	Burst int

	// Backoff configuration
	BaseDelay         time.Duration
	MaxDelay          time.Duration
	BackoffMultiplier float64

	// Per-resource rate limiting
	PerResourceQPS   map[string]float64
	PerResourceBurst map[string]int

	// Circuit breaker configuration
	FailureThreshold int
	RecoveryTimeout  time.Duration
	HalfOpenRequests int

	// Advanced options
	EnableMetrics        bool
	EnableCircuitBreaker bool
}

// DefaultRateLimiterConfig returns default rate limiter configuration
func DefaultRateLimiterConfig() *RateLimiterConfig {
	return &RateLimiterConfig{
		QPS:                  20.0,
		Burst:                30,
		BaseDelay:            1 * time.Second,
		MaxDelay:             60 * time.Second,
		BackoffMultiplier:    2.0,
		PerResourceQPS:       make(map[string]float64),
		PerResourceBurst:     make(map[string]int),
		FailureThreshold:     5,
		RecoveryTimeout:      30 * time.Second,
		HalfOpenRequests:     3,
		EnableMetrics:        true,
		EnableCircuitBreaker: true,
	}
}

// RateLimiter provides rate limiting and circuit breaking for API calls
type RateLimiter struct {
	config *RateLimiterConfig

	// Global rate limiter
	globalLimiter *rate.Limiter

	// Per-resource rate limiters
	resourceLimiters map[string]*rate.Limiter
	limiterMutex     sync.RWMutex

	// Circuit breaker
	circuitBreaker *CircuitBreaker

	// Metrics
	metrics *RateLimiterMetrics

	// Workqueue rate limiter
	workqueueLimiter workqueue.RateLimiter
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config *RateLimiterConfig) *RateLimiter {
	if config == nil {
		config = DefaultRateLimiterConfig()
	}

	rl := &RateLimiter{
		config:           config,
		globalLimiter:    rate.NewLimiter(rate.Limit(config.QPS), config.Burst),
		resourceLimiters: make(map[string]*rate.Limiter),
	}

	// Initialize circuit breaker if enabled
	if config.EnableCircuitBreaker {
		rl.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: config.FailureThreshold,
			RecoveryTimeout:  config.RecoveryTimeout,
			HalfOpenRequests: config.HalfOpenRequests,
		})
	}

	// Initialize metrics if enabled
	if config.EnableMetrics {
		rl.metrics = NewRateLimiterMetrics()
	}

	// Create workqueue rate limiter
	rl.workqueueLimiter = workqueue.NewItemExponentialFailureRateLimiter(
		config.BaseDelay,
		config.MaxDelay,
	)

	return rl
}

// Wait waits until the rate limiter allows a request
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.WaitForResource(ctx, "")
}

// WaitForResource waits until the rate limiter allows a request for a specific resource
func (rl *RateLimiter) WaitForResource(ctx context.Context, resource string) error {
	start := time.Now()

	// Check circuit breaker first
	if rl.circuitBreaker != nil && !rl.circuitBreaker.CanExecute() {
		if rl.metrics != nil {
			rl.metrics.RecordCircuitBreakerTrip(resource)
		}
		return fmt.Errorf("circuit breaker is open for resource: %s", resource)
	}

	// Get the appropriate rate limiter
	limiter := rl.getLimiterForResource(resource)

	// Wait for rate limiter
	err := limiter.Wait(ctx)

	if rl.metrics != nil {
		duration := time.Since(start)
		if err != nil {
			rl.metrics.RecordRateLimitWait(resource, duration, false)
		} else {
			rl.metrics.RecordRateLimitWait(resource, duration, true)
		}
	}

	return err
}

// Allow checks if a request is allowed without waiting
func (rl *RateLimiter) Allow() bool {
	return rl.AllowForResource("")
}

// AllowForResource checks if a request is allowed for a specific resource without waiting
func (rl *RateLimiter) AllowForResource(resource string) bool {
	// Check circuit breaker first
	if rl.circuitBreaker != nil && !rl.circuitBreaker.CanExecute() {
		if rl.metrics != nil {
			rl.metrics.RecordCircuitBreakerTrip(resource)
		}
		return false
	}

	// Get the appropriate rate limiter
	limiter := rl.getLimiterForResource(resource)
	allowed := limiter.Allow()

	if rl.metrics != nil {
		rl.metrics.RecordRateLimitCheck(resource, allowed)
	}

	return allowed
}

// Reserve reserves a token for future use
func (rl *RateLimiter) Reserve() *rate.Reservation {
	return rl.ReserveForResource("")
}

// ReserveForResource reserves a token for a specific resource
func (rl *RateLimiter) ReserveForResource(resource string) *rate.Reservation {
	limiter := rl.getLimiterForResource(resource)
	reservation := limiter.Reserve()

	if rl.metrics != nil {
		rl.metrics.RecordRateLimitReservation(resource)
	}

	return reservation
}

// RecordSuccess records a successful operation for circuit breaker
func (rl *RateLimiter) RecordSuccess(resource string) {
	if rl.circuitBreaker != nil {
		rl.circuitBreaker.RecordSuccess()
	}
	if rl.metrics != nil {
		rl.metrics.RecordOperationSuccess(resource)
	}
}

// RecordFailure records a failed operation for circuit breaker
func (rl *RateLimiter) RecordFailure(resource string, err error) {
	if rl.circuitBreaker != nil {
		rl.circuitBreaker.RecordFailure()
	}
	if rl.metrics != nil {
		rl.metrics.RecordOperationFailure(resource, err)
	}
}

// GetWorkqueueRateLimiter returns a workqueue-compatible rate limiter
func (rl *RateLimiter) GetWorkqueueRateLimiter() workqueue.RateLimiter {
	return rl.workqueueLimiter
}

// UpdateConfig updates the rate limiter configuration
func (rl *RateLimiter) UpdateConfig(config *RateLimiterConfig) {
	rl.config = config

	// Update global limiter
	rl.globalLimiter.SetLimit(rate.Limit(config.QPS))
	rl.globalLimiter.SetBurst(config.Burst)

	// Update resource limiters
	rl.limiterMutex.Lock()
	defer rl.limiterMutex.Unlock()

	for resource, limiter := range rl.resourceLimiters {
		if qps, exists := config.PerResourceQPS[resource]; exists {
			limiter.SetLimit(rate.Limit(qps))
		}
		if burst, exists := config.PerResourceBurst[resource]; exists {
			limiter.SetBurst(burst)
		}
	}
}

// getLimiterForResource gets the rate limiter for a specific resource
func (rl *RateLimiter) getLimiterForResource(resource string) *rate.Limiter {
	if resource == "" {
		return rl.globalLimiter
	}

	rl.limiterMutex.RLock()
	if limiter, exists := rl.resourceLimiters[resource]; exists {
		rl.limiterMutex.RUnlock()
		return limiter
	}
	rl.limiterMutex.RUnlock()

	// Create new resource-specific limiter
	rl.limiterMutex.Lock()
	defer rl.limiterMutex.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := rl.resourceLimiters[resource]; exists {
		return limiter
	}

	// Get QPS and burst for this resource
	qps := rl.config.QPS
	burst := rl.config.Burst

	if resourceQPS, exists := rl.config.PerResourceQPS[resource]; exists {
		qps = resourceQPS
	}
	if resourceBurst, exists := rl.config.PerResourceBurst[resource]; exists {
		burst = resourceBurst
	}

	limiter := rate.NewLimiter(rate.Limit(qps), burst)
	rl.resourceLimiters[resource] = limiter
	return limiter
}

// GetMetrics returns the current rate limiter metrics
func (rl *RateLimiter) GetMetrics() *RateLimiterMetrics {
	return rl.metrics
}

// GetCircuitBreakerState returns the current circuit breaker state
func (rl *RateLimiter) GetCircuitBreakerState() CircuitBreakerState {
	if rl.circuitBreaker == nil {
		return CircuitBreakerStateClosed
	}
	return rl.circuitBreaker.GetState()
}

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	CircuitBreakerStateClosed CircuitBreakerState = iota
	CircuitBreakerStateOpen
	CircuitBreakerStateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitBreakerStateClosed:
		return "closed"
	case CircuitBreakerStateOpen:
		return "open"
	case CircuitBreakerStateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig contains configuration for circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	HalfOpenRequests int
}

// CircuitBreaker implements a circuit breaker pattern
type CircuitBreaker struct {
	config CircuitBreakerConfig

	state            CircuitBreakerState
	failures         int
	lastFailureTime  time.Time
	halfOpenRequests int

	mutex sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  CircuitBreakerStateClosed,
	}
}

// CanExecute checks if a request can be executed
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case CircuitBreakerStateClosed:
		return true
	case CircuitBreakerStateOpen:
		// Check if recovery timeout has passed
		if time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
			cb.mutex.RUnlock()
			cb.mutex.Lock()
			cb.state = CircuitBreakerStateHalfOpen
			cb.halfOpenRequests = 0
			cb.mutex.Unlock()
			cb.mutex.RLock()
			return true
		}
		return false
	case CircuitBreakerStateHalfOpen:
		return cb.halfOpenRequests < cb.config.HalfOpenRequests
	default:
		return false
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	switch cb.state {
	case CircuitBreakerStateHalfOpen:
		cb.halfOpenRequests++
		if cb.halfOpenRequests >= cb.config.HalfOpenRequests {
			cb.state = CircuitBreakerStateClosed
			cb.failures = 0
		}
	case CircuitBreakerStateClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitBreakerStateClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = CircuitBreakerStateOpen
		}
	case CircuitBreakerStateHalfOpen:
		cb.state = CircuitBreakerStateOpen
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// RateLimiterMetrics collects metrics for rate limiting
type RateLimiterMetrics struct {
	// Rate limiting metrics
	totalWaits      int64
	totalWaitTime   time.Duration
	allowedRequests int64
	deniedRequests  int64
	reservations    int64

	// Circuit breaker metrics
	circuitBreakerTrips int64
	operationSuccesses  int64
	operationFailures   int64

	// Per-resource metrics
	resourceMetrics map[string]*ResourceMetrics

	mutex sync.RWMutex
}

// ResourceMetrics contains metrics for a specific resource
type ResourceMetrics struct {
	Waits           int64
	WaitTime        time.Duration
	AllowedRequests int64
	DeniedRequests  int64
	Successes       int64
	Failures        int64
}

// NewRateLimiterMetrics creates new rate limiter metrics
func NewRateLimiterMetrics() *RateLimiterMetrics {
	return &RateLimiterMetrics{
		resourceMetrics: make(map[string]*ResourceMetrics),
	}
}

// RecordRateLimitWait records a rate limit wait operation
func (m *RateLimiterMetrics) RecordRateLimitWait(resource string, duration time.Duration, success bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.totalWaits++
	m.totalWaitTime += duration

	if success {
		m.allowedRequests++
	} else {
		m.deniedRequests++
	}

	// Update resource-specific metrics
	if resource != "" {
		if _, exists := m.resourceMetrics[resource]; !exists {
			m.resourceMetrics[resource] = &ResourceMetrics{}
		}
		rm := m.resourceMetrics[resource]
		rm.Waits++
		rm.WaitTime += duration
		if success {
			rm.AllowedRequests++
		} else {
			rm.DeniedRequests++
		}
	}
}

// RecordRateLimitCheck records a rate limit check operation
func (m *RateLimiterMetrics) RecordRateLimitCheck(resource string, allowed bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if allowed {
		m.allowedRequests++
	} else {
		m.deniedRequests++
	}

	// Update resource-specific metrics
	if resource != "" {
		if _, exists := m.resourceMetrics[resource]; !exists {
			m.resourceMetrics[resource] = &ResourceMetrics{}
		}
		rm := m.resourceMetrics[resource]
		if allowed {
			rm.AllowedRequests++
		} else {
			rm.DeniedRequests++
		}
	}
}

// RecordRateLimitReservation records a rate limit reservation
func (m *RateLimiterMetrics) RecordRateLimitReservation(resource string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.reservations++
}

// RecordCircuitBreakerTrip records a circuit breaker trip
func (m *RateLimiterMetrics) RecordCircuitBreakerTrip(resource string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.circuitBreakerTrips++
}

// RecordOperationSuccess records a successful operation
func (m *RateLimiterMetrics) RecordOperationSuccess(resource string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.operationSuccesses++

	// Update resource-specific metrics
	if resource != "" {
		if _, exists := m.resourceMetrics[resource]; !exists {
			m.resourceMetrics[resource] = &ResourceMetrics{}
		}
		m.resourceMetrics[resource].Successes++
	}
}

// RecordOperationFailure records a failed operation
func (m *RateLimiterMetrics) RecordOperationFailure(resource string, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.operationFailures++

	// Update resource-specific metrics
	if resource != "" {
		if _, exists := m.resourceMetrics[resource]; !exists {
			m.resourceMetrics[resource] = &ResourceMetrics{}
		}
		m.resourceMetrics[resource].Failures++
	}
}

// GetSummary returns a summary of rate limiter metrics
func (m *RateLimiterMetrics) GetSummary() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	summary := map[string]interface{}{
		"total_waits":           m.totalWaits,
		"total_wait_time_ms":    m.totalWaitTime.Milliseconds(),
		"allowed_requests":      m.allowedRequests,
		"denied_requests":       m.deniedRequests,
		"reservations":          m.reservations,
		"circuit_breaker_trips": m.circuitBreakerTrips,
		"operation_successes":   m.operationSuccesses,
		"operation_failures":    m.operationFailures,
	}

	// Add average wait time
	if m.totalWaits > 0 {
		summary["average_wait_time_ms"] = float64(m.totalWaitTime.Milliseconds()) / float64(m.totalWaits)
	}

	// Add success rate
	totalOps := m.operationSuccesses + m.operationFailures
	if totalOps > 0 {
		summary["success_rate"] = float64(m.operationSuccesses) / float64(totalOps)
	}

	return summary
}

// GetResourceMetrics returns metrics for a specific resource
func (m *RateLimiterMetrics) GetResourceMetrics(resource string) *ResourceMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if metrics, exists := m.resourceMetrics[resource]; exists {
		// Return a copy to avoid race conditions
		copy := *metrics
		return &copy
	}
	return nil
}
