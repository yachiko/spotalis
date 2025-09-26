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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Configuration represents the complete Spotalis configuration
type Configuration struct {
	// Controller configuration
	Controller ControllerConfig `yaml:"controller" json:"controller"`

	// Webhook configuration
	Webhook WebhookConfig `yaml:"webhook" json:"webhook"`

	// Kubernetes configuration
	Kubernetes KubernetesConfig `yaml:"kubernetes" json:"kubernetes"`

	// Leader election configuration
	LeaderElection LeaderElectionConfig `yaml:"leaderElection" json:"leaderElection"`

	// Logging configuration
	Logging LoggingConfig `yaml:"logging" json:"logging"`

	// Metrics configuration
	Metrics MetricsConfig `yaml:"metrics" json:"metrics"`

	// Node classification configuration
	NodeClassification NodeClassificationConfig `yaml:"nodeClassification" json:"nodeClassification"`

	// Namespace filtering configuration
	Namespaces NamespacesConfig `yaml:"namespaces" json:"namespaces"`
}

// ControllerConfig contains controller-specific configuration
type ControllerConfig struct {
	// Basic settings
	Namespace               string        `yaml:"namespace" json:"namespace"`
	MaxConcurrentReconciles int           `yaml:"maxConcurrentReconciles" json:"maxConcurrentReconciles"`
	ReconcileInterval       time.Duration `yaml:"reconcileInterval" json:"reconcileInterval"`
	ReconcileTimeout        time.Duration `yaml:"reconcileTimeout" json:"reconcileTimeout"`

	// Feature flags
	EnableDeployments  bool `yaml:"enableDeployments" json:"enableDeployments"`
	EnableStatefulSets bool `yaml:"enableStatefulSets" json:"enableStatefulSets"`
	EnableDaemonSets   bool `yaml:"enableDaemonSets" json:"enableDaemonSets"`

	// Operational settings
	ReadOnlyMode            bool          `yaml:"readOnlyMode" json:"readOnlyMode"`
	GracefulShutdownTimeout time.Duration `yaml:"gracefulShutdownTimeout" json:"gracefulShutdownTimeout"`
}

// WebhookConfig contains webhook-specific configuration
type WebhookConfig struct {
	// Basic settings
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Port     int    `yaml:"port" json:"port"`
	CertDir  string `yaml:"certDir" json:"certDir"`
	CertName string `yaml:"certName" json:"certName"`
	KeyName  string `yaml:"keyName" json:"keyName"`

	// Service configuration
	ServiceName      string `yaml:"serviceName" json:"serviceName"`
	ServiceNamespace string `yaml:"serviceNamespace" json:"serviceNamespace"`
	ServicePath      string `yaml:"servicePath" json:"servicePath"`

	// TLS configuration
	TLSMinVersion   string   `yaml:"tlsMinVersion" json:"tlsMinVersion"`
	TLSCipherSuites []string `yaml:"tlsCipherSuites" json:"tlsCipherSuites"`

	// Webhook behavior
	FailurePolicy  string `yaml:"failurePolicy" json:"failurePolicy"`
	TimeoutSeconds int32  `yaml:"timeoutSeconds" json:"timeoutSeconds"`
}

// KubernetesConfig contains Kubernetes client configuration
type KubernetesConfig struct {
	// Client settings
	Kubeconfig string        `yaml:"kubeconfig" json:"kubeconfig"`
	Context    string        `yaml:"context" json:"context"`
	QPS        float32       `yaml:"qps" json:"qps"`
	Burst      int           `yaml:"burst" json:"burst"`
	Timeout    time.Duration `yaml:"timeout" json:"timeout"`

	// RBAC settings
	ServiceAccount string `yaml:"serviceAccount" json:"serviceAccount"`
	ClusterRole    string `yaml:"clusterRole" json:"clusterRole"`
	RoleBinding    string `yaml:"roleBinding" json:"roleBinding"`
}

// LeaderElectionConfig contains leader election configuration
type LeaderElectionConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	ID            string        `yaml:"id" json:"id"`
	LeaseName     string        `yaml:"leaseName" json:"leaseName"`
	LeaseDuration time.Duration `yaml:"leaseDuration" json:"leaseDuration"`
	RenewDeadline time.Duration `yaml:"renewDeadline" json:"renewDeadline"`
	RetryPeriod   time.Duration `yaml:"retryPeriod" json:"retryPeriod"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level       string `yaml:"level" json:"level"`
	Development bool   `yaml:"development" json:"development"`
	EnablePprof bool   `yaml:"enablePprof" json:"enablePprof"`
}

// MetricsConfig contains metrics configuration
type MetricsConfig struct {
	BindAddress        string        `yaml:"bindAddress" json:"bindAddress"`
	EnableProfiling    bool          `yaml:"enableProfiling" json:"enableProfiling"`
	ProfilingAddress   string        `yaml:"profilingAddress" json:"profilingAddress"`
	HealthBindAddress  string        `yaml:"healthBindAddress" json:"healthBindAddress"`
	CollectionInterval time.Duration `yaml:"collectionInterval" json:"collectionInterval"`
}

// NodeClassificationConfig contains node classification configuration
type NodeClassificationConfig struct {
	CacheSize      int           `yaml:"cacheSize" json:"cacheSize"`
	CacheTTL       time.Duration `yaml:"cacheTTL" json:"cacheTTL"`
	UpdateInterval time.Duration `yaml:"updateInterval" json:"updateInterval"`

	// Cloud provider settings
	AWSEnabled   bool `yaml:"awsEnabled" json:"awsEnabled"`
	GCPEnabled   bool `yaml:"gcpEnabled" json:"gcpEnabled"`
	AzureEnabled bool `yaml:"azureEnabled" json:"azureEnabled"`
}

// NamespacesConfig contains namespace filtering configuration
type NamespacesConfig struct {
	Watch  []string `yaml:"watch" json:"watch"`
	Ignore []string `yaml:"ignore" json:"ignore"`
}

// DefaultConfiguration returns the default configuration
func DefaultConfiguration() *Configuration {
	return &Configuration{
		Controller: ControllerConfig{
			Namespace:               "spotalis-system",
			MaxConcurrentReconciles: 10,
			ReconcileInterval:       30 * time.Second,
			ReconcileTimeout:        5 * time.Minute,
			EnableDeployments:       true,
			EnableStatefulSets:      true,
			EnableDaemonSets:        false,
			ReadOnlyMode:            false,
			GracefulShutdownTimeout: 30 * time.Second,
		},
		Webhook: WebhookConfig{
			Enabled:          true,
			Port:             9443,
			CertDir:          "/tmp/k8s-webhook-server/serving-certs",
			CertName:         "tls.crt",
			KeyName:          "tls.key",
			ServiceName:      "spotalis-webhook-service",
			ServiceNamespace: "spotalis-system",
			ServicePath:      "/mutate",
			TLSMinVersion:    "1.3",
			FailurePolicy:    "Fail",
			TimeoutSeconds:   10,
		},
		Kubernetes: KubernetesConfig{
			QPS:            20.0,
			Burst:          30,
			Timeout:        30 * time.Second,
			ServiceAccount: "spotalis-controller",
			ClusterRole:    "spotalis-controller",
			RoleBinding:    "spotalis-controller",
		},
		LeaderElection: LeaderElectionConfig{
			Enabled:       true,
			ID:            "spotalis-controller-leader",
			LeaseName:     "spotalis-controller-leader",
			LeaseDuration: 15 * time.Second,
			RenewDeadline: 10 * time.Second,
			RetryPeriod:   2 * time.Second,
		},
		Logging: LoggingConfig{
			Level:       "info",
			Development: false,
			EnablePprof: false,
		},
		Metrics: MetricsConfig{
			BindAddress:        ":8080",
			EnableProfiling:    false,
			ProfilingAddress:   ":6060",
			HealthBindAddress:  ":8081",
			CollectionInterval: 30 * time.Second,
		},
		NodeClassification: NodeClassificationConfig{
			CacheSize:      1000,
			CacheTTL:       5 * time.Minute,
			UpdateInterval: 30 * time.Second,
			AWSEnabled:     true,
			GCPEnabled:     true,
			AzureEnabled:   true,
		},
		Namespaces: NamespacesConfig{
			Watch:  []string{},
			Ignore: []string{"kube-system", "kube-public", "kube-node-lease"},
		},
	}
}

// ConfigurationLoader handles loading configuration from multiple sources
type ConfigurationLoader struct {
	config *Configuration
}

// NewConfigurationLoader creates a new configuration loader
func NewConfigurationLoader() *ConfigurationLoader {
	return &ConfigurationLoader{
		config: DefaultConfiguration(),
	}
}

// LoadFromFile loads configuration from a YAML file
func (cl *ConfigurationLoader) LoadFromFile(path string) error {
	if path == "" {
		return nil // No file specified, use defaults
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s", path)
	}

	// Read file content
	data, err := os.ReadFile(path) // #nosec G304 - path is validated configuration file
	if err != nil {
		return fmt.Errorf("failed to read configuration file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, cl.config); err != nil {
		return fmt.Errorf("failed to parse configuration file: %w", err)
	}

	return nil
}

// LoadFromEnvironment loads configuration from environment variables
func (cl *ConfigurationLoader) LoadFromEnvironment() error {
	envMappings := map[string]func(string) error{
		// Controller configuration
		"SPOTALIS_NAMESPACE":                 cl.setControllerNamespace,
		"SPOTALIS_MAX_CONCURRENT_RECONCILES": cl.setMaxConcurrentReconciles,
		"SPOTALIS_RECONCILE_INTERVAL":        cl.setReconcileInterval,
		"SPOTALIS_RECONCILE_TIMEOUT":         cl.setReconcileTimeout,
		"SPOTALIS_ENABLE_DEPLOYMENTS":        cl.setEnableDeployments,
		"SPOTALIS_ENABLE_STATEFULSETS":       cl.setEnableStatefulSets,
		"SPOTALIS_ENABLE_DAEMONSETS":         cl.setEnableDaemonSets,
		"SPOTALIS_READ_ONLY_MODE":            cl.setReadOnlyMode,
		"SPOTALIS_GRACEFUL_SHUTDOWN_TIMEOUT": cl.setGracefulShutdownTimeout,

		// Webhook configuration
		"SPOTALIS_WEBHOOK_ENABLED":           cl.setWebhookEnabled,
		"SPOTALIS_WEBHOOK_PORT":              cl.setWebhookPort,
		"SPOTALIS_WEBHOOK_CERT_DIR":          cl.setWebhookCertDir,
		"SPOTALIS_WEBHOOK_CERT_NAME":         cl.setWebhookCertName,
		"SPOTALIS_WEBHOOK_KEY_NAME":          cl.setWebhookKeyName,
		"SPOTALIS_WEBHOOK_SERVICE_NAME":      cl.setWebhookServiceName,
		"SPOTALIS_WEBHOOK_SERVICE_NAMESPACE": cl.setWebhookServiceNamespace,
		"SPOTALIS_WEBHOOK_TLS_MIN_VERSION":   cl.setWebhookTLSMinVersion,
		"SPOTALIS_WEBHOOK_FAILURE_POLICY":    cl.setWebhookFailurePolicy,
		"SPOTALIS_WEBHOOK_TIMEOUT_SECONDS":   cl.setWebhookTimeoutSeconds,

		// Kubernetes configuration
		"KUBECONFIG":               cl.setKubeconfig,
		"SPOTALIS_KUBE_CONTEXT":    cl.setKubeContext,
		"SPOTALIS_KUBE_QPS":        cl.setKubeQPS,
		"SPOTALIS_KUBE_BURST":      cl.setKubeBurst,
		"SPOTALIS_KUBE_TIMEOUT":    cl.setKubeTimeout,
		"SPOTALIS_SERVICE_ACCOUNT": cl.setServiceAccount,
		"SPOTALIS_CLUSTER_ROLE":    cl.setClusterRole,
		"SPOTALIS_ROLE_BINDING":    cl.setRoleBinding,

		// Leader election configuration
		"SPOTALIS_LEADER_ELECTION_ENABLED":        cl.setLeaderElectionEnabled,
		"SPOTALIS_LEADER_ELECTION_ID":             cl.setLeaderElectionID,
		"SPOTALIS_LEADER_ELECTION_LEASE_NAME":     cl.setLeaderElectionLeaseName,
		"SPOTALIS_LEADER_ELECTION_LEASE_DURATION": cl.setLeaderElectionLeaseDuration,
		"SPOTALIS_LEADER_ELECTION_RENEW_DEADLINE": cl.setLeaderElectionRenewDeadline,
		"SPOTALIS_LEADER_ELECTION_RETRY_PERIOD":   cl.setLeaderElectionRetryPeriod,

		// Logging configuration
		"SPOTALIS_LOG_LEVEL":       cl.setLogLevel,
		"SPOTALIS_LOG_DEVELOPMENT": cl.setLogDevelopment,
		"SPOTALIS_ENABLE_PPROF":    cl.setEnablePprof,

		// Metrics configuration
		"SPOTALIS_METRICS_BIND_ADDRESS":        cl.setMetricsBindAddress,
		"SPOTALIS_METRICS_ENABLE_PROFILING":    cl.setMetricsEnableProfiling,
		"SPOTALIS_METRICS_PROFILING_ADDRESS":   cl.setMetricsProfilingAddress,
		"SPOTALIS_HEALTH_BIND_ADDRESS":         cl.setHealthBindAddress,
		"SPOTALIS_METRICS_COLLECTION_INTERVAL": cl.setMetricsCollectionInterval,

		// Node classification configuration
		"SPOTALIS_NODE_CACHE_SIZE":      cl.setNodeCacheSize,
		"SPOTALIS_NODE_CACHE_TTL":       cl.setNodeCacheTTL,
		"SPOTALIS_NODE_UPDATE_INTERVAL": cl.setNodeUpdateInterval,
		"SPOTALIS_AWS_ENABLED":          cl.setAWSEnabled,
		"SPOTALIS_GCP_ENABLED":          cl.setGCPEnabled,
		"SPOTALIS_AZURE_ENABLED":        cl.setAzureEnabled,

		// Namespace configuration
		"SPOTALIS_WATCH_NAMESPACES":  cl.setWatchNamespaces,
		"SPOTALIS_IGNORE_NAMESPACES": cl.setIgnoreNamespaces,
	}

	for envVar, setter := range envMappings {
		if value := os.Getenv(envVar); value != "" {
			if err := setter(value); err != nil {
				return fmt.Errorf("failed to set %s=%s: %w", envVar, value, err)
			}
		}
	}

	return nil
}

// LoadConfiguration loads configuration from file and environment variables
func (cl *ConfigurationLoader) LoadConfiguration(configFile string) (*Configuration, error) {
	// Start with defaults
	cl.config = DefaultConfiguration()

	// Load from file if specified
	if configFile != "" {
		if err := cl.LoadFromFile(configFile); err != nil {
			return nil, fmt.Errorf("failed to load configuration from file: %w", err)
		}
	}

	// Override with environment variables
	if err := cl.LoadFromEnvironment(); err != nil {
		return nil, fmt.Errorf("failed to load configuration from environment: %w", err)
	}

	// Validate configuration
	if err := cl.ValidateConfiguration(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cl.config, nil
}

// ValidateConfiguration validates the loaded configuration
func (cl *ConfigurationLoader) ValidateConfiguration() error {
	// Validate controller configuration
	if cl.config.Controller.MaxConcurrentReconciles <= 0 {
		return fmt.Errorf("controller.maxConcurrentReconciles must be positive")
	}
	if cl.config.Controller.ReconcileInterval <= 0 {
		return fmt.Errorf("controller.reconcileInterval must be positive")
	}
	if cl.config.Controller.ReconcileTimeout <= 0 {
		return fmt.Errorf("controller.reconcileTimeout must be positive")
	}

	// Validate webhook configuration
	if cl.config.Webhook.Enabled {
		if cl.config.Webhook.Port <= 0 || cl.config.Webhook.Port > 65535 {
			return fmt.Errorf("webhook.port must be between 1 and 65535")
		}
		if cl.config.Webhook.CertDir == "" {
			return fmt.Errorf("webhook.certDir is required when webhook is enabled")
		}
		if cl.config.Webhook.TimeoutSeconds <= 0 {
			return fmt.Errorf("webhook.timeoutSeconds must be positive")
		}
	}

	// Validate Kubernetes configuration
	if cl.config.Kubernetes.QPS <= 0 {
		return fmt.Errorf("kubernetes.qps must be positive")
	}
	if cl.config.Kubernetes.Burst <= 0 {
		return fmt.Errorf("kubernetes.burst must be positive")
	}

	// Validate leader election configuration
	if cl.config.LeaderElection.Enabled {
		if cl.config.LeaderElection.LeaseDuration <= 0 {
			return fmt.Errorf("leaderElection.leaseDuration must be positive")
		}
		if cl.config.LeaderElection.RenewDeadline <= 0 {
			return fmt.Errorf("leaderElection.renewDeadline must be positive")
		}
		if cl.config.LeaderElection.RetryPeriod <= 0 {
			return fmt.Errorf("leaderElection.retryPeriod must be positive")
		}
	}

	return nil
}

// SaveToFile saves the current configuration to a YAML file
func (cl *ConfigurationLoader) SaveToFile(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil { // #nosec G301 - secure directory permissions
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cl.config)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	// Write to file with secure permissions
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write configuration file: %w", err)
	}

	return nil
}

// Helper functions for setting configuration values from environment variables

func (cl *ConfigurationLoader) setControllerNamespace(value string) error {
	cl.config.Controller.Namespace = value
	return nil
}

func (cl *ConfigurationLoader) setMaxConcurrentReconciles(value string) error {
	val, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	cl.config.Controller.MaxConcurrentReconciles = val
	return nil
}

func (cl *ConfigurationLoader) setReconcileInterval(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.Controller.ReconcileInterval = val
	return nil
}

func (cl *ConfigurationLoader) setReconcileTimeout(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.Controller.ReconcileTimeout = val
	return nil
}

func (cl *ConfigurationLoader) setEnableDeployments(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Controller.EnableDeployments = val
	return nil
}

func (cl *ConfigurationLoader) setEnableStatefulSets(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Controller.EnableStatefulSets = val
	return nil
}

func (cl *ConfigurationLoader) setEnableDaemonSets(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Controller.EnableDaemonSets = val
	return nil
}

func (cl *ConfigurationLoader) setReadOnlyMode(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Controller.ReadOnlyMode = val
	return nil
}

func (cl *ConfigurationLoader) setGracefulShutdownTimeout(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.Controller.GracefulShutdownTimeout = val
	return nil
}

func (cl *ConfigurationLoader) setWebhookEnabled(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Webhook.Enabled = val
	return nil
}

func (cl *ConfigurationLoader) setWebhookPort(value string) error {
	val, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	cl.config.Webhook.Port = val
	return nil
}

func (cl *ConfigurationLoader) setWebhookCertDir(value string) error {
	cl.config.Webhook.CertDir = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookCertName(value string) error {
	cl.config.Webhook.CertName = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookKeyName(value string) error {
	cl.config.Webhook.KeyName = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookServiceName(value string) error {
	cl.config.Webhook.ServiceName = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookServiceNamespace(value string) error {
	cl.config.Webhook.ServiceNamespace = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookTLSMinVersion(value string) error {
	cl.config.Webhook.TLSMinVersion = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookFailurePolicy(value string) error {
	cl.config.Webhook.FailurePolicy = value
	return nil
}

func (cl *ConfigurationLoader) setWebhookTimeoutSeconds(value string) error {
	val, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return err
	}
	cl.config.Webhook.TimeoutSeconds = int32(val)
	return nil
}

func (cl *ConfigurationLoader) setKubeconfig(value string) error {
	cl.config.Kubernetes.Kubeconfig = value
	return nil
}

func (cl *ConfigurationLoader) setKubeContext(value string) error {
	cl.config.Kubernetes.Context = value
	return nil
}

func (cl *ConfigurationLoader) setKubeQPS(value string) error {
	val, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return err
	}
	cl.config.Kubernetes.QPS = float32(val)
	return nil
}

func (cl *ConfigurationLoader) setKubeBurst(value string) error {
	val, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	cl.config.Kubernetes.Burst = val
	return nil
}

func (cl *ConfigurationLoader) setKubeTimeout(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.Kubernetes.Timeout = val
	return nil
}

func (cl *ConfigurationLoader) setServiceAccount(value string) error {
	cl.config.Kubernetes.ServiceAccount = value
	return nil
}

func (cl *ConfigurationLoader) setClusterRole(value string) error {
	cl.config.Kubernetes.ClusterRole = value
	return nil
}

func (cl *ConfigurationLoader) setRoleBinding(value string) error {
	cl.config.Kubernetes.RoleBinding = value
	return nil
}

func (cl *ConfigurationLoader) setLeaderElectionEnabled(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.LeaderElection.Enabled = val
	return nil
}

func (cl *ConfigurationLoader) setLeaderElectionID(value string) error {
	cl.config.LeaderElection.ID = value
	return nil
}

func (cl *ConfigurationLoader) setLeaderElectionLeaseName(value string) error {
	cl.config.LeaderElection.LeaseName = value
	return nil
}

func (cl *ConfigurationLoader) setLeaderElectionLeaseDuration(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.LeaderElection.LeaseDuration = val
	return nil
}

func (cl *ConfigurationLoader) setLeaderElectionRenewDeadline(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.LeaderElection.RenewDeadline = val
	return nil
}

func (cl *ConfigurationLoader) setLeaderElectionRetryPeriod(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.LeaderElection.RetryPeriod = val
	return nil
}

func (cl *ConfigurationLoader) setLogLevel(value string) error {
	cl.config.Logging.Level = value
	return nil
}

func (cl *ConfigurationLoader) setLogDevelopment(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Logging.Development = val
	return nil
}

func (cl *ConfigurationLoader) setEnablePprof(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Logging.EnablePprof = val
	return nil
}

func (cl *ConfigurationLoader) setMetricsBindAddress(value string) error {
	cl.config.Metrics.BindAddress = value
	return nil
}

func (cl *ConfigurationLoader) setMetricsEnableProfiling(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.Metrics.EnableProfiling = val
	return nil
}

func (cl *ConfigurationLoader) setMetricsProfilingAddress(value string) error {
	cl.config.Metrics.ProfilingAddress = value
	return nil
}

func (cl *ConfigurationLoader) setHealthBindAddress(value string) error {
	cl.config.Metrics.HealthBindAddress = value
	return nil
}

func (cl *ConfigurationLoader) setMetricsCollectionInterval(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.Metrics.CollectionInterval = val
	return nil
}

func (cl *ConfigurationLoader) setNodeCacheSize(value string) error {
	val, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	cl.config.NodeClassification.CacheSize = val
	return nil
}

func (cl *ConfigurationLoader) setNodeCacheTTL(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.NodeClassification.CacheTTL = val
	return nil
}

func (cl *ConfigurationLoader) setNodeUpdateInterval(value string) error {
	val, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	cl.config.NodeClassification.UpdateInterval = val
	return nil
}

func (cl *ConfigurationLoader) setAWSEnabled(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.NodeClassification.AWSEnabled = val
	return nil
}

func (cl *ConfigurationLoader) setGCPEnabled(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.NodeClassification.GCPEnabled = val
	return nil
}

func (cl *ConfigurationLoader) setAzureEnabled(value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	cl.config.NodeClassification.AzureEnabled = val
	return nil
}

func (cl *ConfigurationLoader) setWatchNamespaces(value string) error {
	if value == "" {
		cl.config.Namespaces.Watch = []string{}
		return nil
	}
	cl.config.Namespaces.Watch = strings.Split(value, ",")
	for i, ns := range cl.config.Namespaces.Watch {
		cl.config.Namespaces.Watch[i] = strings.TrimSpace(ns)
	}
	return nil
}

func (cl *ConfigurationLoader) setIgnoreNamespaces(value string) error {
	if value == "" {
		cl.config.Namespaces.Ignore = []string{}
		return nil
	}
	cl.config.Namespaces.Ignore = strings.Split(value, ",")
	for i, ns := range cl.config.Namespaces.Ignore {
		cl.config.Namespaces.Ignore[i] = strings.TrimSpace(ns)
	}
	return nil
}
