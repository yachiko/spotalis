# Multi-stage build for Spotalis controller
FROM golang:1.24-alpine AS builder

# Install required packages
RUN apk add --no-cache git ca-certificates

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

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -ldflags="-w -s" \
    -o controller \
    ./cmd/controller/

# Final stage - minimal runtime image
FROM gcr.io/distroless/static:nonroot

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /workspace/controller /usr/local/bin/controller

# Use nonroot user
USER 65532:65532

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/controller"]
