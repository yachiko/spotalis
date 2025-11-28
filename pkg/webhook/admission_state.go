package webhook

import (
	"sync"
	"time"
)

const (
	DefaultAdmissionTTL = 30 * time.Second
	CleanupInterval     = 10 * time.Second
)

// PendingAdmission tracks pending pod admissions for a workload
type PendingAdmission struct {
	PendingSpot     int32
	PendingOnDemand int32
	LastUpdated     time.Time
	Generation      int64
}

// AdmissionStateTracker tracks pending admissions across workloads
type AdmissionStateTracker struct {
	mu      sync.RWMutex
	pending map[string]*PendingAdmission
	ttl     time.Duration
	stopCh  chan struct{}
}

// NewAdmissionStateTracker creates a new admission state tracker
func NewAdmissionStateTracker(ttl time.Duration) *AdmissionStateTracker {
	if ttl <= 0 {
		ttl = DefaultAdmissionTTL
	}
	t := &AdmissionStateTracker{
		pending: make(map[string]*PendingAdmission),
		ttl:     ttl,
		stopCh:  make(chan struct{}),
	}
	go t.cleanupLoop()
	return t
}

// Stop stops the cleanup goroutine
func (t *AdmissionStateTracker) Stop() {
	close(t.stopCh)
}

// WorkloadKey generates a unique key for a workload
func (t *AdmissionStateTracker) WorkloadKey(namespace, kind, name string) string {
	return namespace + "/" + kind + "/" + name
}

// GetPendingCounts returns pending spot and on-demand counts for a workload
func (t *AdmissionStateTracker) GetPendingCounts(key string, currentGeneration int64) (spot, onDemand int32) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	p, exists := t.pending[key]
	if !exists {
		return 0, 0
	}

	// If generation changed, counts are stale
	if p.Generation != currentGeneration {
		return 0, 0
	}

	return p.PendingSpot, p.PendingOnDemand
}

// IncrementPending increments the pending count for a capacity type
func (t *AdmissionStateTracker) IncrementPending(key string, capacityType string, generation int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, exists := t.pending[key]
	if !exists || p.Generation != generation {
		p = &PendingAdmission{Generation: generation}
		t.pending[key] = p
	}

	if capacityType == capacityTypeSpot {
		p.PendingSpot++
	} else {
		p.PendingOnDemand++
	}
	p.LastUpdated = time.Now()
}

// cleanupLoop periodically removes stale entries
func (t *AdmissionStateTracker) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.cleanup()
		case <-t.stopCh:
			return
		}
	}
}

// cleanup removes entries older than TTL
func (t *AdmissionStateTracker) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for key, p := range t.pending {
		if now.Sub(p.LastUpdated) > t.ttl {
			delete(t.pending, key)
		}
	}
}
