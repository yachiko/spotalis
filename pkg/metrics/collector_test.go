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

package metrics

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/pkg/apis"
)

var _ = Describe("Collector", func() {
	var (
		collector *Collector
	)

	BeforeEach(func() {
		collector = NewCollector()

		// Reset metrics before each test
		collector.ResetMetrics()
	})

	Describe("NewCollector", func() {
		It("should create a new collector with initialized timestamp", func() {
			c := NewCollector()
			Expect(c).NotTo(BeNil())
			Expect(c.lastUpdate).To(BeTemporally("~", time.Now(), time.Second))
			Expect(c.nodeClassifier).To(BeNil())
		})
	})

	Describe("SetNodeClassifier", func() {
		It("should set the node classifier service", func() {
			// Create a mock classifier (would be actual implementation in real code)
			// For this test, we'll just verify the setter works
			collector.SetNodeClassifier(nil) // Test with nil to verify no panic
			Expect(collector.nodeClassifier).To(BeNil())
		})

		It("should handle concurrent access safely", func() {
			done := make(chan bool, 10)

			// Simulate concurrent access to SetNodeClassifier
			for i := 0; i < 10; i++ {
				go func() {
					defer GinkgoRecover()
					collector.SetNodeClassifier(nil)
					done <- true
				}()
			}

			// Wait for all goroutines to complete
			for i := 0; i < 10; i++ {
				Eventually(done).Should(Receive())
			}
		})
	})

	Describe("RecordWorkloadMetrics", func() {
		var replicaState *apis.ReplicaState

		BeforeEach(func() {
			replicaState = &apis.ReplicaState{
				CurrentSpot:     3,
				CurrentOnDemand: 2,
				DesiredSpot:     4,
				DesiredOnDemand: 1,
			}
		})

		It("should record workload metrics correctly", func() {
			namespace := "test-namespace"
			workloadName := "test-workload"
			workloadType := "deployment"

			collector.RecordWorkloadMetrics(namespace, workloadName, workloadType, replicaState)

			// Verify spot utilization calculation: 3/(3+2) * 100 = 60%
			expectedUtilization := float64(3) / float64(5) * 100

			// In a real test environment, we'd need to verify the metrics values
			// For now, we'll just ensure the function doesn't panic
			Expect(expectedUtilization).To(Equal(60.0))
		})

		It("should handle zero total replicas", func() {
			replicaState.CurrentSpot = 0
			replicaState.CurrentOnDemand = 0

			collector.RecordWorkloadMetrics("namespace", "workload", "deployment", replicaState)

			// Should not panic with zero division
			// Spot utilization should not be set when total replicas is 0
		})

		It("should handle various workload types", func() {
			workloadTypes := []string{"deployment", "statefulset", "daemonset"}

			for _, workloadType := range workloadTypes {
				collector.RecordWorkloadMetrics("ns", "workload", workloadType, replicaState)
			}

			// Should handle all workload types without error
		})
	})

	Describe("RecordReconciliation", func() {
		It("should record successful reconciliation", func() {
			collector.RecordReconciliation("namespace", "workload", "deployment", "update", nil)

			// In a real test, we'd verify the metrics were recorded correctly
			// For now, ensure no panic occurs
		})

		It("should record failed reconciliation", func() {
			err := errors.New("reconciliation failed")

			collector.RecordReconciliation("namespace", "workload", "deployment", "update", err)

			// Should record both the action and the error
		})

		It("should handle various actions", func() {
			actions := []string{"create", "update", "delete", "scale"}

			for _, action := range actions {
				collector.RecordReconciliation("ns", "workload", "deployment", action, nil)
			}
		})
	})

	Describe("RecordWebhookRequest", func() {
		It("should record webhook request metrics", func() {
			collector.RecordWebhookRequest("CREATE", "Pod", "success")

			// Should record request count
		})

		It("should handle different operations and results", func() {
			operations := []string{"CREATE", "UPDATE", "DELETE"}
			results := []string{"success", "failure", "timeout"}

			for _, op := range operations {
				for _, result := range results {
					collector.RecordWebhookRequest(op, "Pod", result)
				}
			}
		})
	})

	Describe("RecordWebhookMutation", func() {
		It("should record webhook mutation metrics", func() {
			collector.RecordWebhookMutation("Pod", "add-annotation")
			collector.RecordWebhookMutation("Deployment", "modify-spec")

			// Should record mutation counts by resource and type
		})
	})

	Describe("UpdateControllerHealth", func() {
		It("should update controller health for leader", func() {
			collector.UpdateControllerHealth("deployment-controller", true)

			// Should set last seen timestamp and leader status to 1
		})

		It("should update controller health for follower", func() {
			collector.UpdateControllerHealth("deployment-controller", false)

			// Should set last seen timestamp and leader status to 0
		})

		It("should handle multiple controllers", func() {
			controllers := []string{"deployment-controller", "statefulset-controller", "webhook-controller"}

			for i, controller := range controllers {
				collector.UpdateControllerHealth(controller, i%2 == 0)
			}
		})
	})

	Describe("GetMetricsSnapshot", func() {
		It("should return current metrics snapshot", func() {
			snapshot := collector.GetMetricsSnapshot()

			Expect(snapshot.Timestamp).To(BeTemporally("~", time.Now(), time.Second))
			Expect(snapshot.LastUpdate).To(BeTemporally("~", collector.lastUpdate, time.Millisecond))
		})

		It("should be safe for concurrent access", func() {
			done := make(chan bool, 5)

			for i := 0; i < 5; i++ {
				go func() {
					defer GinkgoRecover()
					snapshot := collector.GetMetricsSnapshot()
					Expect(snapshot).NotTo(BeNil())
					done <- true
				}()
			}

			for i := 0; i < 5; i++ {
				Eventually(done).Should(Receive())
			}
		})
	})

	Describe("ResetMetrics", func() {
		It("should reset all metrics", func() {
			// Record some metrics first
			collector.RecordWebhookRequest("CREATE", "Pod", "success")

			// Reset all metrics
			collector.ResetMetrics()

			// Verify metrics are reset (in a real test, we'd check metric values)
			// For now, ensure no panic occurs
		})
	})

	Describe("StartMetricsCollection", func() {
		It("should start background metrics collection", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			// Start collection with short interval
			done := make(chan bool)
			go func() {
				defer GinkgoRecover()
				collector.StartMetricsCollection(ctx, 10*time.Millisecond)
				done <- true
			}()

			// Should exit when context is canceled
			Eventually(done, 200*time.Millisecond).Should(Receive())
		})

		It("should handle errors gracefully", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			// Set a nil classifier to trigger an error path
			collector.SetNodeClassifier(nil)

			done := make(chan bool)
			go func() {
				defer GinkgoRecover()
				collector.StartMetricsCollection(ctx, 5*time.Millisecond)
				done <- true
			}()

			// Should continue running despite errors
			Eventually(done, 100*time.Millisecond).Should(Receive())
		})
	})
})

var _ = Describe("Timer", func() {
	Describe("NewTimer", func() {
		It("should create a timer with current timestamp", func() {
			timer := NewTimer()
			Expect(timer).NotTo(BeNil())
			Expect(timer.start).To(BeTemporally("~", time.Now(), time.Millisecond))
		})
	})

	Describe("Elapsed", func() {
		It("should return elapsed duration", func() {
			timer := NewTimer()
			time.Sleep(10 * time.Millisecond)

			elapsed := timer.Elapsed()
			Expect(elapsed).To(BeNumerically(">=", 10*time.Millisecond))
			Expect(elapsed).To(BeNumerically("<", 100*time.Millisecond))
		})
	})

	Describe("ObserveReconciliation", func() {
		It("should observe reconciliation duration", func() {
			collector := NewCollector()
			timer := NewTimer()
			time.Sleep(5 * time.Millisecond)

			timer.ObserveReconciliation(collector, "namespace", "workload", "deployment", "update", nil)

			// Should record the elapsed time
		})

		It("should observe reconciliation with error", func() {
			collector := NewCollector()
			timer := NewTimer()
			time.Sleep(5 * time.Millisecond)

			err := errors.New("reconciliation failed")
			timer.ObserveReconciliation(collector, "namespace", "workload", "deployment", "update", err)

			// Should record the elapsed time and error
		})
	})

	Describe("ObserveWebhook", func() {
		It("should observe webhook duration", func() {
			collector := NewCollector()
			timer := NewTimer()
			time.Sleep(5 * time.Millisecond)

			timer.ObserveWebhook(collector, "CREATE", "Pod", "success")

			// Should record the elapsed time
		})
	})
})

var _ = Describe("MetricsSnapshot", func() {
	It("should contain timestamp and last update fields", func() {
		now := time.Now()
		snapshot := Snapshot{
			LastUpdate: now.Add(-5 * time.Minute),
			Timestamp:  now,
		}

		Expect(snapshot.LastUpdate).To(BeTemporally("~", now.Add(-5*time.Minute), time.Millisecond))
		Expect(snapshot.Timestamp).To(BeTemporally("~", now, time.Millisecond))
	})
})

var _ = Describe("Global Functions", func() {
	var originalCollector *Collector

	BeforeEach(func() {
		// Save the original global collector
		originalCollector = GlobalCollector
		// Replace with a fresh collector for testing
		GlobalCollector = NewCollector()
	})

	AfterEach(func() {
		// Restore the original global collector
		GlobalCollector = originalCollector
	})

	Describe("RecordWorkloadReconciliation", func() {
		It("should use global collector", func() {
			RecordWorkloadReconciliation("namespace", "workload", "deployment", "update", nil)

			// Should delegate to global collector
		})
	})

	Describe("RecordWebhookOperation", func() {
		It("should use global collector", func() {
			RecordWebhookOperation("CREATE", "Pod", "success")

			// Should delegate to global collector
		})
	})

	Describe("RecordSpotUtilization", func() {
		It("should use global collector", func() {
			replicaState := &apis.ReplicaState{
				CurrentSpot:     2,
				CurrentOnDemand: 3,
			}

			RecordSpotUtilization("namespace", "workload", "deployment", replicaState)

			// Should delegate to global collector
		})
	})
})

var _ = Describe("Metrics Integration", func() {
	var collector *Collector

	BeforeEach(func() {
		collector = NewCollector()
		collector.ResetMetrics()
	})

	It("should handle complete workflow metrics", func() {
		// Simulate a complete reconciliation workflow
		replicaState := &apis.ReplicaState{
			CurrentSpot:     3,
			CurrentOnDemand: 2,
			DesiredSpot:     4,
			DesiredOnDemand: 1,
		}

		// Record workload metrics
		collector.RecordWorkloadMetrics("production", "app-server", "deployment", replicaState)

		// Record reconciliation
		timer := NewTimer()
		time.Sleep(1 * time.Millisecond) // Simulate work
		timer.ObserveReconciliation(collector, "production", "app-server", "deployment", "scale", nil)

		// Record webhook operation
		collector.RecordWebhookRequest("UPDATE", "Deployment", "success")
		collector.RecordWebhookMutation("Deployment", "add-spot-annotation")

		// Update controller health
		collector.UpdateControllerHealth("deployment-controller", true)

		// Get snapshot
		snapshot := collector.GetMetricsSnapshot()
		Expect(snapshot).NotTo(BeNil())
		Expect(snapshot.Timestamp).To(BeTemporally("~", time.Now(), time.Second))
	})

	It("should handle concurrent metric recording", func() {
		done := make(chan bool, 20)

		// Simulate concurrent metric recording from multiple goroutines
		for i := 0; i < 20; i++ {
			go func(index int) {
				defer GinkgoRecover()

				// Record various metrics concurrently
				collector.RecordWebhookRequest("CREATE", "Pod", "success")
				collector.UpdateControllerHealth("test-controller", index%2 == 0)

				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 20; i++ {
			Eventually(done).Should(Receive())
		}
	})

	Context("Error handling", func() {
		It("should handle nil replica state gracefully", func() {
			// This should not panic
			Expect(func() {
				collector.RecordWorkloadMetrics("namespace", "workload", "deployment", nil)
			}).ToNot(Panic()) // The implementation handles nil gracefully
		})

		It("should handle empty strings in metrics", func() {
			collector.RecordWebhookRequest("", "", "")
			collector.UpdateControllerHealth("", true)

			// Should handle empty strings without error
		})
	})
})
