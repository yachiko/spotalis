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
	"os/signal"
	"sync"
	"syscall"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

// ShutdownConfig contains configuration for graceful shutdown
type ShutdownConfig struct {
	// Timeouts
	GracefulTimeout  time.Duration
	ForceTimeout     time.Duration
	PreShutdownDelay time.Duration

	// Signals to handle
	ShutdownSignals []os.Signal

	// Hooks
	PreShutdownHooks  []ShutdownHook
	PostShutdownHooks []ShutdownHook

	// Options
	WaitForLeaderElection bool
	DrainConnections      bool
	FinishReconciliation  bool
}

// DefaultShutdownConfig returns default shutdown configuration
func DefaultShutdownConfig() *ShutdownConfig {
	return &ShutdownConfig{
		GracefulTimeout:       30 * time.Second,
		ForceTimeout:          60 * time.Second,
		PreShutdownDelay:      2 * time.Second,
		ShutdownSignals:       []os.Signal{syscall.SIGINT, syscall.SIGTERM},
		WaitForLeaderElection: true,
		DrainConnections:      true,
		FinishReconciliation:  true,
	}
}

// ShutdownHook represents a function called during shutdown
type ShutdownHook func(ctx context.Context) error

// ShutdownManager manages graceful shutdown of the operator
type ShutdownManager struct {
	config   *ShutdownConfig
	operator *Operator

	// State
	shutdownStarted bool
	shutdownReason  string
	shutdownTime    time.Time

	// Coordination
	shutdownChan    chan os.Signal
	shutdownContext context.Context
	shutdownCancel  context.CancelFunc

	// Synchronization
	mu              sync.RWMutex
	shutdownWG      sync.WaitGroup
	componentStates map[string]ComponentShutdownState
}

// ComponentShutdownState represents the shutdown state of a component
type ComponentShutdownState struct {
	Name      string
	State     ShutdownState
	StartTime time.Time
	EndTime   time.Time
	Error     error
}

// ShutdownState represents the state of shutdown for a component
type ShutdownState int

const (
	ShutdownStateUnknown ShutdownState = iota
	ShutdownStateStarted
	ShutdownStateInProgress
	ShutdownStateCompleted
	ShutdownStateFailed
)

func (s ShutdownState) String() string {
	switch s {
	case ShutdownStateStarted:
		return "started"
	case ShutdownStateInProgress:
		return "in_progress"
	case ShutdownStateCompleted:
		return "completed"
	case ShutdownStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// NewShutdownManager creates a new shutdown manager
func NewShutdownManager(config *ShutdownConfig, operator *Operator) *ShutdownManager {
	if config == nil {
		config = DefaultShutdownConfig()
	}

	shutdownContext, shutdownCancel := context.WithCancel(context.Background())

	return &ShutdownManager{
		config:          config,
		operator:        operator,
		shutdownChan:    make(chan os.Signal, 1),
		shutdownContext: shutdownContext,
		shutdownCancel:  shutdownCancel,
		componentStates: make(map[string]ComponentShutdownState),
	}
}

// Start starts the shutdown manager and signal handling
func (sm *ShutdownManager) Start(ctx context.Context) error {
	// Register signal handlers
	signal.Notify(sm.shutdownChan, sm.config.ShutdownSignals...)

	setupLog := ctrl.Log.WithName("shutdown-manager")
	setupLog.Info("Started shutdown manager",
		"graceful-timeout", sm.config.GracefulTimeout,
		"force-timeout", sm.config.ForceTimeout,
		"signals", sm.config.ShutdownSignals,
	)

	// Wait for shutdown signal or context cancellation
	select {
	case sig := <-sm.shutdownChan:
		setupLog.Info("Received shutdown signal", "signal", sig)
		return sm.initiateShutdown(ctx, fmt.Sprintf("signal: %v", sig))
	case <-ctx.Done():
		setupLog.Info("Context cancelled, initiating shutdown")
		return sm.initiateShutdown(ctx, "context cancelled")
	}
}

// InitiateShutdown starts the graceful shutdown process
func (sm *ShutdownManager) initiateShutdown(ctx context.Context, reason string) error {
	sm.mu.Lock()
	if sm.shutdownStarted {
		sm.mu.Unlock()
		return fmt.Errorf("shutdown already started")
	}
	sm.shutdownStarted = true
	sm.shutdownReason = reason
	sm.shutdownTime = time.Now()
	sm.mu.Unlock()

	setupLog := ctrl.Log.WithName("shutdown-manager")
	setupLog.Info("Initiating graceful shutdown",
		"reason", reason,
		"graceful-timeout", sm.config.GracefulTimeout,
	)

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), sm.config.GracefulTimeout)
	defer cancel()

	// Execute pre-shutdown delay
	if sm.config.PreShutdownDelay > 0 {
		setupLog.Info("Pre-shutdown delay", "delay", sm.config.PreShutdownDelay)
		time.Sleep(sm.config.PreShutdownDelay)
	}

	// Execute pre-shutdown hooks
	if err := sm.executePreShutdownHooks(shutdownCtx); err != nil {
		setupLog.Error(err, "Pre-shutdown hooks failed")
	}

	// Start shutdown process
	if err := sm.performGracefulShutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "Graceful shutdown failed, forcing shutdown")
		return sm.performForceShutdown()
	}

	// Execute post-shutdown hooks
	if err := sm.executePostShutdownHooks(shutdownCtx); err != nil {
		setupLog.Error(err, "Post-shutdown hooks failed")
	}

	setupLog.Info("Graceful shutdown completed",
		"duration", time.Since(sm.shutdownTime),
	)

	return nil
}

// performGracefulShutdown performs the graceful shutdown sequence
func (sm *ShutdownManager) performGracefulShutdown(ctx context.Context) error {
	setupLog := ctrl.Log.WithName("shutdown-manager")

	// Phase 1: Stop accepting new requests
	setupLog.Info("Phase 1: Stopping new requests")
	if err := sm.stopNewRequests(ctx); err != nil {
		return fmt.Errorf("failed to stop new requests: %w", err)
	}

	// Phase 2: Wait for leader election to transfer (if enabled)
	if sm.config.WaitForLeaderElection && sm.operator != nil {
		setupLog.Info("Phase 2: Transferring leadership")
		if err := sm.transferLeadership(ctx); err != nil {
			setupLog.Error(err, "Failed to transfer leadership, continuing shutdown")
		}
	}

	// Phase 3: Drain existing connections (if enabled)
	if sm.config.DrainConnections {
		setupLog.Info("Phase 3: Draining connections")
		if err := sm.drainConnections(ctx); err != nil {
			return fmt.Errorf("failed to drain connections: %w", err)
		}
	}

	// Phase 4: Finish ongoing reconciliations (if enabled)
	if sm.config.FinishReconciliation {
		setupLog.Info("Phase 4: Finishing reconciliations")
		if err := sm.finishReconciliations(ctx); err != nil {
			return fmt.Errorf("failed to finish reconciliations: %w", err)
		}
	}

	// Phase 5: Stop controllers and webhooks
	setupLog.Info("Phase 5: Stopping controllers and webhooks")
	if err := sm.stopControllers(ctx); err != nil {
		return fmt.Errorf("failed to stop controllers: %w", err)
	}

	// Phase 6: Cleanup resources
	setupLog.Info("Phase 6: Cleaning up resources")
	if err := sm.cleanupResources(ctx); err != nil {
		return fmt.Errorf("failed to cleanup resources: %w", err)
	}

	return nil
}

// performForceShutdown performs a forced shutdown
func (sm *ShutdownManager) performForceShutdown() error {
	setupLog := ctrl.Log.WithName("shutdown-manager")
	setupLog.Info("Performing force shutdown", "timeout", sm.config.ForceTimeout)

	// Create force shutdown context
	forceCtx, cancel := context.WithTimeout(context.Background(), sm.config.ForceTimeout)
	defer cancel()

	// Force stop all components
	if err := sm.forceStopComponents(forceCtx); err != nil {
		setupLog.Error(err, "Force shutdown failed")
		return err
	}

	return nil
}

// stopNewRequests stops accepting new HTTP requests
func (sm *ShutdownManager) stopNewRequests(ctx context.Context) error {
	sm.updateComponentState("http-server", ShutdownStateStarted)

	// TODO: Implement graceful HTTP server shutdown
	// This would typically involve:
	// 1. Removing the service from load balancer
	// 2. Stopping acceptance of new connections
	// 3. Setting readiness probe to fail

	sm.updateComponentState("http-server", ShutdownStateCompleted)
	return nil
}

// transferLeadership transfers leadership to another instance
func (sm *ShutdownManager) transferLeadership(ctx context.Context) error {
	sm.updateComponentState("leader-election", ShutdownStateStarted)

	if sm.operator == nil {
		sm.updateComponentState("leader-election", ShutdownStateCompleted)
		return nil
	}

	// TODO: Implement leadership transfer
	// This would typically involve:
	// 1. Releasing the leader lease
	// 2. Waiting for another instance to become leader
	// 3. Verifying leadership transfer

	sm.updateComponentState("leader-election", ShutdownStateCompleted)
	return nil
}

// drainConnections drains existing HTTP connections
func (sm *ShutdownManager) drainConnections(ctx context.Context) error {
	sm.updateComponentState("connection-drain", ShutdownStateStarted)

	// Wait for existing connections to finish
	// This is implementation-specific and would depend on the HTTP server

	sm.updateComponentState("connection-drain", ShutdownStateCompleted)
	return nil
}

// finishReconciliations waits for ongoing reconciliations to complete
func (sm *ShutdownManager) finishReconciliations(ctx context.Context) error {
	sm.updateComponentState("reconciliation", ShutdownStateStarted)

	// TODO: Implement reconciliation finishing
	// This would typically involve:
	// 1. Stopping new reconciliation requests
	// 2. Waiting for ongoing reconciliations to complete
	// 3. Setting a timeout for maximum wait time

	sm.updateComponentState("reconciliation", ShutdownStateCompleted)
	return nil
}

// stopControllers stops the controller manager
func (sm *ShutdownManager) stopControllers(ctx context.Context) error {
	sm.updateComponentState("controllers", ShutdownStateStarted)

	if sm.operator != nil && sm.operator.Manager != nil {
		// The manager will be stopped when the main context is cancelled
		sm.shutdownCancel()
	}

	sm.updateComponentState("controllers", ShutdownStateCompleted)
	return nil
}

// cleanupResources performs final resource cleanup
func (sm *ShutdownManager) cleanupResources(ctx context.Context) error {
	sm.updateComponentState("cleanup", ShutdownStateStarted)

	// TODO: Implement resource cleanup
	// This would typically involve:
	// 1. Cleaning up temporary files
	// 2. Closing database connections
	// 3. Releasing locks
	// 4. Flushing logs

	sm.updateComponentState("cleanup", ShutdownStateCompleted)
	return nil
}

// forceStopComponents forcefully stops all components
func (sm *ShutdownManager) forceStopComponents(ctx context.Context) error {
	// Cancel the shutdown context to force stop everything
	sm.shutdownCancel()

	// Give a brief moment for components to react to cancellation
	select {
	case <-time.After(1 * time.Second):
	case <-ctx.Done():
	}

	return nil
}

// executePreShutdownHooks executes pre-shutdown hooks
func (sm *ShutdownManager) executePreShutdownHooks(ctx context.Context) error {
	for i, hook := range sm.config.PreShutdownHooks {
		hookName := fmt.Sprintf("pre-shutdown-hook-%d", i)
		sm.updateComponentState(hookName, ShutdownStateStarted)

		if err := hook(ctx); err != nil {
			sm.updateComponentStateWithError(hookName, ShutdownStateFailed, err)
			return fmt.Errorf("pre-shutdown hook %d failed: %w", i, err)
		}

		sm.updateComponentState(hookName, ShutdownStateCompleted)
	}
	return nil
}

// executePostShutdownHooks executes post-shutdown hooks
func (sm *ShutdownManager) executePostShutdownHooks(ctx context.Context) error {
	for i, hook := range sm.config.PostShutdownHooks {
		hookName := fmt.Sprintf("post-shutdown-hook-%d", i)
		sm.updateComponentState(hookName, ShutdownStateStarted)

		if err := hook(ctx); err != nil {
			sm.updateComponentStateWithError(hookName, ShutdownStateFailed, err)
			return fmt.Errorf("post-shutdown hook %d failed: %w", i, err)
		}

		sm.updateComponentState(hookName, ShutdownStateCompleted)
	}
	return nil
}

// updateComponentState updates the shutdown state of a component
func (sm *ShutdownManager) updateComponentState(componentName string, state ShutdownState) {
	sm.updateComponentStateWithError(componentName, state, nil)
}

// updateComponentStateWithError updates the shutdown state of a component with an error
func (sm *ShutdownManager) updateComponentStateWithError(componentName string, state ShutdownState, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	existing, exists := sm.componentStates[componentName]
	if !exists {
		existing = ComponentShutdownState{
			Name:      componentName,
			StartTime: time.Now(),
		}
	}

	existing.State = state
	existing.Error = err

	if state == ShutdownStateCompleted || state == ShutdownStateFailed {
		existing.EndTime = time.Now()
	}

	sm.componentStates[componentName] = existing
}

// GetShutdownStatus returns the current shutdown status
func (sm *ShutdownManager) GetShutdownStatus() *ShutdownStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	components := make(map[string]ComponentShutdownState)
	for k, v := range sm.componentStates {
		components[k] = v
	}

	return &ShutdownStatus{
		Started:         sm.shutdownStarted,
		Reason:          sm.shutdownReason,
		StartTime:       sm.shutdownTime,
		ComponentStates: components,
	}
}

// ShutdownStatus represents the current shutdown status
type ShutdownStatus struct {
	Started         bool
	Reason          string
	StartTime       time.Time
	ComponentStates map[string]ComponentShutdownState
}

// IsCompleted returns true if shutdown is completed
func (ss *ShutdownStatus) IsCompleted() bool {
	if !ss.Started {
		return false
	}

	for _, state := range ss.ComponentStates {
		if state.State != ShutdownStateCompleted && state.State != ShutdownStateFailed {
			return false
		}
	}

	return true
}

// HasErrors returns true if any component failed during shutdown
func (ss *ShutdownStatus) HasErrors() bool {
	for _, state := range ss.ComponentStates {
		if state.State == ShutdownStateFailed || state.Error != nil {
			return true
		}
	}
	return false
}

// GetDuration returns the total shutdown duration
func (ss *ShutdownStatus) GetDuration() time.Duration {
	if !ss.Started {
		return 0
	}
	return time.Since(ss.StartTime)
}
