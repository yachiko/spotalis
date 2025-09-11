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

package apis

import (
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// NamespaceFilter represents namespace-level filtering state for multi-tenant support
type NamespaceFilter struct {
	// MonitoredNamespaces is the list of namespaces currently being monitored
	MonitoredNamespaces []string `json:"monitoredNamespaces"`

	// LastUpdated is when the filter was last recalculated
	LastUpdated time.Time `json:"lastUpdated"`

	// TotalWorkloads is the number of workloads across all monitored namespaces
	TotalWorkloads int `json:"totalWorkloads"`

	// Selector is the label selector used for filtering
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// AllowAll indicates if all namespaces should be monitored
	AllowAll bool `json:"allowAll"`

	// mutex protects concurrent access to the filter state
	mutex sync.RWMutex `json:"-"`
}

// NewNamespaceFilter creates a new namespace filter with the given selector
func NewNamespaceFilter(selector *metav1.LabelSelector) *NamespaceFilter {
	return &NamespaceFilter{
		MonitoredNamespaces: make([]string, 0),
		LastUpdated:         time.Now(),
		TotalWorkloads:      0,
		Selector:            selector,
		AllowAll:            selector == nil,
	}
}

// ShouldMonitorNamespace checks if a namespace should be monitored
func (nf *NamespaceFilter) ShouldMonitorNamespace(ns *corev1.Namespace) bool {
	nf.mutex.RLock()
	defer nf.mutex.RUnlock()

	if nf.AllowAll {
		return true
	}

	if nf.Selector == nil {
		return false
	}

	selector, err := metav1.LabelSelectorAsSelector(nf.Selector)
	if err != nil {
		return false
	}

	return selector.Matches(labels.Set(ns.Labels))
}

// UpdateNamespaces recalculates the list of monitored namespaces
func (nf *NamespaceFilter) UpdateNamespaces(namespaces []corev1.Namespace) {
	nf.mutex.Lock()
	defer nf.mutex.Unlock()

	var monitoredNamespaces []string

	for _, ns := range namespaces {
		if nf.shouldMonitorNamespaceInternal(&ns) {
			monitoredNamespaces = append(monitoredNamespaces, ns.Name)
		}
	}

	// Sort for consistent ordering
	sort.Strings(monitoredNamespaces)

	nf.MonitoredNamespaces = monitoredNamespaces
	nf.LastUpdated = time.Now()
}

// shouldMonitorNamespaceInternal is the internal version without locking
func (nf *NamespaceFilter) shouldMonitorNamespaceInternal(ns *corev1.Namespace) bool {
	if nf.AllowAll {
		return true
	}

	if nf.Selector == nil {
		return false
	}

	selector, err := metav1.LabelSelectorAsSelector(nf.Selector)
	if err != nil {
		return false
	}

	return selector.Matches(labels.Set(ns.Labels))
}

// GetMonitoredNamespaces returns a copy of the monitored namespaces list
func (nf *NamespaceFilter) GetMonitoredNamespaces() []string {
	nf.mutex.RLock()
	defer nf.mutex.RUnlock()

	// Return a copy to prevent external modification
	result := make([]string, len(nf.MonitoredNamespaces))
	copy(result, nf.MonitoredNamespaces)
	return result
}

// GetNamespaceCount returns the number of monitored namespaces
func (nf *NamespaceFilter) GetNamespaceCount() int {
	nf.mutex.RLock()
	defer nf.mutex.RUnlock()

	return len(nf.MonitoredNamespaces)
}

// UpdateWorkloadCount updates the total workload count
func (nf *NamespaceFilter) UpdateWorkloadCount(count int) {
	nf.mutex.Lock()
	defer nf.mutex.Unlock()

	nf.TotalWorkloads = count
	nf.LastUpdated = time.Now()
}

// GetWorkloadCount returns the current workload count
func (nf *NamespaceFilter) GetWorkloadCount() int {
	nf.mutex.RLock()
	defer nf.mutex.RUnlock()

	return nf.TotalWorkloads
}

// IsStale returns true if the filter should be refreshed
func (nf *NamespaceFilter) IsStale(maxAge time.Duration) bool {
	nf.mutex.RLock()
	defer nf.mutex.RUnlock()

	return time.Since(nf.LastUpdated) > maxAge
}

// GetFilterStatus returns the current status of the namespace filter
func (nf *NamespaceFilter) GetFilterStatus() NamespaceFilterStatus {
	nf.mutex.RLock()
	defer nf.mutex.RUnlock()

	return NamespaceFilterStatus{
		MonitoredCount: len(nf.MonitoredNamespaces),
		TotalWorkloads: nf.TotalWorkloads,
		LastUpdated:    nf.LastUpdated,
		AllowAll:       nf.AllowAll,
		HasSelector:    nf.Selector != nil,
	}
}

// NamespaceFilterStatus provides status information about namespace filtering
type NamespaceFilterStatus struct {
	MonitoredCount int       `json:"monitoredCount"`
	TotalWorkloads int       `json:"totalWorkloads"`
	LastUpdated    time.Time `json:"lastUpdated"`
	AllowAll       bool      `json:"allowAll"`
	HasSelector    bool      `json:"hasSelector"`
}

// APIRateLimiter represents API server load management state
type APIRateLimiter struct {
	// RequestsPerSecond is the current request rate to API server
	RequestsPerSecond float64 `json:"requestsPerSecond"`

	// LastMeasurement is when rate was last calculated
	LastMeasurement time.Time `json:"lastMeasurement"`

	// BackoffUntil is when to retry after rate limiting
	BackoffUntil time.Time `json:"backoffUntil"`

	// SuccessfulRequests is the count of successful API requests
	SuccessfulRequests int64 `json:"successfulRequests"`

	// FailedRequests is the count of failed API requests
	FailedRequests int64 `json:"failedRequests"`

	// RateLimitHits is the number of times rate limiting was triggered
	RateLimitHits int64 `json:"rateLimitHits"`

	// MaxRequestsPerSecond is the maximum allowed request rate
	MaxRequestsPerSecond float64 `json:"maxRequestsPerSecond"`

	// BackoffMultiplier is applied to backoff duration on each rate limit hit
	BackoffMultiplier float64 `json:"backoffMultiplier"`

	// BaseBackoffDuration is the initial backoff duration
	BaseBackoffDuration time.Duration `json:"baseBackoffDuration"`

	// requestTimes tracks recent request timestamps for rate calculation
	requestTimes []time.Time `json:"-"`

	// mutex protects concurrent access to rate limiter state
	mutex sync.Mutex `json:"-"`
}

// NewAPIRateLimiter creates a new API rate limiter with default settings
func NewAPIRateLimiter() *APIRateLimiter {
	return &APIRateLimiter{
		MaxRequestsPerSecond: 50.0, // Default: 50 requests per second
		BackoffMultiplier:    2.0,  // Double backoff on each hit
		BaseBackoffDuration:  1 * time.Second,
		requestTimes:         make([]time.Time, 0, 100),
	}
}

// ShouldBackoff returns true if requests should be delayed due to rate limiting
func (r *APIRateLimiter) ShouldBackoff() bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return time.Now().Before(r.BackoffUntil)
}

// RecordSuccess records a successful API request
func (r *APIRateLimiter) RecordSuccess() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.SuccessfulRequests++
	r.recordRequest()
	r.updateRequestRate()
}

// RecordFailure records a failed API request
func (r *APIRateLimiter) RecordFailure() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.FailedRequests++
	r.recordRequest()
	r.updateRequestRate()

	// Check if we should apply rate limiting
	if r.RequestsPerSecond > r.MaxRequestsPerSecond {
		r.triggerBackoff()
	}
}

// recordRequest adds the current time to the request history
func (r *APIRateLimiter) recordRequest() {
	now := time.Now()
	r.requestTimes = append(r.requestTimes, now)

	// Keep only the last minute of requests for rate calculation
	cutoff := now.Add(-1 * time.Minute)
	for len(r.requestTimes) > 0 && r.requestTimes[0].Before(cutoff) {
		r.requestTimes = r.requestTimes[1:]
	}

	// Limit memory usage by keeping max 1000 entries
	if len(r.requestTimes) > 1000 {
		r.requestTimes = r.requestTimes[len(r.requestTimes)-1000:]
	}
}

// updateRequestRate calculates the current request rate
func (r *APIRateLimiter) updateRequestRate() {
	now := time.Now()
	r.LastMeasurement = now

	if len(r.requestTimes) < 2 {
		r.RequestsPerSecond = 0
		return
	}

	// Calculate requests per second over the last minute
	cutoff := now.Add(-1 * time.Minute)
	recentRequests := 0
	for _, requestTime := range r.requestTimes {
		if requestTime.After(cutoff) {
			recentRequests++
		}
	}

	// Convert to requests per second
	r.RequestsPerSecond = float64(recentRequests) / 60.0
}

// triggerBackoff initiates rate limiting backoff
func (r *APIRateLimiter) triggerBackoff() {
	r.RateLimitHits++

	// Calculate backoff duration with exponential backoff
	backoffDuration := r.BaseBackoffDuration
	for i := int64(1); i < r.RateLimitHits && i < 10; i++ { // Cap at 10 iterations
		backoffDuration = time.Duration(float64(backoffDuration) * r.BackoffMultiplier)
	}

	// Cap maximum backoff at 5 minutes
	if backoffDuration > 5*time.Minute {
		backoffDuration = 5 * time.Minute
	}

	r.BackoffUntil = time.Now().Add(backoffDuration)
}

// GetRateLimitStatus returns the current rate limiting status
func (r *APIRateLimiter) GetRateLimitStatus() RateLimitStatus {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return RateLimitStatus{
		RequestsPerSecond:    r.RequestsPerSecond,
		MaxRequestsPerSecond: r.MaxRequestsPerSecond,
		SuccessfulRequests:   r.SuccessfulRequests,
		FailedRequests:       r.FailedRequests,
		RateLimitHits:        r.RateLimitHits,
		IsBackingOff:         time.Now().Before(r.BackoffUntil),
		BackoffUntil:         r.BackoffUntil,
		LastMeasurement:      r.LastMeasurement,
	}
}

// RateLimitStatus provides status information about API rate limiting
type RateLimitStatus struct {
	RequestsPerSecond    float64   `json:"requestsPerSecond"`
	MaxRequestsPerSecond float64   `json:"maxRequestsPerSecond"`
	SuccessfulRequests   int64     `json:"successfulRequests"`
	FailedRequests       int64     `json:"failedRequests"`
	RateLimitHits        int64     `json:"rateLimitHits"`
	IsBackingOff         bool      `json:"isBackingOff"`
	BackoffUntil         time.Time `json:"backoffUntil"`
	LastMeasurement      time.Time `json:"lastMeasurement"`
}

// GetSuccessRate returns the success rate as a percentage
func (r *APIRateLimiter) GetSuccessRate() float64 {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	total := r.SuccessfulRequests + r.FailedRequests
	if total == 0 {
		return 100.0 // No requests yet, assume 100%
	}

	return (float64(r.SuccessfulRequests) / float64(total)) * 100.0
}

// Reset clears the rate limiter state
func (r *APIRateLimiter) Reset() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.RequestsPerSecond = 0
	r.SuccessfulRequests = 0
	r.FailedRequests = 0
	r.RateLimitHits = 0
	r.BackoffUntil = time.Time{}
	r.requestTimes = r.requestTimes[:0]
	r.LastMeasurement = time.Now()
}

// UpdateMaxRate updates the maximum allowed request rate
func (r *APIRateLimiter) UpdateMaxRate(maxRate float64) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.MaxRequestsPerSecond = maxRate
}
