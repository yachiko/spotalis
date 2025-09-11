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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ControllerConfiguration represents the global controller configuration
// including rate limiting and multi-tenancy settings
type ControllerConfiguration struct {
	// ResyncPeriod is the controller reconciliation interval (default 30s, min 5s)
	ResyncPeriod time.Duration `json:"resyncPeriod"`

	// Workers is the number of concurrent worker goroutines
	Workers int `json:"workers"`

	// NamespaceSelector is optional namespace filtering for multi-tenant clusters
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// LeaderElection contains leader election settings
	LeaderElection LeaderElectionConfig `json:"leaderElection"`

	// NodeLabels contains node classification label configuration
	NodeLabels NodeLabelConfig `json:"nodeLabels"`

	// MetricsAddr is the address to bind the metrics endpoint
	MetricsAddr string `json:"metricsAddr,omitempty"`

	// HealthAddr is the address to bind the health endpoint
	HealthAddr string `json:"healthAddr,omitempty"`

	// WebhookPort is the port for the admission webhook
	WebhookPort int `json:"webhookPort,omitempty"`

	// MaxConcurrentReconciles limits concurrent reconciliation operations
	MaxConcurrentReconciles int `json:"maxConcurrentReconciles,omitempty"`

	// DryRun enables dry-run mode where no actual changes are made
	DryRun bool `json:"dryRun,omitempty"`

	// FeatureGates enables/disables experimental features
	FeatureGates map[string]bool `json:"featureGates,omitempty"`
}

// LeaderElectionConfig contains leader election configuration
type LeaderElectionConfig struct {
	// Enabled indicates whether leader election is enabled
	Enabled bool `json:"enabled"`

	// LeaseDuration is how long the lease is valid
	LeaseDuration time.Duration `json:"leaseDuration,omitempty"`

	// RenewDeadline is when the lease must be renewed by
	RenewDeadline time.Duration `json:"renewDeadline,omitempty"`

	// RetryPeriod is how often to attempt lease renewal
	RetryPeriod time.Duration `json:"retryPeriod,omitempty"`

	// Namespace is the namespace for the leader election lease
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the leader election lease
	Name string `json:"name,omitempty"`
}

// NodeLabelConfig contains node classification configuration
type NodeLabelConfig struct {
	// SpotLabels are labels that identify spot nodes
	SpotLabels map[string]string `json:"spotLabels,omitempty"`

	// OnDemandLabels are labels that identify on-demand nodes
	OnDemandLabels map[string]string `json:"onDemandLabels,omitempty"`

	// RefreshInterval is how often to refresh node classifications
	RefreshInterval time.Duration `json:"refreshInterval,omitempty"`
}

// DefaultControllerConfiguration returns a configuration with sensible defaults
func DefaultControllerConfiguration() *ControllerConfiguration {
	return &ControllerConfiguration{
		ResyncPeriod: 30 * time.Second,
		Workers:      5,
		LeaderElection: LeaderElectionConfig{
			Enabled:       true,
			LeaseDuration: 15 * time.Second,
			RenewDeadline: 10 * time.Second,
			RetryPeriod:   2 * time.Second,
			Namespace:     "kube-system",
			Name:          "spotalis-leader-election",
		},
		NodeLabels: NodeLabelConfig{
			SpotLabels: map[string]string{
				"karpenter.sh/capacity-type": "spot",
			},
			OnDemandLabels: map[string]string{
				"karpenter.sh/capacity-type": "on-demand",
			},
			RefreshInterval: 5 * time.Minute,
		},
		MetricsAddr:             ":8080",
		HealthAddr:              ":8081",
		WebhookPort:             9443,
		MaxConcurrentReconciles: 10,
		DryRun:                  false,
		FeatureGates:            make(map[string]bool),
	}
}

// Validate checks if the configuration is valid
func (c *ControllerConfiguration) Validate() error {
	// Validate ResyncPeriod
	if c.ResyncPeriod < 5*time.Second {
		return fmt.Errorf("resyncPeriod must be >= 5s to prevent API server overload, got %v", c.ResyncPeriod)
	}

	// Validate Workers
	if c.Workers <= 0 {
		return fmt.Errorf("workers must be > 0, got %d", c.Workers)
	}
	if c.Workers > 50 {
		return fmt.Errorf("workers should be <= 50 for reasonable resource usage, got %d", c.Workers)
	}

	// Validate MaxConcurrentReconciles
	if c.MaxConcurrentReconciles <= 0 {
		c.MaxConcurrentReconciles = c.Workers * 2 // Default to 2x workers
	}

	// Validate leader election config
	if err := c.LeaderElection.Validate(); err != nil {
		return fmt.Errorf("leader election config invalid: %v", err)
	}

	// Validate node labels config
	if err := c.NodeLabels.Validate(); err != nil {
		return fmt.Errorf("node labels config invalid: %v", err)
	}

	return nil
}

// Validate checks if the leader election configuration is valid
func (l *LeaderElectionConfig) Validate() error {
	if !l.Enabled {
		return nil // Skip validation if disabled
	}

	if l.LeaseDuration <= 0 {
		return fmt.Errorf("leaseDuration must be > 0")
	}

	if l.RenewDeadline <= 0 {
		return fmt.Errorf("renewDeadline must be > 0")
	}

	if l.RetryPeriod <= 0 {
		return fmt.Errorf("retryPeriod must be > 0")
	}

	if l.RenewDeadline >= l.LeaseDuration {
		return fmt.Errorf("renewDeadline (%v) must be < leaseDuration (%v)", l.RenewDeadline, l.LeaseDuration)
	}

	if l.RetryPeriod >= l.RenewDeadline {
		return fmt.Errorf("retryPeriod (%v) must be < renewDeadline (%v)", l.RetryPeriod, l.RenewDeadline)
	}

	if l.Namespace == "" {
		return fmt.Errorf("namespace must be specified")
	}

	if l.Name == "" {
		return fmt.Errorf("name must be specified")
	}

	return nil
}

// Validate checks if the node labels configuration is valid
func (n *NodeLabelConfig) Validate() error {
	if n.RefreshInterval <= 0 {
		n.RefreshInterval = 5 * time.Minute // Set default
	}

	if len(n.SpotLabels) == 0 && len(n.OnDemandLabels) == 0 {
		return fmt.Errorf("at least one of spotLabels or onDemandLabels must be specified")
	}

	return nil
}

// ShouldMonitorNamespace determines if a namespace should be monitored
// based on the namespace selector configuration
func (c *ControllerConfiguration) ShouldMonitorNamespace(ns *corev1.Namespace) bool {
	if c.NamespaceSelector == nil {
		return true // Monitor all namespaces if no selector
	}

	selector, err := metav1.LabelSelectorAsSelector(c.NamespaceSelector)
	if err != nil {
		return false // Invalid selector, exclude namespace
	}

	return selector.Matches(labels.Set(ns.Labels))
}

// GetNamespaceSelector returns a label selector for monitoring namespaces
func (c *ControllerConfiguration) GetNamespaceSelector() (labels.Selector, error) {
	if c.NamespaceSelector == nil {
		return labels.Everything(), nil
	}

	return metav1.LabelSelectorAsSelector(c.NamespaceSelector)
}

// IsFeatureEnabled checks if a feature gate is enabled
func (c *ControllerConfiguration) IsFeatureEnabled(feature string) bool {
	if c.FeatureGates == nil {
		return false
	}
	return c.FeatureGates[feature]
}

// EnableFeature enables a feature gate
func (c *ControllerConfiguration) EnableFeature(feature string) {
	if c.FeatureGates == nil {
		c.FeatureGates = make(map[string]bool)
	}
	c.FeatureGates[feature] = true
}

// DisableFeature disables a feature gate
func (c *ControllerConfiguration) DisableFeature(feature string) {
	if c.FeatureGates == nil {
		c.FeatureGates = make(map[string]bool)
	}
	c.FeatureGates[feature] = false
}

// GetEffectiveResyncPeriod returns the resync period, ensuring it meets minimum requirements
func (c *ControllerConfiguration) GetEffectiveResyncPeriod() time.Duration {
	if c.ResyncPeriod < 5*time.Second {
		return 5 * time.Second
	}
	return c.ResyncPeriod
}

// GetEffectiveWorkers returns the number of workers, ensuring it's within reasonable bounds
func (c *ControllerConfiguration) GetEffectiveWorkers() int {
	if c.Workers <= 0 {
		return 5 // Default
	}
	if c.Workers > 50 {
		return 50 // Maximum
	}
	return c.Workers
}

// Clone creates a deep copy of the configuration
func (c *ControllerConfiguration) Clone() *ControllerConfiguration {
	clone := *c

	// Deep copy NamespaceSelector
	if c.NamespaceSelector != nil {
		clone.NamespaceSelector = c.NamespaceSelector.DeepCopy()
	}

	// Deep copy maps
	if c.NodeLabels.SpotLabels != nil {
		clone.NodeLabels.SpotLabels = make(map[string]string)
		for k, v := range c.NodeLabels.SpotLabels {
			clone.NodeLabels.SpotLabels[k] = v
		}
	}

	if c.NodeLabels.OnDemandLabels != nil {
		clone.NodeLabels.OnDemandLabels = make(map[string]string)
		for k, v := range c.NodeLabels.OnDemandLabels {
			clone.NodeLabels.OnDemandLabels[k] = v
		}
	}

	if c.FeatureGates != nil {
		clone.FeatureGates = make(map[string]bool)
		for k, v := range c.FeatureGates {
			clone.FeatureGates[k] = v
		}
	}

	return &clone
}

// GetConfigSummary returns a summary of key configuration values
func (c *ControllerConfiguration) GetConfigSummary() ConfigSummary {
	return ConfigSummary{
		ResyncPeriod:            c.ResyncPeriod,
		Workers:                 c.Workers,
		MaxConcurrentReconciles: c.MaxConcurrentReconciles,
		LeaderElectionEnabled:   c.LeaderElection.Enabled,
		DryRun:                  c.DryRun,
		HasNamespaceSelector:    c.NamespaceSelector != nil,
		MetricsAddr:             c.MetricsAddr,
		HealthAddr:              c.HealthAddr,
		WebhookPort:             c.WebhookPort,
	}
}

// ConfigSummary provides a summary of the controller configuration
type ConfigSummary struct {
	ResyncPeriod            time.Duration `json:"resyncPeriod"`
	Workers                 int           `json:"workers"`
	MaxConcurrentReconciles int           `json:"maxConcurrentReconciles"`
	LeaderElectionEnabled   bool          `json:"leaderElectionEnabled"`
	DryRun                  bool          `json:"dryRun"`
	HasNamespaceSelector    bool          `json:"hasNamespaceSelector"`
	MetricsAddr             string        `json:"metricsAddr"`
	HealthAddr              string        `json:"healthAddr"`
	WebhookPort             int           `json:"webhookPort"`
}
