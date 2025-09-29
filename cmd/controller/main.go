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

// Package main provides the entry point for the Spotalis Kubernetes controller.
// It initializes and runs the operator that manages workload replica distribution
// across spot and on-demand instances based on annotation-driven configuration.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ahoma/spotalis/pkg/di"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	// Build-time variables
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	var (
		configFile  = flag.String("config", "", "Path to configuration file. If not specified, uses environment variables and defaults.")
		showVersion = flag.Bool("version", false, "Show version information and exit.")
	)

	flag.Parse()

	// Show version information
	if *showVersion {
		fmt.Printf("Spotalis Controller\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Build Date: %s\n", buildDate)
		return
	}

	// Create context for application lifecycle
	ctx := context.Background()

	// Create application using DI system
	var app *di.Application
	var err error

	if *configFile != "" {
		app, err = di.NewApplicationWithConfig(ctx, *configFile)
	} else {
		app, err = di.NewApplication(ctx)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create application: %v\n", err)
		return
	}

	// Setup logger based on configuration
	config := app.GetConfig()
	opts := zap.Options{
		Development: config.Observability.Logging.Level == "debug",
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)

	setupLog := logger.WithName("setup")
	setupLog.Info("Starting Spotalis controller",
		"version", version,
		"commit", commit,
		"buildDate", buildDate,
		"namespace", config.Operator.Namespace,
		"metrics-addr", config.Observability.Metrics.BindAddress,
		"health-addr", config.Observability.Health.BindAddress,
		"webhook-port", config.Webhook.Port,
		"leader-election", config.Operator.LeaderElection.Enabled,
		"webhook-enabled", config.Webhook.Enabled,
		"read-only", config.Operator.ReadOnlyMode,
		"log-level", config.Observability.Logging.Level,
	)

	// Setup signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start the application
	setupLog.Info("Starting operator")
	if err := app.Start(ctx); err != nil {
		setupLog.Error(err, "failed to start operator")
		return
	}

	setupLog.Info("Operator stopped")
}
