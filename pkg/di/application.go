package di

import (
	"context"
	"fmt"

	"github.com/ahoma/spotalis/pkg/config"
	"github.com/ahoma/spotalis/pkg/operator"
)

// ApplicationBuilder helps build and configure the Spotalis application using DI
type ApplicationBuilder struct {
	container  *Container
	configFile string
}

// NewApplicationBuilder creates a new application builder
func NewApplicationBuilder() *ApplicationBuilder {
	return &ApplicationBuilder{
		container: NewContainer(),
	}
}

// WithConfigFile sets the configuration file path
func (b *ApplicationBuilder) WithConfigFile(path string) *ApplicationBuilder {
	b.configFile = path
	return b
}

// Build builds the application with all dependencies configured
func (b *ApplicationBuilder) Build(_ context.Context) (*Application, error) {
	// Register configuration loader with optional file
	b.container.MustProvide(func() *config.Loader {
		loader := config.NewLoader()
		if b.configFile != "" {
			loader = loader.WithConfigFile(b.configFile)
		}
		return loader
	})

	// Register consolidated configuration
	b.container.MustProvide(func(loader *config.Loader) (*config.SpotalisConfig, error) {
		return loader.Load()
	})

	// Register all services using the service registry
	registry := NewServiceRegistry(b.container)
	if err := registry.RegisterAll(); err != nil {
		return nil, fmt.Errorf("failed to register services: %w", err)
	}

	// Register application with reference to container
	b.container.MustProvide(func(config *config.SpotalisConfig) *Application {
		return &Application{
			Config:    config,
			Container: b.container,
		}
	})

	// Build and return application
	var app *Application
	if err := b.container.Invoke(func(a *Application) {
		app = a
	}); err != nil {
		return nil, fmt.Errorf("failed to build application: %w", err)
	}

	return app, nil
}

// Application represents the main Spotalis application
type Application struct {
	Config    *config.SpotalisConfig
	Container *Container
}

// Start starts the application using DI-registered services
func (a *Application) Start(ctx context.Context) error {
	fmt.Printf("Starting Spotalis operator in namespace: %s\n", a.Config.Operator.Namespace)
	fmt.Printf("Leader election enabled: %v\n", a.Config.Operator.LeaderElection.Enabled)
	fmt.Printf("Max concurrent reconciles: %d\n", a.Config.Controllers.MaxConcurrentReconciles)
	fmt.Printf("Webhook enabled: %v\n", a.Config.Webhook.Enabled)
	fmt.Printf("Metrics enabled: %v\n", a.Config.Observability.Metrics.Enabled)

	// Get the operator from DI container and start it
	var startErr error
	if err := a.Container.Invoke(func(op *operator.Operator) {
		fmt.Println("✅ Operator resolved from DI container - Type:", fmt.Sprintf("%T", op))
		// Now actually start the operator!
		startErr = op.Start(ctx)
	}); err != nil {
		return fmt.Errorf("failed to resolve operator from DI container: %w", err)
	}

	if startErr != nil {
		return fmt.Errorf("failed to start operator: %w", startErr)
	}

	return nil
}

// Stop stops the application
func (a *Application) Stop(_ context.Context) error {
	fmt.Println("Stopping Spotalis operator")
	return nil
}

// GetConfig returns the application configuration
func (a *Application) GetConfig() *config.SpotalisConfig {
	return a.Config
}

// Example usage functions

// NewApplication creates a new application with default configuration
func NewApplication(ctx context.Context) (*Application, error) {
	return NewApplicationBuilder().Build(ctx)
}

// NewApplicationWithConfig creates a new application with configuration from file
func NewApplicationWithConfig(ctx context.Context, configFile string) (*Application, error) {
	return NewApplicationBuilder().WithConfigFile(configFile).Build(ctx)
}
