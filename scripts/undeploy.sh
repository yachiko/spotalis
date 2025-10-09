#!/bin/bash

#
# undeploy.sh - Remove Spotalis from Kubernetes cluster
#
# This script removes all Spotalis resources from the cluster in the correct order
# to ensure clean removal without orphaned resources.
#
# Usage: ./undeploy.sh [--keep-namespace]
#
# Options:
#   --keep-namespace  Do not delete the spotalis-system namespace
#
# Requirements:
# - kubectl (connected to target cluster)
#

set -e

# Configuration
NAMESPACE="spotalis-system"
KEEP_NAMESPACE=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$PROJECT_ROOT/deploy/local"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --keep-namespace)
            KEEP_NAMESPACE=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--keep-namespace]"
            exit 1
            ;;
    esac
done

echo "🗑️  Starting Spotalis undeployment..."
echo "📁 Project root: $PROJECT_ROOT"
echo "📂 Deploy directory: $DEPLOY_DIR"

# Check dependencies
check_dependencies() {
    if ! command -v kubectl &> /dev/null; then
        echo "❌ kubectl is not installed"
        echo "Please install kubectl and try again."
        exit 1
    fi
}

# Check if namespace exists
check_namespace() {
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        echo "ℹ️  Namespace $NAMESPACE does not exist, nothing to undeploy"
        exit 0
    fi
}

# Delete resources in reverse order of deployment
delete_resources() {
    echo "🔄 Deleting Spotalis resources..."
    
    # Delete deployment first to stop the controller
    echo "  ⏹️  Stopping controller deployment..."
    kubectl delete -f "$DEPLOY_DIR/deployment.yaml" --ignore-not-found=true
    
    # Wait for pods to terminate
    echo "  ⏳ Waiting for pods to terminate..."
    kubectl wait --for=delete pod -l app=spotalis-controller -n "$NAMESPACE" --timeout=60s 2>/dev/null || true
    
    # Delete webhook configuration (important: before certificates)
    echo "  🪝 Removing webhook configuration..."
    if kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook &> /dev/null; then
        kubectl delete mutatingwebhookconfiguration spotalis-mutating-webhook --ignore-not-found=true
    fi
    
    # Delete certificate secret
    echo "  🔒 Removing certificate secret..."
    kubectl delete secret spotalis-webhook-certs -n "$NAMESPACE" --ignore-not-found=true
    
    # Delete service
    echo "  🔌 Removing service..."
    kubectl delete -f "$DEPLOY_DIR/service.yaml" --ignore-not-found=true
    
    # Delete RBAC resources
    echo "  🔐 Removing RBAC resources..."
    kubectl delete -f "$DEPLOY_DIR/rbac.yaml" --ignore-not-found=true
    
    # Delete ConfigMap
    echo "  📄 Removing ConfigMap..."
    kubectl delete -f "$DEPLOY_DIR/configmap.yaml" --ignore-not-found=true
    
    # Optionally delete namespace
    if [ "$KEEP_NAMESPACE" = false ]; then
        echo "  📦 Removing namespace..."
        kubectl delete -f "$DEPLOY_DIR/namespace.yaml" --ignore-not-found=true
    else
        echo "  📦 Keeping namespace (--keep-namespace specified)"
    fi
}

# Verify cleanup
verify_cleanup() {
    echo "🔍 Verifying cleanup..."
    
    local resources_remaining=false
    
    # Check for webhook configuration
    if kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook &> /dev/null; then
        echo "  ⚠️  Webhook configuration still exists"
        resources_remaining=true
    fi
    
    # Check for resources in namespace (if namespace still exists)
    if kubectl get namespace "$NAMESPACE" &> /dev/null; then
        local pod_count=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')
        if [ "$pod_count" -gt 0 ]; then
            echo "  ⚠️  $pod_count pod(s) still running in namespace"
            resources_remaining=true
        fi
        
        local deploy_count=$(kubectl get deployments -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l | tr -d ' ')
        if [ "$deploy_count" -gt 0 ]; then
            echo "  ⚠️  $deploy_count deployment(s) still exist in namespace"
            resources_remaining=true
        fi
    fi
    
    if [ "$resources_remaining" = true ]; then
        echo "⚠️  Some resources may still be terminating..."
        echo "Run 'kubectl get all -n $NAMESPACE' to check status"
    else
        echo "✅ All Spotalis resources removed successfully"
    fi
}

# Main execution
main() {
    check_dependencies
    check_namespace
    delete_resources
    verify_cleanup
    
    echo ""
    echo "🎉 Spotalis undeployment completed!"
    echo ""
    
    if [ "$KEEP_NAMESPACE" = false ]; then
        echo "ℹ️  All Spotalis resources have been removed from the cluster."
    else
        echo "ℹ️  All Spotalis resources removed (namespace $NAMESPACE kept)."
        echo "To remove the namespace manually:"
        echo "  kubectl delete namespace $NAMESPACE"
    fi
    echo ""
}

# Run main function
main "$@"
