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

package webhook

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Staleness Detection", func() {
	var tracker *AdmissionStateTracker
	var key string

	BeforeEach(func() {
		tracker = NewAdmissionStateTracker(30 * time.Second)
		key = "default/Deployment/test"
	})

	AfterEach(func() {
		tracker.Stop()
	})

	Context("CheckStaleness", func() {
		It("should not be stale when no prior state exists", func() {
			check := tracker.CheckStaleness(key, 1)
			Expect(check.IsStale).To(BeFalse())
			Expect(check.Reason).To(Equal(StaleReasonNone))
		})

		It("should detect generation changes as stale", func() {
			// Record initial state with generation 1
			tracker.IncrementPending(key, capacityTypeSpot, 1)

			// Check with same generation - not stale
			check := tracker.CheckStaleness(key, 1)
			Expect(check.IsStale).To(BeFalse())

			// Check with different generation - stale
			check = tracker.CheckStaleness(key, 2)
			Expect(check.IsStale).To(BeTrue())
			Expect(check.Reason).To(Equal(StaleReasonGeneration))
			Expect(check.Details).To(ContainSubstring("cached generation 1 != current 2"))
		})

		It("should detect old pod lists as stale", func() {
			tracker.podListMaxAge = 100 * time.Millisecond // Short for testing

			// Record state with pod list time
			tracker.UpdatePodListMetadata(key, "12345", 1)

			// Immediately - not stale
			check := tracker.CheckStaleness(key, 1)
			Expect(check.IsStale).To(BeFalse())

			// Wait for pod list to age
			time.Sleep(150 * time.Millisecond)

			check = tracker.CheckStaleness(key, 1)
			Expect(check.IsStale).To(BeTrue())
			Expect(check.Reason).To(Equal(StaleReasonPodListAge))
			Expect(check.Details).To(ContainSubstring("pod list is"))
			Expect(check.Details).To(ContainSubstring("old"))
		})

		It("should not be stale when pod list time is zero", func() {
			// Add pending without updating pod list metadata
			tracker.IncrementPending(key, capacityTypeSpot, 1)

			check := tracker.CheckStaleness(key, 1)
			Expect(check.IsStale).To(BeFalse())
		})

		It("should prioritize generation check over pod list age", func() {
			tracker.podListMaxAge = 50 * time.Millisecond

			// Record state with old pod list
			tracker.UpdatePodListMetadata(key, "12345", 1)
			time.Sleep(100 * time.Millisecond)

			// Check with different generation - should report generation staleness
			check := tracker.CheckStaleness(key, 2)
			Expect(check.IsStale).To(BeTrue())
			Expect(check.Reason).To(Equal(StaleReasonGeneration))
		})
	})

	Context("UpdatePodListMetadata", func() {
		It("should create new entry if none exists", func() {
			tracker.UpdatePodListMetadata(key, "rv-123", 1)

			tracker.mu.RLock()
			p := tracker.pending[key]
			tracker.mu.RUnlock()

			Expect(p).NotTo(BeNil())
			Expect(p.PodListResourceVersion).To(Equal("rv-123"))
			Expect(p.Generation).To(Equal(int64(1)))
			Expect(p.PodListTime).NotTo(BeZero())
		})

		It("should update existing entry", func() {
			// Create initial entry
			tracker.IncrementPending(key, capacityTypeSpot, 1)

			// Update metadata
			tracker.UpdatePodListMetadata(key, "rv-456", 1)

			tracker.mu.RLock()
			p := tracker.pending[key]
			tracker.mu.RUnlock()

			Expect(p.PodListResourceVersion).To(Equal("rv-456"))
			Expect(p.PendingSpot).To(Equal(int32(1))) // Original pending count preserved
		})

		It("should update last updated time", func() {
			tracker.UpdatePodListMetadata(key, "rv-123", 1)

			tracker.mu.RLock()
			firstUpdate := tracker.pending[key].LastUpdated
			tracker.mu.RUnlock()

			time.Sleep(10 * time.Millisecond)

			tracker.UpdatePodListMetadata(key, "rv-456", 1)

			tracker.mu.RLock()
			secondUpdate := tracker.pending[key].LastUpdated
			tracker.mu.RUnlock()

			Expect(secondUpdate).To(BeTemporally(">", firstUpdate))
		})
	})

	Context("ResetPending", func() {
		It("should clear pending counts", func() {
			// Add pending counts
			tracker.IncrementPending(key, capacityTypeSpot, 1)
			tracker.IncrementPending(key, capacityTypeSpot, 1)
			tracker.IncrementPending(key, capacityTypeOnDemand, 1)

			spot, od := tracker.GetPendingCounts(key, 1)
			Expect(spot).To(Equal(int32(2)))
			Expect(od).To(Equal(int32(1)))

			// Reset with new generation
			tracker.ResetPending(key, 2)

			// Old generation returns 0
			spot, od = tracker.GetPendingCounts(key, 1)
			Expect(spot).To(Equal(int32(0)))
			Expect(od).To(Equal(int32(0)))

			// New generation also 0 (reset)
			spot, od = tracker.GetPendingCounts(key, 2)
			Expect(spot).To(Equal(int32(0)))
			Expect(od).To(Equal(int32(0)))
		})

		It("should update generation", func() {
			tracker.IncrementPending(key, capacityTypeSpot, 1)

			tracker.ResetPending(key, 5)

			tracker.mu.RLock()
			gen := tracker.pending[key].Generation
			tracker.mu.RUnlock()

			Expect(gen).To(Equal(int64(5)))
		})

		It("should update timestamps", func() {
			before := time.Now()
			tracker.ResetPending(key, 1)
			after := time.Now()

			tracker.mu.RLock()
			p := tracker.pending[key]
			tracker.mu.RUnlock()

			Expect(p.LastUpdated).To(BeTemporally(">=", before))
			Expect(p.LastUpdated).To(BeTemporally("<=", after))
			Expect(p.PodListTime).To(BeTemporally(">=", before))
			Expect(p.PodListTime).To(BeTemporally("<=", after))
		})

		It("should create new entry if key doesn't exist", func() {
			tracker.ResetPending(key, 3)

			tracker.mu.RLock()
			p := tracker.pending[key]
			tracker.mu.RUnlock()

			Expect(p).NotTo(BeNil())
			Expect(p.Generation).To(Equal(int64(3)))
			Expect(p.PendingSpot).To(Equal(int32(0)))
			Expect(p.PendingOnDemand).To(Equal(int32(0)))
		})
	})

	Context("Integration with existing tracker functionality", func() {
		It("should maintain pending counts after metadata update", func() {
			// Increment pending
			tracker.IncrementPending(key, capacityTypeSpot, 1)
			tracker.IncrementPending(key, capacityTypeOnDemand, 1)

			// Update metadata
			tracker.UpdatePodListMetadata(key, "rv-123", 1)

			// Counts should still be there
			spot, od := tracker.GetPendingCounts(key, 1)
			Expect(spot).To(Equal(int32(1)))
			Expect(od).To(Equal(int32(1)))

			// Should not be stale
			check := tracker.CheckStaleness(key, 1)
			Expect(check.IsStale).To(BeFalse())
		})

		It("should handle complete workflow: increment -> check -> reset", func() {
			// Phase 1: Add pending
			tracker.IncrementPending(key, capacityTypeSpot, 1)
			tracker.UpdatePodListMetadata(key, "rv-100", 1)

			spot, od := tracker.GetPendingCounts(key, 1)
			Expect(spot).To(Equal(int32(1)))
			Expect(od).To(Equal(int32(0)))

			// Phase 2: Check staleness (generation change)
			check := tracker.CheckStaleness(key, 2)
			Expect(check.IsStale).To(BeTrue())

			// Phase 3: Reset on staleness detection
			tracker.ResetPending(key, 2)

			// Verify reset
			spot, od = tracker.GetPendingCounts(key, 2)
			Expect(spot).To(Equal(int32(0)))
			Expect(od).To(Equal(int32(0)))

			// Phase 4: New increment with new generation
			tracker.IncrementPending(key, capacityTypeOnDemand, 2)
			tracker.UpdatePodListMetadata(key, "rv-200", 2)

			spot, od = tracker.GetPendingCounts(key, 2)
			Expect(spot).To(Equal(int32(0)))
			Expect(od).To(Equal(int32(1)))

			check = tracker.CheckStaleness(key, 2)
			Expect(check.IsStale).To(BeFalse())
		})
	})

	Context("Concurrent access", func() {
		It("should handle concurrent staleness checks", func() {
			tracker.UpdatePodListMetadata(key, "rv-123", 1)

			done := make(chan bool, 10)
			for i := 0; i < 10; i++ {
				go func() {
					defer GinkgoRecover()
					check := tracker.CheckStaleness(key, 1)
					Expect(check.IsStale).To(BeFalse())
					done <- true
				}()
			}

			for i := 0; i < 10; i++ {
				Eventually(done).Should(Receive())
			}
		})

		It("should handle concurrent updates and checks", func() {
			done := make(chan bool, 20)

			// Concurrent updates
			for i := 0; i < 10; i++ {
				go func(gen int64) {
					defer GinkgoRecover()
					tracker.UpdatePodListMetadata(key, "rv-test", gen)
					done <- true
				}(int64(i % 3)) // Mix of different generations
			}

			// Concurrent checks
			for i := 0; i < 10; i++ {
				go func() {
					defer GinkgoRecover()
					_ = tracker.CheckStaleness(key, 1)
					done <- true
				}()
			}

			for i := 0; i < 20; i++ {
				Eventually(done).Should(Receive())
			}
		})
	})
})
