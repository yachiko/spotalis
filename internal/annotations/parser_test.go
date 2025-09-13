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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("AnnotationParser", func() {

	var parser *AnnotationParser

	BeforeEach(func() {
		parser = NewAnnotationParser()
	})

	Describe("ParseWorkloadConfiguration", func() {
		Context("with valid annotations", func() {
			It("should parse all configuration values", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage":  "70",
						"spotalis.io/min-on-demand":    "2",
						"spotalis.io/replica-strategy": "spread",
						"spotalis.io/scaling-policy":   "gradual",
						"spotalis.io/max-replicas":     "20",
					},
				}

				config, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(BeNil())
				Expect(config.Enabled).To(BeTrue())
				Expect(config.SpotPercentage).To(Equal(int32(70)))
				Expect(config.MinOnDemand).To(Equal(int32(2)))
				Expect(config.ReplicaStrategy).To(Equal("spread"))
				Expect(config.ScalingPolicy).To(Equal("gradual"))
				Expect(config.MaxReplicas).To(Equal(int32(20)))
			})

			It("should parse minimal configuration", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "50",
					},
				}

				config, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(BeNil())
				Expect(config.Enabled).To(BeTrue())
				Expect(config.SpotPercentage).To(Equal(int32(50)))
				Expect(config.MinOnDemand).To(Equal(int32(0)))
				Expect(config.ReplicaStrategy).To(BeEmpty())
				Expect(config.ScalingPolicy).To(BeEmpty())
				Expect(config.MaxReplicas).To(Equal(int32(0)))
			})

			It("should handle zero values", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "0",
						"spotalis.io/min-on-demand":   "0",
						"spotalis.io/max-replicas":    "0",
					},
				}

				config, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(BeNil())
				Expect(config.SpotPercentage).To(Equal(int32(0)))
				Expect(config.MinOnDemand).To(Equal(int32(0)))
				Expect(config.MaxReplicas).To(Equal(int32(0)))
			})
		})

		Context("with invalid annotations", func() {
			It("should fail with invalid spot percentage", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "invalid",
					},
				}

				_, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid spot-percentage annotation"))
			})

			It("should fail with invalid min-on-demand", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/min-on-demand": "not-a-number",
					},
				}

				_, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid min-on-demand annotation"))
			})

			It("should fail with invalid max-replicas", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/max-replicas": "not-a-number",
					},
				}

				_, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid max-replicas annotation"))
			})

			It("should fail when no annotations exist", func() {
				obj := &MockObject{
					annotations: nil,
				}

				_, err := parser.ParseWorkloadConfiguration(obj)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no annotations found"))
			})
		})
	})

	Describe("HasSpotalisAnnotations", func() {
		It("should return true when spotalis annotations exist", func() {
			testCases := []map[string]string{
				{"spotalis.io/spot-percentage": "70"},
				{"spotalis.io/min-on-demand": "2"},
				{"spotalis.io/replica-strategy": "spread"},
				{"spotalis.io/scaling-policy": "gradual"},
				{"spotalis.io/max-replicas": "10"},
			}

			for _, annotations := range testCases {
				obj := &MockObject{annotations: annotations}
				Expect(parser.HasSpotalisAnnotations(obj)).To(BeTrue())
			}
		})

		It("should return false when no spotalis annotations exist", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"app":     "myapp",
					"version": "1.0",
				},
			}

			Expect(parser.HasSpotalisAnnotations(obj)).To(BeFalse())
		})

		It("should return false when no annotations exist", func() {
			obj := &MockObject{annotations: nil}
			Expect(parser.HasSpotalisAnnotations(obj)).To(BeFalse())
		})

		It("should return true when multiple spotalis annotations exist", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"spotalis.io/spot-percentage": "70",
					"spotalis.io/min-on-demand":   "2",
					"app":                         "myapp",
				},
			}

			Expect(parser.HasSpotalisAnnotations(obj)).To(BeTrue())
		})
	})

	Describe("GetAnnotationValue", func() {
		It("should return value and true when annotation exists", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"test-key": "test-value",
				},
			}

			value, exists := parser.GetAnnotationValue(obj, "test-key")
			Expect(exists).To(BeTrue())
			Expect(value).To(Equal("test-value"))
		})

		It("should return empty string and false when annotation doesn't exist", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"other-key": "other-value",
				},
			}

			value, exists := parser.GetAnnotationValue(obj, "missing-key")
			Expect(exists).To(BeFalse())
			Expect(value).To(BeEmpty())
		})

		It("should handle objects with no annotations", func() {
			obj := &MockObject{annotations: nil}

			value, exists := parser.GetAnnotationValue(obj, "any-key")
			Expect(exists).To(BeFalse())
			Expect(value).To(BeEmpty())
		})
	})

	Describe("SetAnnotationValue", func() {
		It("should set annotation on object with existing annotations", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"existing": "value",
				},
			}

			parser.SetAnnotationValue(obj, "new-key", "new-value")

			annotations := obj.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("existing", "value"))
			Expect(annotations).To(HaveKeyWithValue("new-key", "new-value"))
		})

		It("should create annotations map when none exists", func() {
			obj := &MockObject{annotations: nil}

			parser.SetAnnotationValue(obj, "first-key", "first-value")

			annotations := obj.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("first-key", "first-value"))
		})

		It("should overwrite existing annotation", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"key": "old-value",
				},
			}

			parser.SetAnnotationValue(obj, "key", "new-value")

			annotations := obj.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("key", "new-value"))
		})
	})

	Describe("RemoveAnnotation", func() {
		It("should remove existing annotation", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"keep":   "this",
					"remove": "this",
				},
			}

			parser.RemoveAnnotation(obj, "remove")

			annotations := obj.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("keep", "this"))
			Expect(annotations).ToNot(HaveKey("remove"))
		})

		It("should handle removal of non-existent annotation", func() {
			obj := &MockObject{
				annotations: map[string]string{
					"existing": "value",
				},
			}

			parser.RemoveAnnotation(obj, "non-existent")

			annotations := obj.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("existing", "value"))
		})

		It("should handle objects with no annotations", func() {
			obj := &MockObject{annotations: nil}

			Expect(func() {
				parser.RemoveAnnotation(obj, "any-key")
			}).ToNot(Panic())
		})
	})

	Describe("ValidateAnnotations", func() {
		Context("with valid annotations", func() {
			It("should return no errors for valid spot percentage", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "75",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(BeEmpty())
			})

			It("should return no errors for valid min-on-demand", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/min-on-demand": "3",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(BeEmpty())
			})

			It("should return no errors for boundary values", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "0",
						"spotalis.io/min-on-demand":   "0",
						"spotalis.io/max-replicas":    "100",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(BeEmpty())
			})

			It("should return no errors for 100% spot", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "100",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(BeEmpty())
			})
		})

		Context("with invalid annotations", func() {
			It("should return error for invalid spot percentage", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "150",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(1))
				Expect(errors[0].Error()).To(ContainSubstring("invalid spotalis.io/spot-percentage"))
				Expect(errors[0].Error()).To(ContainSubstring("must be between 0 and 100"))
			})

			It("should return error for negative spot percentage", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "-10",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(1))
				Expect(errors[0].Error()).To(ContainSubstring("invalid spotalis.io/spot-percentage"))
			})

			It("should return error for non-numeric spot percentage", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "not-a-number",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(1))
				Expect(errors[0].Error()).To(ContainSubstring("invalid spotalis.io/spot-percentage"))
				Expect(errors[0].Error()).To(ContainSubstring("must be an integer"))
			})

			It("should return error for negative min-on-demand", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/min-on-demand": "-5",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(1))
				Expect(errors[0].Error()).To(ContainSubstring("invalid spotalis.io/min-on-demand"))
				Expect(errors[0].Error()).To(ContainSubstring("must be non-negative"))
			})

			It("should return error for non-numeric min-on-demand", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/min-on-demand": "abc",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(1))
				Expect(errors[0].Error()).To(ContainSubstring("invalid spotalis.io/min-on-demand"))
			})

			It("should return error for invalid max-replicas", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/max-replicas": "-1",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(1))
				Expect(errors[0].Error()).To(ContainSubstring("invalid spotalis.io/max-replicas"))
			})

			It("should return multiple errors for multiple invalid annotations", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"spotalis.io/spot-percentage": "150",
						"spotalis.io/min-on-demand":   "-1",
						"spotalis.io/max-replicas":    "not-a-number",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(HaveLen(3))

				errorMessages := make([]string, len(errors))
				for i, err := range errors {
					errorMessages[i] = err.Error()
				}

				Expect(errorMessages).To(ContainElement(ContainSubstring("invalid spotalis.io/spot-percentage")))
				Expect(errorMessages).To(ContainElement(ContainSubstring("invalid spotalis.io/min-on-demand")))
				Expect(errorMessages).To(ContainElement(ContainSubstring("invalid spotalis.io/max-replicas")))
			})
		})

		Context("with no annotations", func() {
			It("should return no errors", func() {
				obj := &MockObject{annotations: nil}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(BeEmpty())
			})
		})

		Context("with non-spotalis annotations", func() {
			It("should return no errors", func() {
				obj := &MockObject{
					annotations: map[string]string{
						"app":     "myapp",
						"version": "1.0",
					},
				}

				errors := parser.ValidateAnnotations(obj)
				Expect(errors).To(BeEmpty())
			})
		})
	})
})

// MockObject implements metav1.Object for testing
type MockObject struct {
	annotations map[string]string
}

func (m *MockObject) GetAnnotations() map[string]string {
	return m.annotations
}

func (m *MockObject) SetAnnotations(annotations map[string]string) {
	m.annotations = annotations
}

func (m *MockObject) GetName() string                                            { return "test-object" }
func (m *MockObject) SetName(name string)                                        {}
func (m *MockObject) GetGenerateName() string                                    { return "" }
func (m *MockObject) SetGenerateName(name string)                                {}
func (m *MockObject) GetNamespace() string                                       { return "default" }
func (m *MockObject) SetNamespace(namespace string)                              {}
func (m *MockObject) GetSelfLink() string                                        { return "" }
func (m *MockObject) SetSelfLink(selfLink string)                                {}
func (m *MockObject) GetUID() types.UID                                          { return types.UID("test-uid") }
func (m *MockObject) SetUID(uid types.UID)                                       {}
func (m *MockObject) GetResourceVersion() string                                 { return "1" }
func (m *MockObject) SetResourceVersion(version string)                          {}
func (m *MockObject) GetGeneration() int64                                       { return 1 }
func (m *MockObject) SetGeneration(generation int64)                             {}
func (m *MockObject) GetCreationTimestamp() metav1.Time                          { return metav1.Time{} }
func (m *MockObject) SetCreationTimestamp(timestamp metav1.Time)                 {}
func (m *MockObject) GetDeletionTimestamp() *metav1.Time                         { return nil }
func (m *MockObject) SetDeletionTimestamp(timestamp *metav1.Time)                {}
func (m *MockObject) GetDeletionGracePeriodSeconds() *int64                      { return nil }
func (m *MockObject) SetDeletionGracePeriodSeconds(seconds *int64)               {}
func (m *MockObject) GetLabels() map[string]string                               { return nil }
func (m *MockObject) SetLabels(labels map[string]string)                         {}
func (m *MockObject) GetOwnerReferences() []metav1.OwnerReference                { return nil }
func (m *MockObject) SetOwnerReferences(references []metav1.OwnerReference)      {}
func (m *MockObject) GetFinalizers() []string                                    { return nil }
func (m *MockObject) SetFinalizers(finalizers []string)                          {}
func (m *MockObject) GetManagedFields() []metav1.ManagedFieldsEntry              { return nil }
func (m *MockObject) SetManagedFields(managedFields []metav1.ManagedFieldsEntry) {}
