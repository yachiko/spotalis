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

package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
)

var (
	// Node metrics
	totalNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_nodes_total",
			Help: "Total number of nodes by type",
		},
		[]string{"node_type", "status"},
	)

	readyNodes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_nodes_ready",
			Help: "Number of ready nodes by type",
		},
		[]string{"node_type"},
	)

	// Workload metrics
	managedWorkloads = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_workloads_managed_total",
			Help: "Total number of workloads managed by Spotalis",
		},
		[]string{"namespace", "workload_type"},
	)

	workloadReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_workload_replicas",
			Help: "Number of replicas for managed workloads",
		},
		[]string{"namespace", "workload", "workload_type", "replica_type"},
	)

	// Spot instance utilization metrics
	spotUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_spot_utilization_percentage",
			Help: "Percentage of spot instance utilization",
		},
		[]string{"namespace", "workload", "workload_type"},
	)

	// Reconciliation metrics
	reconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "spotalis_reconciliation_duration_seconds",
			Help:    "Time spent on workload reconciliation",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "workload", "workload_type", "action"},
	)

	reconciliationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_reconciliations_total",
			Help: "Total number of reconciliations performed",
		},
		[]string{"namespace", "workload", "workload_type", "action", "result"},
	)

	reconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_reconciliation_errors_total",
			Help: "Total number of reconciliation errors",
		},
		[]string{"namespace", "workload", "workload_type", "error_type"},
	)

	// Webhook metrics
	webhookRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_webhook_requests_total",
			Help: "Total number of webhook requests",
		},
		[]string{"operation", "resource_kind", "result"},
	)

	webhookDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "spotalis_webhook_duration_seconds",
			Help:    "Time spent processing webhook requests",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"operation", "resource_kind"},
	)

	webhookMutations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_webhook_mutations_total",
			Help: "Total number of webhook mutations applied",
		},
		[]string{"resource_kind", "mutation_type"},
	)

	// API server load metrics
	apiRequestRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_api_requests_per_second",
			Help: "Current API request rate to Kubernetes API server",
		},
		[]string{"api_group", "resource"},
	)

	apiRequestErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_api_request_errors_total",
			Help: "Total number of API request errors",
		},
		[]string{"api_group", "resource", "error_type"},
	)

	rateLimitHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		},
		[]string{"api_group", "resource"},
	)

	// Controller health metrics
	controllerLastSeen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_controller_last_seen_timestamp",
			Help: "Timestamp when controller was last seen",
		},
		[]string{"controller_name"},
	)

	leaderElectionStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_leader_election_status",
			Help: "Current leader election status (1 for leader, 0 for follower)",
		},
		[]string{"controller_name"},
	)
)

// Collector handles metrics collection for Spotalis
type Collector struct {
	nodeClassifier        *config.NodeClassifierService
	mutex                 sync.RWMutex
	lastUpdate            time.Time
	managedWorkloadsCount int // Track total managed workloads
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		lastUpdate: time.Now(),
	}
}

// SetNodeClassifier sets the node classifier service for metrics collection
func (c *Collector) SetNodeClassifier(classifier *config.NodeClassifierService) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.nodeClassifier = classifier
}

// RegisterMetrics registers all Spotalis metrics with the provided registry
func (c *Collector) RegisterMetrics(registry prometheus.Registerer) {
	if registry == nil {
		registry = metrics.Registry // Use global registry as fallback
	}

	registry.MustRegister(
		totalNodes,
		readyNodes,
		managedWorkloads,
		workloadReplicas,
		spotUtilization,
		reconciliationDuration,
		reconciliationTotal,
		reconciliationErrors,
		webhookRequests,
		webhookDuration,
		webhookMutations,
		apiRequestRate,
		apiRequestErrors,
		rateLimitHits,
		controllerLastSeen,
		leaderElectionStatus,
	)
}

// RegisterMetricsGlobal registers all Spotalis metrics with the global registry (for backwards compatibility)
func (c *Collector) RegisterMetricsGlobal() {
	c.RegisterMetrics(metrics.Registry)
}

// UpdateNodeMetrics updates node-related metrics
func (c *Collector) UpdateNodeMetrics(ctx context.Context) error {
	c.mutex.RLock()
	classifier := c.nodeClassifier
	c.mutex.RUnlock()

	if classifier == nil {
		return nil // No classifier available
	}

	summary, err := classifier.GetNodeClassificationSummary(ctx)
	if err != nil {
		return err
	}

	// Update node count metrics
	totalNodes.WithLabelValues("spot", "total").Set(float64(summary.TotalSpotNodes))
	totalNodes.WithLabelValues("on-demand", "total").Set(float64(summary.TotalOnDemandNodes))

	readyNodes.WithLabelValues("spot").Set(float64(summary.ReadySpotNodes))
	readyNodes.WithLabelValues("on-demand").Set(float64(summary.ReadyOnDemandNodes))

	c.mutex.Lock()
	c.lastUpdate = time.Now()
	c.mutex.Unlock()

	return nil
}

// RecordWorkloadMetrics records metrics for a managed workload
func (c *Collector) RecordWorkloadMetrics(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState) {
	c.mutex.Lock()
	c.managedWorkloadsCount++
	c.mutex.Unlock()

	// Update managed workload count
	managedWorkloads.WithLabelValues(namespace, workloadType).Inc()

	if replicaState != nil {
		// Update replica counts
		workloadReplicas.WithLabelValues(namespace, workloadName, workloadType, "spot").Set(float64(replicaState.CurrentSpot))
		workloadReplicas.WithLabelValues(namespace, workloadName, workloadType, "on-demand").Set(float64(replicaState.CurrentOnDemand))

		// Calculate and record spot utilization percentage
		totalReplicas := replicaState.CurrentSpot + replicaState.CurrentOnDemand
		if totalReplicas > 0 {
			utilizationPercent := float64(replicaState.CurrentSpot) / float64(totalReplicas) * 100
			spotUtilization.WithLabelValues(namespace, workloadName, workloadType).Set(utilizationPercent)
		}
	}
}

// RecordReconciliation records metrics for a reconciliation operation
func (c *Collector) RecordReconciliation(namespace, workloadName, workloadType, action string, duration time.Duration, err error) {
	result := "success"
	if err != nil {
		result = "error"
		reconciliationErrors.WithLabelValues(namespace, workloadName, workloadType, "reconciliation").Inc()
	}

	reconciliationDuration.WithLabelValues(namespace, workloadName, workloadType, action).Observe(duration.Seconds())
	reconciliationTotal.WithLabelValues(namespace, workloadName, workloadType, action, result).Inc()
}

// RecordWebhookRequest records metrics for webhook requests
func (c *Collector) RecordWebhookRequest(operation, resourceKind, result string, duration time.Duration) {
	webhookRequests.WithLabelValues(operation, resourceKind, result).Inc()
	webhookDuration.WithLabelValues(operation, resourceKind).Observe(duration.Seconds())
}

// RecordWebhookMutation records metrics for webhook mutations
func (c *Collector) RecordWebhookMutation(resourceKind, mutationType string) {
	webhookMutations.WithLabelValues(resourceKind, mutationType).Inc()
}

// RecordAPIRequest records metrics for API requests
func (c *Collector) RecordAPIRequest(apiGroup, resource string, requestRate float64) {
	apiRequestRate.WithLabelValues(apiGroup, resource).Set(requestRate)
}

// RecordAPIError records metrics for API errors
func (c *Collector) RecordAPIError(apiGroup, resource, errorType string) {
	apiRequestErrors.WithLabelValues(apiGroup, resource, errorType).Inc()
}

// RecordRateLimitHit records metrics for rate limit hits
func (c *Collector) RecordRateLimitHit(apiGroup, resource string) {
	rateLimitHits.WithLabelValues(apiGroup, resource).Inc()
}

// UpdateControllerHealth updates controller health metrics
func (c *Collector) UpdateControllerHealth(controllerName string, isLeader bool) {
	controllerLastSeen.WithLabelValues(controllerName).SetToCurrentTime()

	if isLeader {
		leaderElectionStatus.WithLabelValues(controllerName).Set(1)
	} else {
		leaderElectionStatus.WithLabelValues(controllerName).Set(0)
	}
}

// GetMetricsSnapshot returns a snapshot of current metrics values
func (c *Collector) GetMetricsSnapshot() MetricsSnapshot {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return MetricsSnapshot{
		LastUpdate:       c.lastUpdate,
		Timestamp:        time.Now(),
		ManagedWorkloads: c.managedWorkloadsCount,
	}
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	LastUpdate       time.Time `json:"lastUpdate"`
	Timestamp        time.Time `json:"timestamp"`
	ManagedWorkloads int       `json:"managedWorkloads"`
}

// ResetMetrics resets all metrics (useful for testing)
func (c *Collector) ResetMetrics() {
	c.mutex.Lock()
	c.managedWorkloadsCount = 0
	c.mutex.Unlock()

	totalNodes.Reset()
	readyNodes.Reset()
	managedWorkloads.Reset()
	workloadReplicas.Reset()
	spotUtilization.Reset()
	reconciliationDuration.Reset()
	reconciliationTotal.Reset()
	reconciliationErrors.Reset()
	webhookRequests.Reset()
	webhookDuration.Reset()
	webhookMutations.Reset()
	apiRequestRate.Reset()
	apiRequestErrors.Reset()
	rateLimitHits.Reset()
	controllerLastSeen.Reset()
	leaderElectionStatus.Reset()
}

// StartMetricsCollection starts background metrics collection
func (c *Collector) StartMetricsCollection(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.UpdateNodeMetrics(ctx); err != nil {
				// Log error but continue
				continue
			}
		}
	}
}

// Timer provides timing functionality for metrics
type Timer struct {
	start time.Time
}

// NewTimer creates a new timer
func NewTimer() *Timer {
	return &Timer{start: time.Now()}
}

// Elapsed returns the elapsed duration since timer creation
func (t *Timer) Elapsed() time.Duration {
	return time.Since(t.start)
}

// ObserveReconciliation observes reconciliation duration and records metrics
func (t *Timer) ObserveReconciliation(collector *Collector, namespace, workloadName, workloadType, action string, err error) {
	duration := t.Elapsed()
	collector.RecordReconciliation(namespace, workloadName, workloadType, action, duration, err)
}

// ObserveWebhook observes webhook duration and records metrics
func (t *Timer) ObserveWebhook(collector *Collector, operation, resourceKind, result string) {
	duration := t.Elapsed()
	collector.RecordWebhookRequest(operation, resourceKind, result, duration)
}

// Global metrics collector instance
var GlobalCollector = NewCollector()

// Helper functions for common metrics operations

// RecordWorkloadReconciliation is a convenience function for recording workload reconciliation
func RecordWorkloadReconciliation(namespace, workloadName, workloadType, action string, duration time.Duration, err error) {
	GlobalCollector.RecordReconciliation(namespace, workloadName, workloadType, action, duration, err)
}

// RecordWebhookOperation is a convenience function for recording webhook operations
func RecordWebhookOperation(operation, resourceKind, result string, duration time.Duration) {
	GlobalCollector.RecordWebhookRequest(operation, resourceKind, result, duration)
}

// UpdateNodeHealth is a convenience function for updating node health metrics
func UpdateNodeHealth(ctx context.Context) error {
	return GlobalCollector.UpdateNodeMetrics(ctx)
}

// RecordSpotUtilization is a convenience function for recording spot utilization
func RecordSpotUtilization(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState) {
	GlobalCollector.RecordWorkloadMetrics(namespace, workloadName, workloadType, replicaState)
}
