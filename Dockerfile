# Multi-stage build for Spotalis controller
FROM --platform=$BUILDPLATFORM golang:1.24-alpine@sha256:fc2cff6625f3c1c92e6c85938ac5bd09034ad0d4bc2dfb08278020b68540dbb5 AS builder

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
FROM gcr.io/distroless/static:nonroot@sha256:e8a4044e0b4ae4257efa45fc026c0bc30ad320d43bd4c1a7d5271bd241e386d0

# Copy the binary
COPY --from=builder /workspace/controller /usr/local/bin/controller

# Use nonroot user
USER 65532:65532

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/controller"]