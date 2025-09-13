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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Config", func() {
	Describe("OperatorConfig", func() {
		var config *OperatorConfig

		BeforeEach(func() {
			config = &OperatorConfig{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"environment": "production",
					},
				},
			}
		})

		Context("when creating a new OperatorConfig", func() {
			It("should have the correct fields", func() {
				Expect(config.NamespaceSelector).NotTo(BeNil())
				Expect(config.NamespaceSelector.MatchLabels).To(HaveKeyWithValue("environment", "production"))
			})

			It("should handle empty values", func() {
				emptyConfig := &OperatorConfig{}
				Expect(emptyConfig.NamespaceSelector).To(BeNil())
			})

			It("should handle namespace selector with multiple labels", func() {
				config.NamespaceSelector.MatchLabels["tier"] = "frontend"
				Expect(config.NamespaceSelector.MatchLabels).To(HaveLen(2))
				Expect(config.NamespaceSelector.MatchLabels).To(HaveKeyWithValue("environment", "production"))
				Expect(config.NamespaceSelector.MatchLabels).To(HaveKeyWithValue("tier", "frontend"))
			})
		})
	})

	Describe("Configuration struct validation", func() {
		Context("when working with Configuration types", func() {
			It("should create empty configuration structs", func() {
				config := &Configuration{}
				Expect(config).NotTo(BeNil())

				// Test that nested structs can be initialized
				config.Controller = ControllerConfig{}
				config.Webhook = WebhookConfig{}
				config.Kubernetes = KubernetesConfig{}
				config.LeaderElection = LeaderElectionConfig{}
				config.Logging = LoggingConfig{}
				config.Metrics = MetricsConfig{}
				config.NodeClassification = NodeClassificationConfig{}
				config.Namespaces = NamespacesConfig{}

				Expect(config.Controller).NotTo(BeNil())
				Expect(config.Webhook).NotTo(BeNil())
				Expect(config.Kubernetes).NotTo(BeNil())
				Expect(config.LeaderElection).NotTo(BeNil())
				Expect(config.Logging).NotTo(BeNil())
				Expect(config.Metrics).NotTo(BeNil())
				Expect(config.NodeClassification).NotTo(BeNil())
				Expect(config.Namespaces).NotTo(BeNil())
			})

			It("should handle nil Configuration gracefully", func() {
				var config *Configuration
				Expect(config).To(BeNil())
			})
		})

		Context("when working with individual config types", func() {
			It("should create ControllerConfig with basic fields", func() {
				controller := ControllerConfig{
					Namespace:               "spotalis-system",
					MaxConcurrentReconciles: 10,
					ReconcileInterval:       30 * time.Second,
					ReconcileTimeout:        5 * time.Minute,
					EnableDeployments:       true,
					EnableStatefulSets:      true,
					ReadOnlyMode:            false,
				}
				Expect(controller.Namespace).To(Equal("spotalis-system"))
				Expect(controller.MaxConcurrentReconciles).To(Equal(10))
				Expect(controller.ReconcileInterval).To(Equal(30 * time.Second))
				Expect(controller.ReconcileTimeout).To(Equal(5 * time.Minute))
				Expect(controller.EnableDeployments).To(BeTrue())
				Expect(controller.EnableStatefulSets).To(BeTrue())
				Expect(controller.ReadOnlyMode).To(BeFalse())
			})

			It("should create WebhookConfig with basic fields", func() {
				webhook := WebhookConfig{
					Enabled:          true,
					Port:             9443,
					CertDir:          "/tmp/certs",
					CertName:         "tls.crt",
					KeyName:          "tls.key",
					ServiceName:      "webhook-service",
					ServiceNamespace: "spotalis-system",
					ServicePath:      "/mutate",
					TLSMinVersion:    "1.2",
					FailurePolicy:    "Fail",
					TimeoutSeconds:   30,
				}
				Expect(webhook.Enabled).To(BeTrue())
				Expect(webhook.Port).To(Equal(9443))
				Expect(webhook.CertDir).To(Equal("/tmp/certs"))
				Expect(webhook.ServiceName).To(Equal("webhook-service"))
				Expect(webhook.FailurePolicy).To(Equal("Fail"))
			})

			It("should create KubernetesConfig with client settings", func() {
				k8s := KubernetesConfig{
					QPS:     50.0,
					Burst:   100,
					Timeout: 30 * time.Second,
				}
				Expect(k8s.QPS).To(Equal(float32(50.0)))
				Expect(k8s.Burst).To(Equal(100))
				Expect(k8s.Timeout).To(Equal(30 * time.Second))
			})

			It("should create LeaderElectionConfig with timing settings", func() {
				leader := LeaderElectionConfig{
					Enabled:       true,
					ID:            "spotalis-controller",
					LeaseName:     "spotalis-lease",
					LeaseDuration: 60 * time.Second,
					RenewDeadline: 40 * time.Second,
					RetryPeriod:   10 * time.Second,
				}
				Expect(leader.Enabled).To(BeTrue())
				Expect(leader.ID).To(Equal("spotalis-controller"))
				Expect(leader.LeaseDuration).To(Equal(60 * time.Second))
			})

			It("should create LoggingConfig with basic settings", func() {
				logging := LoggingConfig{
					Level:       "info",
					Development: false,
					EnablePprof: false,
				}
				Expect(logging.Level).To(Equal("info"))
				Expect(logging.Development).To(BeFalse())
				Expect(logging.EnablePprof).To(BeFalse())
			})

			It("should create MetricsConfig with metrics settings", func() {
				metrics := MetricsConfig{
					BindAddress:        ":8080",
					EnableProfiling:    false,
					ProfilingAddress:   ":6060",
					HealthBindAddress:  ":8081",
					CollectionInterval: 30 * time.Second,
				}
				Expect(metrics.BindAddress).To(Equal(":8080"))
				Expect(metrics.EnableProfiling).To(BeFalse())
				Expect(metrics.CollectionInterval).To(Equal(30 * time.Second))
			})

			It("should create NodeClassificationConfig with node settings", func() {
				nodeConfig := NodeClassificationConfig{
					CacheSize:      1000,
					CacheTTL:       5 * time.Minute,
					UpdateInterval: 30 * time.Second,
					AWSEnabled:     true,
					GCPEnabled:     false,
					AzureEnabled:   false,
				}
				Expect(nodeConfig.CacheSize).To(Equal(1000))
				Expect(nodeConfig.CacheTTL).To(Equal(5 * time.Minute))
				Expect(nodeConfig.AWSEnabled).To(BeTrue())
				Expect(nodeConfig.GCPEnabled).To(BeFalse())
			})

			It("should create NamespacesConfig with namespace patterns", func() {
				nsConfig := NamespacesConfig{
					Watch:  []string{"default", "prod-*"},
					Ignore: []string{"kube-*", "istio-system"},
				}
				Expect(nsConfig.Watch).To(ContainElements("default", "prod-*"))
				Expect(nsConfig.Ignore).To(ContainElements("kube-*", "istio-system"))
			})
		})
	})
})
