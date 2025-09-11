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

package annotations

import (
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ahoma/spotalis/pkg/apis"
)

const (
	// SpotPercentageAnnotation configures the percentage of replicas on spot instances
	SpotPercentageAnnotation = "spotalis.io/spot-percentage"

	// MinOnDemandAnnotation configures minimum number of on-demand replicas
	MinOnDemandAnnotation = "spotalis.io/min-on-demand"

	// MaxSurgeAnnotation configures maximum surge during rolling updates
	MaxSurgeAnnotation = "spotalis.io/max-surge"

	// MaxUnavailableAnnotation configures maximum unavailable during rolling updates
	MaxUnavailableAnnotation = "spotalis.io/max-unavailable"

	// DisruptionBudgetAnnotation configures pod disruption budget
	DisruptionBudgetAnnotation = "spotalis.io/disruption-budget"

	// DisruptionWindowAnnotation configures allowed disruption time windows
	DisruptionWindowAnnotation = "spotalis.io/disruption-window"

	// NodeAffinityAnnotation configures node affinity preferences
	NodeAffinityAnnotation = "spotalis.io/node-affinity"

	// TolerationAnnotation configures tolerations for scheduling
	TolerationAnnotation = "spotalis.io/tolerations"

	// MonitoringEnabledAnnotation enables/disables monitoring for the workload
	MonitoringEnabledAnnotation = "spotalis.io/monitoring-enabled"

	// ReconcileIntervalAnnotation configures custom reconciliation interval
	ReconcileIntervalAnnotation = "spotalis.io/reconcile-interval"
)

// AnnotationParser provides methods for parsing Spotalis annotations
type AnnotationParser struct{}

// NewAnnotationParser creates a new annotation parser
func NewAnnotationParser() *AnnotationParser {
	return &AnnotationParser{}
}

// ParseWorkloadConfiguration extracts WorkloadConfiguration from object annotations
func (p *AnnotationParser) ParseWorkloadConfiguration(obj metav1.Object) (*apis.WorkloadConfiguration, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil, fmt.Errorf("no annotations found on object")
	}

	config := &apis.WorkloadConfiguration{}

	// Parse spot percentage
	if spotPerc, exists := annotations[SpotPercentageAnnotation]; exists {
		percentage, err := strconv.Atoi(spotPerc)
		if err != nil {
			return nil, fmt.Errorf("invalid spot-percentage annotation: %w", err)
		}
		config.SpotPercentage = int32(percentage)
	}

	// Parse minimum on-demand replicas
	if minOnDemand, exists := annotations[MinOnDemandAnnotation]; exists {
		count, err := strconv.Atoi(minOnDemand)
		if err != nil {
			return nil, fmt.Errorf("invalid min-on-demand annotation: %w", err)
		}
		config.MinOnDemand = int32(count)
	}

	// Parse enabled flag (default true if any Spotalis annotations exist)
	config.Enabled = true

	// Parse replica strategy
	if strategy, exists := annotations["spotalis.io/replica-strategy"]; exists {
		config.ReplicaStrategy = strategy
	}

	// Parse scaling policy
	if policy, exists := annotations["spotalis.io/scaling-policy"]; exists {
		config.ScalingPolicy = policy
	}

	// Parse max replicas
	if maxReplicas, exists := annotations["spotalis.io/max-replicas"]; exists {
		count, err := strconv.Atoi(maxReplicas)
		if err != nil {
			return nil, fmt.Errorf("invalid max-replicas annotation: %w", err)
		}
		config.MaxReplicas = int32(count)
	}

	return config, nil
}

// HasSpotalisAnnotations checks if the object has any Spotalis annotations
func (p *AnnotationParser) HasSpotalisAnnotations(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	spotalisAnnotations := []string{
		SpotPercentageAnnotation,
		MinOnDemandAnnotation,
		"spotalis.io/replica-strategy",
		"spotalis.io/scaling-policy",
		"spotalis.io/max-replicas",
	}

	for _, annotation := range spotalisAnnotations {
		if _, exists := annotations[annotation]; exists {
			return true
		}
	}

	return false
}

// GetAnnotationValue safely retrieves an annotation value
func (p *AnnotationParser) GetAnnotationValue(obj metav1.Object, key string) (string, bool) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return "", false
	}

	value, exists := annotations[key]
	return value, exists
}

// SetAnnotationValue safely sets an annotation value
func (p *AnnotationParser) SetAnnotationValue(obj metav1.Object, key, value string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[key] = value
	obj.SetAnnotations(annotations)
}

// RemoveAnnotation safely removes an annotation
func (p *AnnotationParser) RemoveAnnotation(obj metav1.Object, key string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return
	}

	delete(annotations, key)
	obj.SetAnnotations(annotations)
}

// parseIntOrPercentage parses a value that can be either an integer or percentage
func (p *AnnotationParser) parseIntOrPercentage(value string) (interface{}, error) {
	if strings.HasSuffix(value, "%") {
		percentStr := strings.TrimSuffix(value, "%")
		percent, err := strconv.Atoi(percentStr)
		if err != nil {
			return nil, fmt.Errorf("invalid percentage value: %s", value)
		}
		if percent < 0 || percent > 100 {
			return nil, fmt.Errorf("percentage must be between 0 and 100: %s", value)
		}
		return fmt.Sprintf("%d%%", percent), nil
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("invalid integer value: %s", value)
	}
	if intValue < 0 {
		return nil, fmt.Errorf("value must be non-negative: %s", value)
	}

	return intValue, nil
}

// ValidateAnnotations validates that all Spotalis annotations have valid values
func (p *AnnotationParser) ValidateAnnotations(obj metav1.Object) []error {
	var errors []error
	annotations := obj.GetAnnotations()

	if annotations == nil {
		return errors
	}

	// Validate each annotation if present
	if value, exists := annotations[SpotPercentageAnnotation]; exists {
		if err := p.validateSpotPercentage(value); err != nil {
			errors = append(errors, fmt.Errorf("invalid %s: %w", SpotPercentageAnnotation, err))
		}
	}

	if value, exists := annotations[MinOnDemandAnnotation]; exists {
		if err := p.validateMinOnDemand(value); err != nil {
			errors = append(errors, fmt.Errorf("invalid %s: %w", MinOnDemandAnnotation, err))
		}
	}

	if value, exists := annotations["spotalis.io/max-replicas"]; exists {
		if err := p.validateMinOnDemand(value); err != nil { // Same validation as min-on-demand
			errors = append(errors, fmt.Errorf("invalid spotalis.io/max-replicas: %w", err))
		}
	}

	return errors
}

// validateSpotPercentage validates spot percentage annotation value
func (p *AnnotationParser) validateSpotPercentage(value string) error {
	percentage, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("must be an integer: %w", err)
	}

	if percentage < 0 || percentage > 100 {
		return fmt.Errorf("must be between 0 and 100, got %d", percentage)
	}

	return nil
}

// validateMinOnDemand validates minimum on-demand annotation value
func (p *AnnotationParser) validateMinOnDemand(value string) error {
	count, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("must be an integer: %w", err)
	}

	if count < 0 {
		return fmt.Errorf("must be non-negative, got %d", count)
	}

	return nil
}
