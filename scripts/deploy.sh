#!/bin/bash

#
# deploy.sh - Deploy Spotalis with templated certificates
#
# This script generates certificates and applies templated Kubernetes manifests
# for the Spotalis operator deployment.
#
# Usage: ./deploy.sh
#
# Requirements:
# - kubectl (connected to target cluster)
# - openssl
# - sed or envsubst
#

set -e

# Configuration
NAMESPACE="spotalis-system"
SERVICE_NAME="spotalis-webhook" 
SECRET_NAME="spotalis-webhook-certs"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$PROJECT_ROOT/deploy/local"
TEMP_DIR=$(mktemp -d)

echo "🚀 Starting Spotalis deployment..."
echo "📁 Project root: $PROJECT_ROOT"
echo "📂 Deploy directory: $DEPLOY_DIR"
echo "🔒 Temporary directory: $TEMP_DIR"

# Cleanup function
cleanup() {
    echo "🧹 Cleaning up temporary files..."
    rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

# Check dependencies
check_dependencies() {
    local missing_deps=()
    
    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi
    
    if ! command -v openssl &> /dev/null; then
        missing_deps+=("openssl")
    fi
    
    if ! command -v sed &> /dev/null; then
        missing_deps+=("sed")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        echo "❌ Missing required dependencies: ${missing_deps[*]}"
        echo "Please install the missing tools and try again."
        exit 1
    fi
}

# Generate TLS certificates
generate_certificates() {
    echo "🔐 Generating TLS certificates..."
    
    # Generate CA private key
    openssl genrsa -out "$TEMP_DIR/ca.key" 2048
    
    # Generate CA certificate
    openssl req -new -x509 -days 365 -key "$TEMP_DIR/ca.key" -out "$TEMP_DIR/ca.crt" \
        -subj "/CN=spotalis-controller-ca"
    
    # Generate server private key
    openssl genrsa -out "$TEMP_DIR/server.key" 2048
    
    # Create certificate signing request config
    cat <<EOF > "$TEMP_DIR/server.conf"
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE_NAME}
DNS.2 = ${SERVICE_NAME}.${NAMESPACE}
DNS.3 = ${SERVICE_NAME}.${NAMESPACE}.svc
DNS.4 = ${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local
EOF
    
    # Generate certificate signing request
    openssl req -new -key "$TEMP_DIR/server.key" -out "$TEMP_DIR/server.csr" \
        -subj "/CN=${SERVICE_NAME}.${NAMESPACE}.svc" \
        -config "$TEMP_DIR/server.conf"
    
    # Generate server certificate
    openssl x509 -req -in "$TEMP_DIR/server.csr" -CA "$TEMP_DIR/ca.crt" -CAkey "$TEMP_DIR/ca.key" \
        -CAcreateserial -out "$TEMP_DIR/server.crt" -days 365 \
        -extensions v3_req -extfile "$TEMP_DIR/server.conf"
    
    echo "✅ Certificates generated successfully"
}

# Apply templated manifests (with certificates)
apply_templates() {
    echo "📝 Processing templated manifests..."
    
    # Encode certificates in base64
    local tls_crt_base64=$(cat "$TEMP_DIR/server.crt" | base64 | tr -d '\n')
    local tls_key_base64=$(cat "$TEMP_DIR/server.key" | base64 | tr -d '\n')
    local ca_bundle_base64=$(cat "$TEMP_DIR/ca.crt" | base64 | tr -d '\n')
    
    # Process certs.yaml template
    echo "🔒 Creating certificate secret..."
    sed "s/__TLS_CRT_BASE64__/$tls_crt_base64/g; s/__TLS_KEY_BASE64__/$tls_key_base64/g" \
        "$DEPLOY_DIR/certs.yaml.template" > "$TEMP_DIR/certs.yaml"
    
    # Process webhook.yaml template  
    echo "🪝 Creating webhook configuration..."
    sed "s/__CA_BUNDLE_BASE64__/$ca_bundle_base64/g" \
        "$DEPLOY_DIR/webhook.yaml.template" > "$TEMP_DIR/webhook.yaml"
    
    # Apply all manifests in order
    echo "📦 Applying Kubernetes manifests..."
    kubectl apply -f "$DEPLOY_DIR/namespace.yaml"
    kubectl apply -f "$DEPLOY_DIR/configmap.yaml"
    kubectl apply -f "$DEPLOY_DIR/rbac.yaml"
    kubectl apply -f "$DEPLOY_DIR/service.yaml"
    kubectl apply -f "$TEMP_DIR/certs.yaml"
    kubectl apply -f "$TEMP_DIR/webhook.yaml"
    kubectl apply -f "$DEPLOY_DIR/deployment.yaml"
}

# Apply non-certificate manifests (when certs already exist)
apply_non_cert_manifests() {
    echo "📦 Applying non-certificate Kubernetes manifests..."
    kubectl apply -f "$DEPLOY_DIR/namespace.yaml"
    kubectl apply -f "$DEPLOY_DIR/configmap.yaml"
    kubectl apply -f "$DEPLOY_DIR/rbac.yaml"
    kubectl apply -f "$DEPLOY_DIR/service.yaml"
    kubectl apply -f "$DEPLOY_DIR/deployment.yaml"
    echo "ℹ️  Skipped certificate secret and webhook configuration (already exist)"
}

# Wait for deployment to be ready
wait_for_deployment() {
    echo "⏳ Waiting for deployment to be ready..."
    kubectl wait --for=condition=available --timeout=300s deployment/spotalis-controller -n "$NAMESPACE"
}

# Main execution
main() {
    check_dependencies
    
    # Check if certificates already exist
    local certs_exist=false
    if kubectl get secret "$SECRET_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
        echo "ℹ️  Certificate secret already exists, skipping certificate generation..."
        certs_exist=true
    fi
    
    if [ "$certs_exist" = false ]; then
        generate_certificates
        apply_templates
    else
        # Apply only non-certificate manifests
        apply_non_cert_manifests
    fi
    
    wait_for_deployment
    
    echo ""
    echo "🎉 Spotalis deployment completed successfully!"
    echo ""
    echo "📊 To check the status:"
    echo "  kubectl get pods -n $NAMESPACE"
    echo "  kubectl logs -n $NAMESPACE -l app=spotalis-controller"
    echo ""
    echo "🔍 To verify webhook is working:"
    echo "  kubectl get mutatingwebhookconfiguration spotalis-mutating-webhook"
}

# Run main function
main "$@"