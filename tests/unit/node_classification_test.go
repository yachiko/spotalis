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

package unit

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("NodeClassification", func() {

	Describe("NodeType classification", func() {
		It("should identify spot nodes correctly", func() {
			classification := &apis.NodeClassification{
				NodeType: apis.NodeTypeSpot,
			}

			Expect(classification.IsSpot()).To(BeTrue())
			Expect(classification.IsOnDemand()).To(BeFalse())
			Expect(classification.IsUnknown()).To(BeFalse())
		})

		It("should identify on-demand nodes correctly", func() {
			classification := &apis.NodeClassification{
				NodeType: apis.NodeTypeOnDemand,
			}

			Expect(classification.IsSpot()).To(BeFalse())
			Expect(classification.IsOnDemand()).To(BeTrue())
			Expect(classification.IsUnknown()).To(BeFalse())
		})

		It("should identify unknown nodes correctly", func() {
			classification := &apis.NodeClassification{
				NodeType: apis.NodeTypeUnknown,
			}

			Expect(classification.IsSpot()).To(BeFalse())
			Expect(classification.IsOnDemand()).To(BeFalse())
			Expect(classification.IsUnknown()).To(BeTrue())
		})
	})

	Describe("IsReady", func() {
		It("should return true for schedulable nodes", func() {
			classification := &apis.NodeClassification{
				Schedulable: true,
			}

			Expect(classification.IsReady()).To(BeTrue())
		})

		It("should return false for unschedulable nodes", func() {
			classification := &apis.NodeClassification{
				Schedulable: false,
			}

			Expect(classification.IsReady()).To(BeFalse())
		})
	})

	Describe("Age and staleness", func() {
		It("should calculate age correctly", func() {
			now := time.Now()
			classification := &apis.NodeClassification{
				LastUpdated: now.Add(-5 * time.Minute),
			}

			age := classification.Age()
			Expect(age).To(BeNumerically(">=", 5*time.Minute))
			Expect(age).To(BeNumerically("<", 6*time.Minute))
		})

		It("should detect stale classifications", func() {
			classification := &apis.NodeClassification{
				LastUpdated: time.Now().Add(-10 * time.Minute),
			}

			Expect(classification.IsStale(5 * time.Minute)).To(BeTrue())
			Expect(classification.IsStale(15 * time.Minute)).To(BeFalse())
		})
	})

	Describe("Update", func() {
		var (
			node       *corev1.Node
			classifier *MockNodeClassifier
		)

		BeforeEach(func() {
			classifier = &MockNodeClassifier{}
			node = &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"kubernetes.io/os":                 "linux",
						"node.kubernetes.io/lifecycle":     "spot",
						"topology.kubernetes.io/region":    "us-west-2",
						"topology.kubernetes.io/zone":      "us-west-2a",
						"node.kubernetes.io/instance-type": "m5.large",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			}
		})

		It("should update all classification fields", func() {
			classification := &apis.NodeClassification{
				NodeName: "test-node",
			}
			classifier.SetNodeType(apis.NodeTypeSpot)

			before := time.Now()
			classification.Update(node, classifier)

			Expect(classification.Labels).To(Equal(node.Labels))
			Expect(classification.Schedulable).To(BeTrue())
			Expect(classification.LastUpdated).To(BeTemporally(">=", before))
			Expect(classification.NodeType).To(Equal(apis.NodeTypeSpot))
			Expect(classification.Region).To(Equal("us-west-2"))
			Expect(classification.Zone).To(Equal("us-west-2a"))
			Expect(classification.InstanceType).To(Equal("m5.large"))
			Expect(classification.Capacity).To(Equal(node.Status.Capacity))
		})

		It("should handle unschedulable nodes", func() {
			node.Spec.Unschedulable = true
			classification := &apis.NodeClassification{
				NodeName: "test-node",
			}

			classification.Update(node, classifier)

			Expect(classification.Schedulable).To(BeFalse())
		})

		It("should handle missing topology labels", func() {
			node.Labels = map[string]string{
				"kubernetes.io/os": "linux",
			}
			classification := &apis.NodeClassification{
				NodeName: "test-node",
			}

			classification.Update(node, classifier)

			Expect(classification.Region).To(BeEmpty())
			Expect(classification.Zone).To(BeEmpty())
			Expect(classification.InstanceType).To(BeEmpty())
		})
	})
})

var _ = Describe("DefaultNodeClassifier", func() {

	var classifier *apis.DefaultNodeClassifier

	BeforeEach(func() {
		classifier = apis.NewDefaultNodeClassifier()
	})

	Describe("ClassifyNode", func() {
		Context("with Karpenter labels", func() {
			It("should classify spot nodes", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"karpenter.sh/capacity-type": "spot",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should classify on-demand nodes", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"karpenter.sh/capacity-type": "on-demand",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeOnDemand))
			})
		})

		Context("with AWS labels", func() {
			It("should classify spot nodes", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"node.kubernetes.io/lifecycle": "spot",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should classify on-demand nodes", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"node.kubernetes.io/lifecycle": "normal",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeOnDemand))
			})
		})

		Context("with GCP labels", func() {
			It("should classify preemptible nodes as spot", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"cloud.google.com/gke-preemptible": "true",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})
		})

		Context("with Azure labels", func() {
			It("should classify spot nodes", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"kubernetes.azure.com/scalesetpriority": "spot",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})
		})

		Context("with unknown labels", func() {
			It("should classify as unknown", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"kubernetes.io/os": "linux",
						},
					},
				}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeUnknown))
			})
		})

		Context("with no labels", func() {
			It("should classify as unknown", func() {
				node := &corev1.Node{}

				nodeType := classifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeUnknown))
			})
		})
	})

	Describe("Label selectors", func() {
		It("should return valid spot selector", func() {
			selector := classifier.GetSpotSelector()
			Expect(selector).ToNot(BeNil())

			// Test with a spot node
			nodeLabels := labels.Set(map[string]string{
				"karpenter.sh/capacity-type": "spot",
			})
			Expect(selector.Matches(nodeLabels)).To(BeTrue())

			// Test with a non-spot node
			nodeLabels = labels.Set(map[string]string{
				"karpenter.sh/capacity-type": "on-demand",
			})
			Expect(selector.Matches(nodeLabels)).To(BeFalse())
		})

		It("should return valid on-demand selector", func() {
			selector := classifier.GetOnDemandSelector()
			Expect(selector).ToNot(BeNil())

			// Test with an on-demand node
			nodeLabels := labels.Set(map[string]string{
				"karpenter.sh/capacity-type": "on-demand",
			})
			Expect(selector.Matches(nodeLabels)).To(BeTrue())

			// Test with a spot node
			nodeLabels = labels.Set(map[string]string{
				"karpenter.sh/capacity-type": "spot",
			})
			Expect(selector.Matches(nodeLabels)).To(BeFalse())
		})
	})
})

var _ = Describe("NodeCache", func() {

	var (
		cache      *apis.NodeCache
		classifier *MockNodeClassifier
	)

	BeforeEach(func() {
		classifier = &MockNodeClassifier{}
		cache = apis.NewNodeCache(classifier)
	})

	Describe("GetNodeType", func() {
		It("should return unknown for non-existent nodes", func() {
			nodeType := cache.GetNodeType("non-existent")
			Expect(nodeType).To(Equal(apis.NodeTypeUnknown))
		})

		It("should return correct type for cached nodes", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			}
			classifier.SetNodeType(apis.NodeTypeSpot)

			cache.UpdateNode(node)

			nodeType := cache.GetNodeType("test-node")
			Expect(nodeType).To(Equal(apis.NodeTypeSpot))
		})
	})

	Describe("UpdateNode", func() {
		It("should create new classification for unknown nodes", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "new-node",
				},
			}
			classifier.SetNodeType(apis.NodeTypeOnDemand)

			cache.UpdateNode(node)

			Expect(cache.Classifications).To(HaveKey("new-node"))
			classification := cache.Classifications["new-node"]
			Expect(classification.NodeName).To(Equal("new-node"))
			Expect(classification.NodeType).To(Equal(apis.NodeTypeOnDemand))
		})

		It("should update existing classification", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-node",
					Labels: map[string]string{
						"initial": "label",
					},
				},
			}
			classifier.SetNodeType(apis.NodeTypeSpot)

			// First update
			cache.UpdateNode(node)
			initialTime := cache.Classifications["existing-node"].LastUpdated

			// Update with new labels
			time.Sleep(time.Millisecond) // Ensure different timestamp
			node.Labels["updated"] = "label"
			classifier.SetNodeType(apis.NodeTypeOnDemand)
			cache.UpdateNode(node)

			classification := cache.Classifications["existing-node"]
			Expect(classification.Labels).To(HaveKeyWithValue("updated", "label"))
			Expect(classification.NodeType).To(Equal(apis.NodeTypeOnDemand))
			Expect(classification.LastUpdated).To(BeTemporally(">", initialTime))
		})
	})

	Describe("RemoveNode", func() {
		It("should remove node from cache", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "to-remove",
				},
			}

			cache.UpdateNode(node)
			Expect(cache.Classifications).To(HaveKey("to-remove"))

			cache.RemoveNode("to-remove")
			Expect(cache.Classifications).ToNot(HaveKey("to-remove"))
		})

		It("should handle removal of non-existent nodes", func() {
			Expect(func() {
				cache.RemoveNode("non-existent")
			}).ToNot(Panic())
		})
	})

	Describe("GetSpotNodes", func() {
		BeforeEach(func() {
			// Add spot nodes
			spotNode1 := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "spot-1"},
				Spec:       corev1.NodeSpec{Unschedulable: false},
			}
			spotNode2 := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "spot-2"},
				Spec:       corev1.NodeSpec{Unschedulable: false},
			}
			unschedulableSpot := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "spot-unschedulable"},
				Spec:       corev1.NodeSpec{Unschedulable: true},
			}

			classifier.SetNodeType(apis.NodeTypeSpot)
			cache.UpdateNode(spotNode1)
			cache.UpdateNode(spotNode2)
			cache.UpdateNode(unschedulableSpot)

			// Add on-demand node
			onDemandNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "on-demand-1"},
				Spec:       corev1.NodeSpec{Unschedulable: false},
			}
			classifier.SetNodeType(apis.NodeTypeOnDemand)
			cache.UpdateNode(onDemandNode)
		})

		It("should return only ready spot nodes", func() {
			spotNodes := cache.GetSpotNodes()
			Expect(spotNodes).To(ConsistOf("spot-1", "spot-2"))
			Expect(spotNodes).ToNot(ContainElement("spot-unschedulable"))
			Expect(spotNodes).ToNot(ContainElement("on-demand-1"))
		})
	})

	Describe("GetOnDemandNodes", func() {
		BeforeEach(func() {
			// Add on-demand nodes
			onDemandNode1 := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "on-demand-1"},
				Spec:       corev1.NodeSpec{Unschedulable: false},
			}
			onDemandNode2 := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "on-demand-2"},
				Spec:       corev1.NodeSpec{Unschedulable: false},
			}
			unschedulableOnDemand := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "on-demand-unschedulable"},
				Spec:       corev1.NodeSpec{Unschedulable: true},
			}

			classifier.SetNodeType(apis.NodeTypeOnDemand)
			cache.UpdateNode(onDemandNode1)
			cache.UpdateNode(onDemandNode2)
			cache.UpdateNode(unschedulableOnDemand)

			// Add spot node
			spotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "spot-1"},
				Spec:       corev1.NodeSpec{Unschedulable: false},
			}
			classifier.SetNodeType(apis.NodeTypeSpot)
			cache.UpdateNode(spotNode)
		})

		It("should return only ready on-demand nodes", func() {
			onDemandNodes := cache.GetOnDemandNodes()
			Expect(onDemandNodes).To(ConsistOf("on-demand-1", "on-demand-2"))
			Expect(onDemandNodes).ToNot(ContainElement("on-demand-unschedulable"))
			Expect(onDemandNodes).ToNot(ContainElement("spot-1"))
		})
	})

	Describe("Refresh tracking", func() {
		It("should track refresh necessity", func() {
			Expect(cache.NeedsRefresh(1 * time.Minute)).To(BeTrue())

			cache.MarkRefreshed()
			Expect(cache.NeedsRefresh(1 * time.Minute)).To(BeFalse())

			// Simulate time passing
			cache.LastFullRefresh = time.Now().Add(-2 * time.Minute)
			Expect(cache.NeedsRefresh(1 * time.Minute)).To(BeTrue())
		})
	})
})

// MockNodeClassifier is a test helper for NodeClassifier interface
type MockNodeClassifier struct {
	nodeType apis.NodeType
}

func (m *MockNodeClassifier) SetNodeType(nodeType apis.NodeType) {
	m.nodeType = nodeType
}

func (m *MockNodeClassifier) ClassifyNode(_ *corev1.Node) apis.NodeType {
	return m.nodeType
}

func (m *MockNodeClassifier) GetSpotSelector() labels.Selector {
	return labels.Nothing()
}

func (m *MockNodeClassifier) GetOnDemandSelector() labels.Selector {
	return labels.Nothing()
}
