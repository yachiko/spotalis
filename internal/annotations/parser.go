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

// Package annotations provides parsing and validation of Spotalis-specific
// Kubernetes annotations for workload configuration.
package annotations

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yachiko/spotalis/pkg/apis"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// EnabledLabel enables/disables Spotalis management for the workload (applied as a LABEL, not annotation)
	EnabledLabel = "spotalis.io/enabled"

	// SpotPercentageAnnotation configures the percentage of replicas on spot instances
	SpotPercentageAnnotation = "spotalis.io/spot-percentage"

	// MinOnDemandAnnotation configures minimum number of on-demand replicas
	MinOnDemandAnnotation = "spotalis.io/min-on-demand"

	// DisruptionScheduleAnnotation defines when disruptions are allowed (cron format, UTC)
	DisruptionScheduleAnnotation = "spotalis.io/disruption-schedule"

	// DisruptionDurationAnnotation defines how long the disruption window lasts
	DisruptionDurationAnnotation = "spotalis.io/disruption-duration"

	// BooleanTrue represents the string "true" for annotation values
	BooleanTrue = "true"

	// BooleanFalse represents the string "false" for annotation values
	BooleanFalse = "false"
)

// AnnotationParser provides methods for parsing Spotalis annotations
type AnnotationParser struct{}

// NewAnnotationParser creates a new annotation parser
func NewAnnotationParser() *AnnotationParser {
	return &AnnotationParser{}
}

// ParseWorkloadConfiguration extracts WorkloadConfiguration from object labels and annotations
// The enabled flag MUST be set via label: spotalis.io/enabled=true
// Configuration parameters are read from annotations
func (p *AnnotationParser) ParseWorkloadConfiguration(obj metav1.Object) (*apis.WorkloadConfiguration, error) {
	config := &apis.WorkloadConfiguration{}

	// Check if Spotalis is enabled via label (REQUIRED)
	labels := obj.GetLabels()
	if labels != nil {
		if enabled, exists := labels[EnabledLabel]; exists {
			config.Enabled = enabled == BooleanTrue
		}
	}

	// Only process configuration annotations if enabled
	if !config.Enabled {
		return config, nil
	}

	// Parse configuration from annotations
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return config, nil
	}

	// Parse spot percentage
	if spotPerc, exists := annotations[SpotPercentageAnnotation]; exists {
		// Remove % symbol if present
		percentageStr := strings.TrimSuffix(spotPerc, "%")
		percentage, err := strconv.Atoi(percentageStr)
		if err != nil {
			return nil, fmt.Errorf("invalid spot-percentage annotation: %w", err)
		}
		if percentage < 0 || percentage > int(^uint32(0)) {
			return nil, fmt.Errorf("spot-percentage annotation out of valid range: %d", percentage)
		}
		config.SpotPercentage = int32(percentage) // #nosec G109,G115 - Bounds checked above
	}

	// Parse minimum on-demand replicas
	if minOnDemand, exists := annotations[MinOnDemandAnnotation]; exists {
		count, err := strconv.Atoi(minOnDemand)
		if err != nil {
			return nil, fmt.Errorf("invalid min-on-demand annotation: %w", err)
		}
		if count < 0 || count > int(^uint32(0)) {
			return nil, fmt.Errorf("min-on-demand annotation out of valid range: %d", count)
		}
		config.MinOnDemand = int32(count) // #nosec G109,G115 - Bounds checked above
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
	}

	for _, annotation := range spotalisAnnotations {
		if _, exists := annotations[annotation]; exists {
			return true
		}
	}

	return false
}

// IsSpotalisEnabled checks if Spotalis is enabled for the object
// Checks for label spotalis.io/enabled=true (REQUIRED)
// If label exists and is "true", Spotalis is enabled
// If label doesn't exist or is not "true", Spotalis is disabled
func (p *AnnotationParser) IsSpotalisEnabled(obj metav1.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}

	enabled, exists := labels[EnabledLabel]
	if !exists {
		return false
	}

	return strings.ToLower(enabled) == BooleanTrue
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

// validateSpotPercentage validates spot percentage annotation value

// ValidateAnnotations validates that all Spotalis annotations have valid values
func (p *AnnotationParser) ValidateAnnotations(obj metav1.Object) []error {
	var errors []error
	annotations := obj.GetAnnotations()

	if annotations == nil {
		return errors
	}

	// Validate enabled annotation if present
	// No validation for enablement annotation since enablement is label-only

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

	return errors
}

// validateSpotPercentage validates spot percentage annotation value
func (p *AnnotationParser) validateSpotPercentage(value string) error {
	// Remove % symbol if present
	percentageStr := strings.TrimSuffix(value, "%")
	percentage, err := strconv.Atoi(percentageStr)
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
