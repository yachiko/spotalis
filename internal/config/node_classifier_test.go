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

package config

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yachiko/spotalis/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("NodeClassifierService", func() {
	var (
		ctx            context.Context
		fakeClient     client.Client
		scheme         *runtime.Scheme
		nodeClassifier *NodeClassifierService
		config         *NodeClassifierConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()

		config = &NodeClassifierConfig{
			SpotLabels: map[string]string{
				"karpenter.sh/capacity-type":     "spot",
				"eks.amazonaws.com/capacityType": "SPOT",
			},
			OnDemandLabels: map[string]string{
				"karpenter.sh/capacity-type":     "on-demand",
				"eks.amazonaws.com/capacityType": "ON_DEMAND",
			},
			CacheRefreshInterval: 5 * time.Minute,
			CloudProvider:        "aws",
		}

		nodeClassifier = NewNodeClassifierService(fakeClient, config)
	})

	Describe("NewNodeClassifierService", func() {
		Context("when creating with valid config", func() {
			It("should create a service with the provided config", func() {
				service := NewNodeClassifierService(fakeClient, config)
				Expect(service).NotTo(BeNil())
				Expect(service.client).To(Equal(fakeClient))
				Expect(service.config).To(Equal(config))
				Expect(service.cache).NotTo(BeNil())
			})
		})

		Context("when creating with nil config", func() {
			It("should use default configuration", func() {
				service := NewNodeClassifierService(fakeClient, nil)
				Expect(service).NotTo(BeNil())
				Expect(service.config).NotTo(BeNil())

				// Should have default spot labels for multiple cloud providers
				Expect(service.config.SpotLabels).To(HaveKey("karpenter.sh/capacity-type"))
				Expect(service.config.SpotLabels).To(HaveKey("cloud.google.com/gke-preemptible"))
				Expect(service.config.SpotLabels).To(HaveKey("kubernetes.azure.com/scalesetpriority"))
			})
		})
	})

	Describe("getDefaultNodeClassifierConfig", func() {
		It("should return configuration for all major cloud providers", func() {
			defaultConfig := getDefaultNodeClassifierConfig()
			Expect(defaultConfig).NotTo(BeNil())

			// Test AWS labels
			Expect(defaultConfig.SpotLabels).To(HaveKeyWithValue("karpenter.sh/capacity-type", "spot"))
			Expect(defaultConfig.SpotLabels).To(HaveKeyWithValue("eks.amazonaws.com/capacityType", "SPOT"))
			Expect(defaultConfig.OnDemandLabels).To(HaveKeyWithValue("karpenter.sh/capacity-type", "on-demand"))
			Expect(defaultConfig.OnDemandLabels).To(HaveKeyWithValue("eks.amazonaws.com/capacityType", "ON_DEMAND"))

			// Test GCP labels
			Expect(defaultConfig.SpotLabels).To(HaveKeyWithValue("cloud.google.com/gke-preemptible", "true"))

			// Test Azure labels
			Expect(defaultConfig.SpotLabels).To(HaveKeyWithValue("kubernetes.azure.com/scalesetpriority", "spot"))
			Expect(defaultConfig.OnDemandLabels).To(HaveKeyWithValue("kubernetes.azure.com/scalesetpriority", "regular"))

			// Test defaults
			Expect(defaultConfig.CacheRefreshInterval).To(Equal(5 * time.Minute))
			Expect(defaultConfig.CloudProvider).To(Equal("auto"))
		})
	})

	Describe("ClassifyNode", func() {
		Context("when node has spot labels", func() {
			It("should classify as spot for Karpenter labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "spot-node-1",
						Labels: map[string]string{
							"karpenter.sh/capacity-type": "spot",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should classify as spot for EKS labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "spot-node-2",
						Labels: map[string]string{
							"eks.amazonaws.com/capacityType": "SPOT",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should classify as spot for GKE preemptible labels", func() {
				// Update config to include GCP labels
				nodeClassifier.config.SpotLabels["cloud.google.com/gke-preemptible"] = "true"

				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "spot-node-3",
						Labels: map[string]string{
							"cloud.google.com/gke-preemptible": "true",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should classify as spot for Azure priority labels", func() {
				// Update config to include Azure labels
				nodeClassifier.config.SpotLabels["kubernetes.azure.com/scalesetpriority"] = "spot"

				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "spot-node-4",
						Labels: map[string]string{
							"kubernetes.azure.com/scalesetpriority": "spot",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})
		})

		Context("when node has on-demand labels", func() {
			It("should classify as on-demand for Karpenter labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ondemand-node-1",
						Labels: map[string]string{
							"karpenter.sh/capacity-type": "on-demand",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeOnDemand))
			})

			It("should classify as on-demand for EKS labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ondemand-node-2",
						Labels: map[string]string{
							"eks.amazonaws.com/capacityType": "ON_DEMAND",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeOnDemand))
			})
		})

		Context("when node has wildcard matching", func() {
			It("should match wildcard values", func() {
				// Add wildcard config
				nodeClassifier.config.SpotLabels["node.kubernetes.io/instance-type"] = "*"

				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "any-node",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.large",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})
		})

		Context("when node has no matching labels", func() {
			It("should use cloud provider-specific classification", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unknown-node",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.large",
						},
					},
				}

				nodeType := nodeClassifier.ClassifyNode(node)
				Expect(nodeType).To(Equal(apis.NodeTypeUnknown))
			})
		})
	})

	Describe("ClassifyNodesByName", func() {
		It("should return classifications for known nodes", func() {
			spotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "batch-spot-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "spot",
					},
				},
			}
			onDemandNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "batch-ondemand-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "on-demand",
					},
				},
			}

			Expect(fakeClient.Create(ctx, spotNode.DeepCopy())).To(Succeed())
			Expect(fakeClient.Create(ctx, onDemandNode.DeepCopy())).To(Succeed())

			results, err := nodeClassifier.ClassifyNodesByName(ctx, []string{spotNode.Name, onDemandNode.Name, spotNode.Name})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveKeyWithValue(spotNode.Name, apis.NodeTypeSpot))
			Expect(results).To(HaveKeyWithValue(onDemandNode.Name, apis.NodeTypeOnDemand))
		})

		It("should return unknown for missing nodes", func() {
			results, err := nodeClassifier.ClassifyNodesByName(ctx, []string{"missing-node"})
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveKeyWithValue("missing-node", apis.NodeTypeUnknown))
		})
	})

	Describe("classifyByCloudProvider", func() {
		Context("when classifying AWS nodes", func() {
			It("should detect spot instance from EKS capacity type", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.large",
							"eks.amazonaws.com/capacityType":   "SPOT",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should detect on-demand instance from EKS capacity type", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.large",
							"eks.amazonaws.com/capacityType":   "ON_DEMAND",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeOnDemand))
			})

			It("should detect spot instance from Karpenter labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.large",
							"karpenter.sh/capacity-type":       "spot",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should detect spot from instance type name", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "m5.spot.large",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})
		})

		Context("when classifying GCP nodes", func() {
			It("should detect preemptible instances", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"cloud.google.com/gke-preemptible": "true",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})
		})

		Context("when classifying Azure nodes", func() {
			It("should detect spot instances", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"kubernetes.azure.com/scalesetpriority": "spot",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeSpot))
			})

			It("should detect regular instances", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"kubernetes.azure.com/scalesetpriority": "regular",
						},
					},
				}

				nodeType := nodeClassifier.classifyByCloudProvider(node)
				Expect(nodeType).To(Equal(apis.NodeTypeOnDemand))
			})
		})
	})

	Describe("GetNodesOfType", func() {
		BeforeEach(func() {
			// Create test nodes
			spotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spot-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "spot",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			onDemandNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ondemand-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "on-demand",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			Expect(fakeClient.Create(ctx, spotNode)).To(Succeed())
			Expect(fakeClient.Create(ctx, onDemandNode)).To(Succeed())
		})

		It("should return spot nodes when requested", func() {
			nodes, err := nodeClassifier.GetNodesOfType(ctx, apis.NodeTypeSpot)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].Name).To(Equal("spot-node"))
		})

		It("should return on-demand nodes when requested", func() {
			nodes, err := nodeClassifier.GetNodesOfType(ctx, apis.NodeTypeOnDemand)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].Name).To(Equal("ondemand-node"))
		})
	})

	Describe("RefreshCache", func() {
		It("should refresh cache and classify nodes", func() {
			// Create test nodes
			spotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "spot-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "spot",
					},
				},
			}

			Expect(fakeClient.Create(ctx, spotNode)).To(Succeed())

			err := nodeClassifier.RefreshCache(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Cache should contain the spot node
			Expect(nodeClassifier.cache.SpotNodes).To(HaveKey("spot-node"))
			Expect(nodeClassifier.cache.LastRefresh).To(BeTemporally("~", time.Now(), time.Second))
		})

		It("should skip refresh if cache is still fresh", func() {
			// Set cache as recently refreshed
			nodeClassifier.cache.LastRefresh = time.Now()

			err := nodeClassifier.RefreshCache(ctx)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("IsNodeReady", func() {
		It("should return true for ready nodes", func() {
			node := &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			ready := nodeClassifier.IsNodeReady(node)
			Expect(ready).To(BeTrue())
		})

		It("should return false for not ready nodes", func() {
			node := &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			ready := nodeClassifier.IsNodeReady(node)
			Expect(ready).To(BeFalse())
		})

		It("should return false for nodes without ready condition", func() {
			node := &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			}

			ready := nodeClassifier.IsNodeReady(node)
			Expect(ready).To(BeFalse())
		})
	})

	Describe("GetNodeClassificationSummary", func() {
		BeforeEach(func() {
			// Create test nodes with various states
			readySpotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ready-spot-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "spot",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			}

			notReadySpotNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "notready-spot-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "spot",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
					},
				},
			}

			readyOnDemandNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ready-ondemand-node",
					Labels: map[string]string{
						"karpenter.sh/capacity-type": "on-demand",
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			}

			Expect(fakeClient.Create(ctx, readySpotNode)).To(Succeed())
			Expect(fakeClient.Create(ctx, notReadySpotNode)).To(Succeed())
			Expect(fakeClient.Create(ctx, readyOnDemandNode)).To(Succeed())
		})

		It("should return accurate summary", func() {
			summary, err := nodeClassifier.GetNodeClassificationSummary(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(summary).NotTo(BeNil())

			Expect(summary.TotalSpotNodes).To(Equal(2))
			Expect(summary.TotalOnDemandNodes).To(Equal(1))
			Expect(summary.ReadySpotNodes).To(Equal(1))
			Expect(summary.ReadyOnDemandNodes).To(Equal(1))
			Expect(summary.CloudProvider).To(Equal("aws"))
			Expect(summary.CacheRefreshInterval).To(Equal(5 * time.Minute))
		})
	})

	Describe("UpdateConfig", func() {
		It("should update configuration and reset cache", func() {
			newConfig := &NodeClassifierConfig{
				SpotLabels: map[string]string{
					"custom-label": "spot",
				},
				CacheRefreshInterval: 10 * time.Minute,
				CloudProvider:        "gcp",
			}

			nodeClassifier.UpdateConfig(newConfig)

			currentConfig := nodeClassifier.GetConfig()
			Expect(currentConfig.SpotLabels).To(HaveKeyWithValue("custom-label", "spot"))
			Expect(currentConfig.CacheRefreshInterval).To(Equal(10 * time.Minute))
			Expect(currentConfig.CloudProvider).To(Equal("gcp"))

			// Cache should be reset
			Expect(nodeClassifier.cache.LastRefresh).To(BeZero())
		})
	})

	Describe("DetectCloudProvider", func() {
		Context("when detecting AWS", func() {
			It("should detect from EKS labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "aws-eks-node",
						Labels: map[string]string{
							"eks.amazonaws.com/capacityType": "ON_DEMAND",
						},
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("aws"))
			})

			It("should detect from Karpenter labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "aws-karpenter-node",
						Labels: map[string]string{
							"karpenter.sh/capacity-type": "spot",
						},
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("aws"))
			})

			It("should detect from provider ID", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "aws-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "aws:///us-west-2a/i-1234567890abcdef0",
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("aws"))
			})
		})

		Context("when detecting GCP", func() {
			It("should detect from GKE labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gcp-gke-node",
						Labels: map[string]string{
							"cloud.google.com/gke-preemptible": "true",
						},
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("gcp"))
			})

			It("should detect from provider ID", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gcp-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://my-project/us-central1-a/gke-cluster-node-pool-12345678",
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("gcp"))
			})
		})

		Context("when detecting Azure", func() {
			It("should detect from AKS labels", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "azure-aks-node",
						Labels: map[string]string{
							"kubernetes.azure.com/scalesetpriority": "spot",
						},
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("azure"))
			})

			It("should detect from provider ID", func() {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "azure-node",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "azure:///subscriptions/12345/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("azure"))
			})
		})

		Context("when no nodes exist", func() {
			It("should return unknown", func() {
				provider, err := nodeClassifier.DetectCloudProvider(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(provider).To(Equal("unknown"))
			})
		})
	})
})
