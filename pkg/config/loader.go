package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Loader handles loading configuration from various sources
type Loader struct {
	// ConfigFile is the path to the YAML configuration file
	ConfigFile string
	// EnvPrefix is the prefix for environment variables (defaults to "SPOTALIS")
	EnvPrefix string
}

// NewLoader creates a new configuration loader
func NewLoader() *Loader {
	return &Loader{
		EnvPrefix: "SPOTALIS",
	}
}

// WithConfigFile sets the configuration file path
func (l *Loader) WithConfigFile(path string) *Loader {
	l.ConfigFile = path
	return l
}

// WithEnvPrefix sets the environment variable prefix
func (l *Loader) WithEnvPrefix(prefix string) *Loader {
	l.EnvPrefix = prefix
	return l
}

// Load loads configuration from all sources in priority order:
// 1. Default configuration
// 2. Configuration file (if specified)
// 3. Environment variables
func (l *Loader) Load() (*SpotalisConfig, error) {
	config := DefaultConfig()

	// Load from file if specified
	if l.ConfigFile != "" {
		if err := l.loadFromFile(config); err != nil {
			return nil, fmt.Errorf("failed to load config from file: %w", err)
		}
	}

	// Override with environment variables
	l.loadFromEnv(config)

	// Validate the final configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// loadFromFile loads configuration from a YAML file
func (l *Loader) loadFromFile(config *SpotalisConfig) error {
	data, err := os.ReadFile(l.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", l.ConfigFile, err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse YAML config file: %w", err)
	}

	return nil
}

// loadFromEnv loads configuration from environment variables
func (l *Loader) loadFromEnv(config *SpotalisConfig) {
	// Operator configuration
	if val := l.getEnv("OPERATOR_NAMESPACE"); val != "" {
		config.Operator.Namespace = val
	}
	if val := l.getEnv("OPERATOR_READ_ONLY_MODE"); val != "" {
		config.Operator.ReadOnlyMode = l.parseBool(val, config.Operator.ReadOnlyMode)
	}

	// Leader election configuration
	if val := l.getEnv("LEADER_ELECTION_ENABLED"); val != "" {
		config.Operator.LeaderElection.Enabled = l.parseBool(val, config.Operator.LeaderElection.Enabled)
	}
	if val := l.getEnv("LEADER_ELECTION_ID"); val != "" {
		config.Operator.LeaderElection.ID = val
	}
	if val := l.getEnv("LEADER_ELECTION_LEASE_DURATION"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Operator.LeaderElection.LeaseDuration = duration
		}
	}

	// Controller configuration
	if val := l.getEnv("CONTROLLERS_MAX_CONCURRENT_RECONCILES"); val != "" {
		config.Controllers.MaxConcurrentReconciles = l.parseInt(val, config.Controllers.MaxConcurrentReconciles)
	}
	if val := l.getEnv("CONTROLLERS_RECONCILE_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Controllers.ReconcileInterval = duration
		}
	}
	if val := l.getEnv("CONTROLLERS_WORKLOAD_COOLDOWN_PERIOD"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Controllers.WorkloadTiming.CooldownPeriod = duration
		}
	}
	if val := l.getEnv("CONTROLLERS_WORKLOAD_DISRUPTION_RETRY_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Controllers.WorkloadTiming.DisruptionRetryInterval = duration
		}
	}
	if val := l.getEnv("CONTROLLERS_WORKLOAD_DISRUPTION_WINDOW_POLL_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Controllers.WorkloadTiming.DisruptionWindowPollInterval = duration
		}
	}

	// Webhook configuration
	if val := l.getEnv("WEBHOOK_ENABLED"); val != "" {
		config.Webhook.Enabled = l.parseBool(val, config.Webhook.Enabled)
	}
	if val := l.getEnv("WEBHOOK_PORT"); val != "" {
		config.Webhook.Port = l.parseInt(val, config.Webhook.Port)
	}
	if val := l.getEnv("WEBHOOK_CERT_DIR"); val != "" {
		config.Webhook.CertDir = val
	}

	// Metrics configuration
	if val := l.getEnv("METRICS_ENABLED"); val != "" {
		config.Observability.Metrics.Enabled = l.parseBool(val, config.Observability.Metrics.Enabled)
	}
	if val := l.getEnv("METRICS_BIND_ADDRESS"); val != "" {
		config.Observability.Metrics.BindAddress = val
	}

	// Logging configuration
	if val := l.getEnv("LOGGING_LEVEL"); val != "" {
		config.Observability.Logging.Level = val
	}
	if val := l.getEnv("LOGGING_FORMAT"); val != "" {
		config.Observability.Logging.Format = val
	}
	if val := l.getEnv("LOGGING_OUTPUT"); val != "" {
		config.Observability.Logging.Output = val
	}
	if val := l.getEnv("LOGGING_ADDCALLER"); val != "" {
		config.Observability.Logging.AddCaller = l.parseBool(val, config.Observability.Logging.AddCaller)
	}
	if val := l.getEnv("LOGGING_DEVELOPMENT"); val != "" {
		config.Observability.Logging.Development = l.parseBool(val, config.Observability.Logging.Development)
	}

	// Health configuration
	if val := l.getEnv("HEALTH_ENABLED"); val != "" {
		config.Observability.Health.Enabled = l.parseBool(val, config.Observability.Health.Enabled)
	}
	if val := l.getEnv("HEALTH_BIND_ADDRESS"); val != "" {
		config.Observability.Health.BindAddress = val
	}
}

// getEnv gets an environment variable with the configured prefix
func (l *Loader) getEnv(key string) string {
	return os.Getenv(l.EnvPrefix + "_" + key)
}

// parseBool parses a boolean string, returning fallback on error
func (l *Loader) parseBool(val string, fallback bool) bool {
	switch strings.ToLower(val) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

// parseInt parses an integer string, returning fallback on error
func (l *Loader) parseInt(val string, fallback int) int {
	if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return fallback
}

// Save saves the configuration to a YAML file
func (config *SpotalisConfig) Save(filename string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := os.WriteFile(filename, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadFromFile is a convenience function to load configuration from a file
func LoadFromFile(filename string) (*SpotalisConfig, error) {
	return NewLoader().WithConfigFile(filename).Load()
}

// LoadFromEnv is a convenience function to load configuration from environment variables only
func LoadFromEnv() (*SpotalisConfig, error) {
	return NewLoader().Load()
}
