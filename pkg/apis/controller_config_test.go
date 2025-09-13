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

package apis

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ControllerConfiguration", func() {

	var config *ControllerConfiguration

	BeforeEach(func() {
		config = DefaultControllerConfiguration()
	})

	Describe("DefaultControllerConfiguration", func() {
		It("should create a configuration with reasonable defaults", func() {
			Expect(config.ResyncPeriod).To(Equal(30 * time.Second))
			Expect(config.Workers).To(Equal(5)) // Actual default is 5, not 1
			Expect(config.NamespaceSelector).To(BeNil())
			Expect(config.MetricsAddr).To(Equal(":8080"))
			Expect(config.HealthAddr).To(Equal(":8081"))
		})

		It("should have valid leader election defaults", func() {
			Expect(config.LeaderElection.Enabled).To(BeTrue())
			Expect(config.LeaderElection.LeaseDuration).To(Equal(15 * time.Second))
			Expect(config.LeaderElection.RenewDeadline).To(Equal(10 * time.Second))
			Expect(config.LeaderElection.RetryPeriod).To(Equal(2 * time.Second))
			Expect(config.LeaderElection.Name).To(Equal("spotalis-leader-election")) // Actual default
		})

		It("should have valid node label defaults", func() {
			Expect(config.NodeLabels.SpotLabels).ToNot(BeNil())
			Expect(config.NodeLabels.OnDemandLabels).ToNot(BeNil())
			Expect(config.NodeLabels.RefreshInterval).To(Equal(5 * time.Minute)) // Actual default is 5 minutes
		})
	})

	Describe("Validate", func() {
		It("should pass validation with default configuration", func() {
			err := config.Validate()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject invalid resync period", func() {
			config.ResyncPeriod = 1 * time.Second // Below minimum of 5s
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resyncPeriod")) // Actual error message
		})

		It("should reject invalid worker count", func() {
			config.Workers = 0
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("workers"))
		})

		It("should reject negative worker count", func() {
			config.Workers = -1
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("workers"))
		})

		It("should validate leader election configuration", func() {
			config.LeaderElection.LeaseDuration = 1 * time.Second
			config.LeaderElection.RenewDeadline = 2 * time.Second // Greater than lease duration
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("renewDeadline")) // Actual field name
		})

		It("should validate node label configuration", func() {
			config.NodeLabels.SpotLabels = nil
			config.NodeLabels.OnDemandLabels = nil // Both nil
			err := config.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spotLabels or onDemandLabels"))
		})
	})

	Describe("ShouldMonitorNamespace", func() {
		It("should monitor all namespaces when no selector is set", func() {
			config.NamespaceSelector = nil

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
					Labels: map[string]string{
						"app": "my-app",
					},
				},
			}

			Expect(config.ShouldMonitorNamespace(namespace)).To(BeTrue())
		})

		It("should monitor namespace that matches selector", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"managed-by": "spotalis",
				},
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-namespace",
					Labels: map[string]string{
						"managed-by": "spotalis",
					},
				},
			}

			Expect(config.ShouldMonitorNamespace(namespace)).To(BeTrue())
		})

		It("should not monitor namespace that does not match selector", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"managed-by": "spotalis",
				},
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unmanaged-namespace",
					Labels: map[string]string{
						"managed-by": "other",
					},
				},
			}

			Expect(config.ShouldMonitorNamespace(namespace)).To(BeFalse())
		})

		It("should handle namespace with no labels", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"managed-by": "spotalis",
				},
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-labels-namespace",
				},
			}

			Expect(config.ShouldMonitorNamespace(namespace)).To(BeFalse())
		})
	})

	Describe("GetNamespaceSelector", func() {
		It("should return everything selector when no selector is set", func() {
			config.NamespaceSelector = nil

			selector, err := config.GetNamespaceSelector()
			Expect(err).ToNot(HaveOccurred())
			Expect(selector.Empty()).To(BeTrue()) // Empty selector matches everything
		})

		It("should return valid selector when configured", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "production",
				},
			}

			selector, err := config.GetNamespaceSelector()
			Expect(err).ToNot(HaveOccurred())
			Expect(selector.String()).To(ContainSubstring("environment=production"))
		})

		It("should handle invalid selector expressions", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "invalid",
						Operator: "InvalidOperator",
						Values:   []string{"value"},
					},
				},
			}

			_, err := config.GetNamespaceSelector()
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Feature Management", func() {
		It("should enable and check features", func() {
			featureName := "advanced-scheduling"

			Expect(config.IsFeatureEnabled(featureName)).To(BeFalse())

			config.EnableFeature(featureName)
			Expect(config.IsFeatureEnabled(featureName)).To(BeTrue())
		})

		It("should disable features", func() {
			featureName := "advanced-scheduling"

			config.EnableFeature(featureName)
			Expect(config.IsFeatureEnabled(featureName)).To(BeTrue())

			config.DisableFeature(featureName)
			Expect(config.IsFeatureEnabled(featureName)).To(BeFalse())
		})

		It("should handle case-sensitive feature names", func() {
			config.EnableFeature("FeatureA")

			Expect(config.IsFeatureEnabled("FeatureA")).To(BeTrue())
			Expect(config.IsFeatureEnabled("featurea")).To(BeFalse())
			Expect(config.IsFeatureEnabled("FEATUREA")).To(BeFalse())
		})
	})

	Describe("GetEffectiveResyncPeriod", func() {
		It("should return configured resync period", func() {
			config.ResyncPeriod = 45 * time.Second

			effective := config.GetEffectiveResyncPeriod()
			Expect(effective).To(Equal(45 * time.Second))
		})

		It("should return minimum resync period when configured too low", func() {
			config.ResyncPeriod = 1 * time.Second

			effective := config.GetEffectiveResyncPeriod()
			Expect(effective).To(Equal(5 * time.Second)) // Minimum enforced
		})

		It("should return default when zero", func() {
			config.ResyncPeriod = 0

			effective := config.GetEffectiveResyncPeriod()
			Expect(effective).To(Equal(5 * time.Second)) // Minimum enforced when zero
		})
	})

	Describe("GetEffectiveWorkers", func() {
		It("should return configured worker count", func() {
			config.Workers = 5

			effective := config.GetEffectiveWorkers()
			Expect(effective).To(Equal(5))
		})

		It("should return minimum worker count when configured too low", func() {
			config.Workers = 0

			effective := config.GetEffectiveWorkers()
			Expect(effective).To(Equal(5)) // Default is 5, not 1
		})

		It("should cap maximum worker count", func() {
			config.Workers = 100

			effective := config.GetEffectiveWorkers()
			Expect(effective).To(Equal(50)) // Maximum enforced
		})
	})

	Describe("Clone", func() {
		It("should create a deep copy of the configuration", func() {
			config.ResyncPeriod = 60 * time.Second
			config.Workers = 3
			config.EnableFeature("test-feature")

			cloned := config.Clone()

			Expect(cloned).ToNot(BeIdenticalTo(config))
			Expect(cloned.ResyncPeriod).To(Equal(config.ResyncPeriod))
			Expect(cloned.Workers).To(Equal(config.Workers))
			Expect(cloned.IsFeatureEnabled("test-feature")).To(BeTrue())
		})

		It("should not affect original when modifying clone", func() {
			original := config.Clone()

			cloned := config.Clone()
			cloned.ResyncPeriod = 120 * time.Second
			cloned.Workers = 10

			Expect(config.ResyncPeriod).To(Equal(original.ResyncPeriod))
			Expect(config.Workers).To(Equal(original.Workers))
		})
	})

	Describe("GetConfigSummary", func() {
		It("should return comprehensive configuration summary", func() {
			config.ResyncPeriod = 45 * time.Second
			config.Workers = 3
			config.EnableFeature("advanced-scheduling")

			summary := config.GetConfigSummary()

			Expect(summary.ResyncPeriod).To(Equal(config.ResyncPeriod))
			Expect(summary.Workers).To(Equal(config.Workers))
			Expect(summary.LeaderElectionEnabled).To(Equal(config.LeaderElection.Enabled))
			Expect(summary.HasNamespaceSelector).To(BeFalse()) // No selector set
			Expect(summary.DryRun).To(Equal(config.DryRun))
		})

		It("should indicate namespace filtering when selector is set", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "production",
				},
			}

			summary := config.GetConfigSummary()
			Expect(summary.HasNamespaceSelector).To(BeTrue())
		})
	})

	Describe("LeaderElectionConfig Validation", func() {
		var leaderElection *LeaderElectionConfig

		BeforeEach(func() {
			leaderElection = &config.LeaderElection
		})

		It("should validate valid configuration", func() {
			err := leaderElection.Validate()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject when renew deadline exceeds lease duration", func() {
			leaderElection.LeaseDuration = 10 * time.Second
			leaderElection.RenewDeadline = 15 * time.Second

			err := leaderElection.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("renewDeadline")) // Field name
		})

		It("should reject when retry period exceeds renew deadline", func() {
			leaderElection.RenewDeadline = 5 * time.Second
			leaderElection.RetryPeriod = 8 * time.Second

			err := leaderElection.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("retryPeriod")) // Field name
		})

		It("should reject empty lease name", func() {
			leaderElection.Name = ""

			err := leaderElection.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("name"))
		})
	})

	Describe("NodeLabelConfig Validation", func() {
		var nodeLabels *NodeLabelConfig

		BeforeEach(func() {
			nodeLabels = &config.NodeLabels
		})

		It("should validate valid configuration", func() {
			err := nodeLabels.Validate()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should require at least one label configuration", func() {
			nodeLabels.SpotLabels = nil
			nodeLabels.OnDemandLabels = nil

			err := nodeLabels.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spotLabels or onDemandLabels"))
		})

		It("should accept spot labels only", func() {
			nodeLabels.SpotLabels = map[string]string{"type": "spot"}
			nodeLabels.OnDemandLabels = nil

			err := nodeLabels.Validate()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should accept on-demand labels only", func() {
			nodeLabels.SpotLabels = nil
			nodeLabels.OnDemandLabels = map[string]string{"type": "on-demand"}

			err := nodeLabels.Validate()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should accept empty maps if at least one is non-nil", func() {
			nodeLabels.SpotLabels = make(map[string]string)
			nodeLabels.OnDemandLabels = map[string]string{"type": "on-demand"}

			err := nodeLabels.Validate()
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Edge Cases", func() {
		It("should handle nil namespace in ShouldMonitorNamespace", func() {
			// Current implementation returns true when no selector (nil namespace doesn't matter)
			Expect(config.ShouldMonitorNamespace(nil)).To(BeTrue())
		})

		It("should handle malformed namespace selector gracefully", func() {
			config.NamespaceSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "",
						Operator: "In",
						Values:   []string{},
					},
				},
			}

			_, err := config.GetNamespaceSelector()
			Expect(err).To(HaveOccurred())
		})

		It("should handle extreme configuration values", func() {
			config.ResyncPeriod = 24 * time.Hour
			config.Workers = 1000

			// Should not panic
			effective := config.GetEffectiveResyncPeriod()
			Expect(effective).To(Equal(24 * time.Hour))

			effectiveWorkers := config.GetEffectiveWorkers()
			Expect(effectiveWorkers).To(Equal(50)) // Capped at maximum
		})
	})
})
