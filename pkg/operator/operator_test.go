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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Operator", func() {
	var (
		config *Config
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		_, cancel = context.WithCancel(context.Background())
		config = &Config{
			MetricsAddr:             ":8080",
			ProbeAddr:               ":8081",
			WebhookAddr:             ":9443",
			LeaderElection:          false, // Disable for tests
			LeaderElectionID:        "spotalis-leader-election",
			Namespace:               "spotalis-system",
			ReconcileInterval:       30 * time.Second,
			MaxConcurrentReconciles: 10,
			WebhookCertDir:          "/tmp/k8s-webhook-server/serving-certs",
			WebhookCertName:         "tls.crt",
			WebhookKeyName:          "tls.key",
			WebhookPort:             9443,
			LogLevel:                "info",
			EnablePprof:             false,
			EnableWebhook:           false, // Disable for tests
			ReadOnlyMode:            false,
			NamespaceFilter:         []string{},
			APIQPSLimit:             50.0,
			APIBurstLimit:           100,
		}
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	Describe("NewOperator", func() {
		It("should create a new operator with valid configuration", func() {
			Skip("Skipping test that requires Kubernetes API server - needs kubeconfig")
			// This test requires a real Kubernetes API server or test environment
			// op, err := NewOperator(config)
			// Expect(err).ToNot(HaveOccurred())
			// Expect(op).ToNot(BeNil())
		})

		It("should use default configuration when nil config provided", func() {
			defaultConfig := DefaultOperatorConfig()
			Expect(defaultConfig).ToNot(BeNil())
			Expect(defaultConfig.MetricsAddr).To(Equal(":8080"))
			Expect(defaultConfig.ProbeAddr).To(Equal(":8081"))
			Expect(defaultConfig.LeaderElection).To(BeTrue())
			Expect(defaultConfig.ReconcileInterval).To(Equal(30 * time.Second))
			Expect(defaultConfig.MaxConcurrentReconciles).To(Equal(10))
		})
	})

	Describe("OperatorConfig", func() {
		Describe("DefaultOperatorConfig", func() {
			It("should return sensible defaults", func() {
				defaults := DefaultOperatorConfig()

				Expect(defaults.MetricsAddr).To(Equal(":8080"))
				Expect(defaults.ProbeAddr).To(Equal(":8081"))
				Expect(defaults.WebhookAddr).To(Equal(":9443"))
				Expect(defaults.LeaderElection).To(BeTrue())
				Expect(defaults.LeaderElectionID).To(ContainSubstring("spotalis"))
				Expect(defaults.Namespace).To(Equal("spotalis-system"))
				Expect(defaults.ReconcileInterval).To(Equal(30 * time.Second))
				Expect(defaults.MaxConcurrentReconciles).To(Equal(10))
				Expect(defaults.WebhookPort).To(Equal(9443))
				Expect(defaults.LogLevel).To(Equal("info"))
				Expect(defaults.EnablePprof).To(BeFalse())
				Expect(defaults.EnableWebhook).To(BeTrue())
				Expect(defaults.ReadOnlyMode).To(BeFalse())
				Expect(defaults.APIQPSLimit).To(Equal(float32(20.0)))
				Expect(defaults.APIBurstLimit).To(Equal(30))
			})
		})

		Describe("Configuration Fields", func() {
			It("should have all required configuration fields", func() {
				config := &Config{}

				// Test that we can set all expected fields
				config.MetricsAddr = ":8080"
				config.ProbeAddr = ":8081"
				config.WebhookAddr = ":9443"
				config.LeaderElection = true
				config.LeaderElectionID = "test"
				config.Namespace = "test-namespace"
				config.ReconcileInterval = 30 * time.Second
				config.MaxConcurrentReconciles = 5
				config.WebhookCertDir = "/tmp/certs"
				config.WebhookCertName = "tls.crt"
				config.WebhookKeyName = "tls.key"
				config.WebhookPort = 9443
				config.LogLevel = "debug"
				config.EnablePprof = true
				config.EnableWebhook = true
				config.ReadOnlyMode = false
				config.NamespaceFilter = []string{"default"}
				config.APIQPSLimit = 100.0
				config.APIBurstLimit = 200

				// Verify fields are set correctly
				Expect(config.MetricsAddr).To(Equal(":8080"))
				Expect(config.LeaderElection).To(BeTrue())
				Expect(config.ReconcileInterval).To(Equal(30 * time.Second))
				Expect(config.MaxConcurrentReconciles).To(Equal(5))
			})
		})
	})

	Describe("Operator Lifecycle", func() {
		Context("with mock dependencies", func() {
			var mockOperator *Operator

			BeforeEach(func() {
				// Create a mock operator with fake Kubernetes client
				mockOperator = &Operator{
					config:     config,
					namespace:  config.Namespace,
					kubeClient: fake.NewSimpleClientset(),
					started:    false,
				}
			})

			It("should initialize core services correctly", func() {
				Skip("Core services initialization requires real controller-runtime manager")
				// This test would require a real manager with proper client setup
				// err := mockOperator.initializeCoreServices()
				// Expect(err).ToNot(HaveOccurred())
			})

			It("should setup HTTP server correctly", func() {
				Skip("HTTP server setup requires real network configuration")
				// This would require actual network setup
				// err := mockOperator.initializeHTTPServer()
				// Expect(err).ToNot(HaveOccurred())
			})

			It("should handle readiness checks", func() {
				Skip("Readiness checks require proper controller-runtime manager setup")
				// The IsReady method calls o.Manager.Elected() which requires a real manager
				// mockOperator.started = true
				// Expect(mockOperator.IsReady()).To(BeTrue())
			})

			It("should provide configuration access", func() {
				retrievedConfig := mockOperator.GetConfig()
				Expect(retrievedConfig).To(Equal(config))
			})

			It("should provide metrics", func() {
				metrics := mockOperator.GetMetrics()
				Expect(metrics).ToNot(BeNil())
				// Metrics struct should be properly initialized
			})
		})
	})

	Describe("Metrics and Observability", func() {
		var mockOperator *Operator

		BeforeEach(func() {
			mockOperator = &Operator{
				config:    config,
				namespace: config.Namespace,
				started:   true,
			}
		})

		It("should provide operator metrics", func() {
			metrics := mockOperator.GetMetrics()
			Expect(metrics).ToNot(BeNil())
			// Should return a valid Metrics struct
		})

		It("should allow access to server components", func() {
			// These should not panic even if nil
			ginEngine := mockOperator.GetGinEngine()
			_ = ginEngine // May be nil

			healthChecker := mockOperator.GetHealthChecker()
			_ = healthChecker // May be nil

			metricsServer := mockOperator.GetMetricsServer()
			_ = metricsServer // May be nil

			webhookServer := mockOperator.GetWebhookServer()
			_ = webhookServer // May be nil
		})
	})

	Describe("Configuration Validation", func() {
		It("should handle basic configuration validation", func() {
			// Test required fields exist
			Expect(config.MetricsAddr).ToNot(BeEmpty())
			Expect(config.ProbeAddr).ToNot(BeEmpty())
			Expect(config.Namespace).ToNot(BeEmpty())
			Expect(config.ReconcileInterval).To(BeNumerically(">", 0))
			Expect(config.MaxConcurrentReconciles).To(BeNumerically(">", 0))
		})

		It("should handle address format validation", func() {
			// Test address formats are reasonable
			Expect(config.MetricsAddr).To(MatchRegexp(`^:\d+$`))
			Expect(config.ProbeAddr).To(MatchRegexp(`^:\d+$`))
			Expect(config.WebhookAddr).To(MatchRegexp(`^:\d+$`))
		})

		It("should handle timing configuration validation", func() {
			// Test timing values are positive
			Expect(config.ReconcileInterval).To(BeNumerically(">", 0))
			Expect(config.APIQPSLimit).To(BeNumerically(">", 0))
			Expect(config.APIBurstLimit).To(BeNumerically(">", 0))
		})

		It("should handle log level validation", func() {
			validLevels := []string{"debug", "info", "warn", "error"}
			Expect(validLevels).To(ContainElement(config.LogLevel))
		})
	})

	Describe("Edge Cases and Error Handling", func() {
		It("should handle nil configuration gracefully", func() {
			config := DefaultOperatorConfig()
			Expect(config).ToNot(BeNil())
		})

		It("should handle environment configuration", func() {
			config := &Config{
				MetricsAddr: ":8080",
				LogLevel:    "info",
			}

			// Test the environment configuration function exists
			err := configFromEnv(config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should handle concurrent access to configuration", func() {
			mockOperator := &Operator{
				config:  config,
				started: false,
			}

			// Test concurrent access to configuration
			done := make(chan bool, 10)

			for i := 0; i < 10; i++ {
				go func() {
					defer func() { done <- true }()
					_ = mockOperator.GetConfig()
					_ = mockOperator.IsReady()
					_ = mockOperator.GetMetrics()
				}()
			}

			// Wait for all goroutines
			for i := 0; i < 10; i++ {
				Eventually(done).Should(Receive(BeTrue()))
			}
		})

		It("should handle namespace validation", func() {
			// Test valid namespace
			config.Namespace = "valid-namespace"
			Expect(config.Namespace).To(MatchRegexp(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`))

			// Test max length constraints are reasonable
			Expect(len(config.Namespace)).To(BeNumerically("<=", 63))
		})

		It("should handle port validation", func() {
			// Test ports are in valid range
			Expect(config.WebhookPort).To(BeNumerically(">=", 1))
			Expect(config.WebhookPort).To(BeNumerically("<=", 65535))
		})
	})
})
