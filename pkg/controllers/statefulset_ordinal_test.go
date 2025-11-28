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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExtractPodOrdinal(t *testing.T) {
	tests := []struct {
		name            string
		podName         string
		statefulSetName string
		expectedOrdinal int
		expectError     bool
	}{
		{
			name:            "ordinal 0",
			podName:         "web-0",
			statefulSetName: "web",
			expectedOrdinal: 0,
			expectError:     false,
		},
		{
			name:            "ordinal 5",
			podName:         "web-5",
			statefulSetName: "web",
			expectedOrdinal: 5,
			expectError:     false,
		},
		{
			name:            "large ordinal",
			podName:         "my-app-statefulset-15",
			statefulSetName: "my-app-statefulset",
			expectedOrdinal: 15,
			expectError:     false,
		},
		{
			name:            "very large ordinal",
			podName:         "web-99999",
			statefulSetName: "web",
			expectedOrdinal: 99999,
			expectError:     false,
		},
		{
			name:            "mismatched prefix",
			podName:         "other-pod-0",
			statefulSetName: "web",
			expectedOrdinal: -1,
			expectError:     true,
		},
		{
			name:            "invalid ordinal",
			podName:         "web-notanumber",
			statefulSetName: "web",
			expectedOrdinal: -1,
			expectError:     true,
		},
		{
			name:            "no ordinal",
			podName:         "web",
			statefulSetName: "web",
			expectedOrdinal: -1,
			expectError:     true,
		},
		{
			name:            "empty pod name",
			podName:         "",
			statefulSetName: "web",
			expectedOrdinal: -1,
			expectError:     true,
		},
		{
			name:            "hyphenated statefulset name",
			podName:         "my-app-sts-3",
			statefulSetName: "my-app-sts",
			expectedOrdinal: 3,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ordinal, err := extractPodOrdinal(tt.podName, tt.statefulSetName)
			if tt.expectError {
				assert.Error(t, err, "expected error for test case: %s", tt.name)
			} else {
				assert.NoError(t, err, "unexpected error for test case: %s", tt.name)
				assert.Equal(t, tt.expectedOrdinal, ordinal, "ordinal mismatch for test case: %s", tt.name)
			}
		})
	}
}

func TestSortPodsByOrdinal_Descending(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-4"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-0"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-2"}},
	}

	sorted := sortPodsByOrdinal(pods, "web", true)

	expectedOrder := []string{"web-4", "web-2", "web-1", "web-0"}
	assert.Equal(t, len(expectedOrder), len(sorted), "sorted list length mismatch")
	for i, pod := range sorted {
		assert.Equal(t, expectedOrder[i], pod.Name, "pod order mismatch at index %d", i)
	}
}

func TestSortPodsByOrdinal_Ascending(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-3"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-0"}},
	}

	sorted := sortPodsByOrdinal(pods, "web", false)

	expectedOrder := []string{"web-0", "web-1", "web-3"}
	assert.Equal(t, len(expectedOrder), len(sorted), "sorted list length mismatch")
	for i, pod := range sorted {
		assert.Equal(t, expectedOrder[i], pod.Name, "pod order mismatch at index %d", i)
	}
}

func TestSortPodsByOrdinal_InvalidOrdinals(t *testing.T) {
	// Pods with invalid ordinals should be at the end (with ordinal -1)
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unknown-pod"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-invalid"}},
	}

	sorted := sortPodsByOrdinal(pods, "web", true)

	// Valid ordinals first (2, 1 in descending order), then invalid (both -1)
	assert.Equal(t, "web-2", sorted[0].Name, "highest ordinal should be first")
	assert.Equal(t, "web-1", sorted[1].Name, "second highest ordinal should be second")
	// Invalid pods should be at the end
	invalidCount := 0
	for i := 2; i < len(sorted); i++ {
		if sorted[i].Name == "unknown-pod" || sorted[i].Name == "web-invalid" {
			invalidCount++
		}
	}
	assert.Equal(t, 2, invalidCount, "both invalid pods should be at the end")
}

func TestSortPodsByOrdinal_EmptyList(t *testing.T) {
	pods := []corev1.Pod{}
	sorted := sortPodsByOrdinal(pods, "web", true)
	assert.Equal(t, 0, len(sorted), "empty list should remain empty")
}

func TestSortPodsByOrdinal_SinglePod(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-0"}},
	}
	sorted := sortPodsByOrdinal(pods, "web", true)
	assert.Equal(t, 1, len(sorted), "single pod list should have one element")
	assert.Equal(t, "web-0", sorted[0].Name, "single pod should be preserved")
}

func TestSortPodsByOrdinal_LargeOrdinals(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-100"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-50"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-999"}},
	}

	sorted := sortPodsByOrdinal(pods, "web", true)

	expectedOrder := []string{"web-999", "web-100", "web-50"}
	assert.Equal(t, len(expectedOrder), len(sorted), "sorted list length mismatch")
	for i, pod := range sorted {
		assert.Equal(t, expectedOrder[i], pod.Name, "pod order mismatch at index %d", i)
	}
}

func TestGetPodManagementPolicy(t *testing.T) {
	tests := []struct {
		name           string
		policy         appsv1.PodManagementPolicyType
		expectedPolicy appsv1.PodManagementPolicyType
	}{
		{
			name:           "empty defaults to OrderedReady",
			policy:         "",
			expectedPolicy: appsv1.OrderedReadyPodManagement,
		},
		{
			name:           "explicit OrderedReady",
			policy:         appsv1.OrderedReadyPodManagement,
			expectedPolicy: appsv1.OrderedReadyPodManagement,
		},
		{
			name:           "explicit Parallel",
			policy:         appsv1.ParallelPodManagement,
			expectedPolicy: appsv1.ParallelPodManagement,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					PodManagementPolicy: tt.policy,
				},
			}
			result := getPodManagementPolicy(sts)
			assert.Equal(t, tt.expectedPolicy, result, "policy mismatch for test case: %s", tt.name)
		})
	}
}

func TestIsOrderedReadyPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policy   appsv1.PodManagementPolicyType
		expected bool
	}{
		{
			name:     "empty defaults to OrderedReady",
			policy:   "",
			expected: true,
		},
		{
			name:     "explicit OrderedReady",
			policy:   appsv1.OrderedReadyPodManagement,
			expected: true,
		},
		{
			name:     "explicit Parallel",
			policy:   appsv1.ParallelPodManagement,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					PodManagementPolicy: tt.policy,
				},
			}
			result := isOrderedReadyPolicy(sts)
			assert.Equal(t, tt.expected, result, "result mismatch for test case: %s", tt.name)
		})
	}
}

func TestSortPodsByOrdinal_MixedValidInvalid(t *testing.T) {
	// Test with a mix of valid ordinals and invalid pod names
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "web-5"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "other-service-1"}}, // Different prefix
		{ObjectMeta: metav1.ObjectMeta{Name: "web-2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "web-abc"}}, // Invalid ordinal
		{ObjectMeta: metav1.ObjectMeta{Name: "web-7"}},
	}

	sorted := sortPodsByOrdinal(pods, "web", true)

	// First should be valid ordinals in descending order: 7, 5, 2
	assert.Equal(t, "web-7", sorted[0].Name, "highest valid ordinal should be first")
	assert.Equal(t, "web-5", sorted[1].Name, "second highest valid ordinal should be second")
	assert.Equal(t, "web-2", sorted[2].Name, "third highest valid ordinal should be third")

	// Invalid pods should be at the end
	invalidNames := map[string]bool{"other-service-1": true, "web-abc": true}
	for i := 3; i < len(sorted); i++ {
		assert.True(t, invalidNames[sorted[i].Name], "invalid pod should be at the end: %s", sorted[i].Name)
	}
}

func TestSortPodsByOrdinal_Stability(t *testing.T) {
	// Test that pods with same ordinal (-1 for invalid) maintain relative order
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "invalid-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "invalid-b"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "invalid-c"}},
	}

	sorted := sortPodsByOrdinal(pods, "web", true)

	// All should have ordinal -1, order should be preserved by stable sort
	assert.Equal(t, 3, len(sorted), "all pods should be in result")
	// Since all have ordinal -1, the sort should be stable (Go's sort.Slice is stable)
	for i, pod := range sorted {
		assert.Equal(t, pods[i].Name, pod.Name, "stable sort should preserve order for equal ordinals")
	}
}
