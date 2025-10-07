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

// Package metrics provides Prometheus metrics collection and recording
// for Spotalis controller operations and workload management.
package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/apis"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// Workload metrics
	workloadReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "spotalis_workload_replicas",
			Help: "Number of replicas for managed workloads",
		},
		[]string{"namespace", "workload", "workload_type", "replica_type"},
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

	webhookMutations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spotalis_webhook_mutations_total",
			Help: "Total number of webhook mutations applied",
		},
		[]string{"resource_kind", "mutation_type"},
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
	// Initialize metrics with zero values so they appear in Prometheus output
	// even before any workloads are managed
	initializeMetrics()

	return &Collector{
		lastUpdate: time.Now(),
	}
}

// initializeMetrics initializes all metrics with zero/default values
// This ensures they appear in the Prometheus metrics output even if not yet used
func initializeMetrics() {
	// Initialize counters to 0 (they will appear in output)
	// reconciliationTotal: []string{"namespace", "workload", "workload_type", "action", "result"}
	reconciliationTotal.WithLabelValues("", "", "", "", "success").Add(0)

	// reconciliationErrors: []string{"namespace", "workload", "workload_type", "error_type"}
	reconciliationErrors.WithLabelValues("", "", "", "").Add(0)

	// webhookRequests: []string{"operation", "resource_kind", "result"}
	webhookRequests.WithLabelValues("", "", "").Add(0)

	// webhookMutations: []string{"resource_kind", "mutation_type"}
	webhookMutations.WithLabelValues("", "").Add(0)

	// Initialize gauges to 0
	// workloadReplicas: []string{"namespace", "workload", "workload_type", "replica_type"}
	workloadReplicas.WithLabelValues("", "", "", "spot").Set(0)
	workloadReplicas.WithLabelValues("", "", "", "on-demand").Set(0)

	// controllerLastSeen: []string{"controller_name"}
	controllerLastSeen.WithLabelValues("").Set(0)

	// leaderElectionStatus: []string{"controller_name"}
	leaderElectionStatus.WithLabelValues("").Set(0)
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

	// Use Register instead of MustRegister to avoid panics on duplicate registration
	// This can happen during controller restarts or in tests
	collectors := []prometheus.Collector{
		workloadReplicas,
		reconciliationTotal,
		reconciliationErrors,
		webhookRequests,
		webhookMutations,
		controllerLastSeen,
		leaderElectionStatus,
	}

	for _, collector := range collectors {
		_ = registry.Register(collector)
		// Ignore all registration errors (including "already registered")
		// In production, you might want to log unexpected errors
	}
}

// RegisterMetricsGlobal registers all Spotalis metrics with the global registry (for backwards compatibility)
func (c *Collector) RegisterMetricsGlobal() {
	c.RegisterMetrics(metrics.Registry)
}

// RecordWorkloadMetrics records metrics for a managed workload
func (c *Collector) RecordWorkloadMetrics(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState) {
	c.mutex.Lock()
	c.managedWorkloadsCount++
	c.mutex.Unlock()

	if replicaState != nil {
		// Update replica counts
		workloadReplicas.WithLabelValues(namespace, workloadName, workloadType, "spot").Set(float64(replicaState.CurrentSpot))
		workloadReplicas.WithLabelValues(namespace, workloadName, workloadType, "on-demand").Set(float64(replicaState.CurrentOnDemand))
	}
}

// RecordReconciliation records metrics for a reconciliation operation
func (c *Collector) RecordReconciliation(namespace, workloadName, workloadType, action string, err error) {
	result := "success"
	if err != nil {
		result = "error"
		reconciliationErrors.WithLabelValues(namespace, workloadName, workloadType, "reconciliation").Inc()
	}

	reconciliationTotal.WithLabelValues(namespace, workloadName, workloadType, action, result).Inc()
}

// RecordWebhookRequest records metrics for webhook requests
func (c *Collector) RecordWebhookRequest(operation, resourceKind, result string) {
	webhookRequests.WithLabelValues(operation, resourceKind, result).Inc()
}

// RecordWebhookMutation records metrics for webhook mutations
func (c *Collector) RecordWebhookMutation(resourceKind, mutationType string) {
	webhookMutations.WithLabelValues(resourceKind, mutationType).Inc()
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
func (c *Collector) GetMetricsSnapshot() Snapshot {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return Snapshot{
		LastUpdate:       c.lastUpdate,
		Timestamp:        time.Now(),
		ManagedWorkloads: c.managedWorkloadsCount,
	}
}

// Snapshot represents a point-in-time snapshot of metrics
type Snapshot struct {
	LastUpdate       time.Time `json:"lastUpdate"`
	Timestamp        time.Time `json:"timestamp"`
	ManagedWorkloads int       `json:"managedWorkloads"`
}

// ResetMetrics resets all metrics (useful for testing)
func (c *Collector) ResetMetrics() {
	c.mutex.Lock()
	c.managedWorkloadsCount = 0
	c.mutex.Unlock()

	workloadReplicas.Reset()
	reconciliationTotal.Reset()
	reconciliationErrors.Reset()
	webhookRequests.Reset()
	webhookMutations.Reset()
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
			// Metrics collection logic can be added here if needed
			// Currently a placeholder for future enhancements
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
	collector.RecordReconciliation(namespace, workloadName, workloadType, action, err)
}

// ObserveWebhook observes webhook duration and records metrics
func (t *Timer) ObserveWebhook(collector *Collector, operation, resourceKind, result string) {
	collector.RecordWebhookRequest(operation, resourceKind, result)
}

// GlobalCollector is the shared metrics collector instance used throughout the application
var GlobalCollector = NewCollector()

// Helper functions for common metrics operations

// RecordWorkloadReconciliation is a convenience function for recording workload reconciliation
func RecordWorkloadReconciliation(namespace, workloadName, workloadType, action string, err error) {
	GlobalCollector.RecordReconciliation(namespace, workloadName, workloadType, action, err)
}

// RecordWebhookOperation is a convenience function for recording webhook operations
func RecordWebhookOperation(operation, resourceKind, result string) {
	GlobalCollector.RecordWebhookRequest(operation, resourceKind, result)
}

// RecordSpotUtilization is a convenience function for recording spot utilization
func RecordSpotUtilization(namespace, workloadName, workloadType string, replicaState *apis.ReplicaState) {
	GlobalCollector.RecordWorkloadMetrics(namespace, workloadName, workloadType, replicaState)
}
