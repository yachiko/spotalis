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

.PHONY: fmt
fmt: 
	golangci-lint fmt

.PHONY: test
test: envtest ## Run unit tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLS_DIR) -p path)" go clean -testcache && go test ./pkg/... ./internal/... -coverprofile cover.out

.PHONY: test-integration
test-integration:  ## Run integration tests with Kind
	go test ./tests/integration/... -v -tags=integration -timeout=15m

##@ Build
.PHONY: build
docker-build:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN_DIR)/controller cmd/controller/main.go

##@ Deployment
.PHONY: deploy
deploy: ## Deploy Spotalis to Kubernetes cluster (generates certs dynamically with templates)
	@./scripts/deploy.sh

.PHONY: build-deploy
build-deploy: ## Build and deploy to Kind cluster with unique tag
	@generated_tag=$$(openssl rand -hex 3); \
	echo "Generated tag: $$generated_tag"; \
	docker buildx build -t spotalis:$$generated_tag -f build/docker/Dockerfile .; \
	kind load docker-image spotalis:$$generated_tag --name spotalis || true; \
	kubectl set image deployment/spotalis-controller controller=spotalis:$$generated_tag -n spotalis-system || true

##@ Kind
.PHONY: kind
kind: ## Setup Kind cluster for development
	@chmod +x scripts/setup-kind.sh
	@scripts/setup-kind.sh

ENVTEST = $(TOOLS_DIR)/setup-envtest
.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
