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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/internal/annotations"
	"github.com/yachiko/spotalis/internal/config"
	"github.com/yachiko/spotalis/pkg/metrics"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("ControllerManager", func() {
	var (
		manager          *ControllerManager
		managerConfig    *ManagerConfig
		annotationParser *annotations.AnnotationParser
		nodeClassifier   *config.NodeClassifierService
		metricsCollector *metrics.Collector
	)

	BeforeEach(func() {
		annotationParser = annotations.NewAnnotationParser()
		metricsCollector = &metrics.Collector{} // Mock collector

		managerConfig = &ManagerConfig{
			MaxConcurrentReconciles: 5,
			ReconcileTimeout:        2 * time.Minute,
			ReconcileInterval:       15 * time.Second,
			EnableDeployments:       true,
			EnableStatefulSets:      true,
			EnableDaemonSets:        false,
			LeaderElection:          true,
			MetricsBindAddress:      ":8080",
			HealthProbeBindAddress:  ":8081",
			CacheSyncTimeout:        5 * time.Minute,
			GracefulShutdownTimeout: 15 * time.Second,
			WatchNamespaces:         []string{"default", "kube-system"},
			IgnoreNamespaces:        []string{"kube-public"},
		}

		// Initialize nodeClassifier with a mock client for each test
		nodeClassifier = config.NewNodeClassifierService(nil, &config.NodeClassifierConfig{
			SpotLabels: map[string]string{
				"node.kubernetes.io/instance-type": "spot",
			},
			OnDemandLabels: map[string]string{
				"node.kubernetes.io/instance-type": "on-demand",
			},
		})
	})

	Describe("NewControllerManager", func() {
		It("should create a new controller manager with proper initialization", func() {
			kubeClient := fake.NewSimpleClientset()

			newManager := NewControllerManager(
				nil, // manager will be nil for this test
				managerConfig,
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)

			Expect(newManager).ToNot(BeNil())
			Expect(newManager.config).To(Equal(managerConfig))
			Expect(newManager.kubeClient).To(Equal(kubeClient))
			Expect(newManager.annotationParser).To(Equal(annotationParser))
			Expect(newManager.nodeClassifier).To(Equal(nodeClassifier))
			Expect(newManager.metricsCollector).To(Equal(metricsCollector))
			Expect(newManager.controllersRegistered).ToNot(BeNil())
			Expect(newManager.started).To(BeFalse())
		})

		It("should handle nil config by using defaults", func() {
			kubeClient := fake.NewSimpleClientset()

			newManager := NewControllerManager(
				nil,
				nil, // nil config
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)

			Expect(newManager).ToNot(BeNil())
			Expect(newManager.config).ToNot(BeNil())
			// Should use default configuration
			Expect(newManager.config.MaxConcurrentReconciles).To(Equal(1)) // Conservative default
			Expect(newManager.config.ReconcileTimeout).To(Equal(5 * time.Minute))
		})
	})

	Describe("DefaultManagerConfig", func() {
		It("should return sensible defaults", func() {
			defaults := DefaultManagerConfig()

			Expect(defaults.MaxConcurrentReconciles).To(Equal(1)) // Conservative default
			Expect(defaults.ReconcileTimeout).To(Equal(5 * time.Minute))
			Expect(defaults.ReconcileInterval).To(Equal(5 * time.Minute)) // Updated to match new default
			Expect(defaults.EnableDeployments).To(BeTrue())
			Expect(defaults.EnableStatefulSets).To(BeTrue())
			Expect(defaults.EnableDaemonSets).To(BeFalse())
			Expect(defaults.LeaderElection).To(BeTrue())
			Expect(defaults.MetricsBindAddress).To(Equal(":8080"))
			Expect(defaults.HealthProbeBindAddress).To(Equal(":8081"))
			Expect(defaults.CacheSyncTimeout).To(Equal(10 * time.Minute))
			Expect(defaults.GracefulShutdownTimeout).To(Equal(30 * time.Second))
		})
	})

	Describe("ManagerConfig Validation", func() {
		BeforeEach(func() {
			kubeClient := fake.NewSimpleClientset()
			manager = NewControllerManager(
				nil,
				managerConfig,
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)
		})

		It("should validate a good configuration", func() {
			err := manager.ValidateConfiguration()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject zero or negative max concurrent reconciles", func() {
			manager.config.MaxConcurrentReconciles = 0
			err := manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("max concurrent reconciles"))

			manager.config.MaxConcurrentReconciles = -1
			err = manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
		})

		It("should reject zero or negative reconcile timeout", func() {
			manager.config.ReconcileTimeout = 0
			err := manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("reconcile timeout"))

			manager.config.ReconcileTimeout = -1 * time.Second
			err = manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
		})

		It("should reject zero or negative reconcile interval", func() {
			manager.config.ReconcileInterval = 0
			err := manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("reconcile interval"))

			manager.config.ReconcileInterval = -1 * time.Second
			err = manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
		})

		It("should reject configuration with no controllers enabled", func() {
			manager.config.EnableDeployments = false
			manager.config.EnableStatefulSets = false
			manager.config.EnableDaemonSets = false

			err := manager.ValidateConfiguration()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one controller"))
		})
	})

	Describe("GetManagerMetrics", func() {
		BeforeEach(func() {
			kubeClient := fake.NewSimpleClientset()
			manager = NewControllerManager(
				nil,
				managerConfig,
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)
		})

		It("should return comprehensive metrics", func() {
			// Set up some state
			manager.started = true
			manager.controllersRegistered["deployment"] = true
			manager.controllersRegistered["statefulset"] = true

			metrics := manager.GetManagerMetrics()

			Expect(metrics).To(HaveKey("started"))
			Expect(metrics).To(HaveKey("max_concurrent_reconciles"))
			Expect(metrics).To(HaveKey("reconcile_timeout_seconds"))
			Expect(metrics).To(HaveKey("reconcile_interval_seconds"))
			Expect(metrics).To(HaveKey("watch_namespaces"))
			Expect(metrics).To(HaveKey("ignore_namespaces"))
			Expect(metrics).To(HaveKey("controllers_enabled"))
			Expect(metrics).To(HaveKey("controllers_registered"))

			Expect(metrics["started"]).To(BeTrue())
			Expect(metrics["max_concurrent_reconciles"]).To(Equal(5))
			Expect(metrics["reconcile_timeout_seconds"]).To(Equal(120.0)) // 2 minutes
			Expect(metrics["reconcile_interval_seconds"]).To(Equal(15.0)) // 15 seconds

			controllersEnabled, ok := metrics["controllers_enabled"].(map[string]bool)
			Expect(ok).To(BeTrue(), "controllers_enabled should be a map[string]bool")
			Expect(controllersEnabled["deployments"]).To(BeTrue())
			Expect(controllersEnabled["statefulsets"]).To(BeTrue())
			Expect(controllersEnabled["daemonsets"]).To(BeFalse())

			Expect(metrics["controllers_registered"]).To(HaveLen(2))
		})
	})

	Describe("GetControllers", func() {
		BeforeEach(func() {
			kubeClient := fake.NewSimpleClientset()
			manager = NewControllerManager(
				nil,
				managerConfig,
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)

			// Set up controllers
			manager.deploymentController = &DeploymentReconciler{}
			manager.statefulSetController = &StatefulSetReconciler{}
		})

		It("should return deployment controller", func() {
			controller := manager.GetDeploymentController()
			Expect(controller).To(Equal(manager.deploymentController))
		})

		It("should return statefulset controller", func() {
			controller := manager.GetStatefulSetController()
			Expect(controller).To(Equal(manager.statefulSetController))
		})

		It("should return nil when controllers not set", func() {
			manager.deploymentController = nil
			manager.statefulSetController = nil

			Expect(manager.GetDeploymentController()).To(BeNil())
			Expect(manager.GetStatefulSetController()).To(BeNil())
		})
	})

	Describe("ManagerConfig Structure", func() {
		It("should have all required fields", func() {
			config := &ManagerConfig{}

			// Verify the struct has the expected fields by setting them
			config.MaxConcurrentReconciles = 5
			config.ReconcileTimeout = time.Minute
			config.ReconcileInterval = 30 * time.Second
			config.EnableDeployments = true
			config.EnableStatefulSets = true
			config.EnableDaemonSets = false
			config.LeaderElection = true
			config.MetricsBindAddress = ":8080"
			config.HealthProbeBindAddress = ":8081"
			config.CacheSyncTimeout = 10 * time.Minute
			config.GracefulShutdownTimeout = 30 * time.Second
			config.WatchNamespaces = []string{"default"}
			config.IgnoreNamespaces = []string{"kube-system"}

			// If we can set these without compilation errors, the struct is correct
			Expect(config.MaxConcurrentReconciles).To(Equal(5))
			Expect(config.EnableDeployments).To(BeTrue())
		})
	})

	Describe("Edge Cases", func() {
		It("should handle configuration changes", func() {
			kubeClient := fake.NewSimpleClientset()
			manager := NewControllerManager(
				nil,
				managerConfig,
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)

			originalMax := manager.config.MaxConcurrentReconciles
			originalInterval := manager.config.ReconcileInterval

			// Change configuration
			manager.config.MaxConcurrentReconciles = 20
			manager.config.ReconcileInterval = 60 * time.Second

			// Should reflect new configuration
			Expect(manager.config.MaxConcurrentReconciles).To(Equal(20))
			Expect(manager.config.ReconcileInterval).To(Equal(60 * time.Second))

			// Restore for consistency
			manager.config.MaxConcurrentReconciles = originalMax
			manager.config.ReconcileInterval = originalInterval
		})

		It("should handle nil dependencies gracefully", func() {
			// Create manager with some nil dependencies
			kubeClient := fake.NewSimpleClientset()
			manager := NewControllerManager(
				nil,
				managerConfig,
				kubeClient,
				nil, // nil annotation parser
				nil, // nil node classifier
				nil, // nil metrics collector
			)

			Expect(manager).ToNot(BeNil())
			Expect(manager.config).To(Equal(managerConfig))
			Expect(manager.annotationParser).To(BeNil())
			Expect(manager.nodeClassifier).To(BeNil())
			Expect(manager.metricsCollector).To(BeNil())
		})

		It("should handle concurrent access to state safely", func() {
			kubeClient := fake.NewSimpleClientset()
			manager := NewControllerManager(
				nil,
				managerConfig,
				kubeClient,
				annotationParser,
				nodeClassifier,
				metricsCollector,
			)

			// Test concurrent access to started flag (not the map to avoid race conditions)
			done := make(chan bool, 10)

			for i := 0; i < 10; i++ {
				go func(_ int) {
					defer func() { done <- true }()
					manager.started = true
					_ = manager.GetManagerMetrics()
				}(i)
			}

			// Wait for all goroutines
			for i := 0; i < 10; i++ {
				Eventually(done).Should(Receive(BeTrue()))
			}

			Expect(manager.started).To(BeTrue())
		})
	})
})
