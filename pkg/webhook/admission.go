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

// Package webhook provides Kubernetes admission webhook functionality
// for mutating pod specifications with node selectors.
package webhook

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// AdmissionConfig contains configuration for admission webhooks
type AdmissionConfig struct {
	// Basic configuration
	Enabled  bool
	Port     int
	CertDir  string
	CertName string
	KeyName  string
	CABundle []byte

	// Service configuration
	ServiceName      string
	ServiceNamespace string
	ServicePath      string

	// Webhook configuration
	WebhookName             string
	FailurePolicy           *admissionv1.FailurePolicyType
	SideEffects             *admissionv1.SideEffectClass
	AdmissionReviewVersions []string
	TimeoutSeconds          *int32

	// TLS configuration
	TLSMinVersion      string
	TLSCipherSuites    []string
	InsecureSkipVerify bool

	// Namespace selector
	NamespaceSelector *metav1.LabelSelector
	ObjectSelector    *metav1.LabelSelector

	// Rules
	Rules []admissionv1.RuleWithOperations
}

// DefaultAdmissionConfig returns default admission webhook configuration
func DefaultAdmissionConfig() *AdmissionConfig {
	failurePolicy := admissionv1.Fail
	sideEffects := admissionv1.SideEffectClassNone
	timeoutSeconds := int32(10)

	return &AdmissionConfig{
		Enabled:                 true,
		Port:                    9443,
		CertDir:                 "/tmp/k8s-webhook-server/serving-certs",
		CertName:                "tls.crt",
		KeyName:                 "tls.key",
		ServiceName:             "spotalis-webhook-service",
		ServiceNamespace:        "spotalis-system",
		ServicePath:             "/mutate",
		WebhookName:             "spotalis-mutating-webhook",
		FailurePolicy:           &failurePolicy,
		SideEffects:             &sideEffects,
		AdmissionReviewVersions: []string{"v1", "v1beta1"},
		TimeoutSeconds:          &timeoutSeconds,
		TLSMinVersion:           "1.3",
		Rules: []admissionv1.RuleWithOperations{
			{
				Operations: []admissionv1.OperationType{
					admissionv1.Create,
					admissionv1.Update,
				},
				Rule: admissionv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
			{
				Operations: []admissionv1.OperationType{
					admissionv1.Create,
					admissionv1.Update,
				},
				Rule: admissionv1.Rule{
					APIGroups:   []string{"apps"},
					APIVersions: []string{"v1"},
					Resources:   []string{"deployments", "statefulsets"},
				},
			},
		},
	}
}

// AdmissionController manages admission webhook configuration and TLS
type AdmissionController struct {
	config     *AdmissionConfig
	kubeClient kubernetes.Interface
	manager    manager.Manager

	// TLS configuration
	tlsConfig *tls.Config

	// Webhook handlers
	mutatingHandler admission.Handler

	// State
	webhookServer webhook.Server
	registered    bool
}

// NewAdmissionController creates a new admission controller
func NewAdmissionController(
	config *AdmissionConfig,
	kubeClient kubernetes.Interface,
	mgr manager.Manager,
	mutatingHandler admission.Handler,
) (*AdmissionController, error) {
	if config == nil {
		config = DefaultAdmissionConfig()
	}

	controller := &AdmissionController{
		config:          config,
		kubeClient:      kubeClient,
		manager:         mgr,
		mutatingHandler: mutatingHandler,
	}

	// Initialize TLS configuration
	if err := controller.initializeTLS(); err != nil {
		return nil, fmt.Errorf("failed to initialize TLS: %w", err)
	}

	// Setup webhook server
	if err := controller.setupWebhookServer(); err != nil {
		return nil, fmt.Errorf("failed to setup webhook server: %w", err)
	}

	return controller, nil
}

// Start starts the admission controller
func (a *AdmissionController) Start(ctx context.Context) error {
	if !a.config.Enabled {
		return nil
	}

	setupLog := ctrl.Log.WithName("admission-controller")
	setupLog.Info("Starting admission controller",
		"port", a.config.Port,
		"cert-dir", a.config.CertDir,
		"webhook-name", a.config.WebhookName,
	)

	// Register webhook configuration
	if err := a.registerWebhookConfiguration(ctx); err != nil {
		return fmt.Errorf("failed to register webhook configuration: %w", err)
	}

	return nil
}

// GetTLSConfig returns the current TLS configuration
func (a *AdmissionController) GetTLSConfig() *tls.Config {
	return a.tlsConfig
}

// GetConfig returns the admission configuration
func (a *AdmissionController) GetConfig() *AdmissionConfig {
	return a.config
}

// IsRegistered returns true if the webhook is registered
func (a *AdmissionController) IsRegistered() bool {
	return a.registered
}

// initializeTLS initializes TLS configuration
func (a *AdmissionController) initializeTLS() error {
	certPath := filepath.Join(a.config.CertDir, a.config.CertName)
	keyPath := filepath.Join(a.config.CertDir, a.config.KeyName)

	// Check if certificates exist
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return fmt.Errorf("certificate file not found: %s", certPath)
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return fmt.Errorf("private key file not found: %s", keyPath)
	}

	// Load certificates
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to load certificate pair: %w", err)
	}

	// Parse certificate to get CA bundle if needed
	if a.config.CABundle == nil {
		if err := a.extractCABundle(certPath); err != nil {
			return fmt.Errorf("failed to extract CA bundle: %w", err)
		}
	}

	// Create TLS configuration
	a.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		// #nosec G402 - InsecureSkipVerify is configurable for development/testing environments
		InsecureSkipVerify:       a.config.InsecureSkipVerify,
		MinVersion:               a.getTLSVersion(),
		CipherSuites:             a.getCipherSuites(),
		PreferServerCipherSuites: true,
	}

	return nil
}

// setupWebhookServer sets up the webhook server
func (a *AdmissionController) setupWebhookServer() error {
	// Get or create webhook server from manager
	webhookServer := a.manager.GetWebhookServer()

	// Configure webhook server
	// Port and cert directory should be set via manager.Options when creating the manager,
	// not directly on the server object.
	// See: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Options

	// Register webhook handlers
	if a.mutatingHandler != nil {
		webhookServer.Register(a.config.ServicePath, &webhook.Admission{
			Handler: a.mutatingHandler,
		})
	}

	a.webhookServer = webhookServer
	return nil
}

// registerWebhookConfiguration creates or updates the MutatingAdmissionWebhook
func (a *AdmissionController) registerWebhookConfiguration(ctx context.Context) error {
	webhook := &admissionv1.MutatingWebhook{
		Name:                    a.config.WebhookName,
		ClientConfig:            a.createClientConfig(),
		Rules:                   a.config.Rules,
		FailurePolicy:           a.config.FailurePolicy,
		SideEffects:             a.config.SideEffects,
		AdmissionReviewVersions: a.config.AdmissionReviewVersions,
		TimeoutSeconds:          a.config.TimeoutSeconds,
		NamespaceSelector:       a.config.NamespaceSelector,
		ObjectSelector:          a.config.ObjectSelector,
	}

	configuration := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: a.config.WebhookName + "-configuration",
			Labels: map[string]string{
				"app.kubernetes.io/name":       "spotalis",
				"app.kubernetes.io/component":  "webhook",
				"app.kubernetes.io/managed-by": "spotalis-controller",
			},
		},
		Webhooks: []admissionv1.MutatingWebhook{*webhook},
	}

	// Try to update existing configuration
	existing, err := a.kubeClient.AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Get(ctx, configuration.Name, metav1.GetOptions{})

	if err == nil {
		// Update existing configuration
		existing.Webhooks = configuration.Webhooks
		_, err = a.kubeClient.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update webhook configuration: %w", err)
		}
	} else {
		// Create new configuration
		_, err = a.kubeClient.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Create(ctx, configuration, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create webhook configuration: %w", err)
		}
	}

	a.registered = true
	return nil
}

// createClientConfig creates the webhook client configuration
func (a *AdmissionController) createClientConfig() admissionv1.WebhookClientConfig {
	path := a.config.ServicePath
	return admissionv1.WebhookClientConfig{
		Service: &admissionv1.ServiceReference{
			Name:      a.config.ServiceName,
			Namespace: a.config.ServiceNamespace,
			Path:      &path,
		},
		CABundle: a.config.CABundle,
	}
}

// extractCABundle extracts CA bundle from certificate file
func (a *AdmissionController) extractCABundle(certPath string) error {
	certPEM, err := os.ReadFile(certPath) // #nosec G304 - certificate path is trusted
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	// For self-signed certificates, use the certificate itself as CA bundle
	// In production, this should be the actual CA certificate
	a.config.CABundle = certPEM
	return nil
}

// getTLSVersion converts TLS version string to constant
func (a *AdmissionController) getTLSVersion() uint16 {
	switch a.config.TLSMinVersion {
	case "1.0":
		return tls.VersionTLS10
	case "1.1":
		return tls.VersionTLS11
	case "1.2":
		return tls.VersionTLS12
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS13
	}
}

// getCipherSuites converts cipher suite names to constants
func (a *AdmissionController) getCipherSuites() []uint16 {
	if len(a.config.TLSCipherSuites) == 0 {
		return nil // Use default cipher suites
	}

	var suites []uint16
	for _, suite := range a.config.TLSCipherSuites {
		switch suite {
		case "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":
			suites = append(suites, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
		case "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":
			suites = append(suites, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384)
		case "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":
			suites = append(suites, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256)
		case "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":
			suites = append(suites, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384)
		}
	}
	return suites
}

// ValidateCertificates validates the webhook certificates
func (a *AdmissionController) ValidateCertificates() error {
	certPath := filepath.Join(a.config.CertDir, a.config.CertName)

	certPEM, err := os.ReadFile(certPath) // #nosec G304 - certificate path is constructed from trusted config
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}

	block, err := x509.ParseCertificate(certPEM)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	if block == nil {
		return fmt.Errorf("failed to parse certificate")
	}

	// Check certificate expiration
	if time.Now().After(block.NotAfter) {
		return fmt.Errorf("certificate has expired")
	}

	// Check if certificate expires soon (within 30 days)
	if time.Now().Add(30 * 24 * time.Hour).After(block.NotAfter) {
		ctrl.Log.WithName("admission-controller").
			Info("Certificate expires soon", "expiry", block.NotAfter)
	}

	return nil
}

// Cleanup removes the webhook configuration from the cluster
func (a *AdmissionController) Cleanup(ctx context.Context) error {
	if !a.registered {
		return nil
	}

	configName := a.config.WebhookName + "-configuration"
	err := a.kubeClient.AdmissionregistrationV1().
		MutatingWebhookConfigurations().
		Delete(ctx, configName, metav1.DeleteOptions{})

	if err != nil {
		return fmt.Errorf("failed to delete webhook configuration: %w", err)
	}

	a.registered = false
	return nil
}
