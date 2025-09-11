# Docker Bake configuration for Spotalis controller
# Usage: docker buildx bake -f build/docker/docker-bake.hcl

variable "REGISTRY" {
  default = "ghcr.io/spotalis"
}

variable "TAG" {
  default = "latest"
}

variable "PLATFORM" {
  default = "linux/amd64,linux/arm64"
}

# Default target group
group "default" {
  targets = ["controller"]
}

# Main controller target
target "controller" {
  context    = "../../"
  dockerfile = "build/docker/Dockerfile"
  platforms  = split(",", PLATFORM)
  tags = [
    "${REGISTRY}/controller:${TAG}",
    "${REGISTRY}/controller:latest"
  ]
  
  # Build arguments
  args = {
    BUILDTIME = timestamp()
    VERSION   = TAG
  }
  
  # Output configuration
  output = ["type=registry"]
  
  # Cache configuration
  cache-from = [
    "type=gha"
  ]
  cache-to = [
    "type=gha,mode=max"
  ]
}

# Development target (local builds)
target "controller-local" {
  inherits   = ["controller"]
  platforms  = ["linux/amd64"]
  output     = ["type=docker"]
  cache-from = []
  cache-to   = []
  tags = [
    "spotalis/controller:local",
    "spotalis/controller:dev"
  ]
}

# CI target with metadata
target "controller-ci" {
  inherits = ["controller"]
  
  # Additional metadata labels
  labels = {
    "org.opencontainers.image.title"       = "Spotalis Controller"
    "org.opencontainers.image.description" = "Kubernetes native workload replica manager"
    "org.opencontainers.image.url"         = "https://github.com/spotalis/spotalis"
    "org.opencontainers.image.source"      = "https://github.com/spotalis/spotalis"
    "org.opencontainers.image.version"     = TAG
    "org.opencontainers.image.created"     = timestamp()
    "org.opencontainers.image.revision"    = "${GITHUB_SHA}"
    "org.opencontainers.image.licenses"    = "Apache-2.0"
  }
}

# Debug target with debugging tools
target "controller-debug" {
  context    = "../../"
  dockerfile = "build/docker/Dockerfile.debug"
  platforms  = ["linux/amd64"]
  tags = [
    "${REGISTRY}/controller:debug",
    "spotalis/controller:debug"
  ]
  output = ["type=docker"]
}
