# Build variables
GO_VERSION ?= 1.21
IMG_NAME ?= spotalis
IMG_TAG ?= latest
IMG ?= $(IMG_NAME):$(IMG_TAG)

# Go variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0

# Tools
CONTROLLER_GEN_VERSION ?= v0.19.0
ENVTEST_K8S_VERSION = 1.28.0

# Directories
BIN_DIR := bin
BUILD_DIR := build
TOOLS_DIR := $(shell pwd)/$(BIN_DIR)/tools

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: deps
deps: ## Download go modules
	go mod download
	go mod tidy

.PHONY: generate
generate: controller-gen ## Generate code and manifests
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./pkg/apis/..."
	$(CONTROLLER_GEN) crd rbac:roleName=spotalis-controller webhook paths="./pkg/..." output:crd:artifacts:config=configs/crd/bases

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint
	$(GOLANGCI_LINT) run

.PHONY: run
run: generate fmt vet ## Run controller locally against current kubeconfig
	go run cmd/controller/main.go

##@ Testing

.PHONY: test
test: generate fmt vet envtest ## Run unit tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLS_DIR) -p path)" go test ./pkg/... ./internal/... -coverprofile cover.out

.PHONY: test-integration
test-integration: generate fmt vet ## Run integration tests with Kind
	go test ./tests/integration/... -v -tags=integration

.PHONY: test-integration-kind
test-integration-kind: test-integration-cleanup ## Run integration tests against Kind cluster with Spotalis deployed
	@echo "Running integration tests against Kind cluster..."
	@kubectl config current-context | grep -q "kind-" || (echo "Error: Not connected to a Kind cluster. Run 'kubectl config use-context kind-spotalis'" && exit 1)
	go test ./tests/integration/... -v -tags=integration_kind -timeout=10m

.PHONY: test-integration-cleanup
test-integration-cleanup: ## Clean up integration test resources from Kind cluster
	@echo "Cleaning up integration test resources..."
	@kubectl config current-context | grep -q "kind-" || (echo "Error: Not connected to a Kind cluster. Run 'kubectl config use-context kind-spotalis'" && exit 1)
	@echo "Deleting test namespaces..."
	@kubectl delete namespaces -l spotalis.io/test=true --ignore-not-found=true
	@echo "Deleting legacy test namespaces..."
	@kubectl get namespaces -o name | grep -E "(managed-|unmanaged-|spotalis-managed-)" | xargs -r kubectl delete --ignore-not-found=true
	@echo "Deleting test deployments..."
	@kubectl delete deployments -A -l spotalis.io/test=true --ignore-not-found=true
	@echo "Cleanup completed"

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests
	go test ./tests/e2e/... -v -tags=e2e

.PHONY: test-all
test-all: test test-integration test-e2e ## Run all tests (unit, integration, and e2e)

##@ Build

.PHONY: build
build: generate fmt vet ## Build binary
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN_DIR)/controller cmd/controller/main.go

.PHONY: docker-build
docker-build: ## Build docker image
	docker buildx build -t $(IMG) -f build/docker/Dockerfile .

.PHONY: docker-push
docker-push: ## Push docker image
	docker push $(IMG)

##@ Deployment

.PHONY: build-deploy
build-deploy: ## Build and deploy to Kind cluster with unique tag
	@generated_tag=$$(openssl rand -hex 3); \
	echo "Generated tag: $$generated_tag"; \
	docker buildx build -t spotalis:$$generated_tag -f build/docker/Dockerfile .; \
	kind load docker-image spotalis:$$generated_tag --name spotalis || true; \
	kubectl set image deployment/spotalis-controller controller=spotalis:$$generated_tag -n spotalis-system || true

##@ Kind
.PHONY: kind-create
kind-create: ## Create Kind cluster for testing
	kind create cluster --name spotalis --config build/kind/cluster.yaml

.PHONY: kind-delete
kind-delete: ## Delete Kind cluster
	kind delete cluster --name spotalis

.PHONY: kind-load
kind-load: docker-build ## Load image into Kind cluster
	kind load docker-image $(IMG) --name spotalis

.PHONY: generate-certs
generate-certs: ## Generate TLS certificates for webhook
	./generate-certs.sh

.PHONY: kind-integration-full
kind-integration-full: kind-create docker-build kind-load generate-certs ## Full Kind integration setup and test
	@echo "Deploying Spotalis to Kind cluster..."
	kubectl apply -f build/k8s/
	@echo "Waiting for Spotalis to be ready..."
	kubectl wait --for=condition=available --timeout=300s deployment/spotalis-controller -n spotalis-system
	@echo "Running integration tests..."
	$(MAKE) test-integration-kind

.PHONY: kind-cleanup-all
kind-cleanup-all: ## Comprehensive cleanup of Kind cluster test resources
	@echo "Performing comprehensive cleanup of Kind cluster..."
	@kubectl config current-context | grep -q "kind-" || (echo "Error: Not connected to a Kind cluster. Run 'kubectl config use-context kind-spotalis'" && exit 1)
	@echo "Deleting all test namespaces..."
	@kubectl delete namespaces -l spotalis.io/test=true --ignore-not-found=true --timeout=60s
	@echo "Deleting legacy test namespaces..."
	@for ns in $$(kubectl get namespaces -o name | grep -E "(managed-|unmanaged-|spotalis-managed-)" | cut -d/ -f2); do \
		echo "Deleting namespace: $$ns"; \
		kubectl delete namespace "$$ns" --ignore-not-found=true --timeout=30s || true; \
	done
	@echo "Deleting test deployments from all namespaces..."
	@kubectl delete deployments -A -l spotalis.io/test=true --ignore-not-found=true --timeout=30s
	@echo "Deleting test pods from all namespaces..."
	@kubectl delete pods -A -l spotalis.io/test=true --ignore-not-found=true --timeout=30s
	@echo "Force cleanup any remaining test namespaces..."
	@for ns in $$(kubectl get namespaces -o name | grep -E "(managed-|unmanaged-|spotalis-managed-|spotalis-test-)" | cut -d/ -f2); do \
		echo "Force deleting namespace: $$ns"; \
		kubectl patch namespace "$$ns" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true; \
		kubectl delete namespace "$$ns" --ignore-not-found=true --force --grace-period=0 2>/dev/null || true; \
	done
	@echo "Cleanup completed"

##@ Tools

CONTROLLER_GEN = $(TOOLS_DIR)/controller-gen
.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

ENVTEST = $(TOOLS_DIR)/setup-envtest
.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

GOLANGCI_LINT = $(TOOLS_DIR)/golangci-lint
.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

$(TOOLS_DIR):
	mkdir -p $(TOOLS_DIR)

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BIN_DIR) $(BUILD_DIR) cover.out
