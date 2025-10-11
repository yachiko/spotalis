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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ahoma/spotalis/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Capacity types
	capacityTypeSpot = "spot"

	// Cloud providers
	cloudProviderAWS = "aws"
)

// NodeClassifierService provides node classification functionality
type NodeClassifierService struct {
	client client.Client
	cache  *NodeCache
	config *NodeClassifierConfig
	mutex  sync.RWMutex
}

// NodeSummary provides a summary of node classification
type NodeSummary struct {
	TotalSpotNodes       int           `json:"totalSpotNodes"`
	TotalOnDemandNodes   int           `json:"totalOnDemandNodes"`
	ReadySpotNodes       int           `json:"readySpotNodes"`
	ReadyOnDemandNodes   int           `json:"readyOnDemandNodes"`
	LastCacheRefresh     time.Time     `json:"lastCacheRefresh"`
	CacheRefreshInterval time.Duration `json:"cacheRefreshInterval"`
	CloudProvider        string        `json:"cloudProvider"`
}

// NodeClassifierConfig holds configuration for node classification
type NodeClassifierConfig struct {
	// SpotLabels are the labels used to identify spot instances
	SpotLabels map[string]string `json:"spotLabels"`

	// OnDemandLabels are the labels used to identify on-demand instances
	OnDemandLabels map[string]string `json:"onDemandLabels"`

	// CacheRefreshInterval is how often to refresh the node cache
	CacheRefreshInterval time.Duration `json:"cacheRefreshInterval"`

	// CloudProvider is the cloud provider (aws, gcp, azure, etc.)
	CloudProvider string `json:"cloudProvider"`
}

// NodeCache provides cached node classification data
type NodeCache struct {
	SpotNodes     map[string]*corev1.Node  `json:"spotNodes"`
	OnDemandNodes map[string]*corev1.Node  `json:"onDemandNodes"`
	NodeTypes     map[string]apis.NodeType `json:"nodeTypes"`
	LastRefresh   time.Time                `json:"lastRefresh"`
	mutex         sync.RWMutex             `json:"-"`
}

// NewNodeClassifierService creates a new node classifier service
func NewNodeClassifierService(client client.Client, config *NodeClassifierConfig) *NodeClassifierService {
	if config == nil {
		config = getDefaultNodeClassifierConfig()
	}

	return &NodeClassifierService{
		client: client,
		config: config,
		cache: &NodeCache{
			SpotNodes:     make(map[string]*corev1.Node),
			OnDemandNodes: make(map[string]*corev1.Node),
			NodeTypes:     make(map[string]apis.NodeType),
		},
	}
}

// getDefaultNodeClassifierConfig returns default configuration for major cloud providers
func getDefaultNodeClassifierConfig() *NodeClassifierConfig {
	return &NodeClassifierConfig{
		SpotLabels: map[string]string{
			// AWS
			"karpenter.sh/capacity-type":       "spot",
			"eks.amazonaws.com/capacityType":   "SPOT",
			"node.kubernetes.io/instance-type": "*",
			// GCP
			"cloud.google.com/gke-preemptible": "true",
			// Azure
			"kubernetes.azure.com/scalesetpriority": "spot",
		},
		OnDemandLabels: map[string]string{
			// AWS
			"karpenter.sh/capacity-type":     "on-demand",
			"eks.amazonaws.com/capacityType": "ON_DEMAND",
			// GCP (absence of preemptible label indicates on-demand)
			// Azure
			"kubernetes.azure.com/scalesetpriority": "regular",
		},
		CacheRefreshInterval: 5 * time.Minute,
		CloudProvider:        "auto", // Auto-detect
	}
}

// ClassifyNode determines the node type (spot or on-demand)
func (n *NodeClassifierService) ClassifyNode(node *corev1.Node) apis.NodeType {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	// Check for spot instance labels
	for label, expectedValue := range n.config.SpotLabels {
		if nodeValue, exists := node.Labels[label]; exists {
			if expectedValue == "*" || nodeValue == expectedValue {
				return apis.NodeTypeSpot
			}
		}
	}

	// Check for on-demand instance labels
	for label, expectedValue := range n.config.OnDemandLabels {
		if nodeValue, exists := node.Labels[label]; exists {
			if expectedValue == "*" || nodeValue == expectedValue {
				return apis.NodeTypeOnDemand
			}
		}
	}

	// Check provider-specific patterns
	nodeType := n.classifyByCloudProvider(node)
	if nodeType != apis.NodeTypeUnknown {
		return nodeType
	}

	// Default to unknown if we can't determine
	return apis.NodeTypeUnknown
}

// classifyByCloudProvider uses cloud provider-specific logic
func (n *NodeClassifierService) classifyByCloudProvider(node *corev1.Node) apis.NodeType {
	// AWS EKS specific detection
	if instanceType, exists := node.Labels["node.kubernetes.io/instance-type"]; exists {
		// Check EKS managed node groups
		if capacityType, exists := node.Labels["eks.amazonaws.com/capacityType"]; exists {
			if capacityType == "SPOT" {
				return apis.NodeTypeSpot
			}
			if capacityType == "ON_DEMAND" {
				return apis.NodeTypeOnDemand
			}
		}

		// Check Karpenter nodes
		if capacityType, exists := node.Labels["karpenter.sh/capacity-type"]; exists {
			if capacityType == capacityTypeSpot {
				return apis.NodeTypeSpot
			}
			if capacityType == "on-demand" {
				return apis.NodeTypeOnDemand
			}
		}

		// Fallback: parse instance type for spot indicators
		if strings.Contains(strings.ToLower(instanceType), "spot") {
			return apis.NodeTypeSpot
		}
	}

	// GCP GKE specific detection
	if _, exists := node.Labels["cloud.google.com/gke-preemptible"]; exists {
		return apis.NodeTypeSpot
	}

	// Azure AKS specific detection
	if priority, exists := node.Labels["kubernetes.azure.com/scalesetpriority"]; exists {
		if priority == "spot" {
			return apis.NodeTypeSpot
		}
		if priority == "regular" {
			return apis.NodeTypeOnDemand
		}
	}

	return apis.NodeTypeUnknown
}

// GetNodesOfType returns all nodes of the specified type
func (n *NodeClassifierService) GetNodesOfType(ctx context.Context, nodeType apis.NodeType) ([]*corev1.Node, error) {
	if err := n.RefreshCache(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh node cache: %w", err)
	}

	n.cache.mutex.RLock()
	defer n.cache.mutex.RUnlock()

	var result []*corev1.Node

	switch nodeType {
	case apis.NodeTypeSpot:
		for _, node := range n.cache.SpotNodes {
			result = append(result, node)
		}
	case apis.NodeTypeOnDemand:
		for _, node := range n.cache.OnDemandNodes {
			result = append(result, node)
		}
	case apis.NodeTypeUnknown:
		// Return empty slice for unknown node types
		result = []*corev1.Node{}
	default:
		return nil, fmt.Errorf("unexpected node type: %s", nodeType)
	}

	return result, nil
}

// GetNodeCount returns the count of nodes by type
func (n *NodeClassifierService) GetNodeCount(ctx context.Context, nodeType apis.NodeType) (int, error) {
	nodes, err := n.GetNodesOfType(ctx, nodeType)
	if err != nil {
		return 0, err
	}
	return len(nodes), nil
}

// RefreshCache updates the node classification cache
func (n *NodeClassifierService) RefreshCache(ctx context.Context) error {
	n.cache.mutex.Lock()
	defer n.cache.mutex.Unlock()

	// Check if cache is still fresh
	if time.Since(n.cache.LastRefresh) < n.config.CacheRefreshInterval {
		return nil
	}

	// List all nodes
	var nodeList corev1.NodeList
	if err := n.client.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// Clear existing cache
	n.cache.SpotNodes = make(map[string]*corev1.Node)
	n.cache.OnDemandNodes = make(map[string]*corev1.Node)
	n.cache.NodeTypes = make(map[string]apis.NodeType)

	// Classify each node
	for i := range nodeList.Items {
		node := nodeList.Items[i].DeepCopy()
		nodeType := n.ClassifyNode(node)
		n.cache.storeNodeClassificationLocked(node, nodeType)
	}

	n.cache.LastRefresh = time.Now()
	return nil
}

// ClassifyNodesByName returns node types for a set of node names using the internal cache when possible.
func (n *NodeClassifierService) ClassifyNodesByName(ctx context.Context, nodeNames []string) (map[string]apis.NodeType, error) {
	results := make(map[string]apis.NodeType, len(nodeNames))
	if len(nodeNames) == 0 {
		return results, nil
	}

	uniqueNames := make(map[string]struct{}, len(nodeNames))
	for _, name := range nodeNames {
		if name == "" {
			continue
		}
		uniqueNames[name] = struct{}{}
	}

	if len(uniqueNames) == 0 {
		return results, nil
	}

	if err := n.RefreshCache(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh node cache: %w", err)
	}

	missing := make([]string, 0)

	n.cache.mutex.RLock()
	for name := range uniqueNames {
		if nodeType, ok := n.cache.NodeTypes[name]; ok {
			results[name] = nodeType
		} else {
			missing = append(missing, name)
		}
	}
	n.cache.mutex.RUnlock()

	for _, name := range missing {
		var node corev1.Node
		if err := n.client.Get(ctx, types.NamespacedName{Name: name}, &node); err != nil {
			results[name] = apis.NodeTypeUnknown
			continue
		}

		nodeType := n.ClassifyNode(&node)

		n.cache.mutex.Lock()
		n.cache.storeNodeClassificationLocked(node.DeepCopy(), nodeType)
		n.cache.mutex.Unlock()

		results[name] = nodeType
	}

	for name := range uniqueNames {
		if _, ok := results[name]; !ok {
			results[name] = apis.NodeTypeUnknown
		}
	}

	return results, nil
}

func (c *NodeCache) storeNodeClassificationLocked(node *corev1.Node, nodeType apis.NodeType) {
	if node == nil || node.Name == "" {
		return
	}

	if c.SpotNodes == nil {
		c.SpotNodes = make(map[string]*corev1.Node)
	}
	if c.OnDemandNodes == nil {
		c.OnDemandNodes = make(map[string]*corev1.Node)
	}
	if c.NodeTypes == nil {
		c.NodeTypes = make(map[string]apis.NodeType)
	}

	switch nodeType {
	case apis.NodeTypeSpot:
		c.SpotNodes[node.Name] = node
		delete(c.OnDemandNodes, node.Name)
	case apis.NodeTypeOnDemand:
		c.OnDemandNodes[node.Name] = node
		delete(c.SpotNodes, node.Name)
	case apis.NodeTypeUnknown:
		delete(c.SpotNodes, node.Name)
		delete(c.OnDemandNodes, node.Name)
	default:
		delete(c.SpotNodes, node.Name)
		delete(c.OnDemandNodes, node.Name)
	}

	c.NodeTypes[node.Name] = nodeType
}

// GetNodesBySelector returns nodes matching the given label selector
func (n *NodeClassifierService) GetNodesBySelector(ctx context.Context, selector labels.Selector) ([]*corev1.Node, error) {
	var nodeList corev1.NodeList
	if err := n.client.List(ctx, &nodeList, &client.ListOptions{
		LabelSelector: selector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list nodes with selector: %w", err)
	}

	var result []*corev1.Node
	for i := range nodeList.Items {
		result = append(result, &nodeList.Items[i])
	}

	return result, nil
}

// IsNodeReady returns true if the node is in Ready state
func (n *NodeClassifierService) IsNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// GetReadyNodesOfType returns only ready nodes of the specified type
func (n *NodeClassifierService) GetReadyNodesOfType(ctx context.Context, nodeType apis.NodeType) ([]*corev1.Node, error) {
	allNodes, err := n.GetNodesOfType(ctx, nodeType)
	if err != nil {
		return nil, err
	}

	var readyNodes []*corev1.Node
	for _, node := range allNodes {
		if n.IsNodeReady(node) {
			readyNodes = append(readyNodes, node)
		}
	}

	return readyNodes, nil
}

// GetNodeClassificationSummary returns a summary of node classification
func (n *NodeClassifierService) GetNodeClassificationSummary(ctx context.Context) (*NodeSummary, error) {
	if err := n.RefreshCache(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh cache: %w", err)
	}

	n.cache.mutex.RLock()
	defer n.cache.mutex.RUnlock()

	// Count ready nodes
	readySpotNodes := 0
	for _, node := range n.cache.SpotNodes {
		if n.IsNodeReady(node) {
			readySpotNodes++
		}
	}

	readyOnDemandNodes := 0
	for _, node := range n.cache.OnDemandNodes {
		if n.IsNodeReady(node) {
			readyOnDemandNodes++
		}
	}

	return &NodeSummary{
		TotalSpotNodes:       len(n.cache.SpotNodes),
		TotalOnDemandNodes:   len(n.cache.OnDemandNodes),
		ReadySpotNodes:       readySpotNodes,
		ReadyOnDemandNodes:   readyOnDemandNodes,
		LastCacheRefresh:     n.cache.LastRefresh,
		CacheRefreshInterval: n.config.CacheRefreshInterval,
		CloudProvider:        n.config.CloudProvider,
	}, nil
}

// UpdateConfig updates the node classifier configuration
func (n *NodeClassifierService) UpdateConfig(config *NodeClassifierConfig) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	n.config = config

	// Force cache refresh on next access
	n.cache.mutex.Lock()
	n.cache.LastRefresh = time.Time{}
	n.cache.mutex.Unlock()
}

// GetConfig returns the current configuration
func (n *NodeClassifierService) GetConfig() *NodeClassifierConfig {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	// Return a copy to prevent external modification
	config := *n.config
	return &config
}

// DetectCloudProvider attempts to auto-detect the cloud provider
func (n *NodeClassifierService) DetectCloudProvider(ctx context.Context) (string, error) {
	var nodeList corev1.NodeList
	if err := n.client.List(ctx, &nodeList, &client.ListOptions{
		Limit: 1, // We only need one node to detect the provider
	}); err != nil {
		return "", fmt.Errorf("failed to list nodes for provider detection: %w", err)
	}

	if len(nodeList.Items) == 0 {
		return "unknown", nil
	}

	node := &nodeList.Items[0]

	// Check for AWS/EKS
	if _, exists := node.Labels["eks.amazonaws.com/capacityType"]; exists {
		return cloudProviderAWS, nil
	}
	if _, exists := node.Labels["karpenter.sh/capacity-type"]; exists {
		return cloudProviderAWS, nil
	}

	// Check for GCP/GKE
	if _, exists := node.Labels["cloud.google.com/gke-preemptible"]; exists {
		return "gcp", nil
	}

	// Check for Azure/AKS
	if _, exists := node.Labels["kubernetes.azure.com/scalesetpriority"]; exists {
		return "azure", nil
	}

	// Check provider ID for hints
	if node.Spec.ProviderID != "" {
		providerID := strings.ToLower(node.Spec.ProviderID)
		if strings.Contains(providerID, "aws") {
			return cloudProviderAWS, nil
		}
		if strings.Contains(providerID, "gce") || strings.Contains(providerID, "gcp") {
			return "gcp", nil
		}
		if strings.Contains(providerID, "azure") {
			return "azure", nil
		}
	}

	return "unknown", nil
}
