# Multi-stage build for Eshu (Go-only)

ARG ESHU_XX_IMAGE=tonistiigi/xx:1.5.0@sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f
ARG ESHU_GO_IMAGE=golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb
ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

# xx provides cross-compilation helpers (clang + target sysroot selection).
# Using --platform=$BUILDPLATFORM throughout avoids running Go 1.26 under
# QEMU amd64 emulation, which causes runtime crashes on arm64 hosts.
FROM --platform=$BUILDPLATFORM ${ESHU_XX_IMAGE} AS xx

FROM --platform=$BUILDPLATFORM ${ESHU_GO_IMAGE} AS builder
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
# D13 (#9669 removal prep, checklist row 11) RETARGET (#11453): the eshu-api
# repo owns the api build, so the in-tree `./cmd/api` build below is dropped
# and the digest-pinned satellite release binary ships at the old installed
# path instead (the #9679 mcp-server precedent: the api-satellite stage
# below). Rollback is the v0.6.0 release image. Exception: the
# eshu-local-api build further below keeps the relocated serving source
# (./cmd/eshu-local-api, moved out of ./cmd/api under #11514) for the
# session API the supervisor spawns on NornicDB or an unset backend. It is
# NornicDB-owner-bound (it inherits the owner's Postgres and graph
# environment, ESHU_API_ADDR plus /healthz). It never ships at the
# eshu-api path.
RUN cd go \
    && export CGO_ENABLED=1 \
    && export GOFLAGS="-buildvcs=false" \
    && LDFLAGS="-s -w -buildid= -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu ./cmd/eshu \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-ingester ./cmd/ingester \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-bootstrap-index ./cmd/bootstrap-index \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-reducer ./cmd/reducer \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-workflow-coordinator ./cmd/workflow-coordinator \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-projector ./cmd/projector \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-git ./cmd/collector-git \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-component-extension ./cmd/collector-component-extension \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-cicd-run ./cmd/collector-cicd-run \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-webhook-intake ./cmd/webhook-intake \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-bootstrap-data-plane ./cmd/bootstrap-data-plane \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-admin-status ./cmd/admin-status \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-core-api ./cmd/core-api \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-local-api ./cmd/eshu-local-api

# mcp-satellite: the extracted MCP server (#9670). The in-tree go/cmd/mcp-server
# is gone, but the E2E fresh stack (compose/docker-compose.e2e.yaml under
# $ESHU_DEPLOY_CHECKOUT) still builds this image with ESHU_REPO_ROOT at this
# checkout and starts the binary at its old path with satellite flags
# (-addr/-core-base-url).
# COPY the digest-pinned satellite release binary under its old path so the
# stack keeps serving the same contract. 0.2.14 is digest
# sha256:f48d3a5f2ab068cc36cee184211ea9cef9a7d801d1e973cf069299f310e0adc7
# (ghcr.io/eshu-hq/eshu-mcp tags 0.2.14/0.2/0/latest; first release serving
# list_service_downstream_consumers, #10535). Like the mocks below, this
# stage MUST stay before "Production stage".
FROM ghcr.io/eshu-hq/eshu-mcp:0.2.14@sha256:f48d3a5f2ab068cc36cee184211ea9cef9a7d801d1e973cf069299f310e0adc7 AS mcp-satellite

# api-satellite: the extracted API dispatch surface (#11453). The in-tree
# api tree is gone from the builder above (#11456 removed go/cmd/api);
# the E2E fresh stack and the
# release image keep serving the dispatch contract from this digest-pinned
# satellite release binary at the old installed path. Satellite build
# 45658098 (post-v0.6.0, commit 4565809840f689d552d2674a771e28a35326359c);
# rollback is the v0.6.0 release image. The satellite takes -addr (default
# :8080) and requires --core-base-url (eshu-hq/eshu-api cmd/eshu-api/main.go):
# a bare `eshu-api` with no --core-base-url exits 1 fail-closed, so stacks
# must keep passing the core origin explicitly. It serves GET /health (plus
# the core-gated GET /readyz); it has no /healthz.
FROM ghcr.io/eshu-hq/eshu-api:sha-4565809840f689d552d2674a771e28a35326359c@sha256:2a2eaf511d3b0237603a6ff612d72cf1a4423cc1bd911472045dd649cbe7208a AS api-satellite

# mock-oidc-idp: E2E-only synthetic OIDC IdP (issue #4971), built and shipped
# as its own image, never as part of the product image below. A no-auth,
# any-caller token-minting IdP must not ship inside the released Eshu image
# (Trivy/auditors correctly flag auth-bypass tooling in a security product's
# image, and a compromised orchestrator could start it inside the trust
# boundary via an explicit `command:` override). It is built in its own stage
# off `builder`, so /go-bin/ — and therefore the production stage's
# `COPY --from=builder /go-bin/` below — never contains it. This stage and
# mock-github's below it MUST stay before "Production stage": plain
# `docker build .` / `docker compose build` without a target resolves to
# whichever stage is LAST in this file: the production stage must remain
# that last stage, with mock-oidc-idp reached only via an explicit
# `--target mock-oidc-idp` (see compose/docker-compose.e2e.yaml under
# $ESHU_DEPLOY_CHECKOUT; the stack moved to eshu-deploy under #8871).
FROM builder AS mock-oidc-builder
# Redeclare so a provided epoch reaches this stage's RUN too — ARGs do not
# cross stage boundaries, and the builder stage no longer leaks it via ENV.
ARG SOURCE_DATE_EPOCH
RUN cd go \
    && export CGO_ENABLED=1 \
    && export GOFLAGS="-buildvcs=false" \
    && LDFLAGS="-s -w -buildid= -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && xx-go build -trimpath -ldflags="${LDFLAGS}" -o /mock-bin/eshu-mock-oidc-idp ./cmd/mock-oidc-idp

FROM ${ESHU_ALPINE_IMAGE} AS mock-oidc-idp

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

FROM ${ESHU_ALPINE_IMAGE} AS mock-github

RUN apk add --no-cache curl

COPY --from=mock-github-builder /mock-bin/ /usr/local/bin/

RUN adduser -D -u 10001 mock-github

EXPOSE 8080

USER mock-github

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/ || exit 1

CMD ["eshu-mock-github"]


# Production stage. MUST remain the last stage in this file — see the
# mock-oidc-idp comment above. It is named `runtime` because
# docker-publish.yml passes `no-cache-filters: runtime` to the image build.
FROM ${ESHU_ALPINE_IMAGE} AS runtime

# c-ares (a transitive libcurl dependency) is pinned to the patched build for
# CVE-2026-33630. libexpat is likewise pinned for CVE-2026-76956 and
# CVE-2026-76957, then raised to 2.10.0-r0 tracking the served version
# (#9947; was 2.9.0-r0 per #8505). libssl3/libcrypto3 (pulled in
# transitively by curl) are
# pinned for CVE-2026-45447 (OpenSSL PKCS7_verify use-after-free, #7315),
# then raised to 3.3.7-r2 for CVE-2026-75804 and CVE-2026-84782 (#7571).
# musl/musl-utils are held at 1.2.5-r12 for CVE-2026-107891 (HIGH) and zlib
# at 1.4-r0 for CVE-2026-85091 (HIGH); both arrive with the Alpine base at
# older builds and only the floors below force the patched versions in.
#
# Three controls keep these packages current:
#   1. The version floors below are the fail-closed guard: the build fails if
#      the Alpine repo ever regresses below a patched version.
#   2. docker-publish.yml sets `no-cache-filters: runtime` on the image build,
#      so this stage skips the persistent type=gha layer cache and resolves
#      packages afresh on every publish, instead of reshipping an old layer.
#   3. scripts/verify-apk-floors.sh compares these floors with what the Alpine
#      repository serves and fails when a floor is stale. It runs on every PR
#      or merge-queue entry that touches this file, and daily on a schedule
#      (never on the publish run itself, so a stale floor cannot skip Trivy).
#
# openssh-client provides the `ssh` transport git execs for
# ESHU_GIT_AUTH_METHOD=ssh (#7762). Its libcrypto3 dependency is held by the
# floor above. scripts/verify-runtime-image-tools.sh asserts its
# REQUIRED_TOOLS list is present in the built image.
RUN apk add --no-cache git openssh-client curl "c-ares>=1.34.9-r0" \
    "libexpat>=2.11.0-r0" "libssl3>=3.3.7-r2" "libcrypto3>=3.3.7-r2" \
    "musl>=1.2.5-r12" "musl-utils>=1.2.5-r12" "zlib>=1.4-r0"

# Copy Go binaries
COPY --from=builder /go-bin/ /usr/local/bin/
# Removed in-tree MCP server replaced by the pinned satellite release (#9670):
# the E2E fresh stack starts this image with command
# ["/usr/local/bin/eshu-mcp-server"], so the satellite binary keeps its old
# path. Same contract the gate serves (scripts/lib/golden-corpus-host-helpers.sh).
COPY --from=mcp-satellite /eshu-mcp /usr/local/bin/eshu-mcp-server
# Removed in-tree API server replaced by the pinned satellite release
# (#11453): the satellite binary keeps the old installed path, so existing
# `eshu-api` commands keep resolving. It must be started with an explicit
# --core-base-url pointing at the core origin; without one it exits 1
# fail-closed.
COPY --from=api-satellite /eshu-api /usr/local/bin/eshu-api
# Release-image entrypoint (issue #11453): maps ESHU_CORE_BASE_URL to
# --core-base-url when the container starts `eshu-api` without the flag, so
# `docker run -e ESHU_CORE_BASE_URL=... <image>` boots the satellite against
# that core. An explicit --core-base-url, and any other command, execs
# untouched; `eshu-api` with neither fails closed (exit 1) with a usage
# naming both forms.
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

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
ENTRYPOINT ["docker-entrypoint.sh"]

# Health check. The satellite serves GET /health (shallow liveness) plus the
# core-gated GET /readyz; it has no /healthz, so the probe uses /health.
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -fsS http://localhost:8080/health || exit 1

# Default command - the API dispatch satellite behind the unchanged `eshu-api`
# path. The entrypoint maps ESHU_CORE_BASE_URL to --core-base-url; without
# the env or an explicit flag the run exits 1 with a usage naming both
# forms, so stacks that relied on the old bare default must now pass the core
# origin one of those two ways (migration from the in-tree default that
# served :8080 with no flag; in-repo stacks already pass --core-base-url:
# the reachability proof compose and the gate scripts).
CMD ["eshu-api"]
