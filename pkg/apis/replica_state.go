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
	"time"

	corev1 "k8s.io/api/core/v1"
)

// ReplicaState represents the current and desired state of replica distribution for a workload
type ReplicaState struct {
	// WorkloadRef is a reference to the source Deployment/StatefulSet
	WorkloadRef corev1.ObjectReference `json:"workloadRef"`

	// TotalReplicas is the total desired replicas from workload spec
	TotalReplicas int32 `json:"totalReplicas"`

	// CurrentOnDemand is the current number of replicas on on-demand nodes
	CurrentOnDemand int32 `json:"currentOnDemand"`

	// CurrentSpot is the current number of replicas on spot nodes
	CurrentSpot int32 `json:"currentSpot"`

	// DesiredOnDemand is the target number of replicas on on-demand nodes
	DesiredOnDemand int32 `json:"desiredOnDemand"`

	// DesiredSpot is the target number of replicas on spot nodes
	DesiredSpot int32 `json:"desiredSpot"`

	// LastReconciled is the timestamp of last successful reconciliation
	LastReconciled time.Time `json:"lastReconciled"`

	// PendingOnDemand is the number of on-demand replicas being created
	PendingOnDemand int32 `json:"pendingOnDemand,omitempty"`

	// PendingSpot is the number of spot replicas being created
	PendingSpot int32 `json:"pendingSpot,omitempty"`

	// FailedSpot is the number of spot replicas that failed to start
	FailedSpot int32 `json:"failedSpot,omitempty"`
}

// CalculateDesiredDistribution computes the desired distribution based on configuration
func (r *ReplicaState) CalculateDesiredDistribution(config WorkloadConfiguration) {
	if !config.Enabled {
		// When disabled, move everything to on-demand for safety
		r.DesiredOnDemand = r.TotalReplicas
		r.DesiredSpot = 0
		return
	}

	// Apply minimum on-demand first (safety constraint)
	r.DesiredOnDemand = max(config.MinOnDemand, 0)

	// Calculate spot replicas from percentage
	if config.SpotPercentage > 0 {
		spotReplicas := (r.TotalReplicas * config.SpotPercentage) / 100
		r.DesiredSpot = min(spotReplicas, r.TotalReplicas-r.DesiredOnDemand)
	} else {
		r.DesiredSpot = 0
	}

	// Remaining replicas go to on-demand (safe default)
	r.DesiredOnDemand = r.TotalReplicas - r.DesiredSpot
}

// GetCurrentTotal returns the total number of current replicas
func (r *ReplicaState) GetCurrentTotal() int32 {
	return r.CurrentOnDemand + r.CurrentSpot
}

// GetDesiredTotal returns the total number of desired replicas
func (r *ReplicaState) GetDesiredTotal() int32 {
	return r.DesiredOnDemand + r.DesiredSpot
}

// NeedsReconciliation returns true if the current state doesn't match desired state
func (r *ReplicaState) NeedsReconciliation() bool {
	return r.CurrentOnDemand != r.DesiredOnDemand || r.CurrentSpot != r.DesiredSpot
}

// GetOnDemandDrift returns the difference between current and desired on-demand replicas
// Positive values indicate excess on-demand replicas, negative indicates deficit
func (r *ReplicaState) GetOnDemandDrift() int32 {
	return r.CurrentOnDemand - r.DesiredOnDemand
}

// GetSpotDrift returns the difference between current and desired spot replicas
// Positive values indicate excess spot replicas, negative indicates deficit
func (r *ReplicaState) GetSpotDrift() int32 {
	return r.CurrentSpot - r.DesiredSpot
}

// RequiresScaleUp returns true if more replicas need to be created
func (r *ReplicaState) RequiresScaleUp() bool {
	return r.GetCurrentTotal() < r.TotalReplicas
}

// RequiresScaleDown returns true if replicas need to be removed
func (r *ReplicaState) RequiresScaleDown() bool {
	return r.GetCurrentTotal() > r.TotalReplicas
}

// GetNextAction returns the recommended next action for reconciliation
func (r *ReplicaState) GetNextAction() ReplicaAction {
	if !r.NeedsReconciliation() {
		return ReplicaActionNone
	}

	currentTotal := r.GetCurrentTotal()

	// If we have too few replicas overall, scale up first
	if currentTotal < r.TotalReplicas {
		// Determine which type to scale up
		if r.CurrentOnDemand < r.DesiredOnDemand {
			return ReplicaActionScaleUpOnDemand
		}
		if r.CurrentSpot < r.DesiredSpot {
			return ReplicaActionScaleUpSpot
		}
	}

	// If we have too many replicas overall, scale down
	if currentTotal > r.TotalReplicas {
		// Prefer scaling down spot first (less reliable)
		if r.CurrentSpot > r.DesiredSpot {
			return ReplicaActionScaleDownSpot
		}
		if r.CurrentOnDemand > r.DesiredOnDemand {
			return ReplicaActionScaleDownOnDemand
		}
	}

	// We have the right total but wrong distribution - migrate
	if r.CurrentOnDemand > r.DesiredOnDemand && r.CurrentSpot < r.DesiredSpot {
		return ReplicaActionMigrateToSpot
	}
	if r.CurrentSpot > r.DesiredSpot && r.CurrentOnDemand < r.DesiredOnDemand {
		return ReplicaActionMigrateToOnDemand
	}

	return ReplicaActionNone
}

// ReplicaAction represents the next action to take for replica reconciliation
type ReplicaAction string

const (
	ReplicaActionNone              ReplicaAction = "none"
	ReplicaActionScaleUpOnDemand   ReplicaAction = "scale-up-on-demand"
	ReplicaActionScaleUpSpot       ReplicaAction = "scale-up-spot"
	ReplicaActionScaleDownOnDemand ReplicaAction = "scale-down-on-demand"
	ReplicaActionScaleDownSpot     ReplicaAction = "scale-down-spot"
	ReplicaActionMigrateToSpot     ReplicaAction = "migrate-to-spot"
	ReplicaActionMigrateToOnDemand ReplicaAction = "migrate-to-on-demand"
)

// UpdateCurrentState updates the current replica counts
func (r *ReplicaState) UpdateCurrentState(onDemandCount, spotCount int32) {
	r.CurrentOnDemand = onDemandCount
	r.CurrentSpot = spotCount
}

// MarkReconciled updates the last reconciled timestamp
func (r *ReplicaState) MarkReconciled() {
	r.LastReconciled = time.Now()
}

// GetReconciliationAge returns how long since last reconciliation
func (r *ReplicaState) GetReconciliationAge() time.Duration {
	if r.LastReconciled.IsZero() {
		return time.Duration(0)
	}
	return time.Since(r.LastReconciled)
}

// IsStale returns true if the state hasn't been reconciled recently
func (r *ReplicaState) IsStale(maxAge time.Duration) bool {
	return r.GetReconciliationAge() > maxAge
}

// GetSpotPercentage returns the current percentage of spot replicas
func (r *ReplicaState) GetSpotPercentage() int32 {
	total := r.GetCurrentTotal()
	if total == 0 {
		return 0
	}
	return (r.CurrentSpot * 100) / total
}

// GetDesiredSpotPercentage returns the desired percentage of spot replicas
func (r *ReplicaState) GetDesiredSpotPercentage() int32 {
	if r.TotalReplicas == 0 {
		return 0
	}
	return (r.DesiredSpot * 100) / r.TotalReplicas
}

// Helper functions
func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
