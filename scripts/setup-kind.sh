#!/bin/bash

#
# setup-kind.sh - Setup Kind cluster for Spotalis development
#
# This script sets up a Kind cluster ready for Spotalis deployment
#
# Usage: ./setup-kind.sh
#

set -e

echo "🚀 Setting up Kind cluster for Spotalis..."

# Check dependencies
check_dependencies() {
    local missing_deps=()
    
    if ! command -v kind &> /dev/null; then
        missing_deps+=("kind")
    fi
    
    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi
    
    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        echo "❌ Missing required dependencies: ${missing_deps[*]}"
        echo "Please install the missing tools and try again."
        exit 1
    fi
}

# Check if Kind cluster exists
cluster_exists() {
    kind get clusters 2>/dev/null | grep -q "^spotalis$"
}

# Create Kind cluster
create_cluster() {
    echo "📦 Creating Kind cluster 'spotalis'..."
    
    # Check if cluster config exists
    local cluster_config="build/kind/cluster.yaml"
    if [ ! -f "$cluster_config" ]; then
        echo "❌ Cluster configuration not found at $cluster_config"
        exit 1
    fi
    
    kind create cluster --name spotalis --config "$cluster_config"
    echo "✅ Kind cluster created successfully"
}

# Switch to Kind context
switch_context() {
    echo "🔧 Switching to Kind context..."
    kubectl config use-context kind-spotalis
    
    # Verify context switch
    local current_context=$(kubectl config current-context)
    if [ "$current_context" = "kind-spotalis" ]; then
        echo "✅ Successfully switched to kind-spotalis context"
    else
        echo "❌ Failed to switch to kind-spotalis context (current: $current_context)"
        exit 1
    fi
}

# Verify cluster is ready
verify_cluster() {
    echo "🔍 Verifying cluster is ready..."
    kubectl cluster-info --context kind-spotalis > /dev/null
    kubectl get nodes --context kind-spotalis > /dev/null
    echo "✅ Cluster is ready and accessible"
}

# Main execution
main() {
    check_dependencies
    
    if cluster_exists; then
        echo "ℹ️  Kind cluster 'spotalis' already exists"
        switch_context
        verify_cluster
    else
        create_cluster
        switch_context
        verify_cluster
    fi
    
    echo ""
    echo "🎉 Kind cluster is ready for development!"
    echo ""
    echo "Next steps:"
    echo "  1. Build and deploy Spotalis: make deploy"
    echo "  2. Run integration tests: make test-integration"
    echo "  3. Check cluster status: kubectl get nodes"
    echo ""
    echo "To clean up: make kind-delete"
}

# Run main function
main "$@"
