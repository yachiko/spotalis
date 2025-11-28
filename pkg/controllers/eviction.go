package controllers

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EvictionResult represents the outcome of an eviction attempt.
type EvictionResult string

const (
	// EvictionResultEvicted indicates the pod was successfully evicted.
	EvictionResultEvicted EvictionResult = "Evicted"

	// EvictionResultPDBBlocked indicates eviction was blocked by PodDisruptionBudget.
	EvictionResultPDBBlocked EvictionResult = "PDBBlocked"

	// EvictionResultAlreadyGone indicates the pod no longer exists.
	EvictionResultAlreadyGone EvictionResult = "AlreadyGone"

	// EvictionResultError indicates an unexpected error occurred.
	EvictionResultError EvictionResult = "Error"
)

// EvictPod attempts to evict a pod using the Eviction API, which respects PodDisruptionBudgets.
// Returns an EvictionResult and any error encountered.
//
// This function handles three common scenarios:
// 1. Successful eviction - returns (EvictionResultEvicted, nil)
// 2. PDB blocking eviction - returns (EvictionResultPDBBlocked, original error)
// 3. Pod already deleted - returns (EvictionResultAlreadyGone, nil)
// 4. Other errors - returns (EvictionResultError, original error)
func EvictPod(ctx context.Context, c client.Client, pod *corev1.Pod) (EvictionResult, error) {
	// Create eviction object
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{
			// Use default grace period from pod spec
			GracePeriodSeconds: pod.Spec.TerminationGracePeriodSeconds,
		},
	}

	// Attempt eviction via SubResource API
	err := c.SubResource("eviction").Create(ctx, pod, eviction)
	if err == nil {
		return EvictionResultEvicted, nil
	}

	// Handle specific error cases
	if apierrors.IsNotFound(err) {
		// Pod is already gone
		return EvictionResultAlreadyGone, nil
	}

	if apierrors.IsTooManyRequests(err) {
		// PDB is blocking the eviction (429 status code)
		return EvictionResultPDBBlocked, err
	}

	// Unexpected error
	return EvictionResultError, err
}

// CanEvict checks if a pod can be evicted without violating PodDisruptionBudgets.
// This is a dry-run check that doesn't actually evict the pod.
//
// Returns true if eviction would succeed, false if blocked by PDB.
// Returns error for unexpected failures.
func CanEvict(ctx context.Context, c client.Client, pod *corev1.Pod) (bool, error) {
	// Create eviction object with DryRun option
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{
			DryRun: []string{metav1.DryRunAll},
		},
	}

	// Attempt dry-run eviction
	err := c.SubResource("eviction").Create(ctx, pod, eviction)
	if err == nil {
		return true, nil
	}

	if apierrors.IsNotFound(err) {
		// Pod doesn't exist - technically can't evict, but not a PDB issue
		return false, fmt.Errorf("pod not found: %w", err)
	}

	if apierrors.IsTooManyRequests(err) {
		// PDB would block eviction
		return false, nil
	}

	// Unexpected error
	return false, fmt.Errorf("dry-run eviction failed: %w", err)
}

// evictionError is returned when eviction fails due to PDB constraints.
type evictionError struct {
	pod    string
	reason string
	err    error
}

func (e *evictionError) Error() string {
	return fmt.Sprintf("eviction blocked for pod %s: %s", e.pod, e.reason)
}

func (e *evictionError) Unwrap() error {
	return e.err
}

// NewEvictionBlockedError creates an error indicating PDB blocked the eviction.
func NewEvictionBlockedError(podName, reason string, err error) error {
	return &evictionError{
		pod:    podName,
		reason: reason,
		err:    err,
	}
}

// IsEvictionBlocked checks if an error is due to PDB blocking eviction.
func IsEvictionBlocked(err error) bool {
	var evictErr *evictionError
	return errors.As(err, &evictErr)
}
