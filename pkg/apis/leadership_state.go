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
)

// LeadershipState represents the current leader election state for controller coordination
type LeadershipState struct {
	// IsLeader indicates whether this instance is the current leader
	IsLeader bool `json:"isLeader"`

	// LeaderIdentity is the identity of the current leader
	LeaderIdentity string `json:"leaderIdentity"`

	// LeaseName is the name of the coordination lease
	LeaseName string `json:"leaseName"`

	// LeaseNamespace is the namespace of the coordination lease
	LeaseNamespace string `json:"leaseNamespace"`

	// AcquiredAt is when leadership was acquired
	AcquiredAt time.Time `json:"acquiredAt"`

	// RenewedAt is the last successful lease renewal
	RenewedAt time.Time `json:"renewedAt"`

	// LeaseDuration is how long the lease is valid
	LeaseDuration time.Duration `json:"leaseDuration"`

	// RenewDeadline is when the lease must be renewed by
	RenewDeadline time.Duration `json:"renewDeadline"`

	// RetryPeriod is how often to attempt lease renewal
	RetryPeriod time.Duration `json:"retryPeriod"`

	// Transitions tracks leadership change events
	Transitions int64 `json:"transitions"`

	// LastTransition is when leadership last changed
	LastTransition time.Time `json:"lastTransition"`
}

// NewLeadershipState creates a new leadership state with default values
func NewLeadershipState(identity, leaseName, leaseNamespace string) *LeadershipState {
	return &LeadershipState{
		IsLeader:       false,
		LeaderIdentity: identity,
		LeaseName:      leaseName,
		LeaseNamespace: leaseNamespace,
		LeaseDuration:  15 * time.Second, // Default lease duration
		RenewDeadline:  10 * time.Second, // Renew before this deadline
		RetryPeriod:    2 * time.Second,  // Retry every 2 seconds
		Transitions:    0,
	}
}

// BecomeLeader transitions this instance to leader state
func (l *LeadershipState) BecomeLeader() {
	wasLeader := l.IsLeader
	l.IsLeader = true
	l.AcquiredAt = time.Now()
	l.RenewedAt = l.AcquiredAt

	if !wasLeader {
		l.Transitions++
		l.LastTransition = l.AcquiredAt
	}
}

// StepDown transitions this instance from leader to follower state
func (l *LeadershipState) StepDown() {
	wasLeader := l.IsLeader
	l.IsLeader = false

	if wasLeader {
		l.Transitions++
		l.LastTransition = time.Now()
	}
}

// RenewLease updates the lease renewal timestamp
func (l *LeadershipState) RenewLease() {
	l.RenewedAt = time.Now()
}

// GetLeadershipDuration returns how long this instance has been leader
func (l *LeadershipState) GetLeadershipDuration() time.Duration {
	if !l.IsLeader || l.AcquiredAt.IsZero() {
		return 0
	}
	return time.Since(l.AcquiredAt)
}

// GetTimeSinceLastRenewal returns how long since the lease was last renewed
func (l *LeadershipState) GetTimeSinceLastRenewal() time.Duration {
	if l.RenewedAt.IsZero() {
		return time.Duration(0)
	}
	return time.Since(l.RenewedAt)
}

// NeedsRenewal returns true if the lease should be renewed soon
func (l *LeadershipState) NeedsRenewal() bool {
	if !l.IsLeader {
		return false
	}
	return l.GetTimeSinceLastRenewal() >= l.RetryPeriod
}

// IsLeaseExpired returns true if the lease has expired
func (l *LeadershipState) IsLeaseExpired() bool {
	return l.GetTimeSinceLastRenewal() > l.LeaseDuration
}

// ShouldStepDown returns true if this instance should step down due to lease expiration
func (l *LeadershipState) ShouldStepDown() bool {
	if !l.IsLeader {
		return false
	}
	return l.GetTimeSinceLastRenewal() > l.RenewDeadline
}

// GetHealthStatus returns the current health status of leadership
func (l *LeadershipState) GetHealthStatus() LeadershipHealth {
	if !l.IsLeader {
		return LeadershipHealth{
			Status:     "follower",
			Healthy:    true,
			Message:    "Running in follower mode",
			Leadership: "follower",
		}
	}

	timeSinceRenewal := l.GetTimeSinceLastRenewal()

	if timeSinceRenewal > l.RenewDeadline {
		return LeadershipHealth{
			Status:     "unhealthy",
			Healthy:    false,
			Message:    "Lease renewal deadline exceeded",
			Leadership: "leader",
		}
	}

	if timeSinceRenewal > l.RetryPeriod {
		return LeadershipHealth{
			Status:     "warning",
			Healthy:    true,
			Message:    "Lease renewal overdue",
			Leadership: "leader",
		}
	}

	return LeadershipHealth{
		Status:     "healthy",
		Healthy:    true,
		Message:    "Leadership active",
		Leadership: "leader",
	}
}

// LeadershipHealth represents the health status of leadership
type LeadershipHealth struct {
	// Status is the overall health status
	Status string `json:"status"`

	// Healthy indicates if leadership is functioning correctly
	Healthy bool `json:"healthy"`

	// Message provides additional context
	Message string `json:"message"`

	// Leadership indicates current role (leader/follower)
	Leadership string `json:"leadership"`

	// LeadershipDuration is how long in current role
	LeadershipDuration time.Duration `json:"leadershipDuration,omitempty"`

	// LastRenewal is when lease was last renewed
	LastRenewal time.Time `json:"lastRenewal,omitempty"`
}

// LeadershipMetrics provides metrics about leadership state
type LeadershipMetrics struct {
	// LeaderElectionChanges is the number of times leadership has changed
	LeaderElectionChanges int64 `json:"leaderElectionChanges"`

	// LeadershipDuration is how long current instance has been leader
	LeadershipDuration time.Duration `json:"leadershipDuration"`

	// TimeSinceLastRenewal is how long since lease was renewed
	TimeSinceLastRenewal time.Duration `json:"timeSinceLastRenewal"`

	// IsCurrentLeader indicates if this instance is currently the leader
	IsCurrentLeader bool `json:"isCurrentLeader"`

	// LeaderIdentity is the identity of current leader
	LeaderIdentity string `json:"leaderIdentity"`

	// LeaseName is the name of the coordination lease
	LeaseName string `json:"leaseName"`

	// LeaseNamespace is the namespace of the coordination lease
	LeaseNamespace string `json:"leaseNamespace"`
}

// GetMetrics returns leadership metrics for monitoring
func (l *LeadershipState) GetMetrics() LeadershipMetrics {
	return LeadershipMetrics{
		LeaderElectionChanges: l.Transitions,
		LeadershipDuration:    l.GetLeadershipDuration(),
		TimeSinceLastRenewal:  l.GetTimeSinceLastRenewal(),
		IsCurrentLeader:       l.IsLeader,
		LeaderIdentity:        l.LeaderIdentity,
		LeaseName:             l.LeaseName,
		LeaseNamespace:        l.LeaseNamespace,
	}
}

// UpdateLeaderIdentity updates the identity of the current leader
func (l *LeadershipState) UpdateLeaderIdentity(identity string) {
	if l.LeaderIdentity != identity {
		l.LeaderIdentity = identity
		l.Transitions++
		l.LastTransition = time.Now()
	}
}

// GetTimeToNextRenewal returns when the next lease renewal should happen
func (l *LeadershipState) GetTimeToNextRenewal() time.Duration {
	if !l.IsLeader {
		return l.RetryPeriod // Followers try to acquire leadership
	}

	timeSinceRenewal := l.GetTimeSinceLastRenewal()
	if timeSinceRenewal >= l.RetryPeriod {
		return 0 // Should renew immediately
	}

	return l.RetryPeriod - timeSinceRenewal
}

// IsHealthy returns true if leadership is in a healthy state
func (l *LeadershipState) IsHealthy() bool {
	return l.GetHealthStatus().Healthy
}
