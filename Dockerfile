# Build arguments
ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.20

# Build stage
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

# Build arguments for version info
ARG VERSION=dev
ARG GIT_REV=unknown
ARG BUILD_DATE=unknown

# Install build dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata

# Set working directory
WORKDIR /build

# Copy go modules files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=$(go env GOARCH) \
    go build \
    -a \
    -installsuffix cgo \
    -ldflags="-w -s -X main.version=${VERSION} -X main.commit=${GIT_REV} -X main.date=${BUILD_DATE}" \
    -o ghclone \
    ./cmd/ghclone

# Verify the binary
RUN ./ghclone --help

# Runtime stage
FROM alpine:${ALPINE_VERSION}

# Re-declare build arguments for use in this stage
ARG VERSION=dev
ARG GIT_REV=unknown
ARG BUILD_DATE=unknown

# Install runtime dependencies
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata

# Create non-root user
RUN addgroup -g 1001 -S ghclone && \
    adduser -u 1001 -S ghclone -G ghclone

# Create necessary directories
RUN mkdir -p /app /home/ghclone && \
    chown -R ghclone:ghclone /app /home/ghclone

# Copy binary from builder
COPY --from=builder /build/ghclone /app/ghclone

# Copy CA certificates and timezone data
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Set working directory
WORKDIR /home/ghclone

# Switch to non-root user
USER ghclone

# Set environment variables
ENV PATH="/app:${PATH}" \
    HOME="/home/ghclone" \
    USER="ghclone" \
    VERSION="${VERSION}" \
    GIT_REV="${GIT_REV}" \
    BUILD_DATE="${BUILD_DATE}"

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ghclone --help || exit 1

# Add labels
LABEL org.opencontainers.image.title="ghclone" \
      org.opencontainers.image.description="A GitHub repository cloning tool with TUI interface" \
      org.opencontainers.image.url="https://github.com/italoag/ghcloner" \
      org.opencontainers.image.source="https://github.com/italoag/ghcloner" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_REV}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="Italo A. G."

# Default command
ENTRYPOINT ["ghclone"]
CMD ["--help"]