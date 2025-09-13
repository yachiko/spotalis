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
)

var _ = Describe("ReplicaState", func() {

	Describe("CalculateDesiredDistribution", func() {
		Context("when configuration is disabled", func() {
			It("should move all replicas to on-demand", func() {
				state := &ReplicaState{
					TotalReplicas: 10,
				}
				config := WorkloadConfiguration{
					Enabled: false,
				}

				state.CalculateDesiredDistribution(config)

				Expect(state.DesiredOnDemand).To(Equal(int32(10)))
				Expect(state.DesiredSpot).To(Equal(int32(0)))
			})
		})

		Context("when configuration is enabled", func() {
			Context("with minimum on-demand requirement", func() {
				It("should respect minimum on-demand replicas", func() {
					state := &ReplicaState{
						TotalReplicas: 10,
					}
					config := WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    3,
						SpotPercentage: 80,
					}

					state.CalculateDesiredDistribution(config)

					Expect(state.DesiredOnDemand).To(Equal(int32(3))) // MinOnDemand constraint should be enforced
					Expect(state.DesiredSpot).To(Equal(int32(7)))     // Remaining replicas after MinOnDemand
				})

				It("should enforce minimum on-demand even with high spot percentage", func() {
					state := &ReplicaState{
						TotalReplicas: 5,
					}
					config := WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    4,
						SpotPercentage: 80,
					}

					state.CalculateDesiredDistribution(config)

					Expect(state.DesiredOnDemand).To(Equal(int32(4))) // Min enforced
					Expect(state.DesiredSpot).To(Equal(int32(1)))     // Only 1 remaining for spot
				})
			})

			Context("with spot percentage only", func() {
				It("should calculate spot replicas from percentage", func() {
					state := &ReplicaState{
						TotalReplicas: 10,
					}
					config := WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    0,
						SpotPercentage: 70,
					}

					state.CalculateDesiredDistribution(config)

					Expect(state.DesiredSpot).To(Equal(int32(7)))     // 70% of 10 = 7
					Expect(state.DesiredOnDemand).To(Equal(int32(3))) // Remaining
				})

				It("should handle zero spot percentage", func() {
					state := &ReplicaState{
						TotalReplicas: 10,
					}
					config := WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    0,
						SpotPercentage: 0,
					}

					state.CalculateDesiredDistribution(config)

					Expect(state.DesiredSpot).To(Equal(int32(0)))
					Expect(state.DesiredOnDemand).To(Equal(int32(10)))
				})

				It("should handle 100% spot percentage", func() {
					state := &ReplicaState{
						TotalReplicas: 10,
					}
					config := WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    0,
						SpotPercentage: 100,
					}

					state.CalculateDesiredDistribution(config)

					Expect(state.DesiredSpot).To(Equal(int32(10)))
					Expect(state.DesiredOnDemand).To(Equal(int32(0)))
				})
			})

			Context("with fractional calculations", func() {
				It("should handle percentage calculations that don't divide evenly", func() {
					state := &ReplicaState{
						TotalReplicas: 7,
					}
					config := WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    1,
						SpotPercentage: 50,
					}

					state.CalculateDesiredDistribution(config)

					// 50% of 7 = 3.5, truncated to 3
					Expect(state.DesiredSpot).To(Equal(int32(3)))
					Expect(state.DesiredOnDemand).To(Equal(int32(4))) // Remaining
				})
			})
		})
	})

	Describe("GetCurrentTotal", func() {
		It("should return sum of current on-demand and spot replicas", func() {
			state := &ReplicaState{
				CurrentOnDemand: 3,
				CurrentSpot:     7,
			}

			Expect(state.GetCurrentTotal()).To(Equal(int32(10)))
		})
	})

	Describe("GetDesiredTotal", func() {
		It("should return sum of desired on-demand and spot replicas", func() {
			state := &ReplicaState{
				DesiredOnDemand: 4,
				DesiredSpot:     6,
			}

			Expect(state.GetDesiredTotal()).To(Equal(int32(10)))
		})
	})

	Describe("NeedsReconciliation", func() {
		It("should return true when current state doesn't match desired", func() {
			state := &ReplicaState{
				CurrentOnDemand: 3,
				CurrentSpot:     5,
				DesiredOnDemand: 4,
				DesiredSpot:     6,
			}

			Expect(state.NeedsReconciliation()).To(BeTrue())
		})

		It("should return false when current state matches desired", func() {
			state := &ReplicaState{
				CurrentOnDemand: 4,
				CurrentSpot:     6,
				DesiredOnDemand: 4,
				DesiredSpot:     6,
			}

			Expect(state.NeedsReconciliation()).To(BeFalse())
		})
	})

	Describe("GetOnDemandDrift", func() {
		It("should return positive when excess on-demand replicas", func() {
			state := &ReplicaState{
				CurrentOnDemand: 6,
				DesiredOnDemand: 4,
			}

			Expect(state.GetOnDemandDrift()).To(Equal(int32(2)))
		})

		It("should return negative when deficit on-demand replicas", func() {
			state := &ReplicaState{
				CurrentOnDemand: 2,
				DesiredOnDemand: 5,
			}

			Expect(state.GetOnDemandDrift()).To(Equal(int32(-3)))
		})

		It("should return zero when balanced", func() {
			state := &ReplicaState{
				CurrentOnDemand: 4,
				DesiredOnDemand: 4,
			}

			Expect(state.GetOnDemandDrift()).To(Equal(int32(0)))
		})
	})

	Describe("GetSpotDrift", func() {
		It("should return positive when excess spot replicas", func() {
			state := &ReplicaState{
				CurrentSpot: 8,
				DesiredSpot: 5,
			}

			Expect(state.GetSpotDrift()).To(Equal(int32(3)))
		})

		It("should return negative when deficit spot replicas", func() {
			state := &ReplicaState{
				CurrentSpot: 3,
				DesiredSpot: 7,
			}

			Expect(state.GetSpotDrift()).To(Equal(int32(-4)))
		})
	})

	Describe("RequiresScaleUp", func() {
		It("should return true when current total is less than target", func() {
			state := &ReplicaState{
				TotalReplicas:   10,
				CurrentOnDemand: 3,
				CurrentSpot:     5,
			}

			Expect(state.RequiresScaleUp()).To(BeTrue())
		})

		It("should return false when current total matches or exceeds target", func() {
			state := &ReplicaState{
				TotalReplicas:   10,
				CurrentOnDemand: 4,
				CurrentSpot:     6,
			}

			Expect(state.RequiresScaleUp()).To(BeFalse())
		})
	})

	Describe("RequiresScaleDown", func() {
		It("should return true when current total exceeds target", func() {
			state := &ReplicaState{
				TotalReplicas:   10,
				CurrentOnDemand: 5,
				CurrentSpot:     7,
			}

			Expect(state.RequiresScaleDown()).To(BeTrue())
		})

		It("should return false when current total is less than or equal to target", func() {
			state := &ReplicaState{
				TotalReplicas:   10,
				CurrentOnDemand: 4,
				CurrentSpot:     6,
			}

			Expect(state.RequiresScaleDown()).To(BeFalse())
		})
	})

	Describe("GetNextAction", func() {
		Context("when no reconciliation is needed", func() {
			It("should return none action", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 4,
					CurrentSpot:     6,
					DesiredOnDemand: 4,
					DesiredSpot:     6,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionNone))
			})
		})

		Context("when scale up is needed", func() {
			It("should prefer scaling up on-demand when deficit", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 2,
					CurrentSpot:     6,
					DesiredOnDemand: 4,
					DesiredSpot:     6,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionScaleUpOnDemand))
			})

			It("should scale up spot when on-demand is satisfied", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 4,
					CurrentSpot:     4,
					DesiredOnDemand: 4,
					DesiredSpot:     6,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionScaleUpSpot))
			})
		})

		Context("when scale down is needed", func() {
			It("should prefer scaling down spot first", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 4,
					CurrentSpot:     8,
					DesiredOnDemand: 4,
					DesiredSpot:     6,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionScaleDownSpot))
			})

			It("should scale down on-demand when spot is at target", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 6,
					CurrentSpot:     6,
					DesiredOnDemand: 4,
					DesiredSpot:     6,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionScaleDownOnDemand))
			})
		})

		Context("when migration is needed", func() {
			It("should migrate from on-demand to spot", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 7,
					CurrentSpot:     3,
					DesiredOnDemand: 4,
					DesiredSpot:     6,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionMigrateToSpot))
			})

			It("should migrate from spot to on-demand", func() {
				state := &ReplicaState{
					TotalReplicas:   10,
					CurrentOnDemand: 2,
					CurrentSpot:     8,
					DesiredOnDemand: 6,
					DesiredSpot:     4,
				}

				Expect(state.GetNextAction()).To(Equal(ReplicaActionMigrateToOnDemand))
			})
		})
	})

	Describe("UpdateCurrentState", func() {
		It("should update current replica counts", func() {
			state := &ReplicaState{}

			state.UpdateCurrentState(5, 3)

			Expect(state.CurrentOnDemand).To(Equal(int32(5)))
			Expect(state.CurrentSpot).To(Equal(int32(3)))
		})
	})

	Describe("MarkReconciled", func() {
		It("should update last reconciled timestamp", func() {
			state := &ReplicaState{}
			before := time.Now()

			state.MarkReconciled()

			Expect(state.LastReconciled).To(BeTemporally(">=", before))
			Expect(state.LastReconciled).To(BeTemporally("<=", time.Now()))
		})
	})

	Describe("GetReconciliationAge", func() {
		It("should return zero for never reconciled state", func() {
			state := &ReplicaState{}

			Expect(state.GetReconciliationAge()).To(Equal(time.Duration(0)))
		})

		It("should return time since last reconciliation", func() {
			state := &ReplicaState{
				LastReconciled: time.Now().Add(-5 * time.Minute),
			}

			age := state.GetReconciliationAge()
			Expect(age).To(BeNumerically(">=", 5*time.Minute))
			Expect(age).To(BeNumerically("<", 6*time.Minute))
		})
	})

	Describe("IsStale", func() {
		It("should return true when reconciliation age exceeds max age", func() {
			state := &ReplicaState{
				LastReconciled: time.Now().Add(-10 * time.Minute),
			}

			Expect(state.IsStale(5 * time.Minute)).To(BeTrue())
		})

		It("should return false when reconciliation age is within max age", func() {
			state := &ReplicaState{
				LastReconciled: time.Now().Add(-3 * time.Minute),
			}

			Expect(state.IsStale(5 * time.Minute)).To(BeFalse())
		})

		It("should return true for never reconciled state", func() {
			state := &ReplicaState{}

			Expect(state.IsStale(1 * time.Minute)).To(BeTrue())
		})
	})

	Describe("GetSpotPercentage", func() {
		It("should calculate current spot percentage", func() {
			state := &ReplicaState{
				CurrentOnDemand: 3,
				CurrentSpot:     7,
			}

			Expect(state.GetSpotPercentage()).To(Equal(int32(70)))
		})

		It("should return zero when no replicas exist", func() {
			state := &ReplicaState{
				CurrentOnDemand: 0,
				CurrentSpot:     0,
			}

			Expect(state.GetSpotPercentage()).To(Equal(int32(0)))
		})

		It("should handle 100% spot case", func() {
			state := &ReplicaState{
				CurrentOnDemand: 0,
				CurrentSpot:     10,
			}

			Expect(state.GetSpotPercentage()).To(Equal(int32(100)))
		})
	})

	Describe("GetDesiredSpotPercentage", func() {
		It("should calculate desired spot percentage", func() {
			state := &ReplicaState{
				TotalReplicas: 10,
				DesiredSpot:   7,
			}

			Expect(state.GetDesiredSpotPercentage()).To(Equal(int32(70)))
		})

		It("should return zero when total replicas is zero", func() {
			state := &ReplicaState{
				TotalReplicas: 0,
				DesiredSpot:   5,
			}

			Expect(state.GetDesiredSpotPercentage()).To(Equal(int32(0)))
		})
	})

	Describe("WorkloadRef", func() {
		It("should properly store workload reference", func() {
			ref := corev1.ObjectReference{
				Kind:      "Deployment",
				Name:      "test-deployment",
				Namespace: "default",
			}

			state := &ReplicaState{
				WorkloadRef: ref,
			}

			Expect(state.WorkloadRef.Kind).To(Equal("Deployment"))
			Expect(state.WorkloadRef.Name).To(Equal("test-deployment"))
			Expect(state.WorkloadRef.Namespace).To(Equal("default"))
		})
	})
})
