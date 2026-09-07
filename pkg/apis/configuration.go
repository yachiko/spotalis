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

// Package apis defines the core API types and data structures used throughout
// the Spotalis controller for workload configuration and state management.
package apis

import (
	"fmt"
	"strconv"
	"strings"
)

// WorkloadConfiguration represents the parsed configuration from Kubernetes annotations
// on Deployments and StatefulSets.
type WorkloadConfiguration struct {
	// Enabled indicates whether the workload is managed by spotalis
	Enabled bool `json:"enabled"`

	// MinOnDemand is the minimum number of replicas on on-demand nodes
	MinOnDemand int32 `json:"minOnDemand"`

	// SpotPercentage is the target percentage of replicas on spot nodes (0-100)
	SpotPercentage int32 `json:"spotPercentage"`

	// Policy preserves whether each annotation was omitted or explicitly set,
	// including an explicit zero. MinOnDemand and SpotPercentage remain as the
	// resolved compatibility view for existing consumers.
	Policy WorkloadPolicy `json:"-"`
}

// AllocationPolicy returns the resolved policy consumed by the allocation
// engine. Callers resolving inheritance should use Policy.Resolve first.
func (w WorkloadConfiguration) AllocationPolicy() ReplicaAllocationPolicy {
	return ReplicaAllocationPolicy{
		MinOnDemand:    w.MinOnDemand,
		SpotPercentage: w.SpotPercentage,
	}
}

// Validate checks if the WorkloadConfiguration is valid according to business rules
func (w *WorkloadConfiguration) Validate(totalReplicas int32) error {
	if !w.Enabled {
		return nil // Skip validation for disabled workloads
	}

	return ValidateReplicaAllocationPolicy(totalReplicas, w.AllocationPolicy())
}

// IsSpotOptimized returns true if this configuration prefers spot nodes
func (w *WorkloadConfiguration) IsSpotOptimized() bool {
	return w.SpotPercentage > 50
}

// IsOnDemandOnly returns true if this configuration only uses on-demand nodes
func (w *WorkloadConfiguration) IsOnDemandOnly() bool {
	return w.SpotPercentage == 0
}

// ParseFromAnnotations creates a WorkloadConfiguration from Kubernetes annotations.
// Enablement is NOT derived from annotations anymore; it must be set via label externally.
func ParseFromAnnotations(annotations map[string]string, enabled bool) (*WorkloadConfiguration, error) {
	config := &WorkloadConfiguration{Enabled: enabled}

	if !config.Enabled {
		return config, nil
	}

	if annotations == nil {
		return config, nil
	}

	// Parse minOnDemand
	if minOnDemand, exists := annotations["spotalis.io/min-on-demand"]; exists {
		parsed, err := strconv.ParseInt(minOnDemand, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid spotalis.io/min-on-demand value: %v", err)
		}
		config.MinOnDemand = int32(parsed)
		config.Policy.MinOnDemand = &config.MinOnDemand
	}

	// Parse spotPercentage
	if spotPercentage, exists := annotations["spotalis.io/spot-percentage"]; exists {
		percentageStr := strings.TrimSuffix(spotPercentage, "%")
		parsed, err := strconv.ParseInt(percentageStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid spotalis.io/spot-percentage value: %v", err)
		}
		config.SpotPercentage = int32(parsed)
		config.Policy.SpotPercentage = &config.SpotPercentage
	}

	if err := ValidateReplicaAllocationPolicy(0, config.AllocationPolicy()); err != nil {
		return nil, err
	}

	return config, nil
}

// ToAnnotations converts the WorkloadConfiguration back to annotations
func (w *WorkloadConfiguration) ToAnnotations() map[string]string {
	annotations := make(map[string]string)
	if !w.Enabled {
		return annotations
	}
	// Parsed policies retain omission. Programmatic configurations without
	// Policy preserve the legacy behavior of writing both resolved fields.
	if w.Policy.MinOnDemand != nil || (w.Policy.MinOnDemand == nil && w.Policy.SpotPercentage == nil) {
		annotations["spotalis.io/min-on-demand"] = strconv.FormatInt(int64(w.MinOnDemand), 10)
	}
	if w.Policy.SpotPercentage != nil || (w.Policy.MinOnDemand == nil && w.Policy.SpotPercentage == nil) {
		annotations["spotalis.io/spot-percentage"] = strconv.FormatInt(int64(w.SpotPercentage), 10) + "%"
	}
	return annotations
}
