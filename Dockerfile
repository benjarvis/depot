# Build stage
FROM golang:1.21 AS builder

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary
# Using CGO_ENABLED=0 for a fully static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o depot ./cmd/depot

# Runtime stage using distroless for minimal attack surface and rootless by default
FROM gcr.io/distroless/static-debian12:nonroot

# Copy the binary from builder
COPY --from=builder --chown=nonroot:nonroot /build/depot /depot

# Create directories in builder stage since distroless doesn't have mkdir
FROM debian:12-slim AS directories
RUN mkdir -p /var/depot/data /var/depot/certs && \
    chmod 755 /var/depot /var/depot/data /var/depot/certs

# Final runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

# Copy the binary with correct ownership
COPY --from=builder --chown=nonroot:nonroot /build/depot /depot

# Copy directories with correct permissions (nonroot user is 65532:65532)
COPY --from=directories --chown=65532:65532 /var/depot /var/depot

# Expose the HTTPS port
EXPOSE 8443

# Set up volumes for persistent data
VOLUME ["/var/depot/data", "/var/depot/certs"]

# Run as nonroot user (this is already the default for :nonroot tag, but being explicit)
USER nonroot

# Use the binary as entrypoint
ENTRYPOINT ["/depot"]