# Multi-stage build for Eshu (Go-only)

# xx provides cross-compilation helpers (clang + target sysroot selection).
# Using --platform=$BUILDPLATFORM throughout avoids running Go 1.26 under
# QEMU amd64 emulation, which causes runtime crashes on arm64 hosts.
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.5.0@sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f AS xx

FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS builder
COPY --from=xx /usr/bin/xx-* /usr/bin/

ARG TARGETPLATFORM
ARG ESHU_VERSION=dev
ARG SOURCE_DATE_EPOCH
ENV SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH}

# clang+lld for cross-compilation; xx-apk installs the target-arch sysroot.
RUN apk add --no-cache git clang lld
RUN xx-apk add --no-cache musl-dev gcc

WORKDIR /build

# Download modules natively (avoids QEMU + Go 1.26 TLS/crypto panics).
COPY go/go.mod go/go.sum ./go/
COPY sdk/go/collector/go.mod ./sdk/go/collector/
RUN cd go && GONOSUMDB='*' GONOSUMCHECK='*' go mod download

# Copy Go source and local SDK modules referenced by go.mod replacements.
COPY go/ ./go/
COPY sdk/go/collector/ ./sdk/go/collector/

# Build all Go binaries. xx-go sets GOARCH, CGO_ENABLED, and CC automatically
# for the target platform. CGO is required for tree-sitter parser bindings.
RUN cd go \
    && export CGO_ENABLED=1 \
    && export GOFLAGS="-buildvcs=false" \
    && LDFLAGS="-s -w -buildid= -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu ./cmd/eshu \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-api ./cmd/api \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-mcp-server ./cmd/mcp-server \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-ingester ./cmd/ingester \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-bootstrap-index ./cmd/bootstrap-index \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-reducer ./cmd/reducer \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-workflow-coordinator ./cmd/workflow-coordinator \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-projector ./cmd/projector \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-git ./cmd/collector-git \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-confluence ./cmd/collector-confluence \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-terraform-state ./cmd/collector-terraform-state \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-component-extension ./cmd/collector-component-extension \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-oci-registry ./cmd/collector-oci-registry \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-package-registry ./cmd/collector-package-registry \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-vulnerability-intelligence ./cmd/collector-vulnerability-intelligence \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-sbom-attestation ./cmd/collector-sbom-attestation \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-security-alerts ./cmd/collector-security-alerts \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-cicd-run ./cmd/collector-cicd-run \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-pagerduty ./cmd/collector-pagerduty \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-grafana ./cmd/collector-grafana \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-prometheus-mimir ./cmd/collector-prometheus-mimir \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-loki ./cmd/collector-loki \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-tempo ./cmd/collector-tempo \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-jira ./cmd/collector-jira \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-scanner-worker ./cmd/scanner-worker \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-aws-cloud ./cmd/collector-aws-cloud \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-gcp-cloud ./cmd/collector-gcp-cloud \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-azure-cloud ./cmd/collector-azure-cloud \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-webhook-listener ./cmd/webhook-listener \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-bootstrap-data-plane ./cmd/bootstrap-data-plane \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-admin-status ./cmd/admin-status

# Production stage
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

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
