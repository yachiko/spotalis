package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoader_NewLoader(t *testing.T) {
	loader := NewLoader()
	assert.Equal(t, "SPOTALIS", loader.EnvPrefix)
	assert.Empty(t, loader.ConfigFile)
}

func TestLoader_WithConfigFile(t *testing.T) {
	loader := NewLoader().WithConfigFile("/path/to/config.yaml")
	assert.Equal(t, "/path/to/config.yaml", loader.ConfigFile)
}

func TestLoader_WithEnvPrefix(t *testing.T) {
	loader := NewLoader().WithEnvPrefix("TEST")
	assert.Equal(t, "TEST", loader.EnvPrefix)
}

func TestLoader_LoadDefault(t *testing.T) {
	// Clear any existing env vars that might interfere
	clearSpotalisEnvVars()

	loader := NewLoader()
	config, err := loader.Load()

	require.NoError(t, err)
	require.NotNil(t, config)

	// Should be equal to default config
	defaultConfig := DefaultConfig()
	assert.Equal(t, defaultConfig, config)
}

func TestLoader_LoadFromFile(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
operator:
  namespace: "test-namespace"
  readOnlyMode: true
  leaderElection:
    enabled: false
    id: "test-leader"
    leaseDuration: "30s"
controllers:
  maxConcurrentReconciles: 5
  reconcileInterval: "10m"
  workloadTiming:
    cooldownPeriod: "5s"
    disruptionRetryInterval: "45s"
    disruptionWindowPollInterval: "3m"
webhook:
  enabled: false
  port: 8443
  certDir: "/custom/certs"
observability:
  metrics:
    enabled: false
    bindAddress: ":9090"
  logging:
    level: "debug"
    format: "console"
    output: "stderr"
    addCaller: false
    development: true
  health:
    enabled: false
    bindAddress: ":9091"
`

	err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	// Clear env vars
	clearSpotalisEnvVars()

	// Load from file
	config, err := LoadFromFile(configFile)
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify loaded values
	assert.Equal(t, "test-namespace", config.Operator.Namespace)
	assert.True(t, config.Operator.ReadOnlyMode)
	assert.False(t, config.Operator.LeaderElection.Enabled)
	assert.Equal(t, "test-leader", config.Operator.LeaderElection.ID)
	assert.Equal(t, 30*time.Second, config.Operator.LeaderElection.LeaseDuration)

	assert.Equal(t, 5, config.Controllers.MaxConcurrentReconciles)
	assert.Equal(t, 10*time.Minute, config.Controllers.ReconcileInterval)
	assert.Equal(t, 5*time.Second, config.Controllers.WorkloadTiming.CooldownPeriod)
	assert.Equal(t, 45*time.Second, config.Controllers.WorkloadTiming.DisruptionRetryInterval)
	assert.Equal(t, 3*time.Minute, config.Controllers.WorkloadTiming.DisruptionWindowPollInterval)

	assert.False(t, config.Webhook.Enabled)
	assert.Equal(t, 8443, config.Webhook.Port)
	assert.Equal(t, "/custom/certs", config.Webhook.CertDir)

	assert.False(t, config.Observability.Metrics.Enabled)
	assert.Equal(t, ":9090", config.Observability.Metrics.BindAddress)
	assert.Equal(t, "debug", config.Observability.Logging.Level)
	assert.Equal(t, "console", config.Observability.Logging.Format)
	assert.Equal(t, "stderr", config.Observability.Logging.Output)
	assert.False(t, config.Observability.Logging.AddCaller)
	assert.True(t, config.Observability.Logging.Development)
	assert.False(t, config.Observability.Health.Enabled)
	assert.Equal(t, ":9091", config.Observability.Health.BindAddress)
}

func TestLoader_LoadFromEnv(t *testing.T) {
	// Clear any existing env vars
	clearSpotalisEnvVars()

	// Set environment variables
	envVars := map[string]string{
		"SPOTALIS_OPERATOR_NAMESPACE":                                   "env-namespace",
		"SPOTALIS_OPERATOR_READ_ONLY_MODE":                              "true",
		"SPOTALIS_LEADER_ELECTION_ENABLED":                              "false",
		"SPOTALIS_LEADER_ELECTION_ID":                                   "env-leader",
		"SPOTALIS_LEADER_ELECTION_LEASE_DURATION":                       "45s",
		"SPOTALIS_CONTROLLERS_MAX_CONCURRENT_RECONCILES":                "10",
		"SPOTALIS_CONTROLLERS_RECONCILE_INTERVAL":                       "15m",
		"SPOTALIS_CONTROLLERS_WORKLOAD_COOLDOWN_PERIOD":                 "7s",
		"SPOTALIS_CONTROLLERS_WORKLOAD_DISRUPTION_RETRY_INTERVAL":       "35s",
		"SPOTALIS_CONTROLLERS_WORKLOAD_DISRUPTION_WINDOW_POLL_INTERVAL": "5m",
		"SPOTALIS_WEBHOOK_ENABLED":                                      "false",
		"SPOTALIS_WEBHOOK_PORT":                                         "7443",
		"SPOTALIS_WEBHOOK_CERT_DIR":                                     "/env/certs",
		"SPOTALIS_METRICS_ENABLED":                                      "false",
		"SPOTALIS_METRICS_BIND_ADDRESS":                                 ":7070",
		"SPOTALIS_LOGGING_LEVEL":                                        "warn",
		"SPOTALIS_LOGGING_FORMAT":                                       "console",
		"SPOTALIS_LOGGING_OUTPUT":                                       "stderr",
		"SPOTALIS_LOGGING_ADDCALLER":                                    "false",
		"SPOTALIS_LOGGING_DEVELOPMENT":                                  "true",
		"SPOTALIS_HEALTH_ENABLED":                                       "false",
		"SPOTALIS_HEALTH_BIND_ADDRESS":                                  ":7071",
	}

	for key, value := range envVars {
		t.Setenv(key, value)
	}

	// Load from environment
	config, err := LoadFromEnv()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify loaded values
	assert.Equal(t, "env-namespace", config.Operator.Namespace)
	assert.True(t, config.Operator.ReadOnlyMode)
	assert.False(t, config.Operator.LeaderElection.Enabled)
	assert.Equal(t, "env-leader", config.Operator.LeaderElection.ID)
	assert.Equal(t, 45*time.Second, config.Operator.LeaderElection.LeaseDuration)

	assert.Equal(t, 10, config.Controllers.MaxConcurrentReconciles)
	assert.Equal(t, 15*time.Minute, config.Controllers.ReconcileInterval)
	assert.Equal(t, 7*time.Second, config.Controllers.WorkloadTiming.CooldownPeriod)
	assert.Equal(t, 35*time.Second, config.Controllers.WorkloadTiming.DisruptionRetryInterval)
	assert.Equal(t, 5*time.Minute, config.Controllers.WorkloadTiming.DisruptionWindowPollInterval)

	assert.False(t, config.Webhook.Enabled)
	assert.Equal(t, 7443, config.Webhook.Port)
	assert.Equal(t, "/env/certs", config.Webhook.CertDir)

	assert.False(t, config.Observability.Metrics.Enabled)
	assert.Equal(t, ":7070", config.Observability.Metrics.BindAddress)
	assert.Equal(t, "warn", config.Observability.Logging.Level)
	assert.Equal(t, "console", config.Observability.Logging.Format)
	assert.Equal(t, "stderr", config.Observability.Logging.Output)
	assert.False(t, config.Observability.Logging.AddCaller)
	assert.True(t, config.Observability.Logging.Development)
	assert.False(t, config.Observability.Health.Enabled)
	assert.Equal(t, ":7071", config.Observability.Health.BindAddress)
}

func TestLoader_LoadFileOverridesDefault(t *testing.T) {
	// Create a temporary config file with partial config
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "partial-config.yaml")

	yamlContent := `
operator:
  namespace: "file-namespace"
  readOnlyMode: true
# Only override some values, others should remain as defaults
`

	err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	// Clear env vars
	clearSpotalisEnvVars()

	// Load config
	config, err := NewLoader().WithConfigFile(configFile).Load()
	require.NoError(t, err)
	require.NotNil(t, config)

	// File values should override defaults
	assert.Equal(t, "file-namespace", config.Operator.Namespace)
	assert.True(t, config.Operator.ReadOnlyMode)

	// Other values should remain as defaults
	assert.True(t, config.Operator.LeaderElection.Enabled)                           // default
	assert.Equal(t, "spotalis-controller-leader", config.Operator.LeaderElection.ID) // default
	assert.Equal(t, 1, config.Controllers.MaxConcurrentReconciles)                   // default
}

func TestLoader_EnvOverridesFile(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
operator:
  namespace: "file-namespace"
  readOnlyMode: false
`

	err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	// Clear env vars first
	clearSpotalisEnvVars()

	// Set environment variables that should override file
	t.Setenv("SPOTALIS_OPERATOR_NAMESPACE", "env-namespace")
	t.Setenv("SPOTALIS_OPERATOR_READ_ONLY_MODE", "true")

	// Load config
	config, err := NewLoader().WithConfigFile(configFile).Load()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Environment should override file
	assert.Equal(t, "env-namespace", config.Operator.Namespace)
	assert.True(t, config.Operator.ReadOnlyMode)
}

func TestLoader_InvalidFile(t *testing.T) {
	loader := NewLoader().WithConfigFile("/nonexistent/config.yaml")
	_, err := loader.Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config from file")
}

func TestLoader_InvalidYAML(t *testing.T) {
	// Create a temporary file with invalid YAML
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "invalid.yaml")

	invalidYAML := `
operator:
  namespace: "test"
  invalid: [unclosed array
`

	err := os.WriteFile(configFile, []byte(invalidYAML), 0o600)
	require.NoError(t, err)

	loader := NewLoader().WithConfigFile(configFile)
	_, err = loader.Load()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML config file")
}

func TestConfig_Save(t *testing.T) {
	config := DefaultConfig()
	config.Operator.Namespace = "test-save"

	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "saved-config.yaml")

	err := config.Save(filename)
	require.NoError(t, err)

	// Verify file exists and can be loaded
	loadedConfig, err := LoadFromFile(filename)
	require.NoError(t, err)
	assert.Equal(t, "test-save", loadedConfig.Operator.Namespace)
}

func TestLoader_parseBool(t *testing.T) {
	loader := NewLoader()

	// Test true values
	assert.True(t, loader.parseBool("true", false))
	assert.True(t, loader.parseBool("TRUE", false))
	assert.True(t, loader.parseBool("1", false))
	assert.True(t, loader.parseBool("yes", false))
	assert.True(t, loader.parseBool("YES", false))
	assert.True(t, loader.parseBool("on", false))
	assert.True(t, loader.parseBool("ON", false))

	// Test false values
	assert.False(t, loader.parseBool("false", true))
	assert.False(t, loader.parseBool("FALSE", true))
	assert.False(t, loader.parseBool("0", true))
	assert.False(t, loader.parseBool("no", true))
	assert.False(t, loader.parseBool("NO", true))
	assert.False(t, loader.parseBool("off", true))
	assert.False(t, loader.parseBool("OFF", true))

	// Test invalid values use fallback
	assert.True(t, loader.parseBool("invalid", true))
	assert.False(t, loader.parseBool("invalid", false))
}

func TestLoader_parseInt(t *testing.T) {
	loader := NewLoader()

	// Test valid integers
	assert.Equal(t, 42, loader.parseInt("42", 0))
	assert.Equal(t, -10, loader.parseInt("-10", 0))
	assert.Equal(t, 0, loader.parseInt("0", 99))

	// Test invalid integers use fallback
	assert.Equal(t, 99, loader.parseInt("invalid", 99))
	assert.Equal(t, 42, loader.parseInt("", 42))
	assert.Equal(t, 100, loader.parseInt("not-a-number", 100))
}

// clearSpotalisEnvVars clears all SPOTALIS environment variables
func clearSpotalisEnvVars() {
	envVars := []string{
		"SPOTALIS_OPERATOR_NAMESPACE",
		"SPOTALIS_OPERATOR_READ_ONLY_MODE",
		"SPOTALIS_LEADER_ELECTION_ENABLED",
		"SPOTALIS_LEADER_ELECTION_ID",
		"SPOTALIS_LEADER_ELECTION_LEASE_DURATION",
		"SPOTALIS_CONTROLLERS_MAX_CONCURRENT_RECONCILES",
		"SPOTALIS_CONTROLLERS_RECONCILE_INTERVAL",
		"SPOTALIS_CONTROLLERS_WORKLOAD_COOLDOWN_PERIOD",
		"SPOTALIS_CONTROLLERS_WORKLOAD_DISRUPTION_RETRY_INTERVAL",
		"SPOTALIS_CONTROLLERS_WORKLOAD_DISRUPTION_WINDOW_POLL_INTERVAL",
		"SPOTALIS_WEBHOOK_ENABLED",
		"SPOTALIS_WEBHOOK_PORT",
		"SPOTALIS_WEBHOOK_CERT_DIR",
		"SPOTALIS_METRICS_ENABLED",
		"SPOTALIS_METRICS_BIND_ADDRESS",
		"SPOTALIS_LOGGING_LEVEL",
		"SPOTALIS_LOGGING_FORMAT",
		"SPOTALIS_LOGGING_OUTPUT",
		"SPOTALIS_LOGGING_ADDCALLER",
		"SPOTALIS_LOGGING_DEVELOPMENT",
		"SPOTALIS_HEALTH_ENABLED",
		"SPOTALIS_HEALTH_BIND_ADDRESS",
	}

	for _, envVar := range envVars {
		_ = os.Unsetenv(envVar)
	}
}
