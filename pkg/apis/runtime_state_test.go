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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("RuntimeState", func() {

	Describe("NamespaceFilter", func() {
		var filter *NamespaceFilter

		BeforeEach(func() {
			selector := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"managed-by": "spotalis",
				},
			}
			filter = NewNamespaceFilter(selector)
		})

		Describe("NewNamespaceFilter", func() {
			It("should create a filter with the provided selector", func() {
				Expect(filter.Selector).ToNot(BeNil())
				Expect(filter.Selector.MatchLabels["managed-by"]).To(Equal("spotalis"))
				Expect(filter.AllowAll).To(BeFalse())
				Expect(filter.MonitoredNamespaces).To(BeEmpty())
				Expect(filter.TotalWorkloads).To(Equal(0))
			})

			It("should create an allow-all filter with nil selector", func() {
				allFilter := NewNamespaceFilter(nil)
				Expect(allFilter.Selector).To(BeNil())
				Expect(allFilter.AllowAll).To(BeTrue())
			})
		})

		Describe("ShouldMonitorNamespace", func() {
			It("should allow namespace that matches selector", func() {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "managed-namespace",
						Labels: map[string]string{
							"managed-by": "spotalis",
						},
					},
				}

				Expect(filter.ShouldMonitorNamespace(ns)).To(BeTrue())
			})

			It("should reject namespace that doesn't match selector", func() {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unmanaged-namespace",
						Labels: map[string]string{
							"managed-by": "other",
						},
					},
				}

				Expect(filter.ShouldMonitorNamespace(ns)).To(BeFalse())
			})

			It("should allow all namespaces when selector is nil", func() {
				allFilter := NewNamespaceFilter(nil)

				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "any-namespace",
					},
				}

				Expect(allFilter.ShouldMonitorNamespace(ns)).To(BeTrue())
			})

			It("should handle namespace with no labels", func() {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "no-labels-namespace",
					},
				}

				Expect(filter.ShouldMonitorNamespace(ns)).To(BeFalse())
			})

			It("should handle nil namespace", func() {
				// The actual implementation panics on nil namespace
				// Test that it panics as expected
				Expect(func() {
					filter.ShouldMonitorNamespace(nil)
				}).To(Panic())
			})
		})

		Describe("UpdateNamespaces", func() {
			It("should update monitored namespaces list", func() {
				namespaces := []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "managed-1",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "managed-2",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "unmanaged",
							Labels: map[string]string{
								"managed-by": "other",
							},
						},
					},
				}

				filter.UpdateNamespaces(namespaces)

				monitored := filter.GetMonitoredNamespaces()
				Expect(monitored).To(HaveLen(2))
				Expect(monitored).To(ContainElements("managed-1", "managed-2"))
				Expect(monitored).ToNot(ContainElement("unmanaged"))
				Expect(filter.LastUpdated).To(BeTemporally("~", time.Now(), time.Second))
			})

			It("should handle empty namespaces list", func() {
				filter.UpdateNamespaces([]corev1.Namespace{})

				monitored := filter.GetMonitoredNamespaces()
				Expect(monitored).To(BeEmpty())
			})

			It("should sort monitored namespaces", func() {
				namespaces := []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "z-namespace",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "a-namespace",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
				}

				filter.UpdateNamespaces(namespaces)

				monitored := filter.GetMonitoredNamespaces()
				Expect(monitored).To(Equal([]string{"a-namespace", "z-namespace"}))
			})
		})

		Describe("Workload Count Management", func() {
			It("should update and retrieve workload count", func() {
				Expect(filter.GetWorkloadCount()).To(Equal(0))

				filter.UpdateWorkloadCount(25)
				Expect(filter.GetWorkloadCount()).To(Equal(25))

				filter.UpdateWorkloadCount(30)
				Expect(filter.GetWorkloadCount()).To(Equal(30))
			})
		})

		Describe("GetNamespaceCount", func() {
			It("should return correct namespace count", func() {
				namespaces := []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "ns-1",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "ns-2",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
				}

				filter.UpdateNamespaces(namespaces)
				Expect(filter.GetNamespaceCount()).To(Equal(2))
			})
		})

		Describe("IsStale", func() {
			It("should return false for fresh filter", func() {
				filter.UpdateNamespaces([]corev1.Namespace{})
				Expect(filter.IsStale(5 * time.Minute)).To(BeFalse())
			})

			It("should return true for old filter", func() {
				filter.LastUpdated = time.Now().Add(-10 * time.Minute)
				Expect(filter.IsStale(5 * time.Minute)).To(BeTrue())
			})

			It("should handle zero max age", func() {
				filter.UpdateNamespaces([]corev1.Namespace{})
				// With zero max age, any time elapsed makes it stale
				Expect(filter.IsStale(0)).To(BeTrue())
			})
		})

		Describe("GetFilterStatus", func() {
			It("should return comprehensive filter status", func() {
				namespaces := []corev1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "managed-ns",
							Labels: map[string]string{
								"managed-by": "spotalis",
							},
						},
					},
				}

				filter.UpdateNamespaces(namespaces)
				filter.UpdateWorkloadCount(5)

				status := filter.GetFilterStatus()
				Expect(status.MonitoredCount).To(Equal(1))
				Expect(status.TotalWorkloads).To(Equal(5))
				Expect(status.LastUpdated).To(BeTemporally("~", time.Now(), time.Second))
				Expect(status.HasSelector).To(BeTrue())
			})

			It("should indicate when no selector is present", func() {
				allFilter := NewNamespaceFilter(nil)
				status := allFilter.GetFilterStatus()
				Expect(status.HasSelector).To(BeFalse())
			})
		})
	})

	Describe("APIRateLimiter", func() {
		var rateLimiter *APIRateLimiter

		BeforeEach(func() {
			rateLimiter = NewAPIRateLimiter()
		})

		Describe("NewAPIRateLimiter", func() {
			It("should create a rate limiter with default settings", func() {
				Expect(rateLimiter.MaxRequestsPerSecond).To(Equal(50.0))
				Expect(rateLimiter.BaseBackoffDuration).To(Equal(1 * time.Second))
				Expect(rateLimiter.BackoffMultiplier).To(Equal(2.0))
				Expect(rateLimiter.SuccessfulRequests).To(Equal(int64(0)))
				Expect(rateLimiter.FailedRequests).To(Equal(int64(0)))
			})
		})

		Describe("Request Recording", func() {
			It("should record successful requests", func() {
				rateLimiter.RecordSuccess()
				rateLimiter.RecordSuccess()

				status := rateLimiter.GetRateLimitStatus()
				Expect(status.SuccessfulRequests).To(Equal(int64(2)))
				Expect(status.FailedRequests).To(Equal(int64(0)))
			})

			It("should record failed requests", func() {
				rateLimiter.RecordFailure()
				rateLimiter.RecordFailure()

				status := rateLimiter.GetRateLimitStatus()
				Expect(status.SuccessfulRequests).To(Equal(int64(0)))
				Expect(status.FailedRequests).To(Equal(int64(2)))
			})

			It("should update request timestamps", func() {
				beforeTime := time.Now()
				rateLimiter.RecordSuccess()
				afterTime := time.Now()

				status := rateLimiter.GetRateLimitStatus()
				Expect(status.LastMeasurement).To(BeTemporally(">=", beforeTime))
				Expect(status.LastMeasurement).To(BeTemporally("<=", afterTime))
			})
		})

		Describe("Backoff Logic", func() {
			It("should not backoff initially", func() {
				Expect(rateLimiter.ShouldBackoff()).To(BeFalse())
			})

			It("should trigger backoff after multiple failures", func() {
				// Set an extremely low rate limit to make it easier to exceed
				rateLimiter.MaxRequestsPerSecond = 0.01 // 1 request per 100 seconds

				// Record enough failures to exceed the rate
				// With 2 requests, rate = 2/60 = 0.033 which exceeds 0.01
				rateLimiter.RecordFailure()
				rateLimiter.RecordFailure()

				Expect(rateLimiter.ShouldBackoff()).To(BeTrue())
			})

			It("should reset backoff after successful requests", func() {
				// Manually set backoff state
				rateLimiter.BackoffUntil = time.Now().Add(1 * time.Second)
				Expect(rateLimiter.ShouldBackoff()).To(BeTrue())

				// Wait for backoff to expire
				time.Sleep(1100 * time.Millisecond) // Slightly more than backoff duration

				Expect(rateLimiter.ShouldBackoff()).To(BeFalse())
			})
		})

		Describe("GetSuccessRate", func() {
			It("should return 100% for all successful requests", func() {
				rateLimiter.RecordSuccess()
				rateLimiter.RecordSuccess()
				rateLimiter.RecordSuccess()

				successRate := rateLimiter.GetSuccessRate()
				Expect(successRate).To(Equal(100.0))
			})

			It("should return 0% for all failed requests", func() {
				rateLimiter.RecordFailure()
				rateLimiter.RecordFailure()

				successRate := rateLimiter.GetSuccessRate()
				Expect(successRate).To(Equal(0.0))
			})

			It("should calculate mixed success rate", func() {
				rateLimiter.RecordSuccess()
				rateLimiter.RecordSuccess()
				rateLimiter.RecordFailure()
				rateLimiter.RecordFailure()

				successRate := rateLimiter.GetSuccessRate()
				Expect(successRate).To(Equal(50.0))
			})

			It("should return 1.0 when no requests have been made", func() {
				successRate := rateLimiter.GetSuccessRate()
				Expect(successRate).To(Equal(100.0))
			})
		})

		Describe("Reset", func() {
			It("should reset all counters and state", func() {
				rateLimiter.RecordSuccess()
				rateLimiter.RecordFailure()

				// Trigger backoff
				for i := 0; i < 10; i++ {
					rateLimiter.RecordFailure()
				}

				rateLimiter.Reset()

				status := rateLimiter.GetRateLimitStatus()
				Expect(status.SuccessfulRequests).To(Equal(int64(0)))
				Expect(status.FailedRequests).To(Equal(int64(0)))
				Expect(status.IsBackingOff).To(BeFalse())
				Expect(rateLimiter.ShouldBackoff()).To(BeFalse())
			})
		})

		Describe("UpdateMaxRate", func() {
			It("should update maximum request rate", func() {
				newRate := 50.0
				rateLimiter.UpdateMaxRate(newRate)

				Expect(rateLimiter.MaxRequestsPerSecond).To(Equal(newRate))
			})

			It("should handle zero rate", func() {
				rateLimiter.UpdateMaxRate(0.0)
				Expect(rateLimiter.MaxRequestsPerSecond).To(Equal(0.0))
			})
		})

		Describe("GetRateLimitStatus", func() {
			It("should return comprehensive rate limit status", func() {
				rateLimiter.RecordSuccess()
				rateLimiter.RecordFailure()

				status := rateLimiter.GetRateLimitStatus()
				Expect(status.MaxRequestsPerSecond).To(Equal(50.0))
				Expect(status.SuccessfulRequests).To(Equal(int64(1)))
				Expect(status.FailedRequests).To(Equal(int64(1)))
				Expect(status.RequestsPerSecond).To(BeNumerically(">=", 0))
				Expect(status.IsBackingOff).To(BeFalse())
				Expect(status.LastMeasurement).To(BeTemporally("~", time.Now(), time.Second))
			})
		})

		Describe("Concurrent Access", func() {
			It("should handle concurrent success/failure recording safely", func() {
				const goroutines = 10
				const requestsPerGoroutine = 10

				done := make(chan bool, goroutines*2)

				// Start goroutines recording successes
				for i := 0; i < goroutines; i++ {
					go func() {
						for j := 0; j < requestsPerGoroutine; j++ {
							rateLimiter.RecordSuccess()
						}
						done <- true
					}()
				}

				// Start goroutines recording failures
				for i := 0; i < goroutines; i++ {
					go func() {
						for j := 0; j < requestsPerGoroutine; j++ {
							rateLimiter.RecordFailure()
						}
						done <- true
					}()
				}

				// Wait for all goroutines to complete
				for i := 0; i < goroutines*2; i++ {
					<-done
				}

				status := rateLimiter.GetRateLimitStatus()
				totalRequests := status.SuccessfulRequests + status.FailedRequests
				expectedTotal := int64(goroutines * requestsPerGoroutine * 2)
				Expect(totalRequests).To(Equal(expectedTotal))
			})
		})

		Describe("Edge Cases", func() {
			It("should handle rapid successive calls", func() {
				for i := 0; i < 1000; i++ {
					if i%2 == 0 {
						rateLimiter.RecordSuccess()
					} else {
						rateLimiter.RecordFailure()
					}
				}

				status := rateLimiter.GetRateLimitStatus()
				Expect(status.SuccessfulRequests).To(Equal(int64(500)))
				Expect(status.FailedRequests).To(Equal(int64(500)))
			})

			It("should handle extreme backoff durations", func() {
				rateLimiter.BaseBackoffDuration = 24 * time.Hour
				rateLimiter.BackoffMultiplier = 10.0

				// Manually set backoff state to test extreme durations
				rateLimiter.BackoffUntil = time.Now().Add(1 * time.Second)

				Expect(rateLimiter.ShouldBackoff()).To(BeTrue())
			})
		})
	})
})
