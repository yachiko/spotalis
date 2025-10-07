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
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NamespaceFilterConfig contains configuration for namespace filtering
type NamespaceFilterConfig struct {
	// Include/exclude patterns
	IncludeNamespaces []string `yaml:"includeNamespaces"`
	ExcludeNamespaces []string `yaml:"excludeNamespaces"`

	// Regex patterns for advanced filtering
	IncludePatterns []string `yaml:"includePatterns"`
	ExcludePatterns []string `yaml:"excludePatterns"`

	// Label-based filtering
	RequiredLabels  map[string]string `yaml:"requiredLabels"`
	ForbiddenLabels map[string]string `yaml:"forbiddenLabels"`

	// Annotation-based filtering
	RequiredAnnotations  map[string]string `yaml:"requiredAnnotations"`
	ForbiddenAnnotations map[string]string `yaml:"forbiddenAnnotations"`

	// Multi-tenancy support
	TenantMode       bool                `yaml:"tenantMode"`
	TenantLabelKey   string              `yaml:"tenantLabelKey"`
	TenantNamespaces map[string][]string `yaml:"tenantNamespaces"`

	// System namespace handling
	IncludeSystemNamespaces bool     `yaml:"includeSystemNamespaces"`
	SystemNamespaces        []string `yaml:"systemNamespaces"`

	// Dynamic filtering
	EnableDynamicFiltering bool   `yaml:"enableDynamicFiltering"`
	RefreshInterval        string `yaml:"refreshInterval"`

	// Performance options
	CacheEnabled   bool `yaml:"cacheEnabled"`
	CacheSize      int  `yaml:"cacheSize"`
	MetricsEnabled bool `yaml:"metricsEnabled"`
}

// DefaultNamespaceFilterConfig returns default namespace filter configuration
func DefaultNamespaceFilterConfig() *NamespaceFilterConfig {
	return &NamespaceFilterConfig{
		IncludeNamespaces:       []string{},
		ExcludeNamespaces:       []string{"kube-system", "kube-public", "kube-node-lease"},
		IncludePatterns:         []string{},
		ExcludePatterns:         []string{"^kube-.*", "^openshift-.*"},
		RequiredLabels:          make(map[string]string),
		ForbiddenLabels:         make(map[string]string),
		RequiredAnnotations:     make(map[string]string),
		ForbiddenAnnotations:    make(map[string]string),
		TenantMode:              false,
		TenantLabelKey:          "tenant",
		TenantNamespaces:        make(map[string][]string),
		IncludeSystemNamespaces: false,
		SystemNamespaces:        []string{"kube-system", "kube-public", "kube-node-lease", "default"},
		EnableDynamicFiltering:  true,
		RefreshInterval:         "5m",
		CacheEnabled:            true,
		CacheSize:               1000,
		MetricsEnabled:          true,
	}
}

// NamespaceFilter provides namespace filtering capabilities for multi-tenancy
type NamespaceFilter struct {
	config *NamespaceFilterConfig
	client client.Client

	// Compiled regex patterns
	includeRegex []*regexp.Regexp
	excludeRegex []*regexp.Regexp

	// Label selectors
	requiredSelector  labels.Selector
	forbiddenSelector labels.Selector

	// Cache for namespace decisions
	cache    map[string]*FilterResult
	cacheMux sync.RWMutex

	// Metrics
	metrics *NamespaceFilterMetrics

	// Dynamic filtering
	refreshTicker *NamespaceRefreshTicker
}

// FilterResult contains the result of namespace filtering
type FilterResult struct {
	Allowed     bool
	Reason      string
	Tenant      string
	MatchedRule string
	Timestamp   int64
}

// NewNamespaceFilter creates a new namespace filter
func NewNamespaceFilter(config *NamespaceFilterConfig, client client.Client) (*NamespaceFilter, error) {
	if config == nil {
		config = DefaultNamespaceFilterConfig()
	}

	nf := &NamespaceFilter{
		config: config,
		client: client,
		cache:  make(map[string]*FilterResult),
	}

	// Compile regex patterns
	if err := nf.compilePatterns(); err != nil {
		return nil, fmt.Errorf("failed to compile patterns: %w", err)
	}

	// Build label selectors
	if err := nf.buildSelectors(); err != nil {
		return nil, fmt.Errorf("failed to build selectors: %w", err)
	}

	// Initialize metrics
	if config.MetricsEnabled {
		nf.metrics = NewNamespaceFilterMetrics()
	}

	// Start dynamic filtering if enabled
	if config.EnableDynamicFiltering {
		ticker, err := NewNamespaceRefreshTicker(config.RefreshInterval, nf.refreshCache)
		if err != nil {
			return nil, fmt.Errorf("failed to create refresh ticker: %w", err)
		}
		nf.refreshTicker = ticker
	}

	return nf, nil
}

// IsNamespaceAllowed checks if a namespace is allowed for processing
func (nf *NamespaceFilter) IsNamespaceAllowed(ctx context.Context, namespace string) (*FilterResult, error) {
	// Check cache first
	if nf.config.CacheEnabled {
		if result := nf.getCachedResult(namespace); result != nil {
			if nf.metrics != nil {
				nf.metrics.RecordCacheHit(namespace)
			}
			return result, nil
		}
	}

	// Perform filtering
	result, err := nf.performFiltering(ctx, namespace)
	if err != nil {
		if nf.metrics != nil {
			nf.metrics.RecordFilterError(namespace, err)
		}
		return nil, err
	}

	// Cache result
	if nf.config.CacheEnabled {
		nf.setCachedResult(namespace, result)
	}

	// Record metrics
	if nf.metrics != nil {
		nf.metrics.RecordFilterResult(namespace, result.Allowed, result.Reason)
	}

	return result, nil
}

// IsNamespaceAllowedForTenant checks if a namespace is allowed for a specific tenant
func (nf *NamespaceFilter) IsNamespaceAllowedForTenant(ctx context.Context, namespace, tenant string) (*FilterResult, error) {
	// Check basic namespace filtering first
	result, err := nf.IsNamespaceAllowed(ctx, namespace)
	if err != nil {
		return nil, err
	}

	if !result.Allowed {
		return result, nil
	}

	// Check tenant-specific filtering
	if nf.config.TenantMode {
		tenantResult := nf.checkTenantAccess(namespace, tenant)
		if !tenantResult.Allowed {
			return tenantResult, nil
		}
		result.Tenant = tenant
	}

	return result, nil
}

// CreateNamespacePredicate creates a controller-runtime predicate for namespace filtering
func (nf *NamespaceFilter) CreateNamespacePredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		result, err := nf.IsNamespaceAllowed(context.Background(), obj.GetNamespace())
		if err != nil {
			// Log error but don't block processing
			return true
		}
		return result.Allowed
	})
}

// CreateTenantPredicate creates a controller-runtime predicate for tenant-specific filtering
func (nf *NamespaceFilter) CreateTenantPredicate(tenant string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		result, err := nf.IsNamespaceAllowedForTenant(context.Background(), obj.GetNamespace(), tenant)
		if err != nil {
			// Log error but don't block processing
			return true
		}
		return result.Allowed
	})
}

// GetFilteredNamespaces returns a list of namespaces that pass the filter
func (nf *NamespaceFilter) GetFilteredNamespaces(ctx context.Context) ([]string, error) {
	var namespaces []string

	// List all namespaces
	namespaceList := &corev1.NamespaceList{}
	if err := nf.client.List(ctx, namespaceList); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Filter namespaces
	for i := range namespaceList.Items {
		item := &namespaceList.Items[i]
		namespace := item.Name
		result, err := nf.IsNamespaceAllowed(ctx, namespace)
		if err != nil {
			continue // Skip on error
		}
		if result.Allowed {
			namespaces = append(namespaces, namespace)
		}
	}

	return namespaces, nil
}

// GetTenantNamespaces returns namespaces for a specific tenant
func (nf *NamespaceFilter) GetTenantNamespaces(ctx context.Context, tenant string) ([]string, error) {
	if !nf.config.TenantMode {
		return nf.GetFilteredNamespaces(ctx)
	}

	var namespaces []string

	// Check configured tenant namespaces first
	if tenantNs, exists := nf.config.TenantNamespaces[tenant]; exists {
		for _, ns := range tenantNs {
			result, err := nf.IsNamespaceAllowedForTenant(ctx, ns, tenant)
			if err != nil {
				continue
			}
			if result.Allowed {
				namespaces = append(namespaces, ns)
			}
		}
		return namespaces, nil
	}

	// Dynamic discovery using label selector
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement(nf.config.TenantLabelKey, selection.Equals, []string{tenant})
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant selector: %w", err)
	}
	selector = selector.Add(*requirement)

	namespaceList := &corev1.NamespaceList{}
	listOpts := &client.ListOptions{
		LabelSelector: selector,
	}

	if err := nf.client.List(ctx, namespaceList, listOpts); err != nil {
		return nil, fmt.Errorf("failed to list tenant namespaces: %w", err)
	}

	for i := range namespaceList.Items {
		item := &namespaceList.Items[i]
		namespace := item.Name
		result, err := nf.IsNamespaceAllowedForTenant(ctx, namespace, tenant)
		if err != nil {
			continue
		}
		if result.Allowed {
			namespaces = append(namespaces, namespace)
		}
	}

	return namespaces, nil
}

// UpdateConfig updates the namespace filter configuration
func (nf *NamespaceFilter) UpdateConfig(config *NamespaceFilterConfig) error {
	nf.config = config

	// Recompile patterns
	if err := nf.compilePatterns(); err != nil {
		return fmt.Errorf("failed to recompile patterns: %w", err)
	}

	// Rebuild selectors
	if err := nf.buildSelectors(); err != nil {
		return fmt.Errorf("failed to rebuild selectors: %w", err)
	}

	// Clear cache
	if nf.config.CacheEnabled {
		nf.clearCache()
	}

	return nil
}

// GetMetrics returns the current namespace filter metrics
func (nf *NamespaceFilter) GetMetrics() *NamespaceFilterMetrics {
	return nf.metrics
}

// Shutdown gracefully shuts down the namespace filter
func (nf *NamespaceFilter) Shutdown() {
	if nf.refreshTicker != nil {
		nf.refreshTicker.Stop()
	}
}

// performFiltering performs the actual namespace filtering logic
func (nf *NamespaceFilter) performFiltering(ctx context.Context, namespace string) (*FilterResult, error) {
	result := &FilterResult{
		Allowed:   true,
		Reason:    "default",
		Timestamp: metav1.Now().Unix(),
	}

	// Apply exclusion filters first (early termination for rejected namespaces)
	if blocked := nf.applyExclusionFilters(namespace, result); blocked {
		return result, nil
	}

	// Apply inclusion filters
	if blocked := nf.applyInclusionFilters(namespace, result); blocked {
		return result, nil
	}

	// Apply namespace metadata filters (labels and annotations)
	if blocked := nf.applyMetadataFilters(ctx, namespace, result); blocked {
		return result, nil
	}

	result.Reason = "passed all filters"
	return result, nil
}

// applyExclusionFilters checks system namespaces, explicit exclusions, and exclude patterns
func (nf *NamespaceFilter) applyExclusionFilters(namespace string, result *FilterResult) bool {
	// Check system namespace handling
	if nf.isSystemNamespace(namespace) && !nf.config.IncludeSystemNamespaces {
		result.Allowed = false
		result.Reason = "system namespace excluded"
		result.MatchedRule = "system_namespace_exclusion"
		return true
	}

	// Check explicit exclusions first
	if nf.checkExplicitExclusion(namespace, result) {
		return true
	}

	// Check exclude patterns
	return nf.checkExcludePatterns(namespace, result)
}

// checkExplicitExclusion checks if namespace is explicitly excluded
func (nf *NamespaceFilter) checkExplicitExclusion(namespace string, result *FilterResult) bool {
	for _, excluded := range nf.config.ExcludeNamespaces {
		if namespace == excluded {
			result.Allowed = false
			result.Reason = "explicitly excluded"
			result.MatchedRule = fmt.Sprintf("exclude_namespace:%s", excluded)
			return true
		}
	}
	return false
}

// checkExcludePatterns checks if namespace matches any exclude patterns
func (nf *NamespaceFilter) checkExcludePatterns(namespace string, result *FilterResult) bool {
	for i, pattern := range nf.excludeRegex {
		if pattern.MatchString(namespace) {
			result.Allowed = false
			result.Reason = "matched exclude pattern"
			result.MatchedRule = fmt.Sprintf("exclude_pattern:%s", nf.config.ExcludePatterns[i])
			return true
		}
	}
	return false
}

// applyInclusionFilters checks explicit inclusions and include patterns
func (nf *NamespaceFilter) applyInclusionFilters(namespace string, result *FilterResult) bool {
	// Check explicit inclusions
	if nf.checkExplicitInclusion(namespace, result) {
		return true
	}

	// Check include patterns
	return nf.checkIncludePatterns(namespace, result)
}

// checkExplicitInclusion checks if namespace is in explicit inclusion list
func (nf *NamespaceFilter) checkExplicitInclusion(namespace string, result *FilterResult) bool {
	if len(nf.config.IncludeNamespaces) == 0 {
		return false
	}

	for _, included := range nf.config.IncludeNamespaces {
		if namespace == included {
			result.MatchedRule = fmt.Sprintf("include_namespace:%s", included)
			return false // Found in include list, continue processing
		}
	}

	// Not found in include list, block
	result.Allowed = false
	result.Reason = "not in include list"
	result.MatchedRule = "include_namespace_check"
	return true
}

// checkIncludePatterns checks if namespace matches any include patterns
func (nf *NamespaceFilter) checkIncludePatterns(namespace string, result *FilterResult) bool {
	if len(nf.includeRegex) == 0 {
		return false
	}

	for i, pattern := range nf.includeRegex {
		if pattern.MatchString(namespace) {
			result.MatchedRule = fmt.Sprintf("include_pattern:%s", nf.config.IncludePatterns[i])
			return false // Found matching pattern, continue processing
		}
	}

	// No include pattern matched, block
	result.Allowed = false
	result.Reason = "no include pattern matched"
	result.MatchedRule = "include_pattern_check"
	return true
}

// applyMetadataFilters checks namespace labels and annotations
func (nf *NamespaceFilter) applyMetadataFilters(ctx context.Context, namespace string, result *FilterResult) bool {
	if nf.client == nil {
		return false
	}

	namespaceObj := &corev1.Namespace{}
	err := nf.client.Get(ctx, client.ObjectKey{Name: namespace}, namespaceObj)
	if err != nil {
		// Namespace might not exist, allow processing to proceed
		result.Reason = "namespace not found, allowing"
		result.MatchedRule = "namespace_not_found"
		return false
	}

	// Check all metadata requirements
	return nf.checkMetadataRequirements(namespaceObj, result)
}

// checkMetadataRequirements checks all label and annotation requirements
func (nf *NamespaceFilter) checkMetadataRequirements(namespaceObj *corev1.Namespace, result *FilterResult) bool {
	// Check required labels (including spotalis.io/enabled)
	if !nf.checkRequiredLabels(namespaceObj.Labels) {
		result.Allowed = false
		result.Reason = "missing required labels"
		result.MatchedRule = "required_labels_check"
		return true
	}

	// Check forbidden labels
	if nf.checkForbiddenLabels(namespaceObj.Labels) {
		result.Allowed = false
		result.Reason = "has forbidden labels"
		result.MatchedRule = "forbidden_labels_check"
		return true
	}

	// Check forbidden annotations (still useful for blocking namespaces with problematic annotations)
	if nf.checkForbiddenAnnotations(namespaceObj.Annotations) {
		result.Allowed = false
		result.Reason = "has forbidden annotations"
		result.MatchedRule = "forbidden_annotations_check"
		return true
	}

	return false
}

// checkTenantAccess checks if a namespace is accessible by a specific tenant
func (nf *NamespaceFilter) checkTenantAccess(namespace, tenant string) *FilterResult {
	result := &FilterResult{
		Allowed:   true,
		Tenant:    tenant,
		Timestamp: metav1.Now().Unix(),
	}

	// Check configured tenant namespaces
	if tenantNs, exists := nf.config.TenantNamespaces[tenant]; exists {
		found := false
		for _, ns := range tenantNs {
			if ns == namespace {
				found = true
				break
			}
		}
		if !found {
			result.Allowed = false
			result.Reason = "namespace not assigned to tenant"
			result.MatchedRule = "tenant_namespace_assignment"
		}
		return result
	}

	// If no explicit assignment, allow (will be checked by namespace labels)
	result.Reason = "tenant access allowed"
	return result
}

// compilePatterns compiles regex patterns for namespace filtering
func (nf *NamespaceFilter) compilePatterns() error {
	// Compile include patterns
	nf.includeRegex = make([]*regexp.Regexp, len(nf.config.IncludePatterns))
	for i, pattern := range nf.config.IncludePatterns {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
		}
		nf.includeRegex[i] = regex
	}

	// Compile exclude patterns
	nf.excludeRegex = make([]*regexp.Regexp, len(nf.config.ExcludePatterns))
	for i, pattern := range nf.config.ExcludePatterns {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
		}
		nf.excludeRegex[i] = regex
	}

	return nil
}

// buildSelectors builds label selectors for filtering
func (nf *NamespaceFilter) buildSelectors() error {
	// Build required labels selector
	if len(nf.config.RequiredLabels) > 0 {
		nf.requiredSelector = labels.Set(nf.config.RequiredLabels).AsSelector()
	} else {
		nf.requiredSelector = labels.Everything()
	}

	// Build forbidden labels selector
	if len(nf.config.ForbiddenLabels) > 0 {
		nf.forbiddenSelector = labels.Set(nf.config.ForbiddenLabels).AsSelector()
	} else {
		nf.forbiddenSelector = labels.Nothing()
	}

	return nil
}

// isSystemNamespace checks if a namespace is a system namespace
func (nf *NamespaceFilter) isSystemNamespace(namespace string) bool {
	for _, sysNs := range nf.config.SystemNamespaces {
		if namespace == sysNs {
			return true
		}
	}

	// Check common system namespace patterns
	systemPatterns := []string{"^kube-", "^openshift-", "^istio-"}
	for _, pattern := range systemPatterns {
		if matched, err := regexp.MatchString(pattern, namespace); err == nil && matched {
			return true
		}
		// If regexp fails, continue to next pattern
	}

	return false
}

// checkRequiredLabels checks if namespace has all required labels
func (nf *NamespaceFilter) checkRequiredLabels(nsLabels map[string]string) bool {
	for key, value := range nf.config.RequiredLabels {
		if nsValue, exists := nsLabels[key]; !exists || nsValue != value {
			return false
		}
	}
	return true
}

// checkForbiddenLabels checks if namespace has any forbidden labels
func (nf *NamespaceFilter) checkForbiddenLabels(nsLabels map[string]string) bool {
	for key, value := range nf.config.ForbiddenLabels {
		if nsValue, exists := nsLabels[key]; exists && nsValue == value {
			return true
		}
	}
	return false
}

// checkRequiredAnnotations checks if namespace has all required annotations
func (nf *NamespaceFilter) checkRequiredAnnotations(nsAnnotations map[string]string) bool {
	for key, value := range nf.config.RequiredAnnotations {
		if nsValue, exists := nsAnnotations[key]; !exists || nsValue != value {
			return false
		}
	}
	return true
}

// checkForbiddenAnnotations checks if namespace has any forbidden annotations
func (nf *NamespaceFilter) checkForbiddenAnnotations(nsAnnotations map[string]string) bool {
	for key, value := range nf.config.ForbiddenAnnotations {
		if nsValue, exists := nsAnnotations[key]; exists && nsValue == value {
			return true
		}
	}
	return false
}

// Cache management functions
func (nf *NamespaceFilter) getCachedResult(namespace string) *FilterResult {
	nf.cacheMux.RLock()
	defer nf.cacheMux.RUnlock()
	return nf.cache[namespace]
}

func (nf *NamespaceFilter) setCachedResult(namespace string, result *FilterResult) {
	nf.cacheMux.Lock()
	defer nf.cacheMux.Unlock()

	// Implement LRU eviction if cache is full
	if len(nf.cache) >= nf.config.CacheSize {
		// Simple implementation: clear cache when full
		nf.cache = make(map[string]*FilterResult)
	}

	nf.cache[namespace] = result
}

func (nf *NamespaceFilter) clearCache() {
	nf.cacheMux.Lock()
	defer nf.cacheMux.Unlock()
	nf.cache = make(map[string]*FilterResult)
}

func (nf *NamespaceFilter) refreshCache() {
	// Refresh cache by clearing it - next access will repopulate
	nf.clearCache()
}

// NamespaceFilterMetrics collects metrics for namespace filtering
type NamespaceFilterMetrics struct {
	totalChecks    int64
	allowedResults int64
	deniedResults  int64
	cacheHits      int64
	cacheMisses    int64
	errors         int64

	// Per-namespace metrics
	namespaceResults map[string]*NamespaceResult

	mutex sync.RWMutex
}

// NamespaceResult contains metrics for a specific namespace
type NamespaceResult struct {
	Allowed     int64
	Denied      int64
	LastChecked int64
	LastReason  string
}

// NewNamespaceFilterMetrics creates new namespace filter metrics
func NewNamespaceFilterMetrics() *NamespaceFilterMetrics {
	return &NamespaceFilterMetrics{
		namespaceResults: make(map[string]*NamespaceResult),
	}
}

// RecordFilterResult records a namespace filter result
func (m *NamespaceFilterMetrics) RecordFilterResult(namespace string, allowed bool, reason string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.totalChecks++
	if allowed {
		m.allowedResults++
	} else {
		m.deniedResults++
	}

	// Update namespace-specific metrics
	if _, exists := m.namespaceResults[namespace]; !exists {
		m.namespaceResults[namespace] = &NamespaceResult{}
	}

	result := m.namespaceResults[namespace]
	if allowed {
		result.Allowed++
	} else {
		result.Denied++
	}
	result.LastChecked = metav1.Now().Unix()
	result.LastReason = reason
}

// RecordCacheHit records a cache hit
func (m *NamespaceFilterMetrics) RecordCacheHit(_ string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.cacheHits++
}

// RecordCacheMiss records a cache miss
func (m *NamespaceFilterMetrics) RecordCacheMiss(_ string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.cacheMisses++
}

// RecordFilterError records a filter error
func (m *NamespaceFilterMetrics) RecordFilterError(_ string, _ error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.errors++
}

// GetSummary returns a summary of namespace filter metrics
func (m *NamespaceFilterMetrics) GetSummary() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	summary := map[string]interface{}{
		"total_checks":    m.totalChecks,
		"allowed_results": m.allowedResults,
		"denied_results":  m.deniedResults,
		"cache_hits":      m.cacheHits,
		"cache_misses":    m.cacheMisses,
		"errors":          m.errors,
	}

	// Add cache hit rate
	totalCacheAccess := m.cacheHits + m.cacheMisses
	if totalCacheAccess > 0 {
		summary["cache_hit_rate"] = float64(m.cacheHits) / float64(totalCacheAccess)
	}

	// Add allow rate
	if m.totalChecks > 0 {
		summary["allow_rate"] = float64(m.allowedResults) / float64(m.totalChecks)
	}

	return summary
}

// NamespaceRefreshTicker handles periodic cache refresh
type NamespaceRefreshTicker struct {
	ticker   *time.Ticker
	callback func()
	done     chan bool
}

// NewNamespaceRefreshTicker creates a new refresh ticker
func NewNamespaceRefreshTicker(interval string, callback func()) (*NamespaceRefreshTicker, error) {
	duration, err := time.ParseDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh interval: %w", err)
	}

	ticker := &NamespaceRefreshTicker{
		ticker:   time.NewTicker(duration),
		callback: callback,
		done:     make(chan bool),
	}

	go ticker.run()

	return ticker, nil
}

func (t *NamespaceRefreshTicker) run() {
	for {
		select {
		case <-t.ticker.C:
			t.callback()
		case <-t.done:
			return
		}
	}
}

// Stop stops the namespace refresh ticker and signals completion
func (t *NamespaceRefreshTicker) Stop() {
	t.ticker.Stop()
	t.done <- true
}
