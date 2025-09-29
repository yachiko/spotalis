package di

import (
	"context"
	"fmt"

	"github.com/ahoma/spotalis/internal/annotations"
	internalConfig "github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/internal/server"
	"github.com/ahoma/spotalis/pkg/config"
	"github.com/ahoma/spotalis/pkg/controllers"
	"github.com/ahoma/spotalis/pkg/logging"
	"github.com/ahoma/spotalis/pkg/metrics"
	"github.com/ahoma/spotalis/pkg/operator"
	"github.com/ahoma/spotalis/pkg/webhook"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ServiceRegistry registers all Spotalis services with the DI container
type ServiceRegistry struct {
	container  *Container
	configFile string
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry(container *Container) *ServiceRegistry {
	return &ServiceRegistry{
		container: container,
	}
}

// WithConfigFile sets the configuration file path
func (r *ServiceRegistry) WithConfigFile(configFile string) *ServiceRegistry {
	r.configFile = configFile
	return r
}

// RegisterAll registers all core Spotalis services
func (r *ServiceRegistry) RegisterAll() error {
	// Register configuration first (required by other services)
	if err := r.RegisterConfiguration(); err != nil {
		return fmt.Errorf("failed to register configuration: %w", err)
	}

	// Register logger (depends on configuration)
	if err := r.RegisterLogger(); err != nil {
		return fmt.Errorf("failed to register logger: %w", err)
	}

	// Register core services
	if err := r.RegisterCoreServices(); err != nil {
		return fmt.Errorf("failed to register core services: %w", err)
	}

	// Register operator (main service)
	if err := r.RegisterOperator(); err != nil {
		return fmt.Errorf("failed to register operator: %w", err)
	}

	return nil
}

// RegisterConfiguration registers configuration-related services
func (r *ServiceRegistry) RegisterConfiguration() error {
	// Register configuration loader with optional config file
	r.container.MustProvide(func() *config.Loader {
		loader := config.NewLoader()
		if r.configFile != "" {
			loader = loader.WithConfigFile(r.configFile)
		}
		return loader
	})

	// Register consolidated configuration
	r.container.MustProvide(func(loader *config.Loader) (*config.SpotalisConfig, error) {
		return loader.Load()
	})

	return nil
}

// RegisterLogger registers structured JSON logger service
func (r *ServiceRegistry) RegisterLogger() error {
	// Register logger with configuration dependency
	r.container.MustProvide(func(config *config.SpotalisConfig) (*logging.Logger, error) {
		// Convert config logging settings to logger config
		logConfig := &logging.LoggingConfig{
			Level:       config.Observability.Logging.Level,
			Format:      config.Observability.Logging.Format,
			Output:      config.Observability.Logging.Output,
			AddCaller:   config.Observability.Logging.AddCaller,
			Development: config.Observability.Logging.Development,
		}

		return logging.NewLogger(logConfig)
	})

	return nil
}

// RegisterCoreServices registers annotation parser, metrics, and other core services
func (r *ServiceRegistry) RegisterCoreServices() error {
	// Register annotation parser
	r.container.MustProvide(annotations.NewAnnotationParser)

	// Register metrics collector
	r.container.MustProvide(metrics.NewCollector)

	// Register node classifier configuration from consolidated config
	r.container.MustProvide(func(_ *config.SpotalisConfig) *internalConfig.NodeClassifierConfig {
		// Convert consolidated config to node classifier config
		return &internalConfig.NodeClassifierConfig{
			// Add any node classifier specific configuration here
			// For now, use reasonable defaults
		}
	})

	// Register node classifier service (requires client and config)
	r.container.MustProvide(internalConfig.NewNodeClassifierService)

	return nil
}

// RegisterControllers registers controller-related services
func (r *ServiceRegistry) RegisterControllers() error {
	// Register controller manager
	r.container.MustProvide(func(
		mgr manager.Manager,
		config *config.SpotalisConfig,
		kubeClient kubernetes.Interface,
		annotationParser *annotations.AnnotationParser,
		nodeClassifier *internalConfig.NodeClassifierService,
		metricsCollector *metrics.Collector,
	) *controllers.ControllerManager {
		return controllers.NewControllerManager(
			mgr,
			&controllers.ManagerConfig{
				MaxConcurrentReconciles: config.Controllers.MaxConcurrentReconciles,
				ReconcileInterval:       config.Controllers.ReconcileInterval,
			},
			kubeClient,
			annotationParser,
			nodeClassifier,
			metricsCollector,
		)
	})

	return nil
}

// RegisterServers registers server-related services (webhook, metrics, health)
func (r *ServiceRegistry) RegisterServers() error {
	// Register webhook mutation handler
	r.container.MustProvide(webhook.NewMutationHandler)

	// Register webhook server - simplified for now
	r.container.MustProvide(func(
		mutationHandler *webhook.MutationHandler,
	) *webhook.MutationHandler {
		// For now, just return the mutation handler
		// The actual webhook server setup is complex and tied to the operator
		return mutationHandler
	})

	// Register health checker
	r.container.MustProvide(func(
		mgr manager.Manager,
		kubeClient kubernetes.Interface,
		config *config.SpotalisConfig,
	) *server.HealthChecker {
		if !config.Observability.Health.Enabled {
			return nil // Return nil if health checks are disabled
		}

		return server.NewHealthChecker(mgr, kubeClient, config.Operator.Namespace)
	})

	return nil
}

// RegisterOperator registers the main operator service
func (r *ServiceRegistry) RegisterOperator() error {
	// Register operator configuration from consolidated config
	r.container.MustProvide(func(config *config.SpotalisConfig) *operator.Config {
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

	// Register main operator using the existing constructor
	r.container.MustProvide(func(operatorConfig *operator.Config) (*operator.Operator, error) {
		// Use the existing operator constructor which handles all internal setup
		op, err := operator.NewOperator(operatorConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create operator: %w", err)
		}

		return op, nil
	})

	return nil
}

// InitializeOperator is a convenience function to set up and return a fully configured operator
func InitializeOperator(_ context.Context, configFile string) (*operator.Operator, error) {
	container := NewContainer()
	registry := NewServiceRegistry(container)

	// Override config loader to use specified file
	if configFile != "" {
		container.MustProvide(func() *config.Loader {
			return config.NewLoader().WithConfigFile(configFile)
		})
	}

	// Register all services
	if err := registry.RegisterAll(); err != nil {
		return nil, fmt.Errorf("failed to register services: %w", err)
	}

	// Register operator last (depends on other services)
	if err := registry.RegisterOperator(); err != nil {
		return nil, fmt.Errorf("failed to register operator: %w", err)
	}

	// Resolve and return the operator
	var op *operator.Operator
	if err := container.Invoke(func(operator *operator.Operator) {
		op = operator
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize operator: %w", err)
	}

	return op, nil
}
