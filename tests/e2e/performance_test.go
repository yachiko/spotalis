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

package e2e

import (
	"fmt"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ahoma/spotalis/pkg/apis"
)

var _ = Describe("Performance Tests", func() {

	const (
		// Performance requirements from tasks.md
		MaxReconciliationTime = 5 * time.Second
		MaxMemoryUsageMB      = 100
	)

	var (
		memBefore runtime.MemStats
	)

	BeforeEach(func() {
		// Record memory usage before test
		runtime.GC()
		runtime.ReadMemStats(&memBefore)
	})

	Describe("Replica State Calculation Performance", func() {
		It("should calculate replica distribution efficiently", func() {
			By("Performing large-scale replica state calculations")

			startTime := time.Now()
			startMem := getMemoryUsage()

			// Simulate heavy replica state calculations for multiple workloads
			for i := 0; i < 1000; i++ {
				state := &apis.ReplicaState{
					TotalReplicas:   int32(i%50 + 1), // 1-50 replicas
					CurrentOnDemand: int32(i % 25),
					CurrentSpot:     int32(i % 25),
				}

				config := apis.WorkloadConfiguration{
					Enabled:        true,
					MinOnDemand:    int32(i % 5),
					SpotPercentage: int32((i % 10) * 10),
				}

				// Perform all calculations
				state.CalculateDesiredDistribution(config)
				_ = state.GetNextAction()
				_ = state.NeedsReconciliation()
				_ = state.GetOnDemandDrift()
				_ = state.GetSpotDrift()
				_ = state.RequiresScaleUp()
				_ = state.RequiresScaleDown()
				_ = state.GetSpotPercentage()
				_ = state.GetDesiredSpotPercentage()
			}

			calculationTime := time.Since(startTime)
			endMem := getMemoryUsage()
			memoryUsedMB := float64(endMem-startMem) / 1024 / 1024

			By(fmt.Sprintf("1000 calculations completed in %v (requirement: < %v)",
				calculationTime, MaxReconciliationTime))
			By(fmt.Sprintf("Memory used: %.2f MB (requirement: < %d MB)",
				memoryUsedMB, MaxMemoryUsageMB))

			// Should be very fast for in-memory calculations
			Expect(calculationTime).To(BeNumerically("<", 100*time.Millisecond))
			Expect(memoryUsedMB).To(BeNumerically("<", 10.0))
		})

		It("should handle workload configuration parsing efficiently", func() {
			By("Parsing multiple workload configurations")

			startTime := time.Now()

			// Test various annotation combinations
			testAnnotations := []map[string]string{
				{"spotalis.io/spot-percentage": "70", "spotalis.io/min-on-demand": "2"},
				{"spotalis.io/spot-percentage": "50", "spotalis.io/min-on-demand": "1"},
				{"spotalis.io/spot-percentage": "90", "spotalis.io/min-on-demand": "0"},
				{"spotalis.io/spot-percentage": "30", "spotalis.io/min-on-demand": "5"},
			}

			for i := 0; i < 250; i++ { // 1000 total (250 * 4 combinations)
				annotations := testAnnotations[i%len(testAnnotations)]

				config, err := apis.ParseFromAnnotations(annotations)
				Expect(err).To(BeNil())

				// Validate the configuration
				err = config.Validate(10)
				Expect(err).To(BeNil())

				// Convert back to annotations
				_ = config.ToAnnotations()

				// Test helper methods
				_ = config.IsSpotOptimized()
				_ = config.IsOnDemandOnly()
				_ = config.GetEffectiveMaxReplicas(5)
			}

			parsingTime := time.Since(startTime)

			By(fmt.Sprintf("1000 configuration operations completed in %v", parsingTime))

			// Should be very fast for annotation parsing
			Expect(parsingTime).To(BeNumerically("<", 50*time.Millisecond))
		})

		It("should handle node classification efficiently", func() {
			By("Creating and managing node classifications")

			startTime := time.Now()
			startMem := getMemoryUsage()

			classifier := apis.NewDefaultNodeClassifier()
			cache := apis.NewNodeCache(classifier)

			// Simulate managing many nodes
			for i := 0; i < 100; i++ {
				classification := &apis.NodeClassification{
					NodeName:    fmt.Sprintf("node-%d", i),
					NodeType:    apis.NodeType([]string{"spot", "on-demand", "unknown"}[i%3]),
					Schedulable: i%10 != 0, // 90% schedulable
					Labels: map[string]string{
						"node.kubernetes.io/instance-type": fmt.Sprintf("m5.%s", []string{"large", "xlarge", "2xlarge"}[i%3]),
						"topology.kubernetes.io/zone":      fmt.Sprintf("us-west-2%c", 'a'+byte(i%3)),
					},
					LastUpdated: time.Now(),
				}

				cache.Classifications[classification.NodeName] = classification
			}

			// Perform operations on the cache
			for i := 0; i < 100; i++ {
				nodeName := fmt.Sprintf("node-%d", i)
				_ = cache.GetNodeType(nodeName)
			}

			spotNodes := cache.GetSpotNodes()
			onDemandNodes := cache.GetOnDemandNodes()

			_ = cache.NeedsRefresh(5 * time.Minute)
			cache.MarkRefreshed()

			classificationTime := time.Since(startTime)
			endMem := getMemoryUsage()
			memoryUsedMB := float64(endMem-startMem) / 1024 / 1024

			By(fmt.Sprintf("Node classification operations completed in %v", classificationTime))
			By(fmt.Sprintf("Found %d spot nodes and %d on-demand nodes", len(spotNodes), len(onDemandNodes)))
			By(fmt.Sprintf("Memory used: %.2f MB", memoryUsedMB))

			Expect(classificationTime).To(BeNumerically("<", 10*time.Millisecond))
			Expect(memoryUsedMB).To(BeNumerically("<", 5.0))
		})
	})

	Describe("Memory Efficiency", func() {
		It("should not leak memory during repeated operations", func() {
			By("Performing repeated operations and measuring memory growth")

			initialMem := getMemoryUsage()

			// Simulate continuous operation
			for cycle := 0; cycle < 10; cycle++ {
				// Create temporary objects
				states := make([]*apis.ReplicaState, 50)

				for i := 0; i < 50; i++ {
					state := &apis.ReplicaState{
						TotalReplicas:   int32(i + 1),
						CurrentOnDemand: int32(i / 2),
						CurrentSpot:     int32((i + 1) / 2),
					}

					config := apis.WorkloadConfiguration{
						Enabled:        true,
						MinOnDemand:    1,
						SpotPercentage: 70,
					}

					state.CalculateDesiredDistribution(config)
					states[i] = state
				}

				// Force garbage collection between cycles
				runtime.GC()

				// Clear references
				for i := range states {
					states[i] = nil
				}
				states = nil
			}

			// Final garbage collection
			runtime.GC()
			finalMem := getMemoryUsage()

			memoryGrowthMB := float64(finalMem-initialMem) / 1024 / 1024

			By(fmt.Sprintf("Memory growth after 10 cycles: %.2f MB", memoryGrowthMB))

			// Should not have significant memory growth
			Expect(memoryGrowthMB).To(BeNumerically("<", 5.0))
		})
	})

	Describe("Concurrent Operation Performance", func() {
		It("should handle concurrent replica state calculations safely", func() {
			By("Running concurrent replica calculations")

			startTime := time.Now()

			done := make(chan bool, 10)

			// Start 10 concurrent goroutines
			for worker := 0; worker < 10; worker++ {
				go func(workerID int) {
					defer func() { done <- true }()

					for i := 0; i < 100; i++ {
						state := &apis.ReplicaState{
							TotalReplicas:   int32((workerID*100+i)%50 + 1),
							CurrentOnDemand: int32((workerID + i) % 25),
							CurrentSpot:     int32((workerID + i) % 25),
						}

						config := apis.WorkloadConfiguration{
							Enabled:        true,
							MinOnDemand:    int32(workerID % 5),
							SpotPercentage: int32((i % 10) * 10),
						}

						state.CalculateDesiredDistribution(config)
						_ = state.GetNextAction()
						_ = state.NeedsReconciliation()
					}
				}(worker)
			}

			// Wait for all workers to complete
			for i := 0; i < 10; i++ {
				<-done
			}

			concurrentTime := time.Since(startTime)

			By(fmt.Sprintf("1000 concurrent calculations completed in %v", concurrentTime))

			// Should handle concurrency efficiently
			Expect(concurrentTime).To(BeNumerically("<", 500*time.Millisecond))
		})
	})
})

// Helper functions
func getMemoryUsage() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}
