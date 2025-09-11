# Build Infrastructure

This directory contains build-related files for the Spotalis project.

## Structure

```
build/
├── docker/                    # Docker build configuration
│   ├── Dockerfile            # Production multi-stage build
│   ├── Dockerfile.debug      # Debug build with additional tools
│   ├── docker-bake.hcl       # Docker buildx bake configuration
│   ├── docker-compose.yml    # Local development stack
│   └── prometheus.yml        # Prometheus configuration for metrics
└── kind/                      # Kind cluster configuration
    └── cluster.yaml          # Kind cluster setup for testing
```

## Docker Build

### Production Build
```bash
# Build using docker-bake (recommended)
docker buildx bake -f build/docker/docker-bake.hcl

# Or build manually
make docker-build
```

### Debug Build
```bash
# Build debug image with additional tools
docker buildx bake -f build/docker/docker-bake.hcl controller-debug
```

### Local Development
```bash
# Start local development stack
cd build/docker
docker-compose up -d

# View logs
docker-compose logs -f controller

# Stop stack
docker-compose down
```

## Kind Testing

### Create Test Cluster
```bash
# Create cluster with custom configuration
make kind-create

# Load local image into cluster
make kind-load

# Delete cluster
make kind-delete
```

### Manual Cluster Management
```bash
# Create cluster manually
kind create cluster --name spotalis --config build/kind/cluster.yaml

# Load image
kind load docker-image spotalis/controller:local --name spotalis

# Delete cluster
kind delete cluster --name spotalis
```

## Build Targets

The Makefile provides several build targets:

- `make docker-build` - Build production container image
- `make docker-push` - Push image to registry
- `make kind-create` - Create Kind test cluster
- `make kind-load` - Load image into Kind cluster
- `make kind-delete` - Delete Kind cluster

## Environment Variables

Build process supports these environment variables:

- `REGISTRY` - Container registry (default: ghcr.io/ahoma)
- `IMG_NAME` - Image name (default: spotalis)
- `IMG_TAG` - Image tag (default: latest)
- `PLATFORM` - Target platforms (default: linux/amd64,linux/arm64)
