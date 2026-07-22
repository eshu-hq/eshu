# Multi-stage build for Eshu (Go-only)

# xx provides cross-compilation helpers (clang + target sysroot selection).
# Using --platform=$BUILDPLATFORM throughout avoids running Go 1.26 under
# QEMU amd64 emulation, which causes runtime crashes on arm64 hosts.
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.5.0@sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f AS xx

FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine@sha256:99e12cfb19b753915f9b9fdc5a99f1869a24a69d3a0955832d5702e7fa68f1be AS builder
COPY --from=xx /usr/bin/xx-* /usr/bin/

ARG TARGETPLATFORM
ARG ESHU_VERSION=dev
# SOURCE_DATE_EPOCH feeds the cgo toolchain (clang/lld) for reproducible
# builds (docs/internal/design/build-reproducibility.md §3.2). It is a plain
# ARG, never an unconditional ENV: BuildKit injects a *provided* ARG into this
# stage's RUN environment automatically, while `ENV SOURCE_DATE_EPOCH=${...}`
# materializes an EMPTY string when the arg is not passed (any local
# `docker compose up --build`), and clang hard-errors on an empty
# SOURCE_DATE_EPOCH. Unset must stay unset.
ARG SOURCE_DATE_EPOCH

# clang+lld for cross-compilation; xx-apk installs the target-arch sysroot.
RUN apk add --no-cache git clang lld
RUN xx-apk add --no-cache musl-dev gcc

WORKDIR /build

# Download modules natively (avoids QEMU + Go 1.26 TLS/crypto panics).
COPY go/go.mod go/go.sum ./go/
COPY sdk/go/collector/go.mod ./sdk/go/collector/
COPY sdk/go/factschema/go.mod ./sdk/go/factschema/
RUN cd go && GONOSUMDB='*' GONOSUMCHECK='*' go mod download

# Copy Go source and local SDK modules referenced by go.mod replacements.
COPY go/ ./go/
COPY sdk/go/collector/ ./sdk/go/collector/
COPY sdk/go/factschema/ ./sdk/go/factschema/

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
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-kubernetes-live ./cmd/collector-kubernetes-live \
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

# mock-oidc-idp: E2E-only synthetic OIDC IdP (issue #4971), built and shipped
# as its own image, never as part of the product image below. A no-auth,
# any-caller token-minting IdP must not ship inside the released Eshu image
# (Trivy/auditors correctly flag auth-bypass tooling in a security product's
# image, and a compromised orchestrator could start it inside the trust
# boundary via an explicit `command:` override). It is built in its own stage
# off `builder`, so /go-bin/ — and therefore the production stage's
# `COPY --from=builder /go-bin/` below — never contains it. This stage and
# mock-github's below it MUST stay before "Production stage": no
# docker-compose*.yaml `build:` block in this repo sets an explicit
# `target:`, so plain `docker build .` / `docker compose build` resolves to
# whichever stage is LAST in this file: the production stage must remain
# that last stage, with mock-oidc-idp reached only via an explicit
# `--target mock-oidc-idp` (see docker-compose.e2e.yaml).
FROM builder AS mock-oidc-builder
# Redeclare so a provided epoch reaches this stage's RUN too — ARGs do not
# cross stage boundaries, and the builder stage no longer leaks it via ENV.
ARG SOURCE_DATE_EPOCH
RUN cd go \
    && export CGO_ENABLED=1 \
    && export GOFLAGS="-buildvcs=false" \
    && LDFLAGS="-s -w -buildid= -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /mock-bin/eshu-mock-oidc-idp ./cmd/mock-oidc-idp

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS mock-oidc-idp

RUN apk add --no-cache curl

COPY --from=mock-oidc-builder /mock-bin/ /usr/local/bin/

RUN adduser -D -u 10001 mock-oidc-idp

EXPOSE 8080

USER mock-oidc-idp

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/.well-known/openid-configuration || exit 1

CMD ["eshu-mock-oidc-idp"]

# mock-github: E2E-only synthetic GitHub OAuth2/REST counterparty (issue
# #5170), built and shipped as its own image for the same reason
# mock-oidc-idp is: a no-auth, any-caller identity-provider stand-in must not
# ship inside the released Eshu image. Same "own stage off builder, own image,
# must stay before Production stage" rules as mock-oidc-idp above.
FROM builder AS mock-github-builder
ARG SOURCE_DATE_EPOCH
RUN cd go \
    && export CGO_ENABLED=1 \
    && export GOFLAGS="-buildvcs=false" \
    && LDFLAGS="-s -w -buildid= -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /mock-bin/eshu-mock-github ./cmd/mock-github

FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS mock-github

RUN apk add --no-cache curl

COPY --from=mock-github-builder /mock-bin/ /usr/local/bin/

RUN adduser -D -u 10001 mock-github

EXPOSE 8080

USER mock-github

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/ || exit 1

CMD ["eshu-mock-github"]

# Production stage. MUST remain the last stage in this file — see the
# mock-oidc-idp comment above.
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

# c-ares (a transitive libcurl dependency) is pinned to the patched build for
# CVE-2026-33630. Pinning it here — rather than relying on a base-image digest
# bump — is load-bearing: docker-publish.yml imports a persistent type=gha layer
# cache, and this RUN precedes the go-binary COPY, so a go.mod-only change leaves
# this layer's cache key untouched and BuildKit would reship the old vulnerable
# c-ares. The explicit constraint changes the layer's cache key (forcing a
# rebuild that pulls the fixed package) and fails the build closed if the
# Alpine 3.21 repo ever regresses below the patched version.
RUN apk add --no-cache git curl "c-ares>=1.34.8-r0"

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
