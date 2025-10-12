// Package logging provides structured JSON logging for the Spotalis operator.
// It integrates with the controller-runtime logging framework and provides
// consistent log formatting across all components.
package logging

import (
	"context"
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Config defines the logging configuration
type Config struct {
	// Level is the log level (debug, info, warn, error)
	Level string `yaml:"level" json:"level"`

	// Format is the log format (json, console)
	Format string `yaml:"format" json:"format"`
}

// Logger wraps the controller-runtime logger with additional functionality
type Logger struct {
	logr.Logger
	config *Config
}

// DefaultConfig returns default logging configuration
func DefaultConfig() *Config {
	return &Config{
		Level:  "info", // Default to info level for general logging
		Format: "json",
	}
}

// DefaultDebugConfig returns logging configuration optimized for debug mode
func DefaultDebugConfig() *Config {
	return &Config{
		Level:  "debug",
		Format: "json",
	}
}

// NewLogger creates a new structured logger based on the provided configuration
func NewLogger(config *Config) (*Logger, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create controller-runtime compatible logger using zap options
	opts := ctrlzap.Options{
		Development: false,
	}

	// Configure based on format
	if config.Format == "json" {
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.TimeKey = "time"
		encoderConfig.LevelKey = "level"
		encoderConfig.MessageKey = "msg"
		encoderConfig.CallerKey = "caller"
		encoderConfig.StacktraceKey = "stacktrace"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
		opts.Encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoderConfig := zap.NewDevelopmentEncoderConfig()
		opts.Encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// Configure log level
	level := parseLogLevel(config.Level)
	opts.Level = &level

	// Create the controller-runtime logger
	ctrlLogger := ctrlzap.New(ctrlzap.UseFlagOptions(&opts))

	return &Logger{
		Logger: ctrlLogger,
		config: config,
	}, nil
}

// buildZapConfig creates a zap configuration based on logging config
func buildZapConfig(config *Config) zap.Config {
	var zapConfig zap.Config

	if config.Format == "json" {
		zapConfig = zap.NewProductionConfig()
		zapConfig.EncoderConfig = zap.NewProductionEncoderConfig()
		zapConfig.EncoderConfig.TimeKey = "time"
		zapConfig.EncoderConfig.LevelKey = "level"
		zapConfig.EncoderConfig.MessageKey = "msg"
		zapConfig.EncoderConfig.CallerKey = "caller"
		zapConfig.EncoderConfig.StacktraceKey = "stacktrace"
		zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		zapConfig.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	}

	// Set log level
	zapConfig.Level = zap.NewAtomicLevelAt(parseZapLevel(config.Level))

	return zapConfig
}

// parseLogLevel converts string log level to zapcore.Level
func parseLogLevel(level string) zapcore.Level {
	return parseZapLevel(level)
}

// parseZapLevel converts string log level to zap.AtomicLevel
func parseZapLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// WithName returns a logger with the specified name
func (l *Logger) WithName(name string) *Logger {
	return &Logger{
		Logger: l.Logger.WithName(name),
		config: l.config,
	}
}

// WithValues returns a logger with the specified key-value pairs
func (l *Logger) WithValues(keysAndValues ...interface{}) *Logger {
	return &Logger{
		Logger: l.Logger.WithValues(keysAndValues...),
		config: l.config,
	}
}

// WithContext returns a logger with context-specific fields
func (l *Logger) WithContext(ctx context.Context) *Logger {
	// Extract common context values for structured logging
	logger := l.Logger

	if reqID := ctx.Value("request-id"); reqID != nil {
		logger = logger.WithValues("request_id", reqID)
	}

	if traceID := ctx.Value("trace-id"); traceID != nil {
		logger = logger.WithValues("trace_id", traceID)
	}

	return &Logger{
		Logger: logger,
		config: l.config,
	}
}

// WithController returns a logger configured for controller operations
func (l *Logger) WithController(controllerName string) *Logger {
	return &Logger{
		Logger: l.Logger.WithName(controllerName).WithValues(
			"controller", controllerName,
		),
		config: l.config,
	}
}

// WithReconciler returns a logger configured for reconciler operations
func (l *Logger) WithReconciler(namespace, name, kind string) *Logger {
	return &Logger{
		Logger: l.Logger.WithValues(
			"namespace", namespace,
			"name", name,
			"kind", kind,
		),
		config: l.config,
	}
}

// WithWebhook returns a logger configured for webhook operations
func (l *Logger) WithWebhook(operation, namespace, name, kind string) *Logger {
	return &Logger{
		Logger: l.Logger.WithValues(
			"webhook_operation", operation,
			"namespace", namespace,
			"name", name,
			"kind", kind,
		),
		config: l.config,
	}
}

// GetConfig returns the logging configuration
func (l *Logger) GetConfig() *Config {
	return l.config
}

// SetGlobalLogger sets the global logger for the application
func SetGlobalLogger(logger *Logger) error {
	// Create a zap logger from our configuration
	zapConfig := buildZapConfig(logger.config)
	zapLogger, err := zapConfig.Build()
	if err != nil {
		return err
	}

	// Replace global zap logger
	zap.ReplaceGlobals(zapLogger)

	return nil
}

// GetLoggerFromEnv creates a logger from environment variables
func GetLoggerFromEnv() (*Logger, error) {
	config := &Config{
		Level:  getEnvOrDefault("SPOTALIS_LOG_LEVEL", "info"),
		Format: getEnvOrDefault("SPOTALIS_LOG_FORMAT", "json"),
	}

	return NewLogger(config)
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
