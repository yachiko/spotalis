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

// Package di provides service registration examples using Uber Dig.
package di

import (
	"time"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/metrics"
	"github.com/ahoma/spotalis/pkg/operator"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// BasicServiceRegistry demonstrates basic service registration with existing types
type BasicServiceRegistry struct {
	container *Container
}

// NewBasicServiceRegistry creates a simple service registry for demonstration
func NewBasicServiceRegistry() *BasicServiceRegistry {
	return &BasicServiceRegistry{
		container: NewContainer(),
	}
}

// RegisterBasicServices registers core services using existing concrete types
func (sr *BasicServiceRegistry) RegisterBasicServices(operatorConfig *operator.Config) error {
	// Register configuration
	sr.container.MustProvide(func() *operator.Config {
		return operatorConfig
	})

	// Register REST config
	sr.container.MustProvide(func() *rest.Config {
		return ctrl.GetConfigOrDie()
	})

	// Register runtime scheme
	sr.container.MustProvide(func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		return scheme
	})

	// Register Kubernetes client
	sr.container.MustProvide(func(restConfig *rest.Config) kubernetes.Interface {
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			panic(err) // In production, handle this better
		}
		return clientset
	})

	// Register controller-runtime manager
	sr.container.MustProvide(func(restConfig *rest.Config, scheme *runtime.Scheme) manager.Manager {
		mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
			Scheme: scheme,
		})
		if err != nil {
			panic(err) // In production, handle this better
		}
		return mgr
	})

	// Register controller-runtime client
	sr.container.MustProvide(func(mgr manager.Manager) client.Client {
		return mgr.GetClient()
	})

	// Register annotation parser
	sr.container.MustProvide(func() *annotations.AnnotationParser {
		return annotations.NewAnnotationParser()
	})

	// Register node classifier service
	sr.container.MustProvide(func(client client.Client) *config.NodeClassifierService {
		classifierConfig := &config.NodeClassifierConfig{
			CacheRefreshInterval: 5 * time.Minute,
			CloudProvider:        "aws",
			SpotLabels: map[string]string{
				"karpenter.sh/capacity-type": "spot",
			},
			OnDemandLabels: map[string]string{
				"karpenter.sh/capacity-type": "on-demand",
			},
		}
		return config.NewNodeClassifierService(client, classifierConfig)
	})

	// Register metrics collector
	sr.container.MustProvide(func() *metrics.Collector {
		return metrics.NewCollector()
	})

	return nil
}

// GetContainer returns the underlying DI container
func (sr *BasicServiceRegistry) GetContainer() *Container {
	return sr.container
}

// Example of how to use the DI container
func (sr *BasicServiceRegistry) ExampleUsage() error {
	// Example of invoking a function with injected dependencies
	return sr.container.Invoke(func(
		annotationParser *annotations.AnnotationParser,
		nodeClassifier *config.NodeClassifierService,
		metricsCollector *metrics.Collector,
		kubeClient kubernetes.Interface,
	) {
		// Use the injected dependencies
		// This is where your business logic would go

		// Example: Check if annotations indicate Spotalis is enabled
		// Note: We would need a mock object for this example
		// For now just demonstrate that dependencies are injected

		// This shows that all dependencies are properly injected
		_ = annotationParser
		_ = nodeClassifier
		_ = metricsCollector
		_ = kubeClient
	})
}

// ServiceDependencies demonstrates how to structure dependencies for injection
type ServiceDependencies struct {
	AnnotationParser *annotations.AnnotationParser
	NodeClassifier   *config.NodeClassifierService
	MetricsCollector *metrics.Collector
	KubeClient       kubernetes.Interface
	Client           client.Client
	Manager          manager.Manager
}

// ResolveServiceDependencies resolves all service dependencies at once
func (sr *BasicServiceRegistry) ResolveServiceDependencies() (*ServiceDependencies, error) {
	var deps ServiceDependencies
	err := sr.container.Invoke(func(
		annotationParser *annotations.AnnotationParser,
		nodeClassifier *config.NodeClassifierService,
		metricsCollector *metrics.Collector,
		kubeClient kubernetes.Interface,
		client client.Client,
		mgr manager.Manager,
	) {
		deps = ServiceDependencies{
			AnnotationParser: annotationParser,
			NodeClassifier:   nodeClassifier,
			MetricsCollector: metricsCollector,
			KubeClient:       kubeClient,
			Client:           client,
			Manager:          mgr,
		}
	})

	if err != nil {
		return nil, err
	}

	return &deps, nil
}
