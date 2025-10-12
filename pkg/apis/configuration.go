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
}

// Validate checks if the WorkloadConfiguration is valid according to business rules
func (w *WorkloadConfiguration) Validate(totalReplicas int32) error {
	if !w.Enabled {
		return nil // Skip validation for disabled workloads
	}

	if w.MinOnDemand < 0 {
		return fmt.Errorf("minOnDemand must be >= 0, got %d", w.MinOnDemand)
	}

	if w.MinOnDemand > totalReplicas {
		return fmt.Errorf("minOnDemand (%d) cannot exceed total replicas (%d)", w.MinOnDemand, totalReplicas)
	}

	if w.SpotPercentage < 0 || w.SpotPercentage > 100 {
		return fmt.Errorf("spotPercentage must be 0-100, got %d", w.SpotPercentage)
	}

	if w.MinOnDemand == 0 && w.SpotPercentage == 0 {
		return fmt.Errorf("at least one of minOnDemand or spotPercentage must be specified when enabled")
	}

	return nil
}

// IsSpotOptimized returns true if this configuration prefers spot nodes
func (w *WorkloadConfiguration) IsSpotOptimized() bool {
	return w.SpotPercentage > 50
}

// IsOnDemandOnly returns true if this configuration only uses on-demand nodes
func (w *WorkloadConfiguration) IsOnDemandOnly() bool {
	return w.SpotPercentage == 0
}

// ParseFromAnnotations creates a WorkloadConfiguration from Kubernetes annotations
func ParseFromAnnotations(annotations map[string]string) (*WorkloadConfiguration, error) {
	config := &WorkloadConfiguration{}

	// Check if spotalis is enabled
	if enabled, exists := annotations["spotalis.io/enabled"]; exists {
		parsedEnabled, err := strconv.ParseBool(enabled)
		if err != nil {
			return nil, fmt.Errorf("invalid spotalis.io/enabled value: %v", err)
		}
		config.Enabled = parsedEnabled
	}

	if !config.Enabled {
		return config, nil // Return early if not enabled
	}

	// Parse minOnDemand
	if minOnDemand, exists := annotations["spotalis.io/min-on-demand"]; exists {
		parsed, err := strconv.ParseInt(minOnDemand, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid spotalis.io/min-on-demand value: %v", err)
		}
		config.MinOnDemand = int32(parsed)
	}

	// Parse spotPercentage
	if spotPercentage, exists := annotations["spotalis.io/spot-percentage"]; exists {
		// Remove '%' symbol if present
		percentageStr := strings.TrimSuffix(spotPercentage, "%")
		parsed, err := strconv.ParseInt(percentageStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid spotalis.io/spot-percentage value: %v", err)
		}
		config.SpotPercentage = int32(parsed)
	}

	return config, nil
}

// ToAnnotations converts the WorkloadConfiguration back to annotations
func (w *WorkloadConfiguration) ToAnnotations() map[string]string {
	annotations := make(map[string]string)

	annotations["spotalis.io/enabled"] = strconv.FormatBool(w.Enabled)

	if w.Enabled {
		annotations["spotalis.io/min-on-demand"] = strconv.FormatInt(int64(w.MinOnDemand), 10)
		annotations["spotalis.io/spot-percentage"] = strconv.FormatInt(int64(w.SpotPercentage), 10) + "%"
	}

	return annotations
}
