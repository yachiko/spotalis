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

// Package di provides examples of how to integrate the dependency injection container
// with the Spotalis operator.
package di

import (
	"context"
	"fmt"
	"log"

	"github.com/ahoma/spotalis/pkg/operator"
)

// OperatorWithDI demonstrates how to use dependency injection with the Spotalis operator
type OperatorWithDI struct {
	registry  *BasicServiceRegistry
	container *Container
	operator  *operator.Operator
}

// NewOperatorWithDI creates a new operator instance using dependency injection
func NewOperatorWithDI(config *operator.Config) (*OperatorWithDI, error) {
	// Create the service registry
	registry := NewBasicServiceRegistry()

	// Register all services
	if err := registry.RegisterBasicServices(config); err != nil {
		return nil, fmt.Errorf("failed to register services: %w", err)
	}

	return &OperatorWithDI{
		registry:  registry,
		container: registry.GetContainer(),
	}, nil
}

// Start initializes and starts the operator using dependency injection
func (o *OperatorWithDI) Start(ctx context.Context) error {
	// Example of using dependency injection to wire up services
	return o.container.Invoke(func(deps *ServiceDependencies) error {
		log.Printf("Starting operator with dependency injection")
		log.Printf("Annotation parser: %v", deps.AnnotationParser != nil)
		log.Printf("Node classifier: %v", deps.NodeClassifier != nil)
		log.Printf("Metrics collector: %v", deps.MetricsCollector != nil)
		log.Printf("Kubernetes client: %v", deps.KubeClient != nil)
		log.Printf("Controller runtime client: %v", deps.Client != nil)
		log.Printf("Controller runtime manager: %v", deps.Manager != nil)

		// Here you would actually start the operator services
		// For now, just demonstrate that all dependencies are properly injected

		return nil
	})
}

// GetServiceDependencies resolves all service dependencies for external use
func (o *OperatorWithDI) GetServiceDependencies() (*ServiceDependencies, error) {
	return o.registry.ResolveServiceDependencies()
}

// ExampleUsage demonstrates how to use the dependency injection system
func ExampleUsage() {
	// Create operator configuration
	config := &operator.Config{
		MetricsAddr:   ":8080",
		ProbeAddr:     ":8081",
		WebhookPort:   9443,
		LogLevel:      "info",
		EnableWebhook: true,
	}

	// Create operator with DI
	operatorWithDI, err := NewOperatorWithDI(config)
	if err != nil {
		log.Fatalf("Failed to create operator with DI: %v", err)
	}

	// Start the operator
	ctx := context.Background()
	if err := operatorWithDI.Start(ctx); err != nil {
		log.Fatalf("Failed to start operator: %v", err)
	}

	// Example of accessing individual services
	deps, err := operatorWithDI.GetServiceDependencies()
	if err != nil {
		log.Fatalf("Failed to get service dependencies: %v", err)
	}

	// Use the services
	log.Printf("Services resolved successfully: %+v", deps != nil)
}

// ServiceInitializer demonstrates how to create a service that depends on other services
type ServiceInitializer struct {
	container *Container
}

// NewServiceInitializer creates a new service initializer
func NewServiceInitializer(container *Container) *ServiceInitializer {
	return &ServiceInitializer{
		container: container,
	}
}

// InitializeBusinessLogic demonstrates initializing business logic with DI
func (si *ServiceInitializer) InitializeBusinessLogic() error {
	return si.container.Invoke(func(deps *ServiceDependencies) error {
		// Example: Initialize annotation-based workload processing
		// This is where you would set up your actual business logic

		log.Printf("Initializing business logic with dependencies:")
		log.Printf("- Annotation Parser: %T", deps.AnnotationParser)
		log.Printf("- Node Classifier: %T", deps.NodeClassifier)
		log.Printf("- Metrics Collector: %T", deps.MetricsCollector)

		// Example business logic initialization would go here
		// For instance, setting up controllers, webhooks, etc.

		return nil
	})
}

// TestableOperator demonstrates how DI improves testability
type TestableOperator struct {
	annotationParser interface{}
	nodeClassifier   interface{}
	metricsCollector interface{}
}

// NewTestableOperator creates an operator that can be easily tested
func NewTestableOperator(container *Container) (*TestableOperator, error) {
	op := &TestableOperator{}

	// Use DI to inject dependencies
	err := container.Invoke(func(deps *ServiceDependencies) {
		op.annotationParser = deps.AnnotationParser
		op.nodeClassifier = deps.NodeClassifier
		op.metricsCollector = deps.MetricsCollector
	})

	if err != nil {
		return nil, fmt.Errorf("failed to inject dependencies: %w", err)
	}

	return op, nil
}

// ProcessWorkload demonstrates business logic that uses injected dependencies
func (to *TestableOperator) ProcessWorkload(workloadName string) error {
	// In a real implementation, you would use the injected services
	// This demonstrates how clean the code becomes with DI

	log.Printf("Processing workload %s with injected dependencies", workloadName)

	// Example usage (simplified):
	// config, err := to.annotationParser.ParseConfiguration(annotations)
	// nodeType, err := to.nodeClassifier.ClassifyNode(nodeName)
	// to.metricsCollector.RecordWorkloadManaged(namespace, name, workloadType)

	return nil
}
