# Build variables
GO_VERSION ?= 1.21
IMG_NAME ?= spotalis
IMG_TAG ?= latest
IMG ?= $(IMG_NAME):$(IMG_TAG)

# Go variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0

# Docker variables
DOCKER_PLATFORM ?= linux/arm64

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
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(TOOLS_DIR) -p path)" go clean -testcache && go test ./... -v --tags=unit -timeout=5m

.PHONY: test-integration
test-integration:
	go test ./tests/integration/... -v -tags=integration -timeout=45m -ginkgo.v

##@ Build
.PHONY: build
build:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN_DIR)/controller cmd/controller/main.go

##@ Deployment
.PHONY: deploy
deploy: ## Deploy Spotalis to Kubernetes cluster (generates certs dynamically with templates)
	@./scripts/deploy.sh

.PHONY: build-deploy
build-deploy: kind-load ## Build, load to Kind, and deploy
	@echo "✅ Build and deployment completed"

##@ Docker
.PHONY: docker-build-local
docker-build-local: ## Build Docker image for local development (ARM64 by default, use DOCKER_PLATFORM=linux/amd64 to override)
	docker buildx bake controller-local --set controller-local.platform=$(DOCKER_PLATFORM)

.PHONY: docker-build
docker-build: ## Build Docker images for all platforms
	docker buildx bake controller

##@ Kind
.PHONY: kind
kind: ## Setup Kind cluster for development
	@chmod +x scripts/setup-kind.sh
	@scripts/setup-kind.sh

.PHONY: kind-port-forward
kind-port-forward: ## Port-forward Spotalis controller metrics and health endpoints
	kubectl -n spotalis-system port-forward deployment/spotalis-controller 8090:8080 8091:8081 &

.PHONY: kind-load
kind-load: docker-build-local ## Load Docker image into Kind cluster
	kind load docker-image spotalis:local --name spotalis || true; \
	kubectl set image deployment/spotalis-controller controller=spotalis:local -n spotalis-system || true
	kubectl rollout restart deployment spotalis-controller -n spotalis-system

##@ Release
.PHONY: tag
tag: ## Create and push next patch version tag (vX.Y.(Z+1))
	@set -e; \
	last=$$(git tag --list 'v*' --sort=-v:refname | head -1); \
	if [ -z "$$last" ]; then \
	  new="v0.0.1"; \
	else \
	  ver=$${last#v}; \
	  major=$${ver%%.*}; rest=$${ver#*.}; minor=$${rest%%.*}; patch=$${rest#*.}; \
	  patch=$$((patch+1)); \
	  new="v$$major.$$minor.$$patch"; \
	fi; \
	echo "Last tag: $$last"; \
	echo "New tag: $$new"; \
	git tag $$new; \
	git push origin $$new; \
	echo "✅ Created and pushed $$new"
	
ENVTEST = $(TOOLS_DIR)/setup-envtest
.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(TOOLS_DIR)
	GOBIN=$(TOOLS_DIR) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest