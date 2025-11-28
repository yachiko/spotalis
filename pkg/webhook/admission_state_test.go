package webhook

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAdmissionStateTracker_ConcurrentSafety(t *testing.T) {
	tracker := NewAdmissionStateTracker(30 * time.Second)
	defer tracker.Stop()

	const numGoroutines = 100
	const numIncrements = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	key := "default/Deployment/test"
	generation := int64(1)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			capacityType := capacityTypeSpot
			if idx%2 == 0 {
				capacityType = capacityTypeOnDemand
			}
			for j := 0; j < numIncrements; j++ {
				tracker.IncrementPending(key, capacityType, generation)
			}
		}(i)
	}

	wg.Wait()

	spot, onDemand := tracker.GetPendingCounts(key, generation)
	total := spot + onDemand
	expected := int32(numGoroutines * numIncrements)

	assert.Equal(t, expected, total, "Total pending count should match expected")
	assert.Equal(t, int32(500), spot, "Half should be spot")
	assert.Equal(t, int32(500), onDemand, "Half should be on-demand")
}

func TestAdmissionStateTracker_GenerationChange(t *testing.T) {
	tracker := NewAdmissionStateTracker(30 * time.Second)
	defer tracker.Stop()

	key := "default/Deployment/test"

	// Add counts with generation 1
	tracker.IncrementPending(key, capacityTypeSpot, 1)
	tracker.IncrementPending(key, capacityTypeSpot, 1)
	tracker.IncrementPending(key, capacityTypeOnDemand, 1)

	spot, onDemand := tracker.GetPendingCounts(key, 1)
	assert.Equal(t, int32(2), spot)
	assert.Equal(t, int32(1), onDemand)

	// Generation changed - should return 0
	spot, onDemand = tracker.GetPendingCounts(key, 2)
	assert.Equal(t, int32(0), spot)
	assert.Equal(t, int32(0), onDemand)

	// Add counts with generation 2
	tracker.IncrementPending(key, capacityTypeOnDemand, 2)

	spot, onDemand = tracker.GetPendingCounts(key, 2)
	assert.Equal(t, int32(0), spot)
	assert.Equal(t, int32(1), onDemand)
}

func TestAdmissionStateTracker_TTLCleanup(t *testing.T) {
	ttl := 100 * time.Millisecond
	tracker := NewAdmissionStateTracker(ttl)
	defer tracker.Stop()

	key := "default/Deployment/test"
	generation := int64(1)

	// Add entry
	tracker.IncrementPending(key, capacityTypeSpot, generation)

	spot, _ := tracker.GetPendingCounts(key, generation)
	assert.Equal(t, int32(1), spot)

	// Wait for TTL + cleanup interval
	time.Sleep(ttl + CleanupInterval + 50*time.Millisecond)

	// Manually trigger cleanup to ensure it runs
	tracker.cleanup()

	// Entry should be cleaned up
	tracker.mu.RLock()
	_, exists := tracker.pending[key]
	tracker.mu.RUnlock()

	assert.False(t, exists, "Entry should be removed after TTL")
}

func TestAdmissionStateTracker_WorkloadKey(t *testing.T) {
	tracker := NewAdmissionStateTracker(30 * time.Second)
	defer tracker.Stop()

	tests := []struct {
		namespace string
		kind      string
		name      string
		expected  string
	}{
		{"default", "Deployment", "test", "default/Deployment/test"},
		{"kube-system", "StatefulSet", "my-app", "kube-system/StatefulSet/my-app"},
		{"ns1", "Deployment", "app", "ns1/Deployment/app"},
	}

	for _, tt := range tests {
		key := tracker.WorkloadKey(tt.namespace, tt.kind, tt.name)
		assert.Equal(t, tt.expected, key)
	}
}

func TestAdmissionStateTracker_GetPendingCounts_NonExistent(t *testing.T) {
	tracker := NewAdmissionStateTracker(30 * time.Second)
	defer tracker.Stop()

	spot, onDemand := tracker.GetPendingCounts("nonexistent/key", 1)
	assert.Equal(t, int32(0), spot)
	assert.Equal(t, int32(0), onDemand)
}

func TestAdmissionStateTracker_MultipleWorkloads(t *testing.T) {
	tracker := NewAdmissionStateTracker(30 * time.Second)
	defer tracker.Stop()

	key1 := "ns1/Deployment/app1"
	key2 := "ns2/Deployment/app2"
	generation := int64(1)

	tracker.IncrementPending(key1, capacityTypeSpot, generation)
	tracker.IncrementPending(key1, capacityTypeSpot, generation)
	tracker.IncrementPending(key2, capacityTypeOnDemand, generation)

	spot1, od1 := tracker.GetPendingCounts(key1, generation)
	spot2, od2 := tracker.GetPendingCounts(key2, generation)

	assert.Equal(t, int32(2), spot1)
	assert.Equal(t, int32(0), od1)
	assert.Equal(t, int32(0), spot2)
	assert.Equal(t, int32(1), od2)
}
