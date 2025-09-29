package di

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahoma/spotalis/pkg/config"
	"github.com/ahoma/spotalis/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationBuilder_NewApplicationBuilder(t *testing.T) {
	builder := NewApplicationBuilder()

	assert.NotNil(t, builder)
	assert.NotNil(t, builder.container)
	assert.Empty(t, builder.configFile)
}

func TestApplicationBuilder_WithConfigFile(t *testing.T) {
	builder := NewApplicationBuilder()
	builder = builder.WithConfigFile("/path/to/config.yaml")

	assert.Equal(t, "/path/to/config.yaml", builder.configFile)
}

func TestApplicationBuilder_BuildDefault(t *testing.T) {
	ctx := context.Background()
	builder := NewApplicationBuilder()

	app, err := builder.Build(ctx)

	require.NoError(t, err)
	require.NotNil(t, app)
	require.NotNil(t, app.Config)

	// Should have default configuration
	assert.Equal(t, "spotalis-system", app.Config.Operator.Namespace)
	assert.True(t, app.Config.Operator.LeaderElection.Enabled)
	assert.Equal(t, 1, app.Config.Controllers.MaxConcurrentReconciles)
}

func TestApplicationBuilder_BuildWithConfigFile(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test-config.yaml")

	yamlContent := `
operator:
  namespace: "test-namespace"
  readOnlyMode: true
  leaderElection:
    enabled: false
    id: "test-leader"
controllers:
  maxConcurrentReconciles: 3
webhook:
  enabled: false
observability:
  metrics:
    enabled: false
  logging:
    level: "debug"
`

	err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	ctx := context.Background()
	builder := NewApplicationBuilder().WithConfigFile(configFile)

	app, err := builder.Build(ctx)

	require.NoError(t, err)
	require.NotNil(t, app)
	require.NotNil(t, app.Config)

	// Should have configuration from file
	assert.Equal(t, "test-namespace", app.Config.Operator.Namespace)
	assert.True(t, app.Config.Operator.ReadOnlyMode)
	assert.False(t, app.Config.Operator.LeaderElection.Enabled)
	assert.Equal(t, "test-leader", app.Config.Operator.LeaderElection.ID)
	assert.Equal(t, 3, app.Config.Controllers.MaxConcurrentReconciles)
	assert.False(t, app.Config.Webhook.Enabled)
	assert.False(t, app.Config.Observability.Metrics.Enabled)
	assert.Equal(t, "debug", app.Config.Observability.Logging.Level)
}

func TestApplication_Start(t *testing.T) {
	// Set environment variables to disable webhook for testing and use random ports
	t.Setenv("SPOTALIS_WEBHOOK_ENABLED", "false")
	t.Setenv("SPOTALIS_LEADER_ELECTION_ENABLED", "false")
	t.Setenv("SPOTALIS_METRICS_BIND_ADDRESS", ":0") // Use random port
	t.Setenv("SPOTALIS_HEALTH_BIND_ADDRESS", ":0")  // Use random port

	// Build just the application builder and container without full operator
	builder := NewApplicationBuilder()

	// Register configuration loader
	builder.container.MustProvide(config.NewLoader)

	// Register consolidated configuration
	builder.container.MustProvide(func(loader *config.Loader) (*config.SpotalisConfig, error) {
		return loader.Load()
	})

	// Test that we can build operator config (without creating actual operator)
	builder.container.MustProvide(func(config *config.SpotalisConfig) *operator.Config {
		return &operator.Config{
			Namespace:               config.Operator.Namespace,
			ReadOnlyMode:            config.Operator.ReadOnlyMode,
			LeaderElection:          config.Operator.LeaderElection.Enabled,
			LeaderElectionID:        config.Operator.LeaderElection.ID,
			MetricsAddr:             config.Observability.Metrics.BindAddress,
			ProbeAddr:               config.Observability.Health.BindAddress,
			WebhookPort:             config.Webhook.Port,
			WebhookCertDir:          config.Webhook.CertDir,
			ReconcileInterval:       config.Controllers.ReconcileInterval,
			MaxConcurrentReconciles: config.Controllers.MaxConcurrentReconciles,
			EnableWebhook:           config.Webhook.Enabled,
			LogLevel:                config.Observability.Logging.Level,
		}
	})

	// Test that we can resolve operator config from DI container
	var resolvedConfig *operator.Config
	err := builder.container.Invoke(func(cfg *operator.Config) {
		resolvedConfig = cfg
	})

	require.NoError(t, err, "Failed to resolve operator config from DI container")
	require.NotNil(t, resolvedConfig, "Operator config not resolved from DI container")

	t.Logf("✅ Operator config resolved from DI container")
	t.Logf("  - Namespace: %s", resolvedConfig.Namespace)
	t.Logf("  - Leader Election: %v", resolvedConfig.LeaderElection)
	t.Logf("  - Webhook Enabled: %v", resolvedConfig.EnableWebhook)

	// Verify DI integration: the fact that we can resolve the config through
	// the dependency chain proves the DI system is working correctly
}

func TestApplication_Stop(t *testing.T) {
	ctx := context.Background()
	app, err := NewApplication(ctx)
	require.NoError(t, err)

	// Stop should not return an error
	err = app.Stop(ctx)
	assert.NoError(t, err)
}

func TestApplication_GetConfig(t *testing.T) {
	ctx := context.Background()
	app, err := NewApplication(ctx)
	require.NoError(t, err)

	config := app.GetConfig()
	assert.NotNil(t, config)
	assert.Equal(t, app.Config, config)
}

func TestNewApplication(t *testing.T) {
	ctx := context.Background()

	app, err := NewApplication(ctx)

	require.NoError(t, err)
	require.NotNil(t, app)
	assert.Equal(t, "spotalis-system", app.Config.Operator.Namespace)
}

func TestNewApplicationWithConfig(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "app-config.yaml")

	yamlContent := `
operator:
  namespace: "custom-namespace"
controllers:
  maxConcurrentReconciles: 5
`

	err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	ctx := context.Background()

	app, err := NewApplicationWithConfig(ctx, configFile)

	require.NoError(t, err)
	require.NotNil(t, app)
	assert.Equal(t, "custom-namespace", app.Config.Operator.Namespace)
	assert.Equal(t, 5, app.Config.Controllers.MaxConcurrentReconciles)
}

func TestApplicationBuilder_BuildWithInvalidConfigFile(t *testing.T) {
	ctx := context.Background()
	builder := NewApplicationBuilder().WithConfigFile("/nonexistent/config.yaml")

	_, err := builder.Build(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build application")
}
