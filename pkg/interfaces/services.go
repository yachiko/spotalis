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

// Package interfaces defines the service interfaces used throughout the Spotalis operator.
// These interfaces enable dependency injection and improve testability.
package interfaces

import (
	"context"
	"time"

	"github.com/yachiko/spotalis/internal/config"
	"github.com/yachiko/spotalis/pkg/apis"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AnnotationParser defines the interface for parsing Spotalis annotations
type AnnotationParser interface {
	// IsEnabled checks if Spotalis is enabled for the given annotations
	IsEnabled(annotations map[string]string) bool

	// ParseConfiguration parses Spotalis configuration from annotations
	ParseConfiguration(annotations map[string]string) (*apis.WorkloadConfiguration, error)

	// GetSpotPercentage extracts the spot percentage from annotations
	GetSpotPercentage(annotations map[string]string) (int, error)

	// GetMinOnDemand extracts the minimum on-demand replicas from annotations
	GetMinOnDemand(annotations map[string]string) (int, error)
}

// NodeClassifier defines the interface for node classification services
type NodeClassifier interface {
	// ClassifyNode determines if a node is spot or on-demand
	ClassifyNode(nodeName string) (*apis.NodeClassification, error)

	// GetNodeCapacityType returns the capacity type of a node
	GetNodeCapacityType(nodeName string) (string, error)

	// RefreshNodeCache refreshes the internal node cache
	RefreshNodeCache(ctx context.Context) error

	// IsSpotNode checks if a node is a spot instance
	IsSpotNode(nodeName string) (bool, error)
}

// MetricsCollector defines the interface for metrics collection
type MetricsCollector interface {
	// RecordWorkloadManaged records when a workload is managed by Spotalis
	RecordWorkloadManaged(namespace, name, workloadType string)

	// RecordReplicaDistribution records the replica distribution metrics
	RecordReplicaDistribution(namespace, name string, spotReplicas, onDemandReplicas int)

	// RecordReconciliation records reconciliation metrics
	RecordReconciliation(namespace, name string, duration time.Duration, success bool)

	// RecordError records error metrics
	RecordError(namespace, name, errorType string)
}

// HealthChecker defines the interface for health checking
type HealthChecker interface {
	// IsHealthy returns true if the service is healthy
	IsHealthy() bool

	// IsReady returns true if the service is ready to serve requests
	IsReady() bool

	// GetHealthStatus returns detailed health status
	GetHealthStatus() map[string]interface{}
}

// LeaderElectionManager defines the interface for leader election
type LeaderElectionManager interface {
	// IsLeader returns true if this instance is the leader
	IsLeader() bool

	// GetIdentity returns the identity of this instance
	GetIdentity() string

	// GetLeadershipInfo returns information about current leadership
	GetLeadershipInfo() map[string]interface{}

	// Start starts the leader election process
	Start(ctx context.Context) error

	// Stop stops the leader election process
	Stop(ctx context.Context) error
}

// ControllerManager defines the interface for managing Kubernetes controllers
type ControllerManager interface {
	// SetupControllers sets up and registers all controllers
	SetupControllers() error

	// GetControllerStatus returns the status of all controllers
	GetControllerStatus() map[string]ControllerStatus

	// IsStarted returns true if the controller manager has been started
	IsStarted() bool

	// Start starts the controller manager
	Start(ctx context.Context) error

	// Stop stops the controller manager
	Stop(ctx context.Context) error
}

// ControllerStatus represents the status of a controller
type ControllerStatus struct {
	Name       string
	Enabled    bool
	Registered bool
	Reconciles int64
	LastError  error
}

// WebhookServer defines the interface for webhook admission control
type WebhookServer interface {
	// Start starts the webhook server
	Start(ctx context.Context) error

	// Stop stops the webhook server
	Stop(ctx context.Context) error

	// IsRunning returns true if the webhook server is running
	IsRunning() bool

	// GetPort returns the port the webhook server is listening on
	GetPort() int
}

// KubernetesClientProvider defines the interface for providing Kubernetes clients
type KubernetesClientProvider interface {
	// GetClient returns the controller-runtime client
	GetClient() client.Client

	// GetKubernetesClient returns the native Kubernetes client
	GetKubernetesClient() kubernetes.Interface

	// GetManager returns the controller-runtime manager
	GetManager() manager.Manager

	// GetRestConfig returns the REST configuration
	GetRestConfig() *rest.Config
}

// ConfigurationLoader defines the interface for loading configuration
type ConfigurationLoader interface {
	// LoadConfiguration loads the operator configuration
	LoadConfiguration() (*config.Configuration, error)

	// ValidateConfiguration validates the loaded configuration
	ValidateConfiguration(config *config.Configuration) error

	// GetConfigurationPath returns the path to the configuration file
	GetConfigurationPath() string
}
