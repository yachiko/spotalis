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

package controllers

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/ahoma/spotalis/internal/annotations"
	"github.com/ahoma/spotalis/internal/config"
	"github.com/ahoma/spotalis/pkg/metrics"
)

// ManagerConfig contains configuration for the controller manager
type ManagerConfig struct {
	// Controller configuration
	MaxConcurrentReconciles int
	ReconcileTimeout        time.Duration
	ReconcileInterval       time.Duration

	// Namespace filtering
	WatchNamespaces  []string
	IgnoreNamespaces []string

	// Resource filtering
	EnableDeployments  bool
	EnableStatefulSets bool
	EnableDaemonSets   bool

	// Performance configuration
	LeaderElection         bool
	MetricsBindAddress     string
	HealthProbeBindAddress string

	// Advanced configuration
	CacheSyncTimeout        time.Duration
	GracefulShutdownTimeout time.Duration
}

// DefaultManagerConfig returns the default manager configuration
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		MaxConcurrentReconciles: 1, // Low concurrency + single pod deletion = very gradual changes
		ReconcileTimeout:        5 * time.Minute,
		ReconcileInterval:       5 * time.Minute, // Increased from 30s for less aggressive rebalancing
		EnableDeployments:       true,
		EnableStatefulSets:      true,
		EnableDaemonSets:        false,
		LeaderElection:          true,
		MetricsBindAddress:      ":8080",
		HealthProbeBindAddress:  ":8081",
		CacheSyncTimeout:        10 * time.Minute,
		GracefulShutdownTimeout: 30 * time.Second,
	}
}

// ControllerManager manages all Spotalis controllers
type ControllerManager struct {
	manager manager.Manager
	config  *ManagerConfig

	// Dependencies
	kubeClient       kubernetes.Interface
	annotationParser *annotations.AnnotationParser
	nodeClassifier   *config.NodeClassifierService
	metricsCollector *metrics.Collector

	// Controllers
	deploymentController  *DeploymentReconciler
	statefulSetController *StatefulSetReconciler

	// State
	started               bool
	controllersRegistered map[string]bool
}

// NewControllerManager creates a new controller manager
func NewControllerManager(
	mgr manager.Manager,
	config *ManagerConfig,
	kubeClient kubernetes.Interface,
	annotationParser *annotations.AnnotationParser,
	nodeClassifier *config.NodeClassifierService,
	metricsCollector *metrics.Collector,
) *ControllerManager {
	if config == nil {
		config = DefaultManagerConfig()
	}

	return &ControllerManager{
		manager:               mgr,
		config:                config,
		kubeClient:            kubeClient,
		annotationParser:      annotationParser,
		nodeClassifier:        nodeClassifier,
		metricsCollector:      metricsCollector,
		controllersRegistered: make(map[string]bool),
	}
}

// SetupControllers sets up and registers all controllers with the manager
func (cm *ControllerManager) SetupControllers() error {
	if cm.config.EnableDeployments {
		if err := cm.setupDeploymentController(); err != nil {
			return fmt.Errorf("failed to setup deployment controller: %w", err)
		}
	}

	if cm.config.EnableStatefulSets {
		if err := cm.setupStatefulSetController(); err != nil {
			return fmt.Errorf("failed to setup statefulset controller: %w", err)
		}
	}

	return nil
}

// GetControllerStatus returns the status of all registered controllers
func (cm *ControllerManager) GetControllerStatus() map[string]ControllerStatus {
	status := make(map[string]ControllerStatus)

	if cm.deploymentController != nil {
		status["deployment"] = ControllerStatus{
			Name:       "deployment",
			Enabled:    cm.config.EnableDeployments,
			Registered: cm.controllersRegistered["deployment"],
			Reconciles: 0,   // TODO: Implement reconcile count tracking
			LastError:  nil, // TODO: Implement error tracking
		}
	}

	if cm.statefulSetController != nil {
		status["statefulset"] = ControllerStatus{
			Name:       "statefulset",
			Enabled:    cm.config.EnableStatefulSets,
			Registered: cm.controllersRegistered["statefulset"],
			Reconciles: 0,   // TODO: Implement reconcile count tracking
			LastError:  nil, // TODO: Implement error tracking
		}
	}

	return status
}

// ControllerStatus represents the status of a controller
type ControllerStatus struct {
	Name       string
	Enabled    bool
	Registered bool
	Reconciles int64
	LastError  error
}

// GetConfig returns the manager configuration
func (cm *ControllerManager) GetConfig() *ManagerConfig {
	return cm.config
}

// IsStarted returns true if the controller manager has been started
func (cm *ControllerManager) IsStarted() bool {
	return cm.started
}

// setupDeploymentController sets up the deployment controller
func (cm *ControllerManager) setupDeploymentController() error {
	cm.deploymentController = &DeploymentReconciler{
		Client:            cm.manager.GetClient(),
		Scheme:            cm.manager.GetScheme(),
		AnnotationParser:  cm.annotationParser,
		NodeClassifier:    cm.nodeClassifier,
		ReconcileInterval: cm.config.ReconcileInterval,
	}

	// Build controller with configuration
	controllerBuilder := ctrl.NewControllerManagedBy(cm.manager).
		For(&appsv1.Deployment{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: cm.config.MaxConcurrentReconciles,
		})

	// Complete the setup
	if err := controllerBuilder.Complete(cm.deploymentController); err != nil {
		return fmt.Errorf("failed to setup deployment controller: %w", err)
	}

	cm.controllersRegistered["deployment"] = true
	return nil
}

// setupStatefulSetController sets up the statefulset controller
func (cm *ControllerManager) setupStatefulSetController() error {
	cm.statefulSetController = &StatefulSetReconciler{
		Client:            cm.manager.GetClient(),
		Scheme:            cm.manager.GetScheme(),
		AnnotationParser:  cm.annotationParser,
		NodeClassifier:    cm.nodeClassifier,
		ReconcileInterval: cm.config.ReconcileInterval,
	}

	// Build controller with configuration
	controllerBuilder := ctrl.NewControllerManagedBy(cm.manager).
		For(&appsv1.StatefulSet{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: cm.config.MaxConcurrentReconciles,
		})

	// Complete the setup
	if err := controllerBuilder.Complete(cm.statefulSetController); err != nil {
		return fmt.Errorf("failed to setup statefulset controller: %w", err)
	}

	cm.controllersRegistered["statefulset"] = true
	return nil
}

// GetManagerMetrics returns metrics about the controller manager
func (cm *ControllerManager) GetManagerMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"started":                    cm.started,
		"max_concurrent_reconciles":  cm.config.MaxConcurrentReconciles,
		"reconcile_timeout_seconds":  cm.config.ReconcileTimeout.Seconds(),
		"reconcile_interval_seconds": cm.config.ReconcileInterval.Seconds(),
		"watch_namespaces":           cm.config.WatchNamespaces,
		"ignore_namespaces":          cm.config.IgnoreNamespaces,
		"controllers_enabled": map[string]bool{
			"deployments":  cm.config.EnableDeployments,
			"statefulsets": cm.config.EnableStatefulSets,
			"daemonsets":   cm.config.EnableDaemonSets,
		},
		"controllers_registered": cm.controllersRegistered,
	}

	// Add controller-specific metrics
	if cm.deploymentController != nil {
		metrics["deployment_reconciles"] = int64(0) // TODO: Implement reconcile count tracking
	}
	if cm.statefulSetController != nil {
		metrics["statefulset_reconciles"] = int64(0) // TODO: Implement reconcile count tracking
	}

	return metrics
}

// ValidateConfiguration validates the manager configuration
func (cm *ControllerManager) ValidateConfiguration() error {
	if cm.config.MaxConcurrentReconciles <= 0 {
		return fmt.Errorf("max concurrent reconciles must be positive")
	}

	if cm.config.ReconcileTimeout <= 0 {
		return fmt.Errorf("reconcile timeout must be positive")
	}

	if cm.config.ReconcileInterval <= 0 {
		return fmt.Errorf("reconcile interval must be positive")
	}

	if !cm.config.EnableDeployments && !cm.config.EnableStatefulSets && !cm.config.EnableDaemonSets {
		return fmt.Errorf("at least one controller type must be enabled")
	}

	return nil
}

// GetDeploymentController returns the deployment controller
func (cm *ControllerManager) GetDeploymentController() *DeploymentReconciler {
	return cm.deploymentController
}

// GetStatefulSetController returns the statefulset controller
func (cm *ControllerManager) GetStatefulSetController() *StatefulSetReconciler {
	return cm.statefulSetController
}

// SetStarted marks the controller manager as started
func (cm *ControllerManager) SetStarted(started bool) {
	cm.started = started
}
