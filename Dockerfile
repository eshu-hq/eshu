# Multi-stage build for Eshu (Go-only)
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git gcc musl-dev

WORKDIR /build

ARG ESHU_VERSION=dev

# Copy Go module files and download dependencies
COPY go/go.mod go/go.sum ./go/
RUN cd go && go mod download

# Copy Go source
COPY go/ ./go/

# Build all Go binaries (CGO required for tree-sitter parser bindings)
RUN cd go && LDFLAGS="-s -w -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu ./cmd/eshu \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-api ./cmd/api \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-mcp-server ./cmd/mcp-server \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-ingester ./cmd/ingester \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-bootstrap-index ./cmd/bootstrap-index \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-reducer ./cmd/reducer \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-workflow-coordinator ./cmd/workflow-coordinator \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-projector ./cmd/projector \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-git ./cmd/collector-git \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-confluence ./cmd/collector-confluence \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-terraform-state ./cmd/collector-terraform-state \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-bootstrap-data-plane ./cmd/bootstrap-data-plane \
    && CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-admin-status ./cmd/admin-status

# Production stage
FROM alpine:3.21

RUN apk add --no-cache git curl

# Copy Go binaries
COPY --from=builder /go-bin/ /usr/local/bin/

# Create the runtime user and writable working directories.
RUN adduser -D -u 10001 eshu \
    && mkdir -p /workspace /data/.eshu \
    && chown -R eshu:eshu /workspace /data

ENV HOME=/data
ENV ESHU_HOME=/data/.eshu

# Expose the shared admin/status and metrics ports used by the long-running
# runtime shapes in this image.
EXPOSE 8080 9464

WORKDIR /data
USER eshu

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

# Default command - run the Go API server
CMD ["eshu-api"]
