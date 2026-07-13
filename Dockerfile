# Multi-stage build for Spotalis controller
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

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
FROM gcr.io/distroless/static:nonroot@sha256:d29e660cc75a5b6b1334e03c5c81ccf9bc0884a002c6000dbf0fb96034814478

# Copy the binary
COPY --from=builder /workspace/controller /usr/local/bin/controller

# Use nonroot user
USER 65532:65532

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/controller"]