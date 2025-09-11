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

package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// LeaderElectionConfig contains leader election configuration
type LeaderElectionConfig struct {
	// Basic configuration
	Enabled   bool
	ID        string
	Namespace string
	LeaseName string

	// Timing configuration
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration

	// Identity configuration
	Identity string

	// Callbacks
	OnStartedLeading func(context.Context)
	OnStoppedLeading func()
	OnNewLeader      func(identity string)
}

// DefaultLeaderElectionConfig returns default leader election configuration
func DefaultLeaderElectionConfig() *LeaderElectionConfig {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	return &LeaderElectionConfig{
		Enabled:       true,
		ID:            "spotalis-controller-leader",
		Namespace:     "spotalis-system",
		LeaseName:     "spotalis-controller-leader",
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Identity:      hostname,
	}
}

// LeaderElectionManager manages leader election for the controller
type LeaderElectionManager struct {
	config        *LeaderElectionConfig
	kubeClient    kubernetes.Interface
	manager       manager.Manager
	leaderElector *leaderelection.LeaderElector

	// State
	isLeader      bool
	currentLeader string
	startTime     time.Time

	// Channels for communication
	started       chan struct{}
	stopped       chan struct{}
	leaderChanged chan string
}

// NewLeaderElectionManager creates a new leader election manager
func NewLeaderElectionManager(config *LeaderElectionConfig, kubeClient kubernetes.Interface, mgr manager.Manager) (*LeaderElectionManager, error) {
	if config == nil {
		config = DefaultLeaderElectionConfig()
	}

	// Generate identity if not provided
	if config.Identity == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "unknown"
		}
		config.Identity = hostname
	}

	return &LeaderElectionManager{
		config:        config,
		kubeClient:    kubeClient,
		manager:       mgr,
		started:       make(chan struct{}),
		stopped:       make(chan struct{}),
		leaderChanged: make(chan string, 10),
	}, nil
}

// Start starts the leader election process
func (l *LeaderElectionManager) Start(ctx context.Context) error {
	if !l.config.Enabled {
		// If leader election is disabled, we're always the leader
		l.isLeader = true
		l.currentLeader = l.config.Identity
		l.startTime = time.Now()
		close(l.started)

		// Call the started leading callback if provided
		if l.config.OnStartedLeading != nil {
			go l.config.OnStartedLeading(ctx)
		}

		// Wait for context cancellation
		<-ctx.Done()
		close(l.stopped)
		return nil
	}

	// Create resource lock
	lock, err := l.createResourceLock()
	if err != nil {
		return fmt.Errorf("failed to create resource lock: %w", err)
	}

	// Create leader election config
	leConfig := leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: l.config.LeaseDuration,
		RenewDeadline: l.config.RenewDeadline,
		RetryPeriod:   l.config.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: l.onStartedLeading,
			OnStoppedLeading: l.onStoppedLeading,
			OnNewLeader:      l.onNewLeader,
		},
	}

	// Create leader elector
	leaderElector, err := leaderelection.NewLeaderElector(leConfig)
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	l.leaderElector = leaderElector
	l.startTime = time.Now()

	// Start leader election
	setupLog := ctrl.Log.WithName("leader-election")
	setupLog.Info("Starting leader election",
		"identity", l.config.Identity,
		"lease-name", l.config.LeaseName,
		"namespace", l.config.Namespace,
		"lease-duration", l.config.LeaseDuration,
	)

	go func() {
		l.leaderElector.Run(ctx)
		close(l.stopped)
	}()

	return nil
}

// IsLeader returns true if this instance is the current leader
func (l *LeaderElectionManager) IsLeader() bool {
	return l.isLeader
}

// GetCurrentLeader returns the identity of the current leader
func (l *LeaderElectionManager) GetCurrentLeader() string {
	return l.currentLeader
}

// GetIdentity returns the identity of this instance
func (l *LeaderElectionManager) GetIdentity() string {
	return l.config.Identity
}

// WaitForLeadership waits until this instance becomes the leader or context is cancelled
func (l *LeaderElectionManager) WaitForLeadership(ctx context.Context) error {
	if !l.config.Enabled || l.isLeader {
		return nil
	}

	select {
	case <-l.started:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetLeadershipInfo returns information about the current leadership state
func (l *LeaderElectionManager) GetLeadershipInfo() *LeadershipInfo {
	var leaseInfo *LeaseInfo
	if l.leaderElector != nil && l.config.Enabled {
		leaseInfo = l.getLeaseInfo()
	}

	return &LeadershipInfo{
		Enabled:       l.config.Enabled,
		IsLeader:      l.isLeader,
		CurrentLeader: l.currentLeader,
		Identity:      l.config.Identity,
		StartTime:     l.startTime,
		LeaseInfo:     leaseInfo,
	}
}

// LeadershipInfo contains information about the leadership state
type LeadershipInfo struct {
	Enabled       bool
	IsLeader      bool
	CurrentLeader string
	Identity      string
	StartTime     time.Time
	LeaseInfo     *LeaseInfo
}

// LeaseInfo contains information about the lease object
type LeaseInfo struct {
	Name              string
	Namespace         string
	HolderIdentity    string
	LeaseDuration     time.Duration
	AcquireTime       time.Time
	RenewTime         time.Time
	LeaderTransitions int32
}

// createResourceLock creates the resource lock for leader election
func (l *LeaderElectionManager) createResourceLock() (resourcelock.Interface, error) {
	return resourcelock.New(
		resourcelock.LeasesResourceLock,
		l.config.Namespace,
		l.config.LeaseName,
		l.kubeClient.CoreV1(),
		l.kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: l.config.Identity,
		},
	)
}

// onStartedLeading is called when this instance becomes the leader
func (l *LeaderElectionManager) onStartedLeading(ctx context.Context) {
	setupLog := ctrl.Log.WithName("leader-election")
	setupLog.Info("Started leading", "identity", l.config.Identity)

	l.isLeader = true
	l.currentLeader = l.config.Identity
	close(l.started)

	// Notify leadership change
	select {
	case l.leaderChanged <- l.config.Identity:
	default:
	}

	// Call user callback
	if l.config.OnStartedLeading != nil {
		l.config.OnStartedLeading(ctx)
	}
}

// onStoppedLeading is called when this instance stops being the leader
func (l *LeaderElectionManager) onStoppedLeading() {
	setupLog := ctrl.Log.WithName("leader-election")
	setupLog.Info("Stopped leading", "identity", l.config.Identity)

	l.isLeader = false

	// Call user callback
	if l.config.OnStoppedLeading != nil {
		l.config.OnStoppedLeading()
	}
}

// onNewLeader is called when a new leader is elected
func (l *LeaderElectionManager) onNewLeader(identity string) {
	setupLog := ctrl.Log.WithName("leader-election")
	setupLog.Info("New leader elected",
		"new-leader", identity,
		"current-identity", l.config.Identity,
		"is-self", identity == l.config.Identity,
	)

	l.currentLeader = identity

	// Notify leadership change
	select {
	case l.leaderChanged <- identity:
	default:
	}

	// Call user callback
	if l.config.OnNewLeader != nil {
		l.config.OnNewLeader(identity)
	}
}

// getLeaseInfo retrieves information about the lease object
func (l *LeaderElectionManager) getLeaseInfo() *LeaseInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lease, err := l.kubeClient.CoordinationV1().Leases(l.config.Namespace).Get(
		ctx, l.config.LeaseName, metav1.GetOptions{},
	)
	if err != nil {
		return nil
	}

	info := &LeaseInfo{
		Name:              lease.Name,
		Namespace:         lease.Namespace,
		LeaseDuration:     l.config.LeaseDuration,
		LeaderTransitions: lease.Spec.LeaseTransitions,
	}

	if lease.Spec.HolderIdentity != nil {
		info.HolderIdentity = *lease.Spec.HolderIdentity
	}

	if lease.Spec.AcquireTime != nil {
		info.AcquireTime = lease.Spec.AcquireTime.Time
	}

	if lease.Spec.RenewTime != nil {
		info.RenewTime = lease.Spec.RenewTime.Time
	}

	return info
}

// GetLeaderElectionMetrics returns metrics about leader election
func (l *LeaderElectionManager) GetLeaderElectionMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"enabled":        l.config.Enabled,
		"is_leader":      l.isLeader,
		"current_leader": l.currentLeader,
		"identity":       l.config.Identity,
		"uptime_seconds": time.Since(l.startTime).Seconds(),
	}

	if leaseInfo := l.getLeaseInfo(); leaseInfo != nil {
		metrics["lease_holder"] = leaseInfo.HolderIdentity
		metrics["lease_transitions"] = leaseInfo.LeaderTransitions
		if !leaseInfo.RenewTime.IsZero() {
			metrics["lease_renew_age_seconds"] = time.Since(leaseInfo.RenewTime).Seconds()
		}
	}

	return metrics
}

// Resign causes the current leader to give up leadership
func (l *LeaderElectionManager) Resign() error {
	if !l.config.Enabled || !l.isLeader {
		return fmt.Errorf("not currently the leader")
	}

	if l.leaderElector == nil {
		return fmt.Errorf("leader elector not initialized")
	}

	// Delete the lease to resign leadership
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := l.kubeClient.CoordinationV1().Leases(l.config.Namespace).Delete(
		ctx, l.config.LeaseName, metav1.DeleteOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to delete lease: %w", err)
	}

	return nil
}

// GetLeaderChanges returns a channel that receives leader change notifications
func (l *LeaderElectionManager) GetLeaderChanges() <-chan string {
	return l.leaderChanged
}
