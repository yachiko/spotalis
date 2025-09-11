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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/spotalis/spotalis/pkg/operator"
)

var (
	// Build-time variables
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	var (
		metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
		probeAddr            = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
		webhookAddr          = flag.String("webhook-bind-address", ":9443", "The address the webhook endpoint binds to.")
		enableLeaderElection = flag.Bool("leader-elect", true, "Enable leader election for controller manager.")
		leaderElectionID     = flag.String("leader-election-id", "spotalis-controller-leader", "The name of the leader election configmap.")
		namespace            = flag.String("namespace", "spotalis-system", "The namespace to run the controller in.")
		logLevel             = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		enableWebhook        = flag.Bool("enable-webhook", true, "Enable admission webhook.")
		readOnlyMode         = flag.Bool("read-only", false, "Run in read-only mode (no mutations).")
		enablePprof          = flag.Bool("enable-pprof", false, "Enable pprof endpoints for debugging.")
		reconcileInterval    = flag.Duration("reconcile-interval", 30*time.Second, "Interval between reconciliation loops.")
		maxConcurrent        = flag.Int("max-concurrent-reconciles", 10, "Maximum number of concurrent reconciles.")
		webhookCertDir       = flag.String("webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory containing webhook certificates.")
		webhookCertName      = flag.String("webhook-cert-name", "tls.crt", "Name of the webhook certificate file.")
		webhookKeyName       = flag.String("webhook-key-name", "tls.key", "Name of the webhook private key file.")
		webhookPort          = flag.Int("webhook-port", 9443, "Port for the webhook server.")
		apiQPSLimit          = flag.Float64("api-qps-limit", 20.0, "QPS limit for Kubernetes API calls.")
		apiBurstLimit        = flag.Int("api-burst-limit", 30, "Burst limit for Kubernetes API calls.")
		showVersion          = flag.Bool("version", false, "Show version information and exit.")
	)

	flag.Parse()

	// Show version information
	if *showVersion {
		fmt.Printf("Spotalis Controller\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Build Date: %s\n", buildDate)
		os.Exit(0)
	}

	// Setup logger
	opts := zap.Options{
		Development: *logLevel == "debug",
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	setupLog := logger.WithName("setup")
	setupLog.Info("Starting Spotalis controller",
		"version", version,
		"commit", commit,
		"buildDate", buildDate,
		"namespace", *namespace,
		"metrics-addr", *metricsAddr,
		"probe-addr", *probeAddr,
		"webhook-addr", *webhookAddr,
		"leader-election", *enableLeaderElection,
		"webhook-enabled", *enableWebhook,
		"read-only", *readOnlyMode,
		"log-level", *logLevel,
	)

	// Create operator configuration
	config := &operator.OperatorConfig{
		MetricsAddr:             *metricsAddr,
		ProbeAddr:               *probeAddr,
		WebhookAddr:             *webhookAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionID:        *leaderElectionID,
		Namespace:               *namespace,
		ReconcileInterval:       *reconcileInterval,
		MaxConcurrentReconciles: *maxConcurrent,
		WebhookCertDir:          *webhookCertDir,
		WebhookCertName:         *webhookCertName,
		WebhookKeyName:          *webhookKeyName,
		WebhookPort:             *webhookPort,
		LogLevel:                *logLevel,
		EnablePprof:             *enablePprof,
		EnableWebhook:           *enableWebhook,
		ReadOnlyMode:            *readOnlyMode,
		APIQPSLimit:             float32(*apiQPSLimit),
		APIBurstLimit:           *apiBurstLimit,
	}

	// Create operator
	op, err := operator.NewOperator(config)
	if err != nil {
		setupLog.Error(err, "failed to create operator")
		os.Exit(1)
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start the operator
	setupLog.Info("Starting operator")
	if err := op.Start(ctx); err != nil {
		setupLog.Error(err, "failed to start operator")
		os.Exit(1)
	}

	setupLog.Info("Operator stopped")
}
