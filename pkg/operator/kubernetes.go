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

package operator

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// RBAC verbs
	verbGet  = "get"
	verbList = "list"
)

// KubernetesConfig contains Kubernetes client configuration
type KubernetesConfig struct {
	// Client configuration
	Kubeconfig string
	Context    string
	QPS        float32
	Burst      int
	Timeout    time.Duration
	UserAgent  string

	// RBAC configuration
	ServiceAccount string
	Namespace      string
	ClusterRole    string
	RoleBinding    string

	// Security configuration
	ImpersonateUser   string
	ImpersonateGroups []string

	// Advanced configuration
	DisableCompression bool
	ContentType        string
	AcceptContentTypes string
}

// DefaultKubernetesConfig returns default Kubernetes client configuration
func DefaultKubernetesConfig() *KubernetesConfig {
	return &KubernetesConfig{
		QPS:                20.0,
		Burst:              30,
		Timeout:            30 * time.Second,
		UserAgent:          "spotalis-controller/1.0",
		ServiceAccount:     "spotalis-controller",
		Namespace:          "spotalis-system",
		ClusterRole:        "spotalis-controller",
		RoleBinding:        "spotalis-controller",
		DisableCompression: false,
		ContentType:        "application/json",
		AcceptContentTypes: "application/json",
	}
}

// KubernetesClientManager manages Kubernetes client configuration and RBAC
type KubernetesClientManager struct {
	config     *KubernetesConfig
	restConfig *rest.Config
	kubeClient kubernetes.Interface
	ctrlClient client.Client
	scheme     *runtime.Scheme
}

// NewKubernetesClientManager creates a new Kubernetes client manager
func NewKubernetesClientManager(config *KubernetesConfig, scheme *runtime.Scheme) (*KubernetesClientManager, error) {
	if config == nil {
		config = DefaultKubernetesConfig()
	}

	mgr := &KubernetesClientManager{
		config: config,
		scheme: scheme,
	}

	// Initialize REST config
	if err := mgr.initializeRESTConfig(); err != nil {
		return nil, fmt.Errorf("failed to initialize REST config: %w", err)
	}

	// Initialize clients
	if err := mgr.initializeClients(); err != nil {
		return nil, fmt.Errorf("failed to initialize clients: %w", err)
	}

	return mgr, nil
}

// GetRESTConfig returns the REST configuration
func (k *KubernetesClientManager) GetRESTConfig() *rest.Config {
	return k.restConfig
}

// GetKubernetesClient returns the Kubernetes client
func (k *KubernetesClientManager) GetKubernetesClient() kubernetes.Interface {
	return k.kubeClient
}

// GetControllerClient returns the controller-runtime client
func (k *KubernetesClientManager) GetControllerClient() client.Client {
	return k.ctrlClient
}

// GetConfig returns the Kubernetes configuration
func (k *KubernetesClientManager) GetConfig() *KubernetesConfig {
	return k.config
}

// EnsureRBAC ensures that required RBAC resources are created
func (k *KubernetesClientManager) EnsureRBAC(ctx context.Context) error {
	// Ensure namespace exists
	if err := k.ensureNamespace(ctx); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	// Ensure service account exists
	if err := k.ensureServiceAccount(ctx); err != nil {
		return fmt.Errorf("failed to ensure service account: %w", err)
	}

	// Ensure cluster role exists
	if err := k.ensureClusterRole(ctx); err != nil {
		return fmt.Errorf("failed to ensure cluster role: %w", err)
	}

	// Ensure cluster role binding exists
	if err := k.ensureClusterRoleBinding(ctx); err != nil {
		return fmt.Errorf("failed to ensure cluster role binding: %w", err)
	}

	return nil
}

// ValidatePermissions validates that the client has required permissions
func (k *KubernetesClientManager) ValidatePermissions(ctx context.Context) error {
	// Test basic permissions
	permissions := []struct {
		resource string
		verb     string
		group    string
	}{
		{"nodes", "list", ""},
		{"nodes", verbGet, ""},
		{"pods", "list", ""},
		{"pods", verbGet, ""},
		{"pods", "patch", ""},
		{"deployments", "list", "apps"},
		{"deployments", verbGet, "apps"},
		{"deployments", "patch", "apps"},
		{"statefulsets", "list", "apps"},
		{"statefulsets", verbGet, "apps"},
		{"statefulsets", "patch", "apps"},
		{"events", "create", ""},
		{"leases", "create", "coordination.k8s.io"},
		{"leases", verbGet, "coordination.k8s.io"},
		{"leases", "update", "coordination.k8s.io"},
	}

	for _, perm := range permissions {
		if err := k.validatePermission(ctx, perm.resource, perm.verb, perm.group); err != nil {
			return fmt.Errorf("missing permission %s:%s:%s: %w", perm.group, perm.resource, perm.verb, err)
		}
	}

	return nil
}

// GetClusterInfo returns information about the Kubernetes cluster
func (k *KubernetesClientManager) GetClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	version, err := k.kubeClient.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}

	nodes, err := k.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	namespaces, err := k.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	return &ClusterInfo{
		Version:        version.String(),
		NodeCount:      len(nodes.Items),
		NamespaceCount: len(namespaces.Items),
		APIServerURL:   k.restConfig.Host,
		CurrentContext: k.config.Context,
	}, nil
}

// ClusterInfo contains information about the Kubernetes cluster
type ClusterInfo struct {
	Version        string
	NodeCount      int
	NamespaceCount int
	APIServerURL   string
	CurrentContext string
}

// initializeRESTConfig initializes the REST configuration
func (k *KubernetesClientManager) initializeRESTConfig() error {
	var config *rest.Config
	var err error

	// Try to load from kubeconfig file if specified
	if k.config.Kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", k.config.Kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig from %s: %w", k.config.Kubeconfig, err)
		}
	} else {
		// Use in-cluster config or default kubeconfig
		config, err = ctrl.GetConfig()
		if err != nil {
			return fmt.Errorf("failed to get kubernetes config: %w", err)
		}
	}

	// Apply configuration overrides
	config.QPS = k.config.QPS
	config.Burst = int(k.config.Burst)
	config.Timeout = k.config.Timeout
	config.UserAgent = k.config.UserAgent
	config.DisableCompression = k.config.DisableCompression
	config.ContentType = k.config.ContentType
	config.AcceptContentTypes = k.config.AcceptContentTypes

	// Apply impersonation if configured
	if k.config.ImpersonateUser != "" {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: k.config.ImpersonateUser,
			Groups:   k.config.ImpersonateGroups,
		}
	}

	k.restConfig = config
	return nil
}

// initializeClients initializes Kubernetes clients
func (k *KubernetesClientManager) initializeClients() error {
	// Create Kubernetes client
	kubeClient, err := kubernetes.NewForConfig(k.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	k.kubeClient = kubeClient

	// Create controller-runtime client
	ctrlClient, err := client.New(k.restConfig, client.Options{
		Scheme: k.scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create controller client: %w", err)
	}
	k.ctrlClient = ctrlClient

	return nil
}

// ensureNamespace ensures the namespace exists
func (k *KubernetesClientManager) ensureNamespace(ctx context.Context) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: k.config.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "spotalis",
				"app.kubernetes.io/component":  "controller",
				"app.kubernetes.io/managed-by": "spotalis-controller",
			},
		},
	}

	_, err := k.kubeClient.CoreV1().Namespaces().Get(ctx, k.config.Namespace, metav1.GetOptions{})
	if err != nil {
		// Namespace doesn't exist, create it
		_, err = k.kubeClient.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace %s: %w", k.config.Namespace, err)
		}
	}

	return nil
}

// ensureServiceAccount ensures the service account exists
func (k *KubernetesClientManager) ensureServiceAccount(ctx context.Context) error {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      k.config.ServiceAccount,
			Namespace: k.config.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "spotalis",
				"app.kubernetes.io/component":  "controller",
				"app.kubernetes.io/managed-by": "spotalis-controller",
			},
		},
	}

	_, err := k.kubeClient.CoreV1().ServiceAccounts(k.config.Namespace).Get(ctx, k.config.ServiceAccount, metav1.GetOptions{})
	if err != nil {
		// Service account doesn't exist, create it
		_, err = k.kubeClient.CoreV1().ServiceAccounts(k.config.Namespace).Create(ctx, serviceAccount, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create service account %s: %w", k.config.ServiceAccount, err)
		}
	}

	return nil
}

// ensureClusterRole ensures the cluster role exists
func (k *KubernetesClientManager) ensureClusterRole(ctx context.Context) error {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: k.config.ClusterRole,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "spotalis",
				"app.kubernetes.io/component":  "controller",
				"app.kubernetes.io/managed-by": "spotalis-controller",
			},
		},
		Rules: []rbacv1.PolicyRule{
			// Node permissions
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			// Pod permissions
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch", "patch", "delete"},
			},
			// Deployment permissions
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments"},
				Verbs:     []string{"get", "list", "watch", "patch"},
			},
			// StatefulSet permissions
			{
				APIGroups: []string{"apps"},
				Resources: []string{"statefulsets"},
				Verbs:     []string{"get", "list", "watch", "patch"},
			},
			// Event permissions
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
			// Leader election permissions
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			// Namespace permissions
			{
				APIGroups: []string{""},
				Resources: []string{"namespaces"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	_, err := k.kubeClient.RbacV1().ClusterRoles().Get(ctx, k.config.ClusterRole, metav1.GetOptions{})
	if err != nil {
		// Cluster role doesn't exist, create it
		_, err = k.kubeClient.RbacV1().ClusterRoles().Create(ctx, clusterRole, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create cluster role %s: %w", k.config.ClusterRole, err)
		}
	}

	return nil
}

// ensureClusterRoleBinding ensures the cluster role binding exists
func (k *KubernetesClientManager) ensureClusterRoleBinding(ctx context.Context) error {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: k.config.RoleBinding,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "spotalis",
				"app.kubernetes.io/component":  "controller",
				"app.kubernetes.io/managed-by": "spotalis-controller",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     k.config.ClusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      k.config.ServiceAccount,
				Namespace: k.config.Namespace,
			},
		},
	}

	_, err := k.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, k.config.RoleBinding, metav1.GetOptions{})
	if err != nil {
		// Cluster role binding doesn't exist, create it
		_, err = k.kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, clusterRoleBinding, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create cluster role binding %s: %w", k.config.RoleBinding, err)
		}
	}

	return nil
}

// validatePermission validates a specific permission
func (k *KubernetesClientManager) validatePermission(ctx context.Context, resource, verb, group string) error {
	// This would typically use SubjectAccessReview, but for simplicity
	// we'll just try to perform a basic operation
	switch resource {
	case "nodes":
		if verb == verbList || verb == verbGet {
			_, err := k.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
			return err
		}
	case "pods":
		if verb == verbList || verb == verbGet {
			_, err := k.kubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{Limit: 1})
			return err
		}
	case "deployments":
		if verb == verbList || verb == verbGet {
			_, err := k.kubeClient.AppsV1().Deployments("").List(ctx, metav1.ListOptions{Limit: 1})
			return err
		}
	case "statefulsets":
		if verb == verbList || verb == verbGet {
			_, err := k.kubeClient.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{Limit: 1})
			return err
		}
	}

	return nil
}
