#!/bin/bash

#
# generate-certs.sh - Generate TLS certificates for Spotalis webhook
#
# This script generates self-signed certificates for the Spotalis admission webhook
# and creates a Kubernetes secret containing the certificates.
#
# Usage: ./generate-certs.sh
#
# Requirements:
# - kubectl (connected to target cluster)
# - openssl
#

set -e

# Configuration
NAMESPACE="spotalis-system"
SERVICE_NAME="spotalis-webhook" 
SECRET_NAME="spotalis-webhook-certs"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "kubectl could not be found. Please install kubectl first."
    exit 1
fi

# Check if openssl is available
if ! command -v openssl &> /dev/null; then
    echo "openssl could not be found. Please install openssl first."
    exit 1
fi

# Create a temporary directory for certificates
CERT_DIR=$(mktemp -d)
echo "Creating certificates in temporary directory: $CERT_DIR"

# Generate CA private key
openssl genrsa -out $CERT_DIR/ca.key 2048

# Generate CA certificate
openssl req -new -x509 -days 365 -key $CERT_DIR/ca.key -out $CERT_DIR/ca.crt -subj "/CN=spotalis-controller-ca"

# Generate server private key
openssl genrsa -out $CERT_DIR/server.key 2048

# Create certificate signing request
cat <<EOF > $CERT_DIR/server.conf
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
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
openssl req -new -key $CERT_DIR/server.key -out $CERT_DIR/server.csr -config $CERT_DIR/server.conf -subj "/CN=${SERVICE_NAME}.${NAMESPACE}.svc"

# Generate server certificate signed by CA
openssl x509 -req -in $CERT_DIR/server.csr -CA $CERT_DIR/ca.crt -CAkey $CERT_DIR/ca.key -CAcreateserial -out $CERT_DIR/server.crt -days 365 -extensions v3_req -extfile $CERT_DIR/server.conf

# Create namespace if it doesn't exist
kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -

# Create Kubernetes secret with certificates
kubectl create secret generic $SECRET_NAME \
    --from-file=tls.crt=$CERT_DIR/server.crt \
    --from-file=tls.key=$CERT_DIR/server.key \
    --namespace=$NAMESPACE \
    --dry-run=client -o yaml | kubectl apply -f -

# Get CA bundle for webhook configuration
CA_BUNDLE=$(cat $CERT_DIR/ca.crt | base64 | tr -d '\n')

# Get certificate and key for secret
SERVER_CERT=$(cat $CERT_DIR/server.crt | base64 | tr -d '\n')
SERVER_KEY=$(cat $CERT_DIR/server.key | base64 | tr -d '\n')

# Update webhook configurations with CA bundle
if [ -f "build/k8s/webhook.yaml" ]; then
    sed -i.bak "s/caBundle:.*/caBundle: $CA_BUNDLE/" build/k8s/webhook.yaml
    echo "CA Bundle has been updated in build/k8s/webhook.yaml"
else
    echo "Warning: build/k8s/webhook.yaml not found. You'll need to manually update the caBundle."
fi

# Create/update secret.yaml with actual certificate data
cat <<EOF > build/k8s/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: $SECRET_NAME
  namespace: $NAMESPACE
  labels:
    app.kubernetes.io/name: spotalis
    app.kubernetes.io/instance: spotalis
    app.kubernetes.io/component: webhook
    app.kubernetes.io/managed-by: kubectl
type: Opaque
data:
  tls.crt: $SERVER_CERT
  tls.key: $SERVER_KEY
EOF

echo "Secret definition created in build/k8s/secret.yaml"

echo "Certificates generated and secret created successfully!"
echo "Secret name: $SECRET_NAME in namespace: $NAMESPACE"
echo ""
echo "Generated files:"
echo "- build/k8s/secret.yaml (with actual certificate data)"
echo "- build/k8s/webhook.yaml (updated with CA bundle)"
echo ""
echo "Note: The secret.yaml file now contains actual certificate data."
echo "Consider adding build/k8s/secret.yaml to .gitignore if you don't want to commit certificates."
echo ""
echo "Next steps (in order):"
echo "1. Apply the namespace and basic resources:"
echo "   kubectl apply -f build/k8s/install.yaml"
echo "2. Apply the secret with certificates:"
echo "   kubectl apply -f build/k8s/secret.yaml"
echo "3. Apply the webhook configuration:"
echo "   kubectl apply -f build/k8s/webhook.yaml"
echo ""
echo "Or apply everything at once:"
echo "   kubectl apply -f build/k8s/"

# Clean up temporary directory
rm -rf $CERT_DIR
