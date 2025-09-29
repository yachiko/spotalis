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
limitations // GetID returns the operator's unique identifier
func (o *Operator) GetID() string {
	if o.leaderElectionManager != nil {
		return o.leaderElectionManager.GetIdentity()
	}
	return "unknown"
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

package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/internal/server"
	"github.com/ahoma/spotalis/pkg/controllers"
	"github.com/ahoma/spotalis/pkg/metrics"
	webhookMutate "github.com/ahoma/spotalis/pkg/webhook"
	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	// Log levels
	logLevelDebug = "debug"

	// Environment variable values
	envValueTrue = "true"

	// Default values
	defaultHostname = "unknown"
)

// Operator represents the main Spotalis operator following Karpenter architecture pattern
type Operator struct {
	manager.Manager

	// Configuration
	config    *Config
	namespace string

	// Core services
	annotationParser *annotations.AnnotationParser
	nodeClassifier   *config.NodeClassifierService
	metricsCollector *metrics.Collector
	mutationHandler  *webhookMutate.MutationHandler

	// HTTP Server components
	ginEngine     *gin.Engine
	healthChecker *server.HealthChecker
	metricsServer *server.MetricsServer
	webhookServer *server.WebhookServer

	// Kubernetes clients
	kubeClient kubernetes.Interface

	// Leader election
	leaderElectionManager *LeaderElectionManager

	// Runtime state
	started bool
}

// Config contains configuration for the Spotalis operator
type Config struct {
	// Basic configuration
	MetricsAddr      string
	ProbeAddr        string
	WebhookAddr      string
	LeaderElection   bool
	LeaderElectionID string
	Namespace        string

	// Controller configuration
	ReconcileInterval       time.Duration
	MaxConcurrentReconciles int

	// Webhook configuration
	WebhookCertDir  string
	WebhookCertName string
	WebhookKeyName  string
	WebhookPort     int

	// Operational configuration
	LogLevel      string
	EnablePprof   bool
	EnableWebhook bool
	ReadOnlyMode  bool

	// Multi-tenancy
	NamespaceFilter []string

	// Performance tuning
	APIQPSLimit   float32
	APIBurstLimit int
}

// Metrics represents operator metrics (for compatibility)
type Metrics struct {
	WorkloadsManaged      int
	SpotInterruptions     int
	LeaderElectionChanges int
	LeadershipDuration    time.Duration
}

// HealthStatus represents the health status of the operator
type HealthStatus struct {
	Leadership string
	Status     string
}

// DefaultOperatorConfig creates a default configuration
func DefaultOperatorConfig() *Config {
	return &Config{
		MetricsAddr:             ":8080",
		ProbeAddr:               ":8081",
		WebhookAddr:             ":9443",
		LeaderElection:          true,
		LeaderElectionID:        "spotalis-controller-leader",
		Namespace:               "spotalis-system",
		ReconcileInterval:       30 * time.Second,
		MaxConcurrentReconciles: 10,
		WebhookCertDir:          "/tmp/k8s-webhook-server/serving-certs",
		WebhookCertName:         "tls.crt",
		WebhookKeyName:          "tls.key",
		WebhookPort:             9443,
		LogLevel:                "info",
		EnablePprof:             false,
		EnableWebhook:           true,
		ReadOnlyMode:            false,
		APIQPSLimit:             20.0,
		APIBurstLimit:           30,
	}
}

// New creates a new operator instance (for compatibility)
func New(_ *rest.Config) *Operator {
	config := DefaultOperatorConfig()
	operator, err := NewOperator(config)
	if err != nil {
		panic(fmt.Sprintf("failed to create operator: %v", err))
	}
	return operator
}

// NewWithConfig creates a new operator with configuration (for compatibility)
func NewWithConfig(_ *rest.Config, operatorConfig interface{}) *Operator {
	var config *Config
	if operatorConfig != nil {
		if c, ok := operatorConfig.(*Config); ok {
			config = c
		} else {
			config = DefaultOperatorConfig()
		}
	} else {
		config = DefaultOperatorConfig()
	}

	operator, err := NewOperator(config)
	if err != nil {
		panic(fmt.Sprintf("failed to create operator with config: %v", err))
	}
	return operator
}

// NewWithID creates a new operator with a specific ID for testing
func NewWithID(cfg *rest.Config, operatorID string) *Operator {
	return NewWithIDAndPorts(cfg, operatorID, 0, 0, 0)
}

// NewForTesting creates a new operator specifically configured for integration tests
// - Disables webhooks to avoid TLS certificate issues
// - Uses unique controller names per instance
func NewForTesting(cfg *rest.Config, operatorID string) *Operator {
	config := DefaultOperatorConfig()
	config.LeaderElectionID = "spotalis-leader-test" // Shared lease name for testing
	config.EnableWebhook = false                     // Disable webhooks for testing

	// Use auto-assigned ports
	config.MetricsAddr = ":0"
	config.ProbeAddr = ":0"

	// Create custom manager with webhook disabled
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to add client-go scheme: %v", err))
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(config.LogLevel == logLevelDebug)))

	// Create manager options without webhook server for testing
	managerOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server for testing
		},
		HealthProbeBindAddress: config.ProbeAddr,
		LeaderElection:         false, // Disable built-in leader election for testing
		// No WebhookServer for testing
	}

	mgr, err := ctrl.NewManager(cfg, managerOpts)
	if err != nil {
		panic(fmt.Sprintf("failed to create manager: %v", err))
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to create kubernetes client: %v", err))
	}

	operator := &Operator{
		Manager:    mgr,
		config:     config,
		namespace:  config.Namespace,
		kubeClient: kubeClient,
	}

	// Initialize leader election manager
	leaderElectionConfig := &LeaderElectionConfig{
		Enabled:       config.LeaderElection,
		Identity:      operatorID,
		ID:            config.LeaderElectionID,
		LeaseName:     config.LeaderElectionID,
		Namespace:     "kube-system",
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
	}

	operator.leaderElectionManager, err = NewLeaderElectionManager(leaderElectionConfig, kubeClient, mgr)
	if err != nil {
		panic(fmt.Sprintf("failed to create leader election manager: %v", err))
	}

	// Initialize core services
	if err := operator.initializeCoreServices(); err != nil {
		panic(fmt.Sprintf("failed to initialize core services: %v", err))
	}

	// Initialize HTTP server (but not webhook server for testing)
	if err := operator.initializeHTTPServer(); err != nil {
		panic(fmt.Sprintf("failed to initialize HTTP server: %v", err))
	}

	// Setup controllers with unique names for testing
	if err := operator.setupControllers(); err != nil {
		panic(fmt.Sprintf("failed to setup controllers: %v", err))
	}

	return operator
}

// NewWithIDAndPorts creates a new operator with a specific ID and custom ports for testing
func NewWithIDAndPorts(cfg *rest.Config, operatorID string, metricsPort, probePort, webhookPort int) *Operator {
	config := DefaultOperatorConfig()
	config.LeaderElectionID = operatorID

	// Use auto-assigned ports if 0 is provided
	if metricsPort == 0 {
		config.MetricsAddr = ":0"
	} else {
		config.MetricsAddr = fmt.Sprintf(":%d", metricsPort)
	}

	if probePort == 0 {
		config.ProbeAddr = ":0"
	} else {
		config.ProbeAddr = fmt.Sprintf(":%d", probePort)
	}

	if webhookPort == 0 {
		config.WebhookPort = 0 // This will auto-assign a port
	} else {
		config.WebhookPort = webhookPort
	}

	// Create custom manager with specific leader election ID
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to add client-go scheme: %v", err))
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(config.LogLevel == logLevelDebug)))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server for testing
		},
		WebhookServer:           webhook.NewServer(webhook.Options{Port: config.WebhookPort}),
		HealthProbeBindAddress:  config.ProbeAddr,
		LeaderElection:          config.LeaderElection,
		LeaderElectionID:        config.LeaderElectionID,
		LeaderElectionNamespace: config.Namespace,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create manager: %v", err))
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to create kubernetes client: %v", err))
	}

	operator := &Operator{
		Manager:    mgr,
		config:     config,
		namespace:  config.Namespace,
		kubeClient: kubeClient,
	}

	// Initialize leader election manager
	leaderElectionConfig := &LeaderElectionConfig{
		Enabled:       config.LeaderElection,
		Identity:      operatorID,
		ID:            config.LeaderElectionID,
		LeaseName:     config.LeaderElectionID,
		Namespace:     "kube-system",
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
	}

	operator.leaderElectionManager, err = NewLeaderElectionManager(leaderElectionConfig, kubeClient, mgr)
	if err != nil {
		panic(fmt.Sprintf("failed to create leader election manager: %v", err))
	}

	// Initialize core services
	if err := operator.initializeCoreServices(); err != nil {
		panic(fmt.Sprintf("failed to initialize core services: %v", err))
	}

	// Initialize HTTP server
	if err := operator.initializeHTTPServer(); err != nil {
		panic(fmt.Sprintf("failed to initialize HTTP server: %v", err))
	}

	// Setup controllers
	if err := operator.setupControllers(); err != nil {
		panic(fmt.Sprintf("failed to setup controllers: %v", err))
	}

	// Setup webhooks
	if config.EnableWebhook {
		if err := operator.setupWebhooks(); err != nil {
			panic(fmt.Sprintf("failed to setup webhooks: %v", err))
		}
	}

	// Setup health and readiness checks
	operator.setupHealthChecks()

	return operator
}

// NewOperator creates a new Spotalis operator instance
func NewOperator(config *Config) (*Operator, error) {
	if config == nil {
		config = DefaultOperatorConfig()
	}

	// Override from environment variables
	if err := configFromEnv(config); err != nil {
		return nil, fmt.Errorf("failed to load configuration from environment: %w", err)
	}

	// Setup scheme
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add client-go scheme: %w", err)
	}

	// Setup logger
	ctrl.SetLogger(zap.New(zap.UseDevMode(config.LogLevel == logLevelDebug)))

	// Create manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: config.MetricsAddr,
		},
		WebhookServer:           webhook.NewServer(webhook.Options{Port: config.WebhookPort}),
		HealthProbeBindAddress:  config.ProbeAddr,
		LeaderElection:          config.LeaderElection, // Use controller-runtime's built-in leader election
		LeaderElectionID:        config.LeaderElectionID,
		LeaderElectionNamespace: config.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	// Create Kubernetes client
	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	operator := &Operator{
		Manager:    mgr,
		config:     config,
		namespace:  config.Namespace,
		kubeClient: kubeClient,
	}

	// Initialize leader election manager if leader election is enabled
	if config.LeaderElection {
		// Using controller-runtime's built-in leader election instead of custom implementation
		ctrl.Log.WithName("setup").Info("Using controller-runtime's built-in leader election")
	}

	// Initialize core services
	if err := operator.initializeCoreServices(); err != nil {
		return nil, fmt.Errorf("failed to initialize core services: %w", err)
	}

	// Initialize HTTP server
	if err := operator.initializeHTTPServer(); err != nil {
		return nil, fmt.Errorf("failed to initialize HTTP server: %w", err)
	}

	// Setup controllers
	if err := operator.setupControllers(); err != nil {
		return nil, fmt.Errorf("failed to setup controllers: %w", err)
	}

	// Setup webhooks
	if config.EnableWebhook {
		if err := operator.setupWebhooks(); err != nil {
			return nil, fmt.Errorf("failed to setup webhooks: %w", err)
		}
	}

	// Setup health and readiness checks
	operator.setupHealthChecks()

	return operator, nil
}

// Start starts the operator
func (o *Operator) Start(ctx context.Context) error {
	if o.started {
		return fmt.Errorf("operator already started")
	}

	setupLog := ctrl.Log.WithName("setup")
	setupLog.Info("Starting Spotalis operator",
		"namespace", o.namespace,
		"webhook-enabled", o.config.EnableWebhook,
		"read-only", o.config.ReadOnlyMode,
		"leader-election", o.config.LeaderElection,
	)

	// Start the manager (includes controllers and webhooks)
	o.started = true

	// Controller-runtime handles leader election automatically when configured
	return o.Manager.Start(ctx)
}

// IsReady returns true if the operator is ready
func (o *Operator) IsReady() bool {
	if !o.started {
		return false
	}

	// If no manager (e.g., in tests), just check started state
	if o.Manager == nil {
		return o.started
	}

	// Check if manager is elected (for leader election)
	if o.config.LeaderElection {
		select {
		case <-o.Elected():
			return true
		default:
			return false
		}
	}

	// If no leader election, consider ready if started
	return o.started
}

// GetMetrics returns current operator metrics (for compatibility)
func (o *Operator) GetMetrics() Metrics {
	if o.metricsCollector == nil {
		return Metrics{}
	}

	// Get metrics from collector
	snapshot := o.metricsCollector.GetMetricsSnapshot()

	metrics := Metrics{
		WorkloadsManaged:      snapshot.ManagedWorkloads,
		SpotInterruptions:     0, // This would need to be tracked separately
		LeaderElectionChanges: 0, // TODO: Track leader election changes
		LeadershipDuration:    0, // TODO: Track leadership duration
	}

	// Add leader election metrics if available
	if o.leaderElectionManager != nil {
		info := o.leaderElectionManager.GetLeadershipInfo()
		if info.IsLeader && !info.StartTime.IsZero() {
			metrics.LeadershipDuration = time.Since(info.StartTime)
		}
	}

	return metrics
}

// GetConfig returns the operator configuration
func (o *Operator) GetConfig() *Config {
	return o.config
}

// IsLeader returns true if this operator instance is the current leader
func (o *Operator) IsLeader() bool {
	if o.leaderElectionManager != nil {
		return o.leaderElectionManager.IsLeader()
	}
	// For operators without leader election manager, always consider as leader
	return true
}

// GetLeaderElectionDebugInfo returns debug information about leader election state
func (o *Operator) GetLeaderElectionDebugInfo() string {
	if o.leaderElectionManager != nil {
		return o.leaderElectionManager.GetDebugInfo()
	}
	return "no leader election manager"
}

// IsFollower returns true if this operator is ready but not the leader
func (o *Operator) IsFollower() bool {
	return o.IsReady() && !o.IsLeader()
}

// Stop gracefully stops the operator and releases leader election
func (o *Operator) Stop() error {
	if !o.started {
		return nil
	}

	if o.leaderElectionManager != nil {
		if err := o.leaderElectionManager.Resign(); err != nil {
			return fmt.Errorf("failed to resign from leader election: %w", err)
		}
	}

	o.started = false
	return nil
}

// GetID returns the operator's identity/ID
func (o *Operator) GetID() string {
	if o.leaderElectionManager != nil {
		return o.leaderElectionManager.GetIdentity()
	}
	return defaultHostname
}

// SimulateNetworkPartition simulates a network partition for testing
func (o *Operator) SimulateNetworkPartition(_ time.Duration) error {
	// This is a test helper method - in real scenarios this would simulate network issues
	if o.leaderElectionManager != nil {
		return o.leaderElectionManager.Resign()
	}
	return nil
}

// SimulateLeaseRenewalFailure simulates lease renewal failure for testing
func (o *Operator) SimulateLeaseRenewalFailure() error {
	// This is a test helper method - in real scenarios this would simulate lease renewal issues
	if o.leaderElectionManager != nil {
		return o.leaderElectionManager.Resign()
	}
	return nil
}

// GetHealthStatus returns the health status of the operator
func (o *Operator) GetHealthStatus() HealthStatus {
	leadership := "follower"
	if o.IsLeader() {
		leadership = "leader"
	}

	status := "unhealthy"
	if o.IsReady() {
		status = "healthy"
	}

	return HealthStatus{
		Leadership: leadership,
		Status:     status,
	}
}

// GetGinEngine returns the Gin HTTP engine
func (o *Operator) GetGinEngine() *gin.Engine {
	return o.ginEngine
}

// GetHealthChecker returns the health checker
func (o *Operator) GetHealthChecker() *server.HealthChecker {
	return o.healthChecker
}

// GetMetricsServer returns the metrics server
func (o *Operator) GetMetricsServer() *server.MetricsServer {
	return o.metricsServer
}

// GetWebhookServer returns the webhook server
func (o *Operator) GetWebhookServer() *server.WebhookServer {
	return o.webhookServer
}

// initializeCoreServices initializes the core business logic services
func (o *Operator) initializeCoreServices() error {
	// Initialize annotation parser
	o.annotationParser = annotations.NewAnnotationParser()

	// Initialize node classifier
	nodeClassifierConfig := &config.NodeClassifierConfig{
		CacheRefreshInterval: 30 * time.Second,
		CloudProvider:        "aws", // Default to AWS
		SpotLabels: map[string]string{
			"karpenter.sh/capacity-type": "spot",
		},
		OnDemandLabels: map[string]string{
			"karpenter.sh/capacity-type": "on-demand",
		},
	}
	o.nodeClassifier = config.NewNodeClassifierService(o.GetClient(), nodeClassifierConfig)

	// Initialize metrics collector
	o.metricsCollector = metrics.NewCollector()
	o.metricsCollector.SetNodeClassifier(o.nodeClassifier)

	// Initialize mutation handler
	o.mutationHandler = webhookMutate.NewMutationHandler(o.GetClient(), o.GetScheme())
	o.mutationHandler.SetNodeClassifier(o.nodeClassifier)

	return nil
}

// initializeHTTPServer initializes the HTTP server components
func (o *Operator) initializeHTTPServer() error {
	// Setup Gin engine - always use release mode to avoid debug messages
	gin.SetMode(gin.ReleaseMode)
	o.ginEngine = gin.New()
	o.ginEngine.Use(gin.Recovery())
	// Note: We don't use gin.Logger() as we have our own structured logging

	// Initialize health checker
	o.healthChecker = server.NewHealthChecker(o.Manager, o.kubeClient, o.namespace)

	// Initialize metrics server
	o.metricsServer = server.NewMetricsServer(o.metricsCollector)

	// Initialize webhook server (if enabled)
	if o.config.EnableWebhook {
		webhookConfig := server.WebhookServerConfig{
			Port:     o.config.WebhookPort,
			CertPath: o.config.WebhookCertName,
			KeyPath:  o.config.WebhookKeyName,
			ReadOnly: o.config.ReadOnlyMode,
		}
		o.webhookServer = server.NewWebhookServer(webhookConfig, o.mutationHandler, o.GetScheme())
	}

	// Setup HTTP routes
	o.setupHTTPRoutes()

	return nil
}

// setupHTTPRoutes configures the HTTP routes
func (o *Operator) setupHTTPRoutes() {
	// Health endpoints
	o.ginEngine.GET("/healthz", o.healthChecker.HealthzHandler)
	o.ginEngine.GET("/readyz", o.healthChecker.ReadyzHandler)

	// Metrics endpoints
	o.ginEngine.GET("/metrics", o.metricsServer.MetricsHandler)
	o.ginEngine.GET("/metrics/health", o.metricsServer.HealthMetricsHandler)

	// Webhook endpoints (if enabled)
	if o.webhookServer != nil {
		o.webhookServer.SetupRoutes(o.ginEngine)
	}
}

// setupControllers sets up the Kubernetes controllers
func (o *Operator) setupControllers() error {
	setupLog := ctrl.Log.WithName("setup")
	setupLog.Info("Setting up controllers")

	// Setup namespace filter for deployment controller
	namespaceFilterConfig := controllers.DefaultNamespaceFilterConfig()
	namespaceFilterConfig.RequiredAnnotations = map[string]string{
		"spotalis.io/enabled": "true",
	}
	namespaceFilter, err := controllers.NewNamespaceFilter(namespaceFilterConfig, o.GetClient())
	if err != nil {
		setupLog.Error(err, "Failed to create namespace filter, proceeding without namespace filtering")
		namespaceFilter = nil
	} else {
		setupLog.Info("Successfully created namespace filter")
	}

	// Setup Deployment controller
	deploymentController := &controllers.DeploymentReconciler{
		Client:            o.GetClient(),
		Scheme:            o.GetScheme(),
		AnnotationParser:  o.annotationParser,
		NodeClassifier:    o.nodeClassifier,
		NamespaceFilter:   namespaceFilter,
		ReconcileInterval: o.config.ReconcileInterval,
		MetricsCollector:  o.metricsCollector,
	}

	// Use unique controller names based on operator ID for testing
	deploymentName := "deployment"
	if operatorID := o.GetID(); operatorID != "" && operatorID != defaultHostname {
		deploymentName = fmt.Sprintf("deployment-%s", operatorID)
	}

	setupLog.Info("Setting up deployment controller", "name", deploymentName)
	if err := deploymentController.SetupWithManagerNamed(o.Manager, deploymentName); err != nil {
		return fmt.Errorf("failed to setup deployment controller: %w", err)
	}
	setupLog.Info("Successfully set up deployment controller")

	// Setup StatefulSet controller (reuse the same namespace filter)
	statefulSetController := &controllers.StatefulSetReconciler{
		Client:            o.GetClient(),
		Scheme:            o.GetScheme(),
		AnnotationParser:  o.annotationParser,
		NodeClassifier:    o.nodeClassifier,
		NamespaceFilter:   namespaceFilter,
		ReconcileInterval: o.config.ReconcileInterval,
		MetricsCollector:  o.metricsCollector,
	}

	// Use unique controller names based on operator ID for testing
	statefulSetName := "statefulset"
	if operatorID := o.GetID(); operatorID != "" && operatorID != defaultHostname {
		statefulSetName = fmt.Sprintf("statefulset-%s", operatorID)
	}

	setupLog.Info("Setting up statefulset controller", "name", statefulSetName)
	if err := statefulSetController.SetupWithManagerNamed(o.Manager, statefulSetName); err != nil {
		return fmt.Errorf("failed to setup statefulset controller: %w", err)
	}
	setupLog.Info("Successfully set up statefulset controller")

	setupLog.Info("All controllers set up successfully")
	return nil
}

// setupWebhooks sets up the admission webhooks
func (o *Operator) setupWebhooks() error {
	if o.webhookServer == nil {
		return fmt.Errorf("webhook server not initialized")
	}

	// Get the webhook server from manager
	hookServer := o.Manager.GetWebhookServer()

	// Register mutation webhook
	hookServer.Register("/mutate", &webhook.Admission{
		Handler: o.mutationHandler,
	})

	return nil
}

// setupHealthChecks configures health and readiness checks
func (o *Operator) setupHealthChecks() {
	// Add health checks to manager
	if err := o.AddHealthzCheck("healthz", o.healthChecker.GetHealthzChecker()); err != nil {
		ctrl.Log.Error(err, "failed to add healthz check")
	}

	if err := o.AddReadyzCheck("readyz", o.healthChecker.GetReadyzChecker()); err != nil {
		ctrl.Log.Error(err, "failed to add readyz check")
	}
}

// configFromEnv loads configuration from environment variables
func configFromEnv(config *Config) error {
	if addr := os.Getenv("METRICS_ADDR"); addr != "" {
		config.MetricsAddr = addr
	}
	if addr := os.Getenv("PROBE_ADDR"); addr != "" {
		config.ProbeAddr = addr
	}
	if addr := os.Getenv("WEBHOOK_ADDR"); addr != "" {
		config.WebhookAddr = addr
	}
	if ns := os.Getenv("NAMESPACE"); ns != "" {
		config.Namespace = ns
	}
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.LogLevel = level
	}
	if os.Getenv("DISABLE_LEADER_ELECTION") == envValueTrue {
		config.LeaderElection = false
	}
	if os.Getenv("DISABLE_WEBHOOK") == envValueTrue {
		config.EnableWebhook = false
	}
	if os.Getenv("READ_ONLY_MODE") == envValueTrue {
		config.ReadOnlyMode = true
	}
	if os.Getenv("ENABLE_PPROF") == envValueTrue {
		config.EnablePprof = true
	}

	return nil
}
