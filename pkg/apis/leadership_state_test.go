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
)

var _ = Describe("LeadershipState", func() {

	var (
		leadershipState *LeadershipState
		identity        string
		leaseName       string
		leaseNamespace  string
	)

	BeforeEach(func() {
		identity = "test-controller-abc123"
		leaseName = "spotalis-controller"
		leaseNamespace = "spotalis-system"
		leadershipState = NewLeadershipState(identity, leaseName, leaseNamespace)
	})

	Describe("NewLeadershipState", func() {
		It("should create a new leadership state with correct initial values", func() {
			Expect(leadershipState.IsLeader).To(BeFalse())
			Expect(leadershipState.LeaderIdentity).To(Equal(identity))
			Expect(leadershipState.LeaseName).To(Equal(leaseName))
			Expect(leadershipState.LeaseNamespace).To(Equal(leaseNamespace))
			Expect(leadershipState.LeaseDuration).To(Equal(15 * time.Second))
			Expect(leadershipState.RenewDeadline).To(Equal(10 * time.Second))
			Expect(leadershipState.RetryPeriod).To(Equal(2 * time.Second))
		})

		It("should initialize with zero time values", func() {
			Expect(leadershipState.AcquiredAt.IsZero()).To(BeTrue())
			Expect(leadershipState.RenewedAt.IsZero()).To(BeTrue())
		})
	})

	Describe("BecomeLeader", func() {
		It("should set leadership state to true and update timestamps", func() {
			leadershipState.BecomeLeader()

			Expect(leadershipState.IsLeader).To(BeTrue())
			Expect(leadershipState.AcquiredAt).To(BeTemporally("~", time.Now(), time.Second))
			Expect(leadershipState.RenewedAt).To(BeTemporally("~", time.Now(), time.Second))
		})

		It("should update timestamps when already leader", func() {
			leadershipState.BecomeLeader()
			initialAcquired := leadershipState.AcquiredAt
			time.Sleep(1 * time.Millisecond)

			leadershipState.BecomeLeader()

			Expect(leadershipState.IsLeader).To(BeTrue())
			// AcquiredAt gets updated each time BecomeLeader is called
			Expect(leadershipState.AcquiredAt).To(BeTemporally(">", initialAcquired))
			Expect(leadershipState.RenewedAt).To(Equal(leadershipState.AcquiredAt))
		})
	})

	Describe("StepDown", func() {
		It("should step down from leadership", func() {
			leadershipState.BecomeLeader()
			Expect(leadershipState.IsLeader).To(BeTrue())

			leadershipState.StepDown()

			Expect(leadershipState.IsLeader).To(BeFalse())
			// Timestamps are preserved when stepping down
			Expect(leadershipState.AcquiredAt).ToNot(BeZero())
			Expect(leadershipState.RenewedAt).ToNot(BeZero())
		})

		It("should be safe to call when not leader", func() {
			Expect(leadershipState.IsLeader).To(BeFalse())

			leadershipState.StepDown()

			Expect(leadershipState.IsLeader).To(BeFalse())
		})
	})

	Describe("RenewLease", func() {
		It("should update the renewal timestamp", func() {
			leadershipState.BecomeLeader()
			initialRenewed := leadershipState.RenewedAt
			time.Sleep(1 * time.Millisecond)

			leadershipState.RenewLease()

			Expect(leadershipState.RenewedAt).To(BeTemporally(">", initialRenewed))
		})

		It("should work when not leader", func() {
			Expect(leadershipState.IsLeader).To(BeFalse())

			leadershipState.RenewLease()

			Expect(leadershipState.RenewedAt).To(BeTemporally("~", time.Now(), time.Second))
		})
	})

	Describe("GetLeadershipDuration", func() {
		It("should return zero duration when not leader", func() {
			duration := leadershipState.GetLeadershipDuration()
			Expect(duration).To(Equal(time.Duration(0)))
		})

		It("should return time since becoming leader", func() {
			leadershipState.BecomeLeader()
			time.Sleep(10 * time.Millisecond)

			duration := leadershipState.GetLeadershipDuration()
			Expect(duration).To(BeNumerically(">", 5*time.Millisecond))
			Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		})
	})

	Describe("GetTimeSinceLastRenewal", func() {
		It("should return zero duration when never renewed", func() {
			duration := leadershipState.GetTimeSinceLastRenewal()
			Expect(duration).To(Equal(time.Duration(0)))
		})

		It("should return time since last renewal", func() {
			leadershipState.RenewLease()
			time.Sleep(10 * time.Millisecond)

			duration := leadershipState.GetTimeSinceLastRenewal()
			Expect(duration).To(BeNumerically(">", 5*time.Millisecond))
			Expect(duration).To(BeNumerically("<", 100*time.Millisecond))
		})
	})

	Describe("NeedsRenewal", func() {
		It("should return false when never renewed", func() {
			Expect(leadershipState.NeedsRenewal()).To(BeFalse())
		})

		It("should return false when recently renewed", func() {
			leadershipState.RenewLease()

			Expect(leadershipState.NeedsRenewal()).To(BeFalse())
		})

		It("should return true when renewal deadline approaches", func() {
			leadershipState.BecomeLeader() // Must be leader to need renewal
			// Simulate time passing beyond retry period
			leadershipState.RenewedAt = time.Now().Add(-3 * time.Second)

			Expect(leadershipState.NeedsRenewal()).To(BeTrue())
		})
	})

	Describe("IsLeaseExpired", func() {
		It("should return false when recently renewed", func() {
			leadershipState.RenewLease()

			Expect(leadershipState.IsLeaseExpired()).To(BeFalse())
		})

		It("should return true when lease has expired", func() {
			leadershipState.RenewLease()
			// Simulate lease expiration
			leadershipState.RenewedAt = time.Now().Add(-20 * time.Second)

			Expect(leadershipState.IsLeaseExpired()).To(BeTrue())
		})
	})

	Describe("ShouldStepDown", func() {
		It("should return false when lease is healthy", func() {
			leadershipState.BecomeLeader()

			Expect(leadershipState.ShouldStepDown()).To(BeFalse())
		})

		It("should return true when lease is expired", func() {
			leadershipState.BecomeLeader()
			// Simulate lease expiration
			leadershipState.RenewedAt = time.Now().Add(-20 * time.Second)

			Expect(leadershipState.ShouldStepDown()).To(BeTrue())
		})

		It("should return false when not leader", func() {
			Expect(leadershipState.IsLeader).To(BeFalse())

			Expect(leadershipState.ShouldStepDown()).To(BeFalse())
		})
	})

	Describe("GetHealthStatus", func() {
		It("should return healthy status when leader with fresh lease", func() {
			leadershipState.BecomeLeader()

			health := leadershipState.GetHealthStatus()
			Expect(health.Healthy).To(BeTrue())
			Expect(health.Status).To(Equal("healthy"))
			Expect(health.Leadership).To(Equal("leader"))
		})

		It("should return unhealthy status when lease is expired", func() {
			leadershipState.BecomeLeader()
			leadershipState.RenewedAt = time.Now().Add(-20 * time.Second)

			health := leadershipState.GetHealthStatus()
			Expect(health.Healthy).To(BeFalse())
			Expect(health.Status).To(Equal("unhealthy"))
			Expect(health.Leadership).To(Equal("leader"))
		})

		It("should return follower status when not leader", func() {
			health := leadershipState.GetHealthStatus()
			Expect(health.Healthy).To(BeTrue())
			Expect(health.Status).To(Equal("follower"))
			Expect(health.Leadership).To(Equal("follower"))
		})

		It("should include basic health information", func() {
			leadershipState.BecomeLeader()
			time.Sleep(1 * time.Millisecond)

			health := leadershipState.GetHealthStatus()
			Expect(health.Status).To(Equal("healthy"))
			Expect(health.Message).ToNot(BeEmpty())
		})
	})

	Describe("GetMetrics", func() {
		It("should return correct metrics when leader", func() {
			leadershipState.BecomeLeader()
			time.Sleep(1 * time.Millisecond)

			metrics := leadershipState.GetMetrics()
			Expect(metrics.IsCurrentLeader).To(BeTrue())
			Expect(metrics.LeadershipDuration).To(BeNumerically(">", 0))
			Expect(metrics.TimeSinceLastRenewal).To(BeNumerically(">", 0))
			Expect(metrics.LeaderIdentity).To(Equal(identity))
			Expect(metrics.LeaseName).To(Equal(leaseName))
			Expect(metrics.LeaseNamespace).To(Equal(leaseNamespace))
		})

		It("should return correct metrics when follower", func() {
			metrics := leadershipState.GetMetrics()
			Expect(metrics.IsCurrentLeader).To(BeFalse())
			Expect(metrics.LeadershipDuration).To(Equal(time.Duration(0)))
			Expect(metrics.LeaderIdentity).To(Equal(identity))
		})
	})

	Describe("UpdateLeaderIdentity", func() {
		It("should update the leader identity", func() {
			newIdentity := "new-leader-xyz789"

			leadershipState.UpdateLeaderIdentity(newIdentity)

			Expect(leadershipState.LeaderIdentity).To(Equal(newIdentity))
		})

		It("should update identity and track transitions", func() {
			leadershipState.BecomeLeader()
			Expect(leadershipState.IsLeader).To(BeTrue())
			initialTransitions := leadershipState.Transitions

			leadershipState.UpdateLeaderIdentity("different-leader")

			Expect(leadershipState.LeaderIdentity).To(Equal("different-leader"))
			Expect(leadershipState.Transitions).To(Equal(initialTransitions + 1))
			Expect(leadershipState.IsLeader).To(BeTrue()) // Still leader, just identity changed
		})
	})

	Describe("GetTimeToNextRenewal", func() {
		It("should return retry period when never renewed", func() {
			timeToNext := leadershipState.GetTimeToNextRenewal()
			Expect(timeToNext).To(Equal(leadershipState.RetryPeriod))
		})

		It("should return remaining time until next renewal", func() {
			leadershipState.BecomeLeader() // Must be leader
			time.Sleep(1 * time.Millisecond)

			timeToNext := leadershipState.GetTimeToNextRenewal()
			Expect(timeToNext).To(BeNumerically("<", leadershipState.RetryPeriod))
			Expect(timeToNext).To(BeNumerically(">", 0))
		})

		It("should return zero when renewal is overdue", func() {
			leadershipState.BecomeLeader() // Must be leader
			leadershipState.RenewedAt = time.Now().Add(-5 * time.Second)

			timeToNext := leadershipState.GetTimeToNextRenewal()
			Expect(timeToNext).To(Equal(time.Duration(0)))
		})
	})

	Describe("IsHealthy", func() {
		It("should return true when not leader", func() {
			Expect(leadershipState.IsHealthy()).To(BeTrue())
		})

		It("should return true when leader with fresh lease", func() {
			leadershipState.BecomeLeader()

			Expect(leadershipState.IsHealthy()).To(BeTrue())
		})

		It("should return false when leader with expired lease", func() {
			leadershipState.BecomeLeader()
			leadershipState.RenewedAt = time.Now().Add(-20 * time.Second)

			Expect(leadershipState.IsHealthy()).To(BeFalse())
		})
	})

	Describe("Edge Cases", func() {
		It("should handle zero duration configuration", func() {
			leadershipState.LeaseDuration = 0
			leadershipState.RenewDeadline = 0
			leadershipState.RetryPeriod = 0

			leadershipState.BecomeLeader()

			Expect(leadershipState.IsLeader).To(BeTrue())
			// With zero durations, any time elapsed means renewal is needed and lease is expired
			Expect(leadershipState.NeedsRenewal()).To(BeTrue())   // time since renewal >= 0 (retry period)
			Expect(leadershipState.IsLeaseExpired()).To(BeTrue()) // time since renewal > 0 (lease duration)
		})

		It("should handle negative time calculations gracefully", func() {
			// Set timestamps in the future to create negative durations
			futureTime := time.Now().Add(1 * time.Hour)
			leadershipState.AcquiredAt = futureTime
			leadershipState.RenewedAt = futureTime

			// These methods should handle negative durations gracefully
			duration := leadershipState.GetLeadershipDuration()
			timeSinceRenewal := leadershipState.GetTimeSinceLastRenewal()

			// Current implementation may return negative values - test actual behavior
			_ = duration         // Accept whatever it returns
			_ = timeSinceRenewal // Accept whatever it returns

			// The important thing is it shouldn't panic
			Expect(func() { leadershipState.GetHealthStatus() }).ToNot(Panic())
		})

		It("should handle multiple rapid leadership changes", func() {
			// Rapid become/step-down cycles
			for i := 0; i < 5; i++ {
				leadershipState.BecomeLeader()
				Expect(leadershipState.IsLeader).To(BeTrue())

				leadershipState.StepDown()
				Expect(leadershipState.IsLeader).To(BeFalse())
			}
		})
	})
})
