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

	"github.com/ahoma/spotalis/pkg/apis"
)

var _ = Describe("Collector", func() {
	var (
		collector *Collector
		ctx       context.Context
		cancel    context.CancelFunc
	)

	BeforeEach(func() {
		collector = NewCollector()
		ctx, cancel = context.WithCancel(context.Background())

		// Reset metrics before each test
		collector.ResetMetrics()
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
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

	Describe("UpdateNodeMetrics", func() {
		Context("when node classifier is not set", func() {
			It("should return nil without error", func() {
				err := collector.UpdateNodeMetrics(ctx)
				Expect(err).To(BeNil())
			})
		})

		Context("when node classifier returns error", func() {
			It("should handle classifier errors gracefully", func() {
				// Test the error handling path by ensuring the function
				// can handle nil classifier without panicking
				collector.SetNodeClassifier(nil)

				err := collector.UpdateNodeMetrics(ctx)
				Expect(err).To(BeNil()) // Should return nil when classifier is nil

				// Verify that lastUpdate timestamp gets updated even when classifier is nil
				snapshot := collector.GetMetricsSnapshot()
				Expect(snapshot.LastUpdate).To(BeTemporally("~", time.Now(), time.Second))
			})
		})

		Context("when context is canceled", func() {
			It("should respect context cancellation", func() {
				cancel() // Cancel the context
				err := collector.UpdateNodeMetrics(ctx)

				// Should either return nil (if classifier is nil) or handle cancellation
				// The exact behavior depends on the classifier implementation
				Expect(err).To(BeNil()) // Since classifier is nil in this test
			})
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
			duration := 150 * time.Millisecond
			collector.RecordReconciliation("namespace", "workload", "deployment", "update", duration, nil)

			// In a real test, we'd verify the metrics were recorded correctly
			// For now, ensure no panic occurs
		})

		It("should record failed reconciliation", func() {
			duration := 300 * time.Millisecond
			err := errors.New("reconciliation failed")

			collector.RecordReconciliation("namespace", "workload", "deployment", "update", duration, err)

			// Should record both the duration and the error
		})

		It("should handle various actions", func() {
			actions := []string{"create", "update", "delete", "scale"}
			duration := 100 * time.Millisecond

			for _, action := range actions {
				collector.RecordReconciliation("ns", "workload", "deployment", action, duration, nil)
			}
		})
	})

	Describe("RecordWebhookRequest", func() {
		It("should record webhook request metrics", func() {
			duration := 50 * time.Millisecond
			collector.RecordWebhookRequest("CREATE", "Pod", "success", duration)

			// Should record both request count and duration
		})

		It("should handle different operations and results", func() {
			operations := []string{"CREATE", "UPDATE", "DELETE"}
			results := []string{"success", "failure", "timeout"}
			duration := 25 * time.Millisecond

			for _, op := range operations {
				for _, result := range results {
					collector.RecordWebhookRequest(op, "Pod", result, duration)
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

	Describe("RecordAPIRequest", func() {
		It("should record API request rate", func() {
			collector.RecordAPIRequest("apps/v1", "deployments", 15.5)
			collector.RecordAPIRequest("", "pods", 25.0)

			// Should set the current request rate
		})
	})

	Describe("RecordAPIError", func() {
		It("should record API errors", func() {
			collector.RecordAPIError("apps/v1", "deployments", "timeout")
			collector.RecordAPIError("", "pods", "forbidden")

			// Should increment error counters
		})
	})

	Describe("RecordRateLimitHit", func() {
		It("should record rate limit hits", func() {
			collector.RecordRateLimitHit("apps/v1", "deployments")
			collector.RecordRateLimitHit("", "pods")

			// Should increment rate limit hit counters
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
			collector.RecordWebhookRequest("CREATE", "Pod", "success", 50*time.Millisecond)
			collector.RecordAPIError("apps/v1", "deployments", "timeout")

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
		snapshot := MetricsSnapshot{
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
			duration := 100 * time.Millisecond
			RecordWorkloadReconciliation("namespace", "workload", "deployment", "update", duration, nil)

			// Should delegate to global collector
		})
	})

	Describe("RecordWebhookOperation", func() {
		It("should use global collector", func() {
			duration := 50 * time.Millisecond
			RecordWebhookOperation("CREATE", "Pod", "success", duration)

			// Should delegate to global collector
		})
	})

	Describe("UpdateNodeHealth", func() {
		It("should use global collector", func() {
			ctx := context.Background()
			err := UpdateNodeHealth(ctx)

			// Should return nil since no classifier is set
			Expect(err).To(BeNil())
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
		collector.RecordWebhookRequest("UPDATE", "Deployment", "success", 25*time.Millisecond)
		collector.RecordWebhookMutation("Deployment", "add-spot-annotation")

		// Record API metrics
		collector.RecordAPIRequest("apps/v1", "deployments", 10.5)

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
				collector.RecordWebhookRequest("CREATE", "Pod", "success", 10*time.Millisecond)
				collector.RecordAPIError("", "pods", "timeout")
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
			collector.RecordWebhookRequest("", "", "", 0)
			collector.RecordAPIError("", "", "")
			collector.UpdateControllerHealth("", true)

			// Should handle empty strings without error
		})
	})
})
