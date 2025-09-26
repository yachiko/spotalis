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
	// EnabledAnnotation enables/disables Spotalis management for the workload
	EnabledAnnotation = "spotalis.io/enabled"

	// SpotPercentageAnnotation configures the percentage of replicas on spot instances
	SpotPercentageAnnotation = "spotalis.io/spot-percentage"

	// MinOnDemandAnnotation configures minimum number of on-demand replicas
	MinOnDemandAnnotation = "spotalis.io/min-on-demand"
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

	// Check if Spotalis is enabled for this workload
	if enabled, exists := annotations[EnabledAnnotation]; exists {
		config.Enabled = enabled == "true"
	} else {
		// If no enabled annotation, default to false
		config.Enabled = false
	}

	// Only process other annotations if enabled
	if !config.Enabled {
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

	return config, nil
}

// HasSpotalisAnnotations checks if the object has any Spotalis annotations
func (p *AnnotationParser) HasSpotalisAnnotations(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	spotalisAnnotations := []string{
		EnabledAnnotation,
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
// If spotalis.io/enabled exists, it must be "true"
// If spotalis.io/enabled doesn't exist, but other Spotalis annotations exist, it's enabled by default
func (p *AnnotationParser) IsSpotalisEnabled(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	// Check if spotalis.io/enabled annotation exists
	enabled, exists := annotations[EnabledAnnotation]
	if exists {
		// If explicit enabled annotation exists, it must be "true"
		return strings.ToLower(enabled) == "true"
	}

	// If no explicit enabled annotation, check if any other Spotalis annotations exist
	// This maintains backward compatibility
	return p.HasSpotalisAnnotations(obj)
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
	if value, exists := annotations[EnabledAnnotation]; exists {
		if value != "true" && value != "false" {
			errors = append(errors, fmt.Errorf("invalid %s: must be 'true' or 'false', got '%s'", EnabledAnnotation, value))
		}
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
