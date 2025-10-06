package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, "info", config.Level)
	assert.Equal(t, "json", config.Format)
	assert.Equal(t, "stdout", config.Output)
	assert.True(t, config.AddCaller)
	assert.False(t, config.Development)
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   *Config
	}{
		{
			name:   "nil config uses defaults",
			config: nil,
			want:   DefaultConfig(),
		},
		{
			name: "json format configuration",
			config: &Config{
				Level:       "debug",
				Format:      "json",
				Output:      "stdout",
				AddCaller:   true,
				Development: false,
			},
			want: &Config{
				Level:       "debug",
				Format:      "json",
				Output:      "stdout",
				AddCaller:   true,
				Development: false,
			},
		},
		{
			name: "console format configuration",
			config: &Config{
				Level:       "warn",
				Format:      "console",
				Output:      "stderr",
				AddCaller:   false,
				Development: true,
			},
			want: &Config{
				Level:       "warn",
				Format:      "console",
				Output:      "stderr",
				AddCaller:   false,
				Development: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewLogger(tt.config)
			require.NoError(t, err)
			require.NotNil(t, logger)

			assert.Equal(t, tt.want, logger.GetConfig())
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		level    string
		expected string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"warning", "warn"},
		{"error", "error"},
		{"panic", "panic"},
		{"fatal", "fatal"},
		{"invalid", "info"}, // defaults to info
		{"", "info"},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			level := parseLogLevel(tt.level)
			assert.Equal(t, tt.expected, level.String())
		})
	}
}

func TestLoggerWithMethods(t *testing.T) {
	config := &Config{
		Level:       "info",
		Format:      "json",
		Output:      "stdout",
		AddCaller:   true,
		Development: false,
	}

	logger, err := NewLogger(config)
	require.NoError(t, err)

	// Test WithName
	namedLogger := logger.WithName("test")
	assert.NotNil(t, namedLogger)
	assert.Equal(t, config, namedLogger.GetConfig())

	// Test WithValues
	valuedLogger := logger.WithValues("key", "value")
	assert.NotNil(t, valuedLogger)
	assert.Equal(t, config, valuedLogger.GetConfig())

	// Test WithController
	controllerLogger := logger.WithController("deployment-controller")
	assert.NotNil(t, controllerLogger)
	assert.Equal(t, config, controllerLogger.GetConfig())

	// Test WithReconciler
	reconcilerLogger := logger.WithReconciler("default", "test-deployment", "Deployment")
	assert.NotNil(t, reconcilerLogger)
	assert.Equal(t, config, reconcilerLogger.GetConfig())

	// Test WithWebhook
	webhookLogger := logger.WithWebhook("CREATE", "default", "test-pod", "Pod")
	assert.NotNil(t, webhookLogger)
	assert.Equal(t, config, webhookLogger.GetConfig())
}

func TestGetLoggerFromEnv(t *testing.T) {
	// Test without environment variables (should use defaults)
	logger, err := GetLoggerFromEnv()
	require.NoError(t, err)
	require.NotNil(t, logger)

	config := logger.GetConfig()
	assert.Equal(t, "info", config.Level)
	assert.Equal(t, "json", config.Format)
	assert.Equal(t, "stdout", config.Output)
}

func TestBuildZapConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "json format",
			config: &Config{
				Level:       "debug",
				Format:      "json",
				Output:      "stdout",
				AddCaller:   true,
				Development: false,
			},
		},
		{
			name: "console format",
			config: &Config{
				Level:       "info",
				Format:      "console",
				Output:      "stderr",
				AddCaller:   false,
				Development: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zapConfig := buildZapConfig(tt.config)

			// Verify configuration is properly set
			assert.NotNil(t, zapConfig)
			assert.Equal(t, parseZapLevel(tt.config.Level), zapConfig.Level.Level())
			assert.Equal(t, !tt.config.AddCaller, zapConfig.DisableCaller)

			if tt.config.Output == "stderr" {
				assert.Contains(t, zapConfig.OutputPaths, "stderr")
			} else if tt.config.Output != "stdout" && tt.config.Output != "" {
				assert.Contains(t, zapConfig.OutputPaths, tt.config.Output)
			}
		})
	}
}
