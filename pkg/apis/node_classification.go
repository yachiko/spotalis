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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// NodeType represents the classification of a Kubernetes node
type NodeType string

const (
	// NodeTypeSpot indicates a spot/preemptible node
	NodeTypeSpot NodeType = "spot"

	// NodeTypeOnDemand indicates an on-demand/regular node
	NodeTypeOnDemand NodeType = "on-demand"

	// NodeTypeUnknown indicates the node type could not be determined
	NodeTypeUnknown NodeType = "unknown"
)

// NodeClassification represents the categorization of cluster nodes based on their type
type NodeClassification struct {
	// NodeName is the Kubernetes node name
	NodeName string `json:"nodeName"`

	// NodeType is the spot or on-demand classification
	NodeType NodeType `json:"nodeType"`

	// Labels are the node labels used for classification
	Labels map[string]string `json:"labels"`

	// Schedulable indicates whether the node accepts new pods
	Schedulable bool `json:"schedulable"`

	// LastUpdated is when classification was last verified
	LastUpdated time.Time `json:"lastUpdated"`

	// Region is the cloud region of the node
	Region string `json:"region,omitempty"`

	// Zone is the availability zone of the node
	Zone string `json:"zone,omitempty"`

	// InstanceType is the cloud instance type
	InstanceType string `json:"instanceType,omitempty"`

	// Capacity is the node's resource capacity
	Capacity corev1.ResourceList `json:"capacity,omitempty"`
}

// IsSpot returns true if this node is classified as spot
func (n *NodeClassification) IsSpot() bool {
	return n.NodeType == NodeTypeSpot
}

// IsOnDemand returns true if this node is classified as on-demand
func (n *NodeClassification) IsOnDemand() bool {
	return n.NodeType == NodeTypeOnDemand
}

// IsUnknown returns true if this node type is unknown
func (n *NodeClassification) IsUnknown() bool {
	return n.NodeType == NodeTypeUnknown
}

// IsReady returns true if the node is schedulable and not cordoned
func (n *NodeClassification) IsReady() bool {
	return n.Schedulable
}

// Age returns how long since the classification was last updated
func (n *NodeClassification) Age() time.Duration {
	return time.Since(n.LastUpdated)
}

// IsStale returns true if the classification needs to be refreshed
func (n *NodeClassification) IsStale(maxAge time.Duration) bool {
	return n.Age() > maxAge
}

// Update refreshes the classification with new node information
func (n *NodeClassification) Update(node *corev1.Node, classifier NodeClassifier) {
	n.Labels = node.Labels
	n.Schedulable = !node.Spec.Unschedulable
	n.LastUpdated = time.Now()
	n.NodeType = classifier.ClassifyNode(node)

	// Extract standard topology labels
	if region, exists := node.Labels[corev1.LabelTopologyRegion]; exists {
		n.Region = region
	}
	if zone, exists := node.Labels[corev1.LabelTopologyZone]; exists {
		n.Zone = zone
	}
	if instanceType, exists := node.Labels[corev1.LabelInstanceTypeStable]; exists {
		n.InstanceType = instanceType
	}

	n.Capacity = node.Status.Capacity
}

// NodeClassifier provides methods for classifying nodes by type
type NodeClassifier interface {
	// ClassifyNode determines the node type based on labels
	ClassifyNode(node *corev1.Node) NodeType

	// GetSpotSelector returns a label selector for spot nodes
	GetSpotSelector() labels.Selector

	// GetOnDemandSelector returns a label selector for on-demand nodes
	GetOnDemandSelector() labels.Selector
}

// DefaultNodeClassifier implements node classification using common cloud provider labels
type DefaultNodeClassifier struct {
	// SpotLabels are label selectors that identify spot nodes
	SpotLabels []metav1.LabelSelector `json:"spotLabels"`

	// OnDemandLabels are label selectors that identify on-demand nodes
	OnDemandLabels []metav1.LabelSelector `json:"onDemandLabels"`
}

// NewDefaultNodeClassifier creates a classifier with Karpenter labels as default
func NewDefaultNodeClassifier() *DefaultNodeClassifier {
	return &DefaultNodeClassifier{
		SpotLabels: []metav1.LabelSelector{
			{
				MatchLabels: map[string]string{
					"karpenter.sh/capacity-type": "spot",
				},
			},
		},
		OnDemandLabels: []metav1.LabelSelector{
			{
				MatchLabels: map[string]string{
					"karpenter.sh/capacity-type": "on-demand",
				},
			},
		},
	}
}

// ClassifyNode determines the node type based on configured label selectors
func (d *DefaultNodeClassifier) ClassifyNode(node *corev1.Node) NodeType {
	nodeLabels := labels.Set(node.Labels)

	// Check if node matches spot patterns
	for _, labelSelector := range d.SpotLabels {
		if selector, err := metav1.LabelSelectorAsSelector(&labelSelector); err == nil {
			if selector.Matches(nodeLabels) {
				return NodeTypeSpot
			}
		}
	}

	// Check if node matches on-demand patterns
	for _, labelSelector := range d.OnDemandLabels {
		if selector, err := metav1.LabelSelectorAsSelector(&labelSelector); err == nil {
			if selector.Matches(nodeLabels) {
				return NodeTypeOnDemand
			}
		}
	}

	// Default to unknown if no patterns match
	return NodeTypeUnknown
}

// GetSpotSelector returns a selector that matches spot nodes
func (d *DefaultNodeClassifier) GetSpotSelector() labels.Selector {
	// Use the first spot label selector as the primary one
	if len(d.SpotLabels) > 0 {
		if selector, err := metav1.LabelSelectorAsSelector(&d.SpotLabels[0]); err == nil {
			return selector
		}
	}
	return labels.Nothing()
}

// GetOnDemandSelector returns a selector that matches on-demand nodes
func (d *DefaultNodeClassifier) GetOnDemandSelector() labels.Selector {
	// Use the first on-demand label selector as the primary one
	if len(d.OnDemandLabels) > 0 {
		if selector, err := metav1.LabelSelectorAsSelector(&d.OnDemandLabels[0]); err == nil {
			return selector
		}
	}
	return labels.Nothing()
}

// NodeCache manages cached node classifications
type NodeCache struct {
	// Classifications maps node names to their classifications
	Classifications map[string]*NodeClassification `json:"classifications"`

	// LastFullRefresh is when all nodes were last re-classified
	LastFullRefresh time.Time `json:"lastFullRefresh"`

	// Classifier is used to classify nodes
	Classifier NodeClassifier `json:"-"`
}

// NewNodeCache creates a new node cache with the given classifier
func NewNodeCache(classifier NodeClassifier) *NodeCache {
	return &NodeCache{
		Classifications: make(map[string]*NodeClassification),
		Classifier:      classifier,
	}
}

// GetNodeType returns the type of the specified node
func (nc *NodeCache) GetNodeType(nodeName string) NodeType {
	if classification, exists := nc.Classifications[nodeName]; exists {
		return classification.NodeType
	}
	return NodeTypeUnknown
}

// UpdateNode updates the classification for a single node
func (nc *NodeCache) UpdateNode(node *corev1.Node) {
	nodeName := node.Name

	classification, exists := nc.Classifications[nodeName]
	if !exists {
		classification = &NodeClassification{
			NodeName: nodeName,
		}
		nc.Classifications[nodeName] = classification
	}

	classification.Update(node, nc.Classifier)
}

// RemoveNode removes a node from the cache
func (nc *NodeCache) RemoveNode(nodeName string) {
	delete(nc.Classifications, nodeName)
}

// GetSpotNodes returns all currently known spot nodes
func (nc *NodeCache) GetSpotNodes() []string {
	var spotNodes []string
	for _, classification := range nc.Classifications {
		if classification.IsSpot() && classification.IsReady() {
			spotNodes = append(spotNodes, classification.NodeName)
		}
	}
	return spotNodes
}

// GetOnDemandNodes returns all currently known on-demand nodes
func (nc *NodeCache) GetOnDemandNodes() []string {
	var onDemandNodes []string
	for _, classification := range nc.Classifications {
		if classification.IsOnDemand() && classification.IsReady() {
			onDemandNodes = append(onDemandNodes, classification.NodeName)
		}
	}
	return onDemandNodes
}

// NeedsRefresh returns true if the cache should be refreshed
func (nc *NodeCache) NeedsRefresh(maxAge time.Duration) bool {
	return time.Since(nc.LastFullRefresh) > maxAge
}

// MarkRefreshed updates the last refresh timestamp
func (nc *NodeCache) MarkRefreshed() {
	nc.LastFullRefresh = time.Now()
}
