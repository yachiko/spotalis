# Docker Bake configuration for Spotalis controller
# Usage: docker buildx bake

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
  context    = "."
  dockerfile = "Dockerfile"
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
  platforms  = ["linux/arm64"]
  output     = ["type=docker"]
  cache-from = []
  cache-to   = []
  tags = [
    "spotalis:local",
  ]
  args = {
    BUILDTIME = timestamp()
    VERSION   = "local"
  }
}