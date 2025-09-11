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

package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// LoggerConfig contains logging configuration
type LoggerConfig struct {
	// Basic configuration
	Level       string
	Format      string // "json", "text", "console"
	Development bool

	// Output configuration
	Output      io.Writer
	ErrorOutput io.Writer

	// Structured logging options
	AddSource     bool
	TimeFormat    string
	DisableTime   bool
	DisableLevel  bool
	DisableCaller bool

	// Context and correlation
	RequestIDKey     string
	CorrelationIDKey string
	ComponentKey     string

	// Performance options
	BufferSize   int
	AsyncLogging bool
}

// DefaultLoggerConfig returns default logger configuration
func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Level:            "info",
		Format:           "json",
		Development:      false,
		Output:           os.Stdout,
		ErrorOutput:      os.Stderr,
		AddSource:        true,
		TimeFormat:       time.RFC3339,
		RequestIDKey:     "request_id",
		CorrelationIDKey: "correlation_id",
		ComponentKey:     "component",
		BufferSize:       1024,
		AsyncLogging:     false,
	}
}

// Logger wraps slog.Logger with additional functionality for Spotalis
type Logger struct {
	*slog.Logger
	config *LoggerConfig

	// Context fields that are added to all log entries
	contextFields map[string]interface{}

	// Component identifier
	component string
}

// NewLogger creates a new structured logger
func NewLogger(config *LoggerConfig) (*Logger, error) {
	if config == nil {
		config = DefaultLoggerConfig()
	}

	// Create handler based on format
	var handler slog.Handler
	var err error

	opts := &slog.HandlerOptions{
		Level:     parseLogLevel(config.Level),
		AddSource: config.AddSource,
	}

	switch strings.ToLower(config.Format) {
	case "json":
		handler = slog.NewJSONHandler(config.Output, opts)
	case "text":
		handler = slog.NewTextHandler(config.Output, opts)
	case "console":
		handler = NewConsoleHandler(config.Output, opts)
	default:
		return nil, fmt.Errorf("unsupported log format: %s", config.Format)
	}

	// Add custom attributes to handler
	handler = NewContextHandler(handler, config)

	logger := &Logger{
		Logger:        slog.New(handler),
		config:        config,
		contextFields: make(map[string]interface{}),
	}

	return logger, nil
}

// NewControllerRuntimeLogger creates a controller-runtime compatible logger
func NewControllerRuntimeLogger(config *LoggerConfig) (logr.Logger, error) {
	if config == nil {
		config = DefaultLoggerConfig()
	}

	// Use zap for controller-runtime integration
	opts := zap.Options{
		Development: config.Development,
		Level:       parseZapLogLevel(config.Level),
	}

	return zap.New(zap.UseFlagOptions(&opts)), nil
}

// SetupControllerRuntimeLogging sets up logging for controller-runtime
func SetupControllerRuntimeLogging(config *LoggerConfig) error {
	logger, err := NewControllerRuntimeLogger(config)
	if err != nil {
		return fmt.Errorf("failed to create controller-runtime logger: %w", err)
	}

	log.SetLogger(logger)
	return nil
}

// WithComponent returns a logger with a component field
func (l *Logger) WithComponent(component string) *Logger {
	newLogger := &Logger{
		Logger:        l.Logger.With("component", component),
		config:        l.config,
		contextFields: make(map[string]interface{}),
		component:     component,
	}

	// Copy context fields
	for k, v := range l.contextFields {
		newLogger.contextFields[k] = v
	}

	return newLogger
}

// WithContext returns a logger with context fields
func (l *Logger) WithContext(ctx context.Context) *Logger {
	attrs := []slog.Attr{}

	// Extract request ID from context
	if reqID := getRequestIDFromContext(ctx); reqID != "" {
		attrs = append(attrs, slog.String(l.config.RequestIDKey, reqID))
	}

	// Extract correlation ID from context
	if corrID := getCorrelationIDFromContext(ctx); corrID != "" {
		attrs = append(attrs, slog.String(l.config.CorrelationIDKey, corrID))
	}

	// Convert attrs to Logger
	logger := l.Logger
	for _, attr := range attrs {
		logger = logger.With(attr.Key, attr.Value)
	}

	return &Logger{
		Logger:        logger,
		config:        l.config,
		contextFields: l.contextFields,
		component:     l.component,
	}
}

// WithFields returns a logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	logger := l.Logger
	for k, v := range fields {
		logger = logger.With(k, v)
	}

	newContextFields := make(map[string]interface{})
	for k, v := range l.contextFields {
		newContextFields[k] = v
	}
	for k, v := range fields {
		newContextFields[k] = v
	}

	return &Logger{
		Logger:        logger,
		config:        l.config,
		contextFields: newContextFields,
		component:     l.component,
	}
}

// LogReconciliation logs reconciliation events with structured data
func (l *Logger) LogReconciliation(ctx context.Context, resource, namespace, name string, duration time.Duration, err error) {
	fields := map[string]interface{}{
		"resource":    resource,
		"namespace":   namespace,
		"name":        name,
		"duration":    duration.String(),
		"duration_ms": duration.Milliseconds(),
	}

	logger := l.WithContext(ctx).WithFields(fields)

	if err != nil {
		logger.Error("Reconciliation failed", "error", err)
	} else {
		logger.Info("Reconciliation completed")
	}
}

// LogWebhookRequest logs webhook admission requests
func (l *Logger) LogWebhookRequest(ctx context.Context, operation, resource, namespace, name string, allowed bool, duration time.Duration, err error) {
	fields := map[string]interface{}{
		"operation":   operation,
		"resource":    resource,
		"namespace":   namespace,
		"name":        name,
		"allowed":     allowed,
		"duration":    duration.String(),
		"duration_ms": duration.Milliseconds(),
	}

	logger := l.WithContext(ctx).WithFields(fields)

	if err != nil {
		logger.Error("Webhook request failed", "error", err)
	} else {
		logger.Info("Webhook request processed")
	}
}

// LogNodeClassification logs node classification events
func (l *Logger) LogNodeClassification(ctx context.Context, nodeName, nodeType string, spotInstances, onDemandInstances int, duration time.Duration) {
	fields := map[string]interface{}{
		"node_name":          nodeName,
		"node_type":          nodeType,
		"spot_instances":     spotInstances,
		"ondemand_instances": onDemandInstances,
		"duration":           duration.String(),
		"duration_ms":        duration.Milliseconds(),
	}

	logger := l.WithContext(ctx).WithFields(fields)
	logger.Info("Node classification completed")
}

// LogMetricsCollection logs metrics collection events
func (l *Logger) LogMetricsCollection(ctx context.Context, metricsCount int, duration time.Duration, err error) {
	fields := map[string]interface{}{
		"metrics_count": metricsCount,
		"duration":      duration.String(),
		"duration_ms":   duration.Milliseconds(),
	}

	logger := l.WithContext(ctx).WithFields(fields)

	if err != nil {
		logger.Error("Metrics collection failed", "error", err)
	} else {
		logger.Info("Metrics collection completed")
	}
}

// LogLeaderElection logs leader election events
func (l *Logger) LogLeaderElection(ctx context.Context, event, identity, leaderIdentity string) {
	fields := map[string]interface{}{
		"event":           event,
		"identity":        identity,
		"leader_identity": leaderIdentity,
	}

	logger := l.WithContext(ctx).WithFields(fields)
	logger.Info("Leader election event")
}

// parseLogLevel parses string log level to slog.Level
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// parseZapLogLevel parses string log level to zap level (for controller-runtime)
func parseZapLogLevel(level string) zap.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn", "warning":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}

// Context key types for request/correlation IDs
type contextKey string

const (
	requestIDKey     contextKey = "request_id"
	correlationIDKey contextKey = "correlation_id"
)

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDKey, correlationID)
}

// getRequestIDFromContext extracts request ID from context
func getRequestIDFromContext(ctx context.Context) string {
	if reqID, ok := ctx.Value(requestIDKey).(string); ok {
		return reqID
	}
	return ""
}

// getCorrelationIDFromContext extracts correlation ID from context
func getCorrelationIDFromContext(ctx context.Context) string {
	if corrID, ok := ctx.Value(correlationIDKey).(string); ok {
		return corrID
	}
	return ""
}

// ConsoleHandler provides a console-friendly log format
type ConsoleHandler struct {
	slog.Handler
	output io.Writer
}

// NewConsoleHandler creates a new console handler
func NewConsoleHandler(output io.Writer, opts *slog.HandlerOptions) *ConsoleHandler {
	return &ConsoleHandler{
		Handler: slog.NewTextHandler(output, opts),
		output:  output,
	}
}

// ContextHandler adds context fields to log records
type ContextHandler struct {
	slog.Handler
	config *LoggerConfig
}

// NewContextHandler creates a new context handler
func NewContextHandler(handler slog.Handler, config *LoggerConfig) *ContextHandler {
	return &ContextHandler{
		Handler: handler,
		config:  config,
	}
}

// Handle processes log records with context information
func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	// Add timestamp if not disabled
	if !h.config.DisableTime {
		record.Time = time.Now()
	}

	return h.Handler.Handle(ctx, record)
}
