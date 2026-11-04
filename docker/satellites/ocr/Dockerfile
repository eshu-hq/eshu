# OCR satellite image (#8879). Product image promoted from the
# scripts/ocr-sandbox/Dockerfile proof: the pinned tesseract 5.5.0 engine,
# its ldd-resolved runtime library set, and the pinned eng.traineddata,
# plus the statically built eshu-collector-document-import caller that
# execs the engine by absolute path.
#
# Layout (matches the tesseract.Config the caller must pass):
#   /usr/local/bin/eshu-collector-document-import  caller (static, CGO)
#   /usr/bin/tesseract                             engine binary
#   /usr/share/tessdata/eng.traineddata            model
#   <ldd-resolved readlink -f targets>              runtime libraries
#
# Build from the repository root (classic builder compatible: no
# BuildKit-only syntax). All four build args are required; an empty one
# fails the gate closed. Values come from specs/satellite-pins/ocr.<arch>.json:
#   docker build -f docker/satellites/ocr/Dockerfile \
#     --build-arg EXPECT_BINARY_SHA256=<binary.sha256> \
#     --build-arg EXPECT_LIBS_SHA256=<libraries_set_sha256> \
#     --build-arg EXPECT_MODEL_SHA256=<model.sha256> \
#     --build-arg EXPECT_ENGINE_VERSION=<engine.version> \
#     -t eshu-satellite-ocr:<arch> .
#
# The image runs one-shot under the gate-2 sandbox profile (no network,
# read-only root, dropped capabilities, non-root user, memory/PID caps);
# see docker/satellites/README.md.

ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
ARG ESHU_GO_IMAGE=golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca

# go-caller: statically linked eshu-collector-document-import. The caller
# transitively needs CGO (tree-sitter parser bindings), so this builds with
# CGO_ENABLED=1 and -extldflags '-static', the same shape as the
# repository-root image builder.
FROM ${ESHU_GO_IMAGE} AS go-caller
ARG ESHU_VERSION=dev
RUN apk add --no-cache build-base git
WORKDIR /build
COPY go/go.mod go/go.sum ./go/
COPY sdk/go/collector/go.mod ./sdk/go/collector/
COPY sdk/go/factschema/go.mod ./sdk/go/factschema/
RUN cd go && go mod download
COPY go/ ./go/
COPY sdk/go/collector/ ./sdk/go/collector/
COPY sdk/go/factschema/ ./sdk/go/factschema/
RUN cd go \
    && export CGO_ENABLED=1 \
    && export GOFLAGS="-buildvcs=false" \
    && LDFLAGS="-s -w -buildid= -extldflags '-static' -X github.com/eshu-hq/eshu/go/internal/buildinfo.Version=${ESHU_VERSION}" \
    && go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-document-import ./cmd/collector-document-import

# Runtime: pinned engine plus caller. The apk floors below are the
# fail-closed drift guard scripts/verify-apk-floors.sh checks with
# --dockerfile: a floor above any arch's served version breaks that arch's
# build, and a stale floor fails the script. The exact pins live in
# specs/satellite-pins/ocr.<arch>.json and the RUN digest gate enforces
# them, so a repository update that changes package bytes fails this build
# until the pins are re-recorded. This stage MUST stay last: a plain
# `docker build -f` resolves to the last stage.
FROM ${ESHU_ALPINE_IMAGE} AS runtime
ARG EXPECT_BINARY_SHA256
ARG EXPECT_LIBS_SHA256
ARG EXPECT_MODEL_SHA256
ARG EXPECT_ENGINE_VERSION
RUN apk add --no-cache \
        "tesseract-ocr>=5.5.0-r0" "tesseract-ocr-data-eng>=5.5.0-r0" \
        "leptonica>=1.84.1-r0" "libwebp>=1.4.0-r0" "libwebpmux>=1.4.0-r0" \
        "libsharpyuv>=1.4.0-r0" "musl>=1.2.5-r12" "libgcc>=14.2.0-r4" \
        "libgomp>=14.2.0-r4" "libstdc++>=14.2.0-r4" "giflib>=5.2.2-r2" \
        "libjpeg-turbo>=3.0.4-r0" "libpng>=1.6.59-r0" "tiff>=4.7.2-r0" \
        "zlib>=1.3.2-r1" "zstd-libs>=1.5.6-r2"
COPY --from=go-caller /go-bin/eshu-collector-document-import /usr/local/bin/eshu-collector-document-import
# Build-time digest gate over the binary, the ldd-resolved libraries as a
# set, and the model. The set hash uses the same formula the engine records
# as libs=sha256:... (SHA-256 over sorted "<path> <hex>\n" lines), so a
# swapped library fails the build even though only one hash is compared.
# The per-file list is printed for the operator's
# ESHU_DOC_IMPORT_TESSERACT_LIBRARIES pin.
RUN set -eu; \
    [ -n "${EXPECT_BINARY_SHA256:-}" ] || { echo "GATE FAIL: EXPECT_BINARY_SHA256 build arg is required (see specs/satellite-pins/ocr.<arch>.json)" >&2; exit 1; }; \
    [ -n "${EXPECT_LIBS_SHA256:-}" ] || { echo "GATE FAIL: EXPECT_LIBS_SHA256 build arg is required (see specs/satellite-pins/ocr.<arch>.json)" >&2; exit 1; }; \
    [ -n "${EXPECT_MODEL_SHA256:-}" ] || { echo "GATE FAIL: EXPECT_MODEL_SHA256 build arg is required (see specs/satellite-pins/ocr.<arch>.json)" >&2; exit 1; }; \
    [ -n "${EXPECT_ENGINE_VERSION:-}" ] || { echo "GATE FAIL: EXPECT_ENGINE_VERSION build arg is required (see specs/satellite-pins/ocr.<arch>.json)" >&2; exit 1; }; \
    got="$(sha256sum /usr/bin/tesseract | awk '{print $1}')"; \
    [ "${got}" = "${EXPECT_BINARY_SHA256}" ] || { echo "GATE FAIL: tesseract sha256:${got} != pinned ${EXPECT_BINARY_SHA256}; re-record specs/satellite-pins (see docker/satellites/README.md)" >&2; exit 1; }; \
    got="$(sha256sum /usr/share/tessdata/eng.traineddata | awk '{print $1}')"; \
    [ "${got}" = "${EXPECT_MODEL_SHA256}" ] || { echo "GATE FAIL: eng.traineddata sha256:${got} != pinned ${EXPECT_MODEL_SHA256}; re-record specs/satellite-pins (see docker/satellites/README.md)" >&2; exit 1; }; \
    reported="$(tesseract --version 2>/dev/null | head -n 1)"; \
    [ "${reported}" = "tesseract ${EXPECT_ENGINE_VERSION}" ] || { echo "GATE FAIL: binary reports '${reported}', configured '${EXPECT_ENGINE_VERSION}'" >&2; exit 1; }; \
    libs="$(ldd /usr/bin/tesseract | awk '{ if ($2 == "=>") print $3; else print $1 }' | xargs -n1 readlink -f | LC_ALL=C sort -u | xargs sha256sum | awk '{print $2" "$1}' | LC_ALL=C sort)"; \
    [ -n "${libs}" ] || { echo "GATE FAIL: ldd resolved no libraries for /usr/bin/tesseract" >&2; exit 1; }; \
    printf 'pinned runtime libraries:\n%s\n' "${libs}"; \
    got="$(printf '%s\n' "${libs}" | sha256sum | awk '{print $1}')"; \
    [ "${got}" = "${EXPECT_LIBS_SHA256}" ] || { echo "GATE FAIL: library set sha256:${got} != pinned ${EXPECT_LIBS_SHA256}; re-record specs/satellite-pins (see docker/satellites/README.md)" >&2; exit 1; }; \
    echo "GATE PASS: ocr satellite pins hold (binary, ${EXPECT_ENGINE_VERSION}, model, $(printf '%s\n' "${libs}" | wc -l) libraries)"
USER 65532:65532
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/eshu-collector-document-import"]
