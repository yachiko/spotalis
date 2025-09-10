# Build variables
GO_VERSION ?= 1.21
REGISTRY ?= ghcr.io/ahoma
IMG_NAME ?= spotalis
IMG_TAG ?= latest
IMG ?= $(REGISTRY)/$(IMG_NAME):$(IMG_TAG)

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

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests
	go test ./tests/e2e/... -v -tags=e2e

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

.PHONY: deploy
deploy: generate ## Deploy to current kubectl context
	kubectl apply -f configs/crd/bases/
	kubectl apply -f configs/samples/

.PHONY: undeploy
undeploy: ## Remove from current kubectl context
	kubectl delete -f configs/samples/ --ignore-not-found=true
	kubectl delete -f configs/crd/bases/ --ignore-not-found=true

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
