/*
Copyright 2026 The Spotalis Authors.

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
	"fmt"
	"math"
)

// ReplicaAllocationPolicy is the fully resolved placement policy for one
// workload. It deliberately contains values rather than pointers: allocation
// only operates on a policy after inheritance has been resolved.
type ReplicaAllocationPolicy struct {
	MinOnDemand    int32 `json:"minOnDemand"`
	SpotPercentage int32 `json:"spotPercentage"`
}

// WorkloadPolicy contains the policy values explicitly supplied by a workload.
// A nil value means the field was omitted; a pointer to zero means it was
// explicitly set to zero. Resolvers must use this type, rather than a zero
// value ReplicaAllocationPolicy, when applying inheritance.
type WorkloadPolicy struct {
	MinOnDemand    *int32 `json:"minOnDemand,omitempty"`
	SpotPercentage *int32 `json:"spotPercentage,omitempty"`
}

// Resolve applies explicitly supplied policy fields over defaults.
func (p WorkloadPolicy) Resolve(defaults ReplicaAllocationPolicy) ReplicaAllocationPolicy {
	resolved := defaults
	if p.MinOnDemand != nil {
		resolved.MinOnDemand = *p.MinOnDemand
	}
	if p.SpotPercentage != nil {
		resolved.SpotPercentage = *p.SpotPercentage
	}
	return resolved
}

// ReplicaAllocation is the desired placement target calculated for a workload.
// EffectiveOnDemandFloor is bounded by TotalReplicas, so a policy floor above a
// scaled-down workload does not produce a negative allocation.
type ReplicaAllocation struct {
	TotalReplicas          int32 `json:"totalReplicas"`
	EffectiveOnDemandFloor int32 `json:"effectiveOnDemandFloor"`
	TargetOnDemand         int32 `json:"targetOnDemand"`
	TargetSpot             int32 `json:"targetSpot"`
}

// ValidateReplicaAllocationPolicy validates inputs accepted by
// AllocateReplicaDistribution. A configured floor greater than total replicas
// is valid: the effective floor is clamped to the desired total.
func ValidateReplicaAllocationPolicy(totalReplicas int32, policy ReplicaAllocationPolicy) error {
	if totalReplicas < 0 {
		return fmt.Errorf("totalReplicas must be >= 0, got %d", totalReplicas)
	}
	if policy.MinOnDemand < 0 {
		return fmt.Errorf("minOnDemand must be >= 0, got %d", policy.MinOnDemand)
	}
	if policy.SpotPercentage < 0 || policy.SpotPercentage > 100 {
		return fmt.Errorf("spotPercentage must be 0-100, got %d", policy.SpotPercentage)
	}
	return nil
}

// AllocateReplicaDistribution is the single source of truth for desired
// replica targets. Percentage multiplication uses int64 before division so
// every legal int32 input is safe without floating-point arithmetic.
func AllocateReplicaDistribution(totalReplicas int32, policy ReplicaAllocationPolicy) (ReplicaAllocation, error) {
	if err := ValidateReplicaAllocationPolicy(totalReplicas, policy); err != nil {
		return ReplicaAllocation{}, err
	}

	effectiveFloor := minInt32(policy.MinOnDemand, totalReplicas)
	percentageTarget64 := (int64(totalReplicas) * int64(policy.SpotPercentage)) / 100
	// This cannot occur for validated int32 inputs and P <= 100, but keep the
	// checked conversion explicit if this function's input types ever widen.
	if percentageTarget64 < 0 || percentageTarget64 > math.MaxInt32 {
		return ReplicaAllocation{}, fmt.Errorf("spot target exceeds supported replica range: %d", percentageTarget64)
	}
	percentageTarget := int32(percentageTarget64)
	targetSpot := minInt32(percentageTarget, totalReplicas-effectiveFloor)

	return ReplicaAllocation{
		TotalReplicas:          totalReplicas,
		EffectiveOnDemandFloor: effectiveFloor,
		TargetOnDemand:         totalReplicas - targetSpot,
		TargetSpot:             targetSpot,
	}, nil
}
