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

.PHONY: deps
deps: 
	go mod download
	go mod tidy

.PHONY: lint
lint: 
	golangci-lint run

.PHONY: test
test: envtest ## Run unit tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLS_DIR) -p path)" go clean -testcache && go test ./pkg/... ./internal/... -coverprofile cover.out

.PHONY: test-integration
test-integration:  ## Run integration tests with Kind
	go test ./tests/integration/... -v -tags=integration

.PHONY: test-integration-cleanup
test-integration-cleanup: ## Clean up integration test resources from Kind cluster
	@kubectl config current-context | grep -q "kind-" || (echo "Error: Not connected to a Kind cluster. Run 'kubectl config use-context kind-spotalis'" && exit 1)
	@kubectl delete namespaces -l spotalis.io/test=true --ignore-not-found=true
	@kubectl get namespaces -o name | grep -E "(managed-|unmanaged-|spotalis-managed-)" | xargs -r kubectl delete --ignore-not-found=true
	@kubectl delete deployments -A -l spotalis.io/test=true --ignore-not-found=true

##@ Build
.PHONY: build
docker-build:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN_DIR)/controller cmd/controller/main.go

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

.PHONY: generate-certs
generate-certs: ## Generate TLS certificates for webhook
	./generate-certs.sh

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
