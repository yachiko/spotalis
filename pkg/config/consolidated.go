// Package config provides consolidated configuration structures and defaults for the Spotalis operator.
package config

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// DisruptionWindow defines when pod disruptions (rebalancing) are allowed
	DisruptionWindow DisruptionWindowConfig `yaml:"disruptionWindow" json:"disruptionWindow"`
}

// DisruptionWindowConfig defines time windows when pod disruptions are allowed
type DisruptionWindowConfig struct {
	// Schedule is a 5-field cron expression in UTC (e.g., "0 2 * * *" for daily at 2 AM)
	Schedule string `yaml:"schedule" json:"schedule"`

	// Duration is how long the window lasts (e.g., "4h", "30m", "2h30m")
	Duration string `yaml:"duration" json:"duration"`
}

// Validate validates the disruption window configuration
func (c *DisruptionWindowConfig) Validate() error {
	// Empty schedule and duration means no window configured (always allowed)
	if c.Schedule == "" && c.Duration == "" {
		return nil
	}

	// Both must be present or both absent
	if c.Schedule == "" || c.Duration == "" {
		return fmt.Errorf("both schedule and duration must be specified together")
	}

	// Validate cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(c.Schedule); err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", c.Schedule, err)
	}

	// Validate duration
	if _, err := time.ParseDuration(c.Duration); err != nil {
		return fmt.Errorf("invalid duration %q: %w", c.Duration, err)
	}

	return nil
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

	// NodeClassifier contains node classification configuration
	NodeClassifier NodeClassifierConfig `yaml:"nodeClassifier" json:"nodeClassifier"`

	// WorkloadTiming contains shared timing controls applied to all workload reconcilers
	WorkloadTiming WorkloadTimingConfig `yaml:"workloadTiming" json:"workloadTiming"`
}

// WorkloadTimingConfig contains configurable timing knobs for workload controllers
type WorkloadTimingConfig struct {
	// CooldownPeriod is the minimum duration to wait after deleting a pod before rebalancing again
	CooldownPeriod time.Duration `yaml:"cooldownPeriod" json:"cooldownPeriod"`

	// DisruptionRetryInterval is how long to wait before retrying when disruption window resolution fails
	DisruptionRetryInterval time.Duration `yaml:"disruptionRetryInterval" json:"disruptionRetryInterval"`

	// DisruptionWindowPollInterval is how long to wait before checking the disruption window again when outside of it
	DisruptionWindowPollInterval time.Duration `yaml:"disruptionWindowPollInterval" json:"disruptionWindowPollInterval"`
}

func validateWorkloadTimingConfig(name string, cfg *WorkloadTimingConfig) error {
	if cfg.CooldownPeriod <= 0 {
		return fmt.Errorf("%s.cooldownPeriod must be positive", name)
	}
	if cfg.DisruptionRetryInterval <= 0 {
		return fmt.Errorf("%s.disruptionRetryInterval must be positive", name)
	}
	if cfg.DisruptionWindowPollInterval <= 0 {
		return fmt.Errorf("%s.disruptionWindowPollInterval must be positive", name)
	}
	return nil
}

// NodeClassifierConfig contains node classification configuration
type NodeClassifierConfig struct {
	// SpotLabels are label selectors that identify spot nodes
	// Default: karpenter.sh/capacity-type=spot
	SpotLabels []metav1.LabelSelector `yaml:"spotLabels" json:"spotLabels"`

	// OnDemandLabels are label selectors that identify on-demand nodes
	// Default: karpenter.sh/capacity-type=on-demand
	OnDemandLabels []metav1.LabelSelector `yaml:"onDemandLabels" json:"onDemandLabels"`
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

	// Output is the output destination (stdout, stderr, file path)
	Output string `yaml:"output" json:"output"`

	// AddCaller adds caller information to logs
	AddCaller bool `yaml:"addCaller" json:"addCaller"`

	// Development enables development mode (pretty printing, etc.)
	Development bool `yaml:"development" json:"development"`
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
			NodeClassifier: NodeClassifierConfig{
				SpotLabels: []metav1.LabelSelector{
					{
						MatchLabels: map[string]string{
							"karpenter.sh/capacity-type": "spot",
						},
					},
				},
				OnDemandLabels: []metav1.LabelSelector{
					{
						MatchLabels: map[string]string{
							"karpenter.sh/capacity-type": "on-demand",
						},
					},
				},
			},
			WorkloadTiming: WorkloadTimingConfig{
				CooldownPeriod:               10 * time.Second,
				DisruptionRetryInterval:      1 * time.Minute,
				DisruptionWindowPollInterval: 10 * time.Minute,
			},
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
				Level:       "info",
				Format:      "json",
				Output:      "stdout",
				AddCaller:   true,
				Development: false,
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

	// Validate disruption window configuration
	if err := c.Operator.DisruptionWindow.Validate(); err != nil {
		return fmt.Errorf("operator.disruptionWindow: %w", err)
	}

	if err := validateWorkloadTimingConfig("controllers.workloadTiming", &c.Controllers.WorkloadTiming); err != nil {
		return err
	}

	return nil
}
