package webhook

import (
	"fmt"
	"sync"
	"time"
)

const (
	DefaultAdmissionTTL  = 30 * time.Second
	CleanupInterval      = 10 * time.Second
	DefaultPodListMaxAge = 5 * time.Second
)

// StaleReason describes why state is considered stale
type StaleReason string

const (
	StaleReasonNone       StaleReason = ""
	StaleReasonGeneration StaleReason = "workload_generation_changed"
	StaleReasonPodListAge StaleReason = "pod_list_too_old"
)

// StalenessCheck contains the result of staleness validation
type StalenessCheck struct {
	IsStale bool
	Reason  StaleReason
	Details string
}

// PendingAdmission tracks pending pod admissions for a workload
type PendingAdmission struct {
	PendingSpot     int32
	PendingOnDemand int32
	LastUpdated     time.Time
	Generation      int64

	// Task 005: Optimistic Concurrency fields
	PodListResourceVersion string    // RV when pods were listed
	PodListTime            time.Time // When pod list was fetched
}

// AdmissionStateTracker tracks pending admissions across workloads
type AdmissionStateTracker struct {
	mu      sync.RWMutex
	pending map[string]*PendingAdmission
	ttl     time.Duration
	stopCh  chan struct{}

	// Task 005: Optimistic Concurrency
	podListMaxAge time.Duration // Max age of pod list before considered stale
}

// NewAdmissionStateTracker creates a new admission state tracker
func NewAdmissionStateTracker(ttl time.Duration) *AdmissionStateTracker {
	if ttl <= 0 {
		ttl = DefaultAdmissionTTL
	}
	t := &AdmissionStateTracker{
		pending:       make(map[string]*PendingAdmission),
		ttl:           ttl,
		stopCh:        make(chan struct{}),
		podListMaxAge: DefaultPodListMaxAge,
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

// CheckStaleness validates if the cached state is still valid
func (t *AdmissionStateTracker) CheckStaleness(key string, currentGeneration int64) StalenessCheck {
	t.mu.RLock()
	defer t.mu.RUnlock()

	p, exists := t.pending[key]
	if !exists {
		// No prior state - not stale, just new
		return StalenessCheck{IsStale: false}
	}

	// Check generation mismatch
	if p.Generation != currentGeneration {
		return StalenessCheck{
			IsStale: true,
			Reason:  StaleReasonGeneration,
			Details: fmt.Sprintf("cached generation %d != current %d", p.Generation, currentGeneration),
		}
	}

	// Check pod list age
	if !p.PodListTime.IsZero() && time.Since(p.PodListTime) > t.podListMaxAge {
		return StalenessCheck{
			IsStale: true,
			Reason:  StaleReasonPodListAge,
			Details: fmt.Sprintf("pod list is %v old, max %v", time.Since(p.PodListTime), t.podListMaxAge),
		}
	}

	return StalenessCheck{IsStale: false}
}

// UpdatePodListMetadata records when pods were listed
func (t *AdmissionStateTracker) UpdatePodListMetadata(key string, resourceVersion string, generation int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p, exists := t.pending[key]
	if !exists {
		p = &PendingAdmission{Generation: generation}
		t.pending[key] = p
	}

	p.PodListResourceVersion = resourceVersion
	p.PodListTime = time.Now()
	p.LastUpdated = time.Now()
}

// ResetPending clears pending counts for a workload, typically due to staleness
func (t *AdmissionStateTracker) ResetPending(key string, newGeneration int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pending[key] = &PendingAdmission{
		Generation:  newGeneration,
		LastUpdated: time.Now(),
		PodListTime: time.Now(),
	}
}
