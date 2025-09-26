package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Test that default config is not nil
	require.NotNil(t, config)

	// Test operator config
	assert.Equal(t, "spotalis-system", config.Operator.Namespace)
	assert.False(t, config.Operator.ReadOnlyMode)
	assert.True(t, config.Operator.LeaderElection.Enabled)
	assert.Equal(t, "spotalis-controller-leader", config.Operator.LeaderElection.ID)
	assert.Equal(t, 15*time.Second, config.Operator.LeaderElection.LeaseDuration)

	// Test controller config
	assert.Equal(t, 1, config.Controllers.MaxConcurrentReconciles)
	assert.Equal(t, 5*time.Minute, config.Controllers.ReconcileInterval)

	// Test webhook config
	assert.True(t, config.Webhook.Enabled)
	assert.Equal(t, 9443, config.Webhook.Port)
	assert.Equal(t, "/tmp/k8s-webhook-server/serving-certs", config.Webhook.CertDir)

	// Test observability config
	assert.True(t, config.Observability.Metrics.Enabled)
	assert.Equal(t, ":8080", config.Observability.Metrics.BindAddress)
	assert.Equal(t, "info", config.Observability.Logging.Level)
	assert.Equal(t, "json", config.Observability.Logging.Format)
	assert.True(t, config.Observability.Health.Enabled)
	assert.Equal(t, ":8081", config.Observability.Health.BindAddress)
}

func TestConfigValidation(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := DefaultConfig()
		err := config.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty namespace", func(t *testing.T) {
		config := DefaultConfig()
		config.Operator.Namespace = ""
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "operator.namespace cannot be empty")
	})

	t.Run("invalid max concurrent reconciles", func(t *testing.T) {
		config := DefaultConfig()
		config.Controllers.MaxConcurrentReconciles = 0
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "controllers.maxConcurrentReconciles must be positive")
	})

	t.Run("invalid webhook port when enabled", func(t *testing.T) {
		config := DefaultConfig()
		config.Webhook.Enabled = true
		config.Webhook.Port = 0
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "webhook.port must be positive when webhook is enabled")
	})

	t.Run("disabled webhook with invalid port is ok", func(t *testing.T) {
		config := DefaultConfig()
		config.Webhook.Enabled = false
		config.Webhook.Port = 0
		err := config.Validate()
		assert.NoError(t, err)
	})
}

func TestConfigStructures(t *testing.T) {
	// Test that all config structures can be created
	t.Run("SpotalisConfig", func(t *testing.T) {
		config := &SpotalisConfig{}
		assert.NotNil(t, config)
	})

	t.Run("OperatorConfig", func(t *testing.T) {
		config := &OperatorConfig{
			Namespace:    "test",
			ReadOnlyMode: true,
			LeaderElection: LeaderElectionConfig{
				Enabled:       false,
				ID:            "test-leader",
				LeaseDuration: 30 * time.Second,
			},
		}
		assert.Equal(t, "test", config.Namespace)
		assert.True(t, config.ReadOnlyMode)
		assert.False(t, config.LeaderElection.Enabled)
	})

	t.Run("ControllerConfig", func(t *testing.T) {
		config := &ControllerConfig{
			MaxConcurrentReconciles: 5,
			ReconcileInterval:       10 * time.Minute,
		}
		assert.Equal(t, 5, config.MaxConcurrentReconciles)
		assert.Equal(t, 10*time.Minute, config.ReconcileInterval)
	})

	t.Run("WebhookConfig", func(t *testing.T) {
		config := &WebhookConfig{
			Enabled: false,
			Port:    8443,
			CertDir: "/custom/certs",
		}
		assert.False(t, config.Enabled)
		assert.Equal(t, 8443, config.Port)
		assert.Equal(t, "/custom/certs", config.CertDir)
	})

	t.Run("ObservabilityConfig", func(t *testing.T) {
		config := &ObservabilityConfig{
			Metrics: MetricsConfig{
				Enabled:     false,
				BindAddress: ":9090",
			},
			Logging: LoggingConfig{
				Level:  "debug",
				Format: "console",
			},
			Health: HealthConfig{
				Enabled:     false,
				BindAddress: ":9091",
			},
		}
		assert.False(t, config.Metrics.Enabled)
		assert.Equal(t, ":9090", config.Metrics.BindAddress)
		assert.Equal(t, "debug", config.Logging.Level)
		assert.Equal(t, "console", config.Logging.Format)
		assert.False(t, config.Health.Enabled)
		assert.Equal(t, ":9091", config.Health.BindAddress)
	})
}
