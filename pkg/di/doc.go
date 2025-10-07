/*
Package di provides dependency injection infrastructure for Spotalis.

The di package implements a dependency injection container using Uber Dig,
managing service lifecycle and dependency resolution throughout the operator.

# Core Components

Application provides the main application lifecycle:
  - Initializes DI container
  - Registers all services
  - Resolves and starts operator
  - Manages graceful shutdown

Container wraps Uber Dig container:
  - Service registration (Provide)
  - Dependency resolution (Invoke)
  - Lifecycle management

ServiceRegistry handles service registration:
  - Configuration services
  - Controller services
  - Webhook services
  - Observability services

# Architecture Pattern

Spotalis uses constructor-based dependency injection:

	// Service constructor receives dependencies
	func NewDeploymentReconciler(
		client client.Client,
		parser annotations.AnnotationParser,
		classifier config.NodeClassifier,
		logger logging.Logger,
	) *DeploymentReconciler {
		return &DeploymentReconciler{
			client:     client,
			parser:     parser,
			classifier: classifier,
			logger:     logger,
		}
	}

	// DI container automatically resolves dependencies
	container.Provide(NewDeploymentReconciler)

# Usage

Basic application setup:

	import (
		"context"
		"github.com/ahoma/spotalis/pkg/di"
	)

	func main() {
		ctx := context.Background()

		// Create application with DI
		app, err := di.NewApplication(ctx)
		if err != nil {
			log.Fatal(err)
		}

		// Start operator (blocks until shutdown)
		if err := app.Start(ctx); err != nil {
			log.Fatal(err)
		}
	}

Advanced usage with custom services:

	// Create custom DI container
	container := di.NewContainer()

	// Register services
	container.Provide(NewCustomService)
	container.Provide(NewAnotherService)

	// Resolve and use services
	container.Invoke(func(svc *CustomService) error {
		return svc.Start()
	})

# Service Lifecycle

Services follow a consistent lifecycle:

 1. Configuration Loading
    - Load config.yaml
    - Apply environment variable overrides
    - Validate configuration

 2. Service Registration
    - Register configuration provider
    - Register Kubernetes client
    - Register annotation parser
    - Register node classifier
    - Register controllers
    - Register webhook
    - Register metrics collector
    - Register operator

 3. Dependency Resolution
    - DI container resolves dependency graph
    - Detects circular dependencies
    - Validates all dependencies satisfied

 4. Service Startup
    - Start HTTP servers (metrics, health, webhook)
    - Start controller manager
    - Start leader election (if enabled)
    - Start operator

 5. Graceful Shutdown
    - Handle SIGTERM/SIGINT
    - Stop accepting new requests
    - Complete in-flight reconciliations
    - Stop HTTP servers
    - Close Kubernetes client connections

# Configuration Provider

Configuration is provided through DI:

	container.Provide(func() (*config.SpotalisConfig, error) {
		return config.LoadConfiguration("config.yaml")
	})

	// Services receive configuration
	func NewControllerManager(cfg *config.SpotalisConfig) *ControllerManager {
		return &ControllerManager{
			workers: cfg.Controllers.Workers,
			// ...
		}
	}

# Error Handling

DI container validates dependencies at startup:

	app, err := di.NewApplication(ctx)
	if err != nil {
		// Dependency resolution failed
		// - Missing dependency
		// - Circular dependency
		// - Invalid configuration
		log.Fatal(err)
	}

Fail fast: All dependency errors are caught before operator starts.

# Testing

DI system supports testing with mock services:

	import (
		"testing"
		"github.com/ahoma/spotalis/pkg/di"
	)

	func TestApplication(t *testing.T) {
		container := di.NewContainer()

		// Provide mock services
		container.Provide(func() annotations.AnnotationParser {
			return &mockParser{}
		})

		// Test service resolution
		err := container.Invoke(func(parser annotations.AnnotationParser) {
			// Test with mock parser
		})
		if err != nil {
			t.Fatal(err)
		}
	}

See application_test.go for integration test examples.

# Dependency Graph

Current Spotalis dependency graph:

	Configuration (config.yaml)
	  ↓
	Logger (from config)
	  ↓
	Kubernetes Client (controller-runtime)
	  ↓
	├─ AnnotationParser
	├─ NodeClassifier
	├─ MetricsCollector
	│
	├─ Controllers
	│  ├─ DeploymentReconciler (parser, classifier, client)
	│  ├─ StatefulSetReconciler (parser, classifier, client)
	│  └─ ControllerManager (reconcilers, config)
	│
	├─ Webhook
	│  ├─ PodMutator (parser, classifier, client)
	│  └─ AdmissionHandler (mutator, config)
	│
	└─ Operator (manager, webhook, metrics, config)

# Benefits

Dependency injection provides:
  - **Testability**: Easy mocking of dependencies
  - **Modularity**: Services are loosely coupled
  - **Maintainability**: Clear dependency relationships
  - **Flexibility**: Easy to swap implementations
  - **Type Safety**: Compile-time dependency validation

# Migration from Manual Wiring

Old approach (manual dependency wiring):

	func main() {
		cfg := loadConfig()
		client := createClient()
		parser := annotations.NewParser()
		classifier := config.NewClassifier()
		reconciler := controllers.NewDeploymentReconciler(
			client, parser, classifier,
		)
		// ... more manual wiring
	}

New approach (DI-based):

	func main() {
		app, _ := di.NewApplication(ctx)
		app.Start(ctx)  // Dependencies resolved automatically
	}

# Related Packages

  - pkg/config: Configuration loading and management
  - pkg/operator: Main operator orchestration
  - pkg/controllers: Workload reconcilers
  - pkg/webhook: Admission webhook
  - pkg/logging: Structured logging
*/
package di
