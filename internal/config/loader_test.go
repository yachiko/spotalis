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

package config

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConfigurationLoader", func() {
	var (
		tempDir    string
		configFile string
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "config-test-*")
		Expect(err).NotTo(HaveOccurred())

		configFile = filepath.Join(tempDir, "config.yaml")
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		// Clean up any environment variables we may have set
		envVars := []string{
			"SPOTALIS_NAMESPACE",
			"SPOTALIS_MAX_CONCURRENT_RECONCILES",
			"SPOTALIS_WEBHOOK_PORT",
			"SPOTALIS_WEBHOOK_ENABLED",
			"SPOTALIS_METRICS_BIND_ADDRESS",
			"SPOTALIS_LOG_LEVEL",
			"SPOTALIS_LOG_DEVELOPMENT",
			"SPOTALIS_LEADER_ELECTION_ENABLED",
			"SPOTALIS_RECONCILE_INTERVAL",
			"SPOTALIS_RECONCILE_TIMEOUT",
			"SPOTALIS_KUBE_TIMEOUT",
		}
		for _, env := range envVars {
			os.Unsetenv(env)
		}
	})

	Describe("NewConfigurationLoader", func() {
		It("should create a new configuration loader", func() {
			loader := NewConfigurationLoader()
			Expect(loader).NotTo(BeNil())
		})
	})

	Describe("DefaultConfiguration", func() {
		It("should return a valid default configuration", func() {
			config := DefaultConfiguration()
			Expect(config).NotTo(BeNil())

			// Test controller defaults
			Expect(config.Controller.Namespace).To(Equal("spotalis-system"))
			Expect(config.Controller.MaxConcurrentReconciles).To(Equal(10))
			Expect(config.Controller.ReconcileInterval).To(Equal(30 * time.Second))
			Expect(config.Controller.ReconcileTimeout).To(Equal(5 * time.Minute))
			Expect(config.Controller.EnableDeployments).To(BeTrue())
			Expect(config.Controller.EnableStatefulSets).To(BeTrue())
			Expect(config.Controller.ReadOnlyMode).To(BeFalse())

			// Test webhook defaults
			Expect(config.Webhook.Enabled).To(BeTrue())
			Expect(config.Webhook.Port).To(Equal(9443))
			Expect(config.Webhook.CertDir).To(Equal("/tmp/k8s-webhook-server/serving-certs"))
			Expect(config.Webhook.FailurePolicy).To(Equal("Fail"))

			// Test kubernetes defaults
			Expect(config.Kubernetes.QPS).To(Equal(float32(20)))
			Expect(config.Kubernetes.Burst).To(Equal(30))
			Expect(config.Kubernetes.Timeout).To(Equal(30 * time.Second))

			// Test leader election defaults
			Expect(config.LeaderElection.Enabled).To(BeTrue())
			Expect(config.LeaderElection.LeaseDuration).To(Equal(15 * time.Second))
			Expect(config.LeaderElection.RenewDeadline).To(Equal(10 * time.Second))
			Expect(config.LeaderElection.RetryPeriod).To(Equal(2 * time.Second)) // Test logging defaults
			Expect(config.Logging.Level).To(Equal("info"))
			Expect(config.Logging.Development).To(BeFalse())

			// Test metrics defaults
			Expect(config.Metrics.BindAddress).To(Equal(":8080"))
			Expect(config.Metrics.HealthBindAddress).To(Equal(":8081"))
			Expect(config.Metrics.CollectionInterval).To(Equal(30 * time.Second))

			// Test node classification defaults
			Expect(config.NodeClassification.CacheSize).To(Equal(1000))
			Expect(config.NodeClassification.CacheTTL).To(Equal(5 * time.Minute))
			Expect(config.NodeClassification.UpdateInterval).To(Equal(30 * time.Second))
		})
	})

	Describe("LoadConfiguration", func() {
		Context("when loading from a valid YAML file", func() {
			It("should load configuration correctly", func() {
				yamlContent := `
controller:
  namespace: "custom-namespace"
  maxConcurrentReconciles: 5
  reconcileInterval: "15s"
  enableDeployments: false
webhook:
  enabled: false
  port: 8443
logging:
  level: "debug"
  development: true
metrics:
  bindAddress: ":9090"
`
				err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
				Expect(err).NotTo(HaveOccurred())

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration(configFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(config).NotTo(BeNil())

				Expect(config.Controller.Namespace).To(Equal("custom-namespace"))
				Expect(config.Controller.MaxConcurrentReconciles).To(Equal(5))
				Expect(config.Controller.ReconcileInterval).To(Equal(15 * time.Second))
				Expect(config.Controller.EnableDeployments).To(BeFalse())
				Expect(config.Webhook.Enabled).To(BeFalse())
				Expect(config.Webhook.Port).To(Equal(8443))
				Expect(config.Logging.Level).To(Equal("debug"))
				Expect(config.Logging.Development).To(BeTrue())
				Expect(config.Metrics.BindAddress).To(Equal(":9090"))
			})

			It("should merge with defaults for missing fields", func() {
				yamlContent := `
controller:
  namespace: "test-namespace"
logging:
  level: "error"
`
				err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
				Expect(err).NotTo(HaveOccurred())

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration(configFile)
				Expect(err).NotTo(HaveOccurred())

				// Custom values should be set
				Expect(config.Controller.Namespace).To(Equal("test-namespace"))
				Expect(config.Logging.Level).To(Equal("error"))

				// Default values should be preserved for missing fields
				Expect(config.Controller.MaxConcurrentReconciles).To(Equal(10))
				Expect(config.Webhook.Port).To(Equal(9443))
				Expect(config.Kubernetes.QPS).To(Equal(float32(20)))
			})
		})

		Context("when file does not exist", func() {
			It("should load from environment and defaults only", func() {
				err := os.Setenv("SPOTALIS_NAMESPACE", "env-only-namespace")
				Expect(err).NotTo(HaveOccurred())

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration("")
				Expect(err).NotTo(HaveOccurred())

				Expect(config.Controller.Namespace).To(Equal("env-only-namespace"))
				// Should have defaults for other values
				Expect(config.Webhook.Port).To(Equal(9443))
			})
		})

		Context("when environment variables override file values", func() {
			It("should prioritize environment variables over file", func() {
				// Create config file
				yamlContent := `
controller:
  namespace: "file-namespace"
  maxConcurrentReconciles: 5
webhook:
  port: 8443
logging:
  level: "error"
`
				err := os.WriteFile(configFile, []byte(yamlContent), 0o600)
				Expect(err).NotTo(HaveOccurred())

				// Set environment variables that should override
				// Set environment variables
				envVars := map[string]string{
					"SPOTALIS_NAMESPACE":    "env-namespace",
					"SPOTALIS_WEBHOOK_PORT": "7443",
				}

				for key, value := range envVars {
					err := os.Setenv(key, value)
					Expect(err).NotTo(HaveOccurred())
				}

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration(configFile)
				Expect(err).NotTo(HaveOccurred())

				// Environment should override file
				Expect(config.Controller.Namespace).To(Equal("env-namespace"))
				Expect(config.Webhook.Port).To(Equal(7443))

				// File values should be used when env is not set
				Expect(config.Controller.MaxConcurrentReconciles).To(Equal(5))
				Expect(config.Logging.Level).To(Equal("error"))
			})
		})

		Context("when file has invalid YAML", func() {
			It("should return an error", func() {
				invalidYAML := `
controller:
  namespace: "test
  invalid: yaml content
    missing quote
`
				err := os.WriteFile(configFile, []byte(invalidYAML), 0o600)
				Expect(err).NotTo(HaveOccurred())

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration(configFile)
				Expect(err).To(HaveOccurred())
				Expect(config).To(BeNil())
			})
		})

		Context("when configuration validation fails", func() {
			It("should return an error for invalid values", func() {
				invalidConfig := `
controller:
  maxConcurrentReconciles: -1
webhook:
  port: -1
`
				err := os.WriteFile(configFile, []byte(invalidConfig), 0o600)
				Expect(err).NotTo(HaveOccurred())

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration(configFile)
				Expect(err).To(HaveOccurred())
				Expect(config).To(BeNil())
			})
		})
	})

	Describe("environment variable handling", func() {
		Context("when environment variables are set", func() {
			It("should load configuration from environment variables", func() {
				envVars := map[string]string{
					"SPOTALIS_NAMESPACE":                 "env-namespace",
					"SPOTALIS_MAX_CONCURRENT_RECONCILES": "15",
					"SPOTALIS_WEBHOOK_PORT":              "7443",
					"SPOTALIS_WEBHOOK_ENABLED":           "false",
					"SPOTALIS_LOG_LEVEL":                 "debug",
					"SPOTALIS_LOG_DEVELOPMENT":           "true",
					"SPOTALIS_METRICS_BIND_ADDRESS":      ":7080",
					"SPOTALIS_LEADER_ELECTION_ENABLED":   "false",
				}

				for key, value := range envVars {
					err := os.Setenv(key, value)
					Expect(err).NotTo(HaveOccurred())
				}

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration("")
				Expect(err).NotTo(HaveOccurred())
				Expect(config).NotTo(BeNil())

				Expect(config.Controller.Namespace).To(Equal("env-namespace"))
				Expect(config.Controller.MaxConcurrentReconciles).To(Equal(15))
				Expect(config.Webhook.Port).To(Equal(7443))
				Expect(config.Webhook.Enabled).To(BeFalse())
				Expect(config.Logging.Level).To(Equal("debug"))
				Expect(config.Logging.Development).To(BeTrue())
				Expect(config.Metrics.BindAddress).To(Equal(":7080"))
				Expect(config.LeaderElection.Enabled).To(BeFalse())
			})

			It("should handle duration environment variables", func() {
				envVars := map[string]string{
					"SPOTALIS_RECONCILE_INTERVAL": "45s",
					"SPOTALIS_RECONCILE_TIMEOUT":  "10m",
					"SPOTALIS_KUBE_TIMEOUT":       "60s",
				}

				for key, value := range envVars {
					err := os.Setenv(key, value)
					Expect(err).NotTo(HaveOccurred())
				}

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration("")
				Expect(err).NotTo(HaveOccurred())

				Expect(config.Controller.ReconcileInterval).To(Equal(45 * time.Second))
				Expect(config.Controller.ReconcileTimeout).To(Equal(10 * time.Minute))
				Expect(config.Kubernetes.Timeout).To(Equal(60 * time.Second))
			})

			It("should handle invalid environment variable values", func() {
				err := os.Setenv("SPOTALIS_WEBHOOK_PORT", "invalid-port")
				Expect(err).NotTo(HaveOccurred())

				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration("")
				Expect(err).To(HaveOccurred())
				Expect(config).To(BeNil())
			})
		})

		Context("when no environment variables are set", func() {
			It("should return default configuration", func() {
				loader := NewConfigurationLoader()
				config, err := loader.LoadConfiguration("")
				Expect(err).NotTo(HaveOccurred())
				Expect(config).NotTo(BeNil())

				// Should have default values
				Expect(config.Controller.Namespace).To(Equal("spotalis-system"))
				Expect(config.Webhook.Port).To(Equal(9443))
				Expect(config.Logging.Level).To(Equal("info"))
			})
		})
	})
})
