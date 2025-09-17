#!/bin/bash

#
# setup-kind-integration.sh - Setup Kind cluster for Spotalis integration testing
#
# This script sets up a complete Kind cluster with Spotalis deployed for integration testing
#
# Usage: ./setup-kind-integration.sh
#

set -e

echo "🚀 Setting up Kind integration environment for Spotalis..."

# Check if Kind cluster exists
if ! kind get clusters | grep -q "^spotalis$"; then
    echo "📦 Creating Kind cluster..."
    make kind-create
else
    echo "✅ Kind cluster 'spotalis' already exists"
fi

# Switch to Kind context
echo "🔧 Switching to Kind context..."
kubectl config use-context kind-spotalis

# Build and load image
echo "🏗️  Building and loading Spotalis image..."
make docker-build
make kind-load

# Generate certificates
echo "🔐 Generating webhook certificates..."
make generate-certs

# Deploy Spotalis
echo "🚀 Deploying Spotalis to Kind cluster..."
kubectl apply -f build/k8s/

# Wait for deployment to be ready
echo "⏳ Waiting for Spotalis controller to be ready..."
kubectl wait --for=condition=available --timeout=300s deployment/spotalis-controller -n spotalis-system

# Verify deployment
echo "✅ Verifying Spotalis deployment..."
kubectl get pods -n spotalis-system
kubectl get services -n spotalis-system

echo ""
echo "🎉 Kind integration environment is ready!"
echo ""
echo "Next steps:"
echo "1. Run integration tests: make test-integration-kind"
echo "2. Check logs: kubectl logs -n spotalis-system deployment/spotalis-controller"
echo "3. Port forward for debugging: kubectl port-forward -n spotalis-system svc/spotalis-metrics 8080:8080"
echo ""
echo "To clean up: make kind-delete"
