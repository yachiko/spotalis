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

package annotations

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DisruptionWindow", func() {
	Describe("ParseDisruptionWindow", func() {
		Context("with valid annotations", func() {
			It("should parse schedule and duration", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 2 * * *",
					"spotalis.io/disruption-duration": "4h",
				}
				window, err := ParseDisruptionWindow(annots)
				Expect(err).NotTo(HaveOccurred())
				Expect(window).NotTo(BeNil())
			})

			It("should return nil when no annotations present", func() {
				annots := map[string]string{}
				window, err := ParseDisruptionWindow(annots)
				Expect(err).NotTo(HaveOccurred())
				Expect(window).To(BeNil())
			})
		})

		Context("with partial annotations", func() {
			It("should fail if only schedule is present", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 2 * * *",
				}
				_, err := ParseDisruptionWindow(annots)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("must be specified together"))
			})

			It("should fail if only duration is present", func() {
				annots := map[string]string{
					"spotalis.io/disruption-duration": "4h",
				}
				_, err := ParseDisruptionWindow(annots)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("must be specified together"))
			})
		})

		Context("with invalid cron expression", func() {
			It("should fail with invalid cron format", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "invalid",
					"spotalis.io/disruption-duration": "4h",
				}
				_, err := ParseDisruptionWindow(annots)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid schedule"))
			})

			It("should fail with 6-field cron (only 5-field supported)", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 0 2 * * *", // 6 fields
					"spotalis.io/disruption-duration": "4h",
				}
				_, err := ParseDisruptionWindow(annots)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with invalid duration", func() {
			It("should fail with invalid duration format", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 2 * * *",
					"spotalis.io/disruption-duration": "invalid",
				}
				_, err := ParseDisruptionWindow(annots)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid duration"))
			})
		})
	})

	Describe("IsWithinWindow", func() {
		Context("with no window configured", func() {
			It("should always return true", func() {
				var window *DisruptionWindow
				result := window.IsWithinWindow(time.Now())
				Expect(result).To(BeTrue())
			})
		})

		Context("with daily 2 AM UTC window", func() {
			var window *DisruptionWindow

			BeforeEach(func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 2 * * *", // Daily at 2 AM UTC
					"spotalis.io/disruption-duration": "4h",        // 4-hour window (2 AM - 6 AM)
				}
				var err error
				window, err = ParseDisruptionWindow(annots)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return true at 4 AM UTC (within window)", func() {
				testTime := time.Date(2025, 10, 6, 4, 0, 0, 0, time.UTC)
				result := window.IsWithinWindow(testTime)
				Expect(result).To(BeTrue())
			})

			It("should return false at 8 AM UTC (outside window)", func() {
				testTime := time.Date(2025, 10, 6, 8, 0, 0, 0, time.UTC)
				result := window.IsWithinWindow(testTime)
				Expect(result).To(BeFalse())
			})

			It("should return true at 2:00 AM UTC (window start)", func() {
				testTime := time.Date(2025, 10, 6, 2, 0, 0, 0, time.UTC)
				result := window.IsWithinWindow(testTime)
				Expect(result).To(BeTrue())
			})

			It("should return false at 6:00 AM UTC (window end)", func() {
				testTime := time.Date(2025, 10, 6, 6, 0, 0, 0, time.UTC)
				result := window.IsWithinWindow(testTime)
				Expect(result).To(BeFalse())
			})
		})

		Context("with different duration formats", func() {
			It("should handle 30m duration", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 12 * * *", // Daily at noon
					"spotalis.io/disruption-duration": "30m",
				}
				window, err := ParseDisruptionWindow(annots)
				Expect(err).NotTo(HaveOccurred())

				// 12:15 PM should be within window
				testTime := time.Date(2025, 10, 6, 12, 15, 0, 0, time.UTC)
				Expect(window.IsWithinWindow(testTime)).To(BeTrue())

				// 1:00 PM should be outside window
				testTime = time.Date(2025, 10, 6, 13, 0, 0, 0, time.UTC)
				Expect(window.IsWithinWindow(testTime)).To(BeFalse())
			})

			It("should handle 2h30m duration", func() {
				annots := map[string]string{
					"spotalis.io/disruption-schedule": "0 14 * * *", // Daily at 2 PM
					"spotalis.io/disruption-duration": "2h30m",
				}
				window, err := ParseDisruptionWindow(annots)
				Expect(err).NotTo(HaveOccurred())

				// 4:00 PM should be within window (2 PM + 2h = 4 PM, still within 2h30m)
				testTime := time.Date(2025, 10, 6, 16, 0, 0, 0, time.UTC)
				Expect(window.IsWithinWindow(testTime)).To(BeTrue())

				// 5:00 PM should be outside window
				testTime = time.Date(2025, 10, 6, 17, 0, 0, 0, time.UTC)
				Expect(window.IsWithinWindow(testTime)).To(BeFalse())
			})
		})
	})
})
