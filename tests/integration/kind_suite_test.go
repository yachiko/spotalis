//go:build integration

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

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ahoma/spotalis/pkg/logging"
	"github.com/ahoma/spotalis/tests/integration/shared"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	k8sClient client.Client
	clientset *kubernetes.Clientset
	cfg       *rest.Config
	ctx       context.Context
	cancel    context.CancelFunc
)

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	By("Setting up logger for integration tests")

	// Create logger configuration for integration tests
	// Use debug level to see controller reconciliation details
	loggerConfig := &logging.Config{
		Level:  "debug", // Use debug to see detailed controller logs
		Format: "json",
	}

	logger, err := logging.NewLogger(loggerConfig)
	Expect(err).NotTo(HaveOccurred())

	// Setup controller-runtime to use our logger (both ctrl and root log packages)
	ctrl.SetLogger(logger.Logger)
	// Explicitly set the root delegating logger to avoid the
	// "log.SetLogger(...) was never called" warning when the
	// client issues requests before other suites run.
	crlog.SetLogger(logger.Logger)

	// Configure klog to use the same structured logger
	// This ensures all Kubernetes client logs use structured format
	klog.SetLogger(logger.Logger)

	setupLog := logger.WithName("integration-test-setup")
	setupLog.Info("Integration test logger initialized",
		"level", loggerConfig.Level,
		"format", loggerConfig.Format)

	By("Configuring test timeouts for fail-fast behavior")
	// Set default timeout for all Eventually/Consistently calls to 2 minutes
	// This ensures tests fail fast rather than hanging indefinitely
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)
	SetDefaultConsistentlyDuration(30 * time.Second)
	SetDefaultConsistentlyPollingInterval(1 * time.Second)
	setupLog.Info("Test timeouts configured",
		"eventuallyTimeout", "2m",
		"eventuallyPollingInterval", "2s",
		"consistentlyDuration", "30s",
		"consistentlyPollingInterval", "1s")
	setupLog.Info("Integration test logger initialized",
		"level", loggerConfig.Level,
		"format", loggerConfig.Format)

	By("Setting up Kubernetes client for Kind cluster")

	// Load kubeconfig - try Kind context first, then default
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		homeDir, err := os.UserHomeDir()
		Expect(err).NotTo(HaveOccurred())
		kubeconfig = filepath.Join(homeDir, ".kube", "config")
	}

	// Load the kubeconfig
	config, err := clientcmd.LoadFromFile(kubeconfig)
	Expect(err).NotTo(HaveOccurred())

	// Try to use kind-spotalis context if available
	contextName := os.Getenv("KUBE_CONTEXT")
	if contextName == "" {
		contextName = "kind-spotalis"
	}

	// Check if the context exists
	if _, exists := config.Contexts[contextName]; !exists {
		// Fall back to current context
		contextName = config.CurrentContext
		GinkgoWriter.Printf("Kind context 'kind-spotalis' not found, using current context: %s\n", contextName)
	} else {
		GinkgoWriter.Printf("Using Kind context: %s\n", contextName)
	}

	// Build the rest config with the specific context
	cfg, err = clientcmd.NewNonInteractiveClientConfig(
		*config,
		contextName,
		&clientcmd.ConfigOverrides{},
		nil,
	).ClientConfig()
	Expect(err).NotTo(HaveOccurred())

	// Create controller-runtime client
	k8sClient, err = client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())

	// Create clientset for direct API access
	clientset, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("Verifying Spotalis controller is running")
	Eventually(func() error {
		// Check if the Spotalis controller deployment is ready
		return verifySpotalisDeployment()
	}, 2*time.Minute, 10*time.Second).Should(Succeed())

	GinkgoWriter.Println("Integration test setup complete - connected to Kind cluster with Spotalis")
})

var _ = AfterSuite(func() {
	By("Cleaning up test resources")
	cleanupTestResources()

	if cancel != nil {
		cancel()
	}
})

func verifySpotalisDeployment() error {
	// Check if Spotalis deployment exists and is ready
	deployment := &appsv1.Deployment{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: "spotalis-system",
		Name:      "spotalis-controller",
	}, deployment)
	if err != nil {
		return err
	}

	if deployment.Status.ReadyReplicas < 1 {
		return fmt.Errorf("spotalis controller not ready: %d/%d replicas ready",
			deployment.Status.ReadyReplicas, deployment.Status.Replicas)
	}

	return nil
}

func cleanupTestResources() {
	// Clean up any test resources created during integration tests
	// This helps ensure tests don't interfere with each other

	// Create a helper for cleanup operations
	kindHelper, err := shared.NewKindClusterHelper(ctx)
	if err != nil {
		// Use a different approach - fail the suite if we can't create helper
		fmt.Printf("CRITICAL: Failed to create kind helper for cleanup: %v\n", err)
		// Continue with manual cleanup attempt
		manualCleanup()
		return
	}

	// Perform comprehensive cleanup
	if err := kindHelper.CleanupAllTestResources(); err != nil {
		// Don't fail the suite, but log the error and attempt manual cleanup
		fmt.Printf("WARNING: Failed to cleanup test resources: %v\n", err)
		fmt.Println("Attempting manual cleanup...")
		manualCleanup()
	}

	GinkgoWriter.Println("Test resource cleanup completed")
}

func manualCleanup() {
	// Manual cleanup using direct kubectl-like operations
	namespaces := &corev1.NamespaceList{}
	if err := k8sClient.List(ctx, namespaces, client.MatchingLabels{
		"spotalis.io/test": "true",
	}); err == nil {
		for _, ns := range namespaces.Items {
			if err := k8sClient.Delete(ctx, &ns); err != nil {
				fmt.Printf("Failed to delete test namespace %s: %v\n", ns.Name, err)
			}
		}
	}
}
