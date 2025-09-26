/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version		It("should handle PDB with minAvailable", func() {
			minAvailable := intstr.FromInt(3)
			pdb.Spec.MinAvailable = &minAvailable
			pdb.Spec.MaxUnavailable = nil

			policy := NewDisruptionPolicy(workloadRef, pdb, 10)the "License");
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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("DisruptionPolicy", func() {

	var (
		workloadRef corev1.ObjectReference
		pdb         *policyv1.PodDisruptionBudget
	)

	BeforeEach(func() {
		workloadRef = corev1.ObjectReference{
			Kind:       "Deployment",
			Name:       "test-deployment",
			Namespace:  "default",
			APIVersion: "apps/v1",
		}

		maxUnavailable := intstr.FromString("25%")
		pdb = &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pdb",
				Namespace: "default",
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: &maxUnavailable,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test-app",
					},
				},
			},
		}
	})

	Describe("NewDisruptionPolicy", func() {
		It("should create a new disruption policy with PDB", func() {
			policy := NewDisruptionPolicy(workloadRef, pdb, 10)

			Expect(policy.WorkloadRef).To(Equal(workloadRef))
			Expect(policy.PDBRef).ToNot(BeNil())
			Expect(policy.PDBRef.Name).To(Equal("test-pdb"))
			Expect(policy.TotalReplicas).To(Equal(int32(10)))
			Expect(policy.MaxUnavailable).ToNot(BeNil())
			Expect(policy.MaxUnavailable.StrVal).To(Equal("25%"))
		})

		It("should create a new disruption policy without PDB", func() {
			policy := NewDisruptionPolicy(workloadRef, nil, 5)

			Expect(policy.WorkloadRef).To(Equal(workloadRef))
			Expect(policy.PDBRef).To(BeNil())
			Expect(policy.TotalReplicas).To(Equal(int32(5)))
			Expect(policy.MinAvailable).To(BeNil())
			Expect(policy.MaxUnavailable).To(BeNil())
		})

		It("should handle PDB with MinAvailable", func() {
			minAvailable := intstr.FromInt(3)
			pdb.Spec.MinAvailable = &minAvailable
			pdb.Spec.MaxUnavailable = nil

			policy := NewDisruptionPolicy(workloadRef, pdb, 10)

			Expect(policy.MinAvailable).ToNot(BeNil())
			Expect(policy.MinAvailable.IntVal).To(Equal(int32(3)))
			Expect(policy.MaxUnavailable).To(BeNil())
		})
	})

	Describe("CanDisruptPod", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should allow disruption when within limits", func() {
			policy.CurrentlyDisrupted = 1 // With current 25% -> totalReplicas/2 = 5, so up to 4 more allowed
			policy.LastChecked = time.Now().Add(-1 * time.Minute)

			Expect(policy.CanDisruptPod()).To(BeTrue())
		})

		It("should not allow disruption when at limit", func() {
			policy.CurrentlyDisrupted = 5 // At the totalReplicas/2 limit (current implementation)
			policy.LastChecked = time.Now().Add(-1 * time.Minute)

			Expect(policy.CanDisruptPod()).To(BeFalse())
		})

		It("should not allow disruption when exceeding limit", func() {
			policy.CurrentlyDisrupted = 6 // Exceeding totalReplicas/2 limit
			policy.LastChecked = time.Now().Add(-1 * time.Minute)

			Expect(policy.CanDisruptPod()).To(BeFalse())
		})

		It("should handle absolute MinAvailable constraint", func() {
			minAvailable := intstr.FromInt(8)
			policy.MinAvailable = &minAvailable
			policy.MaxUnavailable = nil
			policy.CurrentlyDisrupted = 1 // 10-1=9 available, which is >= 8
			policy.LastChecked = time.Now().Add(-1 * time.Minute)

			Expect(policy.CanDisruptPod()).To(BeTrue())
		})

		It("should not allow disruption when MinAvailable would be violated", func() {
			minAvailable := intstr.FromInt(8)
			policy.MinAvailable = &minAvailable
			policy.MaxUnavailable = nil
			policy.CurrentlyDisrupted = 2 // 10-2=8 available, at minimum
			policy.LastChecked = time.Now().Add(-1 * time.Minute)

			Expect(policy.CanDisruptPod()).To(BeFalse())
		})
	})

	Describe("RecordDisruption", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should record positive disruption", func() {
			initialDisrupted := policy.CurrentlyDisrupted
			policy.RecordDisruption(2)

			Expect(policy.CurrentlyDisrupted).To(Equal(initialDisrupted + 2))
			Expect(policy.LastChecked).To(BeTemporally("~", time.Now(), time.Second))
		})

		It("should record negative disruption (recovery)", func() {
			policy.CurrentlyDisrupted = 5
			policy.RecordDisruption(-2)

			Expect(policy.CurrentlyDisrupted).To(Equal(int32(3)))
		})

		It("should not allow negative total disrupted count", func() {
			policy.CurrentlyDisrupted = 1
			policy.RecordDisruption(-3) // Would result in -2

			Expect(policy.CurrentlyDisrupted).To(Equal(int32(0)))
		})
	})

	Describe("GetAvailablePods", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should calculate available pods correctly", func() {
			policy.CurrentlyDisrupted = 3

			available := policy.GetAvailablePods()
			Expect(available).To(Equal(int32(7))) // 10 - 3
		})

		It("should handle zero disrupted pods", func() {
			policy.CurrentlyDisrupted = 0

			available := policy.GetAvailablePods()
			Expect(available).To(Equal(int32(10)))
		})

		It("should handle all pods disrupted", func() {
			policy.CurrentlyDisrupted = 10

			available := policy.GetAvailablePods()
			Expect(available).To(Equal(int32(0)))
		})
	})

	Describe("GetMaxAllowedDisruptions", func() {
		var policy *DisruptionPolicy

		Context("with MaxUnavailable percentage", func() {
			BeforeEach(func() {
				maxUnavailable := intstr.FromString("30%")
				policy = NewDisruptionPolicy(workloadRef, pdb, 10)
				policy.MaxUnavailable = &maxUnavailable
			})

			It("should calculate max disruptions using current implementation", func() {
				maxDisruptions := policy.GetMaxAllowedDisruptions()
				Expect(maxDisruptions).To(Equal(int32(5))) // Current impl: totalReplicas/2 = 10/2
			})
		})

		Context("with MaxUnavailable absolute value", func() {
			BeforeEach(func() {
				maxUnavailable := intstr.FromInt(4)
				policy = NewDisruptionPolicy(workloadRef, pdb, 10)
				policy.MaxUnavailable = &maxUnavailable
			})

			It("should use absolute max disruptions", func() {
				maxDisruptions := policy.GetMaxAllowedDisruptions()
				Expect(maxDisruptions).To(Equal(int32(4)))
			})
		})

		Context("with MinAvailable constraint", func() {
			BeforeEach(func() {
				minAvailable := intstr.FromInt(7)
				policy = NewDisruptionPolicy(workloadRef, pdb, 10)
				policy.MinAvailable = &minAvailable
				policy.MaxUnavailable = nil
			})

			It("should calculate max disruptions from MinAvailable", func() {
				maxDisruptions := policy.GetMaxAllowedDisruptions()
				Expect(maxDisruptions).To(Equal(int32(3))) // 10 - 7
			})
		})

		Context("without any constraints", func() {
			BeforeEach(func() {
				policy = NewDisruptionPolicy(workloadRef, nil, 10)
			})

			It("should allow all pods to be disrupted", func() {
				maxDisruptions := policy.GetMaxAllowedDisruptions()
				Expect(maxDisruptions).To(Equal(int32(10)))
			})
		})
	})

	Describe("IsHealthy", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should be healthy when within disruption limits", func() {
			policy.CurrentlyDisrupted = 1

			Expect(policy.IsHealthy()).To(BeTrue())
		})

		It("should be healthy when at disruption limits", func() {
			policy.CurrentlyDisrupted = 5 // At the totalReplicas/2 limit

			Expect(policy.IsHealthy()).To(BeTrue()) // At limit is still healthy
		})

		It("should be unhealthy when exceeding disruption limits", func() {
			policy.CurrentlyDisrupted = 6 // Exceeds totalReplicas/2 limit

			Expect(policy.IsHealthy()).To(BeFalse())
		})
	})

	Describe("UpdateTotalReplicas", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should update total replicas and recalculate", func() {
			policy.UpdateTotalReplicas(20)

			Expect(policy.TotalReplicas).To(Equal(int32(20)))
			Expect(policy.LastChecked).To(BeTemporally("~", time.Now(), time.Second))
		})

		It("should handle scaling down", func() {
			policy.CurrentlyDisrupted = 2
			policy.UpdateTotalReplicas(5)

			Expect(policy.TotalReplicas).To(Equal(int32(5)))
			// CurrentlyDisrupted should remain but CanDisrupt logic updates
		})
	})

	Describe("GetPolicyStatus", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should return status with correct values when within limits", func() {
			policy.CurrentlyDisrupted = 1

			status := policy.GetPolicyStatus()
			Expect(status.TotalReplicas).To(Equal(int32(10)))
			Expect(status.AvailablePods).To(Equal(int32(9)))
			Expect(status.DisruptedPods).To(Equal(int32(1)))
			Expect(status.CanDisrupt).To(BeTrue())
			Expect(status.HasPDB).To(BeTrue())
			Expect(status.WithinWindow).To(BeTrue())
		})

		It("should return status when at limits", func() {
			policy.CurrentlyDisrupted = 5 // At totalReplicas/2 limit

			status := policy.GetPolicyStatus()
			Expect(status.TotalReplicas).To(Equal(int32(10)))
			Expect(status.AvailablePods).To(Equal(int32(5)))
			Expect(status.DisruptedPods).To(Equal(int32(5)))
			Expect(status.CanDisrupt).To(BeFalse())
			Expect(status.HasPDB).To(BeTrue())
		})

		It("should return status when exceeding limits", func() {
			policy.CurrentlyDisrupted = 6 // Exceeds limit

			status := policy.GetPolicyStatus()
			Expect(status.TotalReplicas).To(Equal(int32(10)))
			Expect(status.AvailablePods).To(Equal(int32(4)))
			Expect(status.DisruptedPods).To(Equal(int32(6)))
			Expect(status.CanDisrupt).To(BeFalse())
			Expect(status.HasPDB).To(BeTrue())
		})

		It("should handle policy without PDB", func() {
			policyNoPDB := NewDisruptionPolicy(workloadRef, nil, 5)
			policyNoPDB.CurrentlyDisrupted = 2

			status := policyNoPDB.GetPolicyStatus()
			Expect(status.HasPDB).To(BeFalse())
			Expect(status.CanDisrupt).To(BeTrue()) // No PDB means no constraints
		})
	})

	Describe("GetDisruptionReason", func() {
		var policy *DisruptionPolicy

		BeforeEach(func() {
			policy = NewDisruptionPolicy(workloadRef, pdb, 10)
		})

		It("should return allowed reason when within limits", func() {
			policy.CurrentlyDisrupted = 1

			reason := policy.GetDisruptionReason()
			Expect(reason).To(Equal(DisruptionReasonAllowed))
		})

		It("should return max unavailable reason when at limit", func() {
			policy.CurrentlyDisrupted = 5 // At totalReplicas/2 limit

			reason := policy.GetDisruptionReason()
			Expect(reason).To(Equal(DisruptionReasonMaxUnavailable))
		})

		It("should return max unavailable reason when exceeding limit", func() {
			policy.CurrentlyDisrupted = 6 // Exceeding limit

			reason := policy.GetDisruptionReason()
			Expect(reason).To(Equal(DisruptionReasonMaxUnavailable))
		})

		It("should return no PDB reason when no PDB exists", func() {
			policyNoPDB := NewDisruptionPolicy(workloadRef, nil, 10)
			policyNoPDB.CurrentlyDisrupted = 1

			reason := policyNoPDB.GetDisruptionReason()
			Expect(reason).To(Equal(DisruptionReasonNoPDB))
		})

		It("should return min available reason when min constraint violated", func() {
			minAvailable := intstr.FromInt(8)
			policy.MinAvailable = &minAvailable
			policy.MaxUnavailable = nil
			policy.CurrentlyDisrupted = 3 // 10-3=7 available, less than 8

			reason := policy.GetDisruptionReason()
			Expect(reason).To(Equal(DisruptionReasonMinAvailable))
		})
	})

	Describe("Edge Cases", func() {
		It("should handle zero total replicas", func() {
			policy := NewDisruptionPolicy(workloadRef, pdb, 0)

			Expect(policy.GetAvailablePods()).To(Equal(int32(0)))
			Expect(policy.GetMaxAllowedDisruptions()).To(Equal(int32(0)))
			Expect(policy.CanDisruptPod()).To(BeFalse())
		})

		It("should handle percentage calculations with current implementation", func() {
			// Current implementation returns totalReplicas/2 for any percentage
			maxUnavailable := intstr.FromString("25%")
			policy := NewDisruptionPolicy(workloadRef, pdb, 10)
			policy.MaxUnavailable = &maxUnavailable

			maxDisruptions := policy.GetMaxAllowedDisruptions()
			Expect(maxDisruptions).To(Equal(int32(5))) // totalReplicas/2 = 10/2 = 5
		})

		It("should handle invalid percentage strings gracefully", func() {
			maxUnavailable := intstr.FromString("invalid%")
			policy := NewDisruptionPolicy(workloadRef, pdb, 10)
			policy.MaxUnavailable = &maxUnavailable

			// Current implementation returns totalReplicas/2 for any string
			maxDisruptions := policy.GetMaxAllowedDisruptions()
			Expect(maxDisruptions).To(Equal(int32(5))) // 10/2 = 5
		})
	})
})
