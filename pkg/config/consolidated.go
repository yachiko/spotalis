package config

import (
	"fmt"
	"time"
)

// SpotalisConfig is the root configuration structure for the entire Spotalis operator
type SpotalisConfig struct {
	// Operator contains the core operator configuration
	Operator OperatorConfig `yaml:"operator" json:"operator"`

	// Controllers contains controller-specific configuration
	Controllers ControllerConfig `yaml:"controllers" json:"controllers"`

	// Webhook contains webhook server configuration
	Webhook WebhookConfig `yaml:"webhook" json:"webhook"`

	// Observability contains metrics, logging, and health check configuration
	Observability ObservabilityConfig `yaml:"observability" json:"observability"`
}

// OperatorConfig contains core operator configuration
type OperatorConfig struct {
	// Namespace is the namespace where the operator is deployed
	Namespace string `yaml:"namespace" json:"namespace"`

	// ReadOnlyMode when true, prevents the operator from making changes
	ReadOnlyMode bool `yaml:"readOnlyMode" json:"readOnlyMode"`

	// LeaderElection configuration
	LeaderElection LeaderElectionConfig `yaml:"leaderElection" json:"leaderElection"`
}

// LeaderElectionConfig contains leader election configuration
type LeaderElectionConfig struct {
	// Enabled enables/disables leader election
	Enabled bool `yaml:"enabled" json:"enabled"`

	// ID is the leader election ID
	ID string `yaml:"id" json:"id"`

	// LeaseDuration is the duration of the leader election lease
	LeaseDuration time.Duration `yaml:"leaseDuration" json:"leaseDuration"`
}

// ControllerConfig contains controller-specific configuration
type ControllerConfig struct {
	// MaxConcurrentReconciles is the maximum number of concurrent reconciles
	MaxConcurrentReconciles int `yaml:"maxConcurrentReconciles" json:"maxConcurrentReconciles"`

	// ReconcileInterval is the interval between reconciliations
	ReconcileInterval time.Duration `yaml:"reconcileInterval" json:"reconcileInterval"`
}

// WebhookConfig contains webhook server configuration
type WebhookConfig struct {
	// Enabled enables/disables the webhook server
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Port is the port for the webhook server
	Port int `yaml:"port" json:"port"`

	// CertDir is the directory containing TLS certificates
	CertDir string `yaml:"certDir" json:"certDir"`
}

// ObservabilityConfig contains metrics, logging, and health check configuration
type ObservabilityConfig struct {
	// Metrics configuration
	Metrics MetricsConfig `yaml:"metrics" json:"metrics"`

	// Logging configuration
	Logging LoggingConfig `yaml:"logging" json:"logging"`

	// Health checks configuration
	Health HealthConfig `yaml:"health" json:"health"`
}

// MetricsConfig contains metrics configuration
type MetricsConfig struct {
	// Enabled enables/disables metrics collection
	Enabled bool `yaml:"enabled" json:"enabled"`

	// BindAddress is the address to bind the metrics server
	BindAddress string `yaml:"bindAddress" json:"bindAddress"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	// Level is the log level (debug, info, warn, error)
	Level string `yaml:"level" json:"level"`

	// Format is the log format (json, console)
	Format string `yaml:"format" json:"format"`
}

// HealthConfig contains health check configuration
type HealthConfig struct {
	// Enabled enables/disables health checks
	Enabled bool `yaml:"enabled" json:"enabled"`

	// BindAddress is the address to bind the health server
	BindAddress string `yaml:"bindAddress" json:"bindAddress"`
}

// DefaultConfig returns the default Spotalis configuration
func DefaultConfig() *SpotalisConfig {
	return &SpotalisConfig{
		Operator: OperatorConfig{
			Namespace:    "spotalis-system",
			ReadOnlyMode: false,
			LeaderElection: LeaderElectionConfig{
				Enabled:       true,
				ID:            "spotalis-controller-leader",
				LeaseDuration: 15 * time.Second,
			},
		},
		Controllers: ControllerConfig{
			MaxConcurrentReconciles: 1,
			ReconcileInterval:       5 * time.Minute,
		},
		Webhook: WebhookConfig{
			Enabled: true,
			Port:    9443,
			CertDir: "/tmp/k8s-webhook-server/serving-certs",
		},
		Observability: ObservabilityConfig{
			Metrics: MetricsConfig{
				Enabled:     true,
				BindAddress: ":8080",
			},
			Logging: LoggingConfig{
				Level:  "info",
				Format: "json",
			},
			Health: HealthConfig{
				Enabled:     true,
				BindAddress: ":8081",
			},
		},
	}
}

// Validate validates the configuration
func (c *SpotalisConfig) Validate() error {
	if c.Operator.Namespace == "" {
		return fmt.Errorf("operator.namespace cannot be empty")
	}

	if c.Controllers.MaxConcurrentReconciles <= 0 {
		return fmt.Errorf("controllers.maxConcurrentReconciles must be positive")
	}

	if c.Webhook.Enabled && c.Webhook.Port <= 0 {
		return fmt.Errorf("webhook.port must be positive when webhook is enabled")
	}

	return nil
}
