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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WorkloadConfiguration", func() {

	Describe("Validate", func() {
		Context("when workload is disabled", func() {
			It("should not perform validation", func() {
				config := &WorkloadConfiguration{
					Enabled:        false,
					MinOnDemand:    -1,  // Invalid value
					SpotPercentage: 150, // Invalid value
				}

				err := config.Validate(10)
				Expect(err).To(BeNil())
			})
		})

		Context("when workload is enabled", func() {
			Context("with valid configuration", func() {
				It("should pass validation", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    2,
						SpotPercentage: 70,
					}

					err := config.Validate(10)
					Expect(err).To(BeNil())
				})

				It("should pass with only minOnDemand specified", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    3,
						SpotPercentage: 0,
					}

					err := config.Validate(10)
					Expect(err).To(BeNil())
				})

				It("should pass with only spotPercentage specified", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    0,
						SpotPercentage: 80,
					}

					err := config.Validate(10)
					Expect(err).To(BeNil())
				})

				It("should pass with boundary values", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    0,
						SpotPercentage: 100,
					}

					err := config.Validate(10)
					Expect(err).To(BeNil())
				})
			})

			Context("with invalid minOnDemand", func() {
				It("should fail when minOnDemand is negative", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    -1,
						SpotPercentage: 50,
					}

					err := config.Validate(10)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("minOnDemand must be >= 0"))
				})

				It("should fail when minOnDemand exceeds total replicas", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    15,
						SpotPercentage: 50,
					}

					err := config.Validate(10)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("minOnDemand (15) cannot exceed total replicas (10)"))
				})
			})

			Context("with invalid spotPercentage", func() {
				It("should fail when spotPercentage is negative", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    2,
						SpotPercentage: -10,
					}

					err := config.Validate(10)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("spotPercentage must be 0-100"))
				})

				It("should fail when spotPercentage exceeds 100", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    2,
						SpotPercentage: 150,
					}

					err := config.Validate(10)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("spotPercentage must be 0-100"))
				})
			})

			Context("when both minOnDemand and spotPercentage are zero", func() {
				It("should fail validation", func() {
					config := &WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    0,
						SpotPercentage: 0,
					}

					err := config.Validate(10)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("at least one of minOnDemand or spotPercentage must be specified"))
				})
			})
		})
	})

	Describe("IsSpotOptimized", func() {
		It("should return true when spotPercentage > 50", func() {
			config := &WorkloadConfiguration{
				SpotPercentage: 70,
			}
			Expect(config.IsSpotOptimized()).To(BeTrue())
		})

		It("should return false when spotPercentage <= 50", func() {
			config := &WorkloadConfiguration{
				SpotPercentage: 50,
			}
			Expect(config.IsSpotOptimized()).To(BeFalse())

			config.SpotPercentage = 30
			Expect(config.IsSpotOptimized()).To(BeFalse())
		})
	})

	Describe("IsOnDemandOnly", func() {
		It("should return true when spotPercentage is 0", func() {
			config := &WorkloadConfiguration{
				SpotPercentage: 0,
			}
			Expect(config.IsOnDemandOnly()).To(BeTrue())
		})

		It("should return false when spotPercentage > 0", func() {
			config := &WorkloadConfiguration{
				SpotPercentage: 20,
			}
			Expect(config.IsOnDemandOnly()).To(BeFalse())
		})
	})

	Describe("ParseFromAnnotations", func() {
		Context("when spotalis is disabled", func() {
			It("should return disabled configuration", func() {
				annotations := map[string]string{
					"spotalis.io/enabled": "false",
				}

				config, err := ParseFromAnnotations(annotations)
				Expect(err).To(BeNil())
				Expect(config.Enabled).To(BeFalse())
			})

			It("should return disabled when annotation is missing", func() {
				annotations := map[string]string{}

				config, err := ParseFromAnnotations(annotations)
				Expect(err).To(BeNil())
				Expect(config.Enabled).To(BeFalse())
			})
		})

		Context("when spotalis is enabled", func() {
			It("should parse all valid annotations", func() {
				annotations := map[string]string{
					"spotalis.io/enabled":          "true",
					"spotalis.io/min-on-demand":    "2",
					"spotalis.io/spot-percentage":  "70%",
					"spotalis.io/replica-strategy": "spread",
					"spotalis.io/scaling-policy":   "gradual",
					"spotalis.io/max-replicas":     "20",
				}

				config, err := ParseFromAnnotations(annotations)
				Expect(err).To(BeNil())
				Expect(config.Enabled).To(BeTrue())
				Expect(config.MinOnDemand).To(Equal(int32(2)))
				Expect(config.SpotPercentage).To(Equal(int32(70)))
				Expect(config.ReplicaStrategy).To(Equal("spread"))
				Expect(config.ScalingPolicy).To(Equal("gradual"))
				Expect(config.MaxReplicas).To(Equal(int32(20)))
			})

			It("should handle spot percentage without % symbol", func() {
				annotations := map[string]string{
					"spotalis.io/enabled":         "true",
					"spotalis.io/spot-percentage": "80",
				}

				config, err := ParseFromAnnotations(annotations)
				Expect(err).To(BeNil())
				Expect(config.SpotPercentage).To(Equal(int32(80)))
			})
		})

		Context("with invalid annotations", func() {
			It("should fail with invalid enabled value", func() {
				annotations := map[string]string{
					"spotalis.io/enabled": "invalid",
				}

				_, err := ParseFromAnnotations(annotations)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid spotalis.io/enabled value"))
			})

			It("should fail with invalid min-on-demand value", func() {
				annotations := map[string]string{
					"spotalis.io/enabled":       "true",
					"spotalis.io/min-on-demand": "invalid",
				}

				_, err := ParseFromAnnotations(annotations)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid spotalis.io/min-on-demand value"))
			})

			It("should fail with invalid spot-percentage value", func() {
				annotations := map[string]string{
					"spotalis.io/enabled":         "true",
					"spotalis.io/spot-percentage": "invalid%",
				}

				_, err := ParseFromAnnotations(annotations)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid spotalis.io/spot-percentage value"))
			})

			It("should fail with invalid max-replicas value", func() {
				annotations := map[string]string{
					"spotalis.io/enabled":      "true",
					"spotalis.io/max-replicas": "invalid",
				}

				_, err := ParseFromAnnotations(annotations)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid spotalis.io/max-replicas value"))
			})
		})
	})

	Describe("ToAnnotations", func() {
		It("should convert enabled configuration to annotations", func() {
			config := &WorkloadConfiguration{
				Enabled:         true,
				MinOnDemand:     2,
				SpotPercentage:  70,
				ReplicaStrategy: "spread",
				ScalingPolicy:   "gradual",
				MaxReplicas:     20,
			}

			annotations := config.ToAnnotations()
			Expect(annotations["spotalis.io/enabled"]).To(Equal("true"))
			Expect(annotations["spotalis.io/min-on-demand"]).To(Equal("2"))
			Expect(annotations["spotalis.io/spot-percentage"]).To(Equal("70%"))
			Expect(annotations["spotalis.io/replica-strategy"]).To(Equal("spread"))
			Expect(annotations["spotalis.io/scaling-policy"]).To(Equal("gradual"))
			Expect(annotations["spotalis.io/max-replicas"]).To(Equal("20"))
		})

		It("should convert disabled configuration to minimal annotations", func() {
			config := &WorkloadConfiguration{
				Enabled: false,
			}

			annotations := config.ToAnnotations()
			Expect(annotations["spotalis.io/enabled"]).To(Equal("false"))
			Expect(annotations).To(HaveLen(1))
		})

		It("should omit empty optional fields", func() {
			config := &WorkloadConfiguration{
				Enabled:        true,
				MinOnDemand:    1,
				SpotPercentage: 50,
			}

			annotations := config.ToAnnotations()
			Expect(annotations).ToNot(HaveKey("spotalis.io/replica-strategy"))
			Expect(annotations).ToNot(HaveKey("spotalis.io/scaling-policy"))
			Expect(annotations).ToNot(HaveKey("spotalis.io/max-replicas"))
		})
	})

	Describe("GetEffectiveMaxReplicas", func() {
		It("should return configured max replicas when set", func() {
			config := &WorkloadConfiguration{
				MaxReplicas: 15,
			}

			effective := config.GetEffectiveMaxReplicas(5)
			Expect(effective).To(Equal(int32(15)))
		})

		It("should return 3x current replicas when max replicas is not set", func() {
			config := &WorkloadConfiguration{
				MaxReplicas: 0,
			}

			effective := config.GetEffectiveMaxReplicas(5)
			Expect(effective).To(Equal(int32(15)))
		})

		It("should handle zero current replicas", func() {
			config := &WorkloadConfiguration{
				MaxReplicas: 0,
			}

			effective := config.GetEffectiveMaxReplicas(0)
			Expect(effective).To(Equal(int32(0)))
		})
	})
})
