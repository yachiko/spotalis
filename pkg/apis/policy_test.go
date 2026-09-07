/*
Copyright 2026 The Spotalis Authors.

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
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replica allocation policy", func() {
	DescribeTable("allocates desired replicas with a bounded floor",
		func(total, percentage, floor, wantSpot, wantOnDemand, wantEffectiveFloor int32) {
			allocation, err := AllocateReplicaDistribution(total, ReplicaAllocationPolicy{
				MinOnDemand:    floor,
				SpotPercentage: percentage,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(allocation.TargetSpot).To(Equal(wantSpot))
			Expect(allocation.TargetOnDemand).To(Equal(wantOnDemand))
			Expect(allocation.EffectiveOnDemandFloor).To(Equal(wantEffectiveFloor))
			Expect(allocation.TargetSpot + allocation.TargetOnDemand).To(Equal(total))
			Expect(allocation.TargetOnDemand).To(BeNumerically(">=", wantEffectiveFloor))
		},
		Entry("scale to zero", int32(0), int32(70), int32(1), int32(0), int32(0), int32(0)),
		Entry("floor greater than total", int32(3), int32(80), int32(5), int32(0), int32(3), int32(3)),
		Entry("floor equal to total", int32(3), int32(100), int32(3), int32(0), int32(3), int32(3)),
		Entry("zero percentage", int32(10), int32(0), int32(1), int32(0), int32(10), int32(1)),
		Entry("one hundred percent", int32(10), int32(100), int32(0), int32(10), int32(0), int32(0)),
		Entry("fraction truncates", int32(10), int32(73), int32(1), int32(7), int32(3), int32(1)),
		Entry("large legal replica count does not overflow", int32(math.MaxInt32), int32(100), int32(0), int32(math.MaxInt32), int32(0), int32(0)),
	)

	It("rejects invalid inputs", func() {
		for _, test := range []struct {
			total  int32
			policy ReplicaAllocationPolicy
		}{
			{total: -1},
			{total: 1, policy: ReplicaAllocationPolicy{MinOnDemand: -1}},
			{total: 1, policy: ReplicaAllocationPolicy{SpotPercentage: -1}},
			{total: 1, policy: ReplicaAllocationPolicy{SpotPercentage: 101}},
		} {
			_, err := AllocateReplicaDistribution(test.total, test.policy)
			Expect(err).To(HaveOccurred())
		}
	})

	It("never decreases the effective floor as the configured floor increases", func() {
		var previous int32
		for floor := int32(0); floor <= 12; floor++ {
			allocation, err := AllocateReplicaDistribution(10, ReplicaAllocationPolicy{
				MinOnDemand:    floor,
				SpotPercentage: 100,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(allocation.EffectiveOnDemandFloor).To(BeNumerically(">=", previous))
			Expect(allocation.TargetOnDemand).To(BeNumerically(">=", allocation.EffectiveOnDemandFloor))
			previous = allocation.EffectiveOnDemandFloor
		}
	})

	It("distinguishes an omitted field from an explicit zero during inheritance", func() {
		zero := int32(0)
		defaults := ReplicaAllocationPolicy{MinOnDemand: 2, SpotPercentage: 70}

		Expect((WorkloadPolicy{}).Resolve(defaults)).To(Equal(defaults))
		Expect((WorkloadPolicy{MinOnDemand: &zero, SpotPercentage: &zero}).Resolve(defaults)).To(Equal(ReplicaAllocationPolicy{}))
	})
})
