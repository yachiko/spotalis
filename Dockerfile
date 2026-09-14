# Multi-stage build for Spotalis controller
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

# Build arguments provided by buildx
ARG TARGETOS
ARG TARGETARCH
ARG BUILDTIME
ARG VERSION

# Set working directory
WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY internal/ internal/

# Build the binary for target architecture
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildTime=${BUILDTIME}" \
    -o controller \
    ./cmd/controller/

# Final stage - minimal runtime image  
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

# Copy the binary
COPY --from=builder /workspace/controller /usr/local/bin/controller

# Use nonroot user
USER 65532:65532

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/controller"]
