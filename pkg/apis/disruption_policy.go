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
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DisruptionPolicy represents pod disruption budget constraints for safe rescheduling
type DisruptionPolicy struct {
	// WorkloadRef is a reference to the source workload
	WorkloadRef corev1.ObjectReference `json:"workloadRef"`

	// PDBRef is a reference to the associated PodDisruptionBudget
	PDBRef *corev1.ObjectReference `json:"pdbRef,omitempty"`

	// MinAvailable is the minimum pods that must remain available
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// MaxUnavailable is the maximum pods that can be unavailable
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// TotalReplicas is the total number of replicas for the workload
	TotalReplicas int32 `json:"totalReplicas"`

	// CurrentlyDisrupted is the number of pods currently disrupted
	CurrentlyDisrupted int32 `json:"currentlyDisrupted"`

	// CanDisrupt indicates whether additional disruption is allowed
	CanDisrupt bool `json:"canDisrupt"`

	// LastChecked is when the policy was last evaluated
	LastChecked time.Time `json:"lastChecked"`

	// DisruptionWindow defines when disruptions are allowed
	DisruptionWindow *DisruptionWindow `json:"disruptionWindow,omitempty"`
}

// DisruptionWindow defines time-based constraints for when disruptions can occur
type DisruptionWindow struct {
	// StartTime is when disruptions are allowed to begin (RFC3339 format)
	StartTime string `json:"startTime,omitempty"`

	// EndTime is when disruptions must stop (RFC3339 format)
	EndTime string `json:"endTime,omitempty"`

	// Timezone for the window (default UTC)
	Timezone string `json:"timezone,omitempty"`

	// Days of week when disruptions are allowed (0=Sunday, 6=Saturday)
	DaysOfWeek []int `json:"daysOfWeek,omitempty"`
}

// NewDisruptionPolicy creates a new disruption policy from a PDB
func NewDisruptionPolicy(workloadRef corev1.ObjectReference, pdb *policyv1.PodDisruptionBudget, totalReplicas int32) *DisruptionPolicy {
	policy := &DisruptionPolicy{
		WorkloadRef:        workloadRef,
		TotalReplicas:      totalReplicas,
		CurrentlyDisrupted: 0,
		LastChecked:        time.Now(),
	}

	if pdb != nil {
		policy.PDBRef = &corev1.ObjectReference{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
			Name:       pdb.Name,
			Namespace:  pdb.Namespace,
		}
		policy.MinAvailable = pdb.Spec.MinAvailable
		policy.MaxUnavailable = pdb.Spec.MaxUnavailable
	}

	policy.CanDisrupt = policy.calculateCanDisrupt()
	return policy
}

// CanDisruptPod returns true if an additional pod can be safely disrupted
func (d *DisruptionPolicy) CanDisruptPod() bool {
	d.LastChecked = time.Now()
	d.CanDisrupt = d.calculateCanDisrupt()
	return d.CanDisrupt
}

// calculateCanDisrupt determines if disruption is currently allowed
func (d *DisruptionPolicy) calculateCanDisrupt() bool {
	// Check time-based disruption window
	if !d.isWithinDisruptionWindow() {
		return false
	}

	// If no PDB exists, allow disruption (workload chose not to set constraints)
	if d.PDBRef == nil {
		return true
	}

	availablePods := d.TotalReplicas - d.CurrentlyDisrupted

	// Check MinAvailable constraint
	if d.MinAvailable != nil {
		minAvailable := d.resolveIntOrString(d.MinAvailable, d.TotalReplicas)
		if availablePods <= minAvailable {
			return false
		}
	}

	// Check MaxUnavailable constraint
	if d.MaxUnavailable != nil {
		maxUnavailable := d.resolveIntOrString(d.MaxUnavailable, d.TotalReplicas)
		if d.CurrentlyDisrupted >= maxUnavailable {
			return false
		}
	}

	return true
}

// resolveIntOrString converts IntOrString to actual integer value
func (d *DisruptionPolicy) resolveIntOrString(val *intstr.IntOrString, totalReplicas int32) int32 {
	if val == nil {
		return 0
	}

	switch val.Type {
	case intstr.Int:
		return val.IntVal
	case intstr.String:
		// Parse percentage
		if val.StrVal == "" {
			return 0
		}
		// This is a simplified percentage parsing - in real implementation
		// you'd want to use intstr.GetValueFromIntOrPercent
		return totalReplicas / 2 // Placeholder - should parse percentage properly
	default:
		return 0
	}
}

// isWithinDisruptionWindow checks if current time is within allowed disruption window
func (d *DisruptionPolicy) isWithinDisruptionWindow() bool {
	if d.DisruptionWindow == nil {
		return true // No time constraints
	}

	now := time.Now()

	// Check day of week constraints
	if len(d.DisruptionWindow.DaysOfWeek) > 0 {
		currentDay := int(now.Weekday())
		allowed := false
		for _, day := range d.DisruptionWindow.DaysOfWeek {
			if day == currentDay {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// TODO: Implement start/end time checking with timezone support
	// This would require parsing RFC3339 times and timezone handling

	return true
}

// RecordDisruption updates the count of currently disrupted pods
func (d *DisruptionPolicy) RecordDisruption(delta int32) {
	d.CurrentlyDisrupted += delta
	if d.CurrentlyDisrupted < 0 {
		d.CurrentlyDisrupted = 0
	}
	d.LastChecked = time.Now()
	d.CanDisrupt = d.calculateCanDisrupt()
}

// GetAvailablePods returns the number of pods currently available
func (d *DisruptionPolicy) GetAvailablePods() int32 {
	return d.TotalReplicas - d.CurrentlyDisrupted
}

// GetMaxAllowedDisruptions returns the maximum number of additional disruptions allowed
func (d *DisruptionPolicy) GetMaxAllowedDisruptions() int32 {
	if d.PDBRef == nil {
		return d.TotalReplicas // No constraints
	}

	maxDisruptions := d.TotalReplicas

	// Apply MinAvailable constraint
	if d.MinAvailable != nil {
		minAvailable := d.resolveIntOrString(d.MinAvailable, d.TotalReplicas)
		maxFromMinAvailable := d.TotalReplicas - minAvailable
		if maxFromMinAvailable < maxDisruptions {
			maxDisruptions = maxFromMinAvailable
		}
	}

	// Apply MaxUnavailable constraint
	if d.MaxUnavailable != nil {
		maxUnavailable := d.resolveIntOrString(d.MaxUnavailable, d.TotalReplicas)
		if maxUnavailable < maxDisruptions {
			maxDisruptions = maxUnavailable
		}
	}

	// Subtract already disrupted pods
	remainingDisruptions := maxDisruptions - d.CurrentlyDisrupted
	if remainingDisruptions < 0 {
		remainingDisruptions = 0
	}

	return remainingDisruptions
}

// IsHealthy returns true if the disruption policy is not being violated
func (d *DisruptionPolicy) IsHealthy() bool {
	return d.GetMaxAllowedDisruptions() >= 0
}

// UpdateTotalReplicas updates the total replica count and recalculates constraints
func (d *DisruptionPolicy) UpdateTotalReplicas(newTotal int32) {
	d.TotalReplicas = newTotal
	d.LastChecked = time.Now()
	d.CanDisrupt = d.calculateCanDisrupt()
}

// GetPolicyStatus returns a human-readable status of the disruption policy
func (d *DisruptionPolicy) GetPolicyStatus() DisruptionPolicyStatus {
	return DisruptionPolicyStatus{
		TotalReplicas:  d.TotalReplicas,
		AvailablePods:  d.GetAvailablePods(),
		DisruptedPods:  d.CurrentlyDisrupted,
		MaxDisruptions: d.GetMaxAllowedDisruptions(),
		CanDisrupt:     d.CanDisrupt,
		HasPDB:         d.PDBRef != nil,
		LastChecked:    d.LastChecked,
		WithinWindow:   d.isWithinDisruptionWindow(),
	}
}

// DisruptionPolicyStatus provides a summary of the current disruption policy state
type DisruptionPolicyStatus struct {
	TotalReplicas  int32     `json:"totalReplicas"`
	AvailablePods  int32     `json:"availablePods"`
	DisruptedPods  int32     `json:"disruptedPods"`
	MaxDisruptions int32     `json:"maxDisruptions"`
	CanDisrupt     bool      `json:"canDisrupt"`
	HasPDB         bool      `json:"hasPDB"`
	LastChecked    time.Time `json:"lastChecked"`
	WithinWindow   bool      `json:"withinWindow"`
}

// DisruptionReason explains why disruption is or isn't allowed
type DisruptionReason string

const (
	DisruptionReasonAllowed          DisruptionReason = "allowed"
	DisruptionReasonNoPDB            DisruptionReason = "no-pdb"
	DisruptionReasonMinAvailable     DisruptionReason = "min-available-violated"
	DisruptionReasonMaxUnavailable   DisruptionReason = "max-unavailable-reached"
	DisruptionReasonOutsideWindow    DisruptionReason = "outside-disruption-window"
	DisruptionReasonTooManyDisrupted DisruptionReason = "too-many-disrupted"
)

// GetDisruptionReason returns the reason why disruption is or isn't allowed
func (d *DisruptionPolicy) GetDisruptionReason() DisruptionReason {
	if !d.isWithinDisruptionWindow() {
		return DisruptionReasonOutsideWindow
	}

	if d.PDBRef == nil {
		return DisruptionReasonNoPDB
	}

	if d.MinAvailable != nil {
		minAvailable := d.resolveIntOrString(d.MinAvailable, d.TotalReplicas)
		if d.GetAvailablePods() <= minAvailable {
			return DisruptionReasonMinAvailable
		}
	}

	if d.MaxUnavailable != nil {
		maxUnavailable := d.resolveIntOrString(d.MaxUnavailable, d.TotalReplicas)
		if d.CurrentlyDisrupted >= maxUnavailable {
			return DisruptionReasonMaxUnavailable
		}
	}

	return DisruptionReasonAllowed
}
