# Transcription satellite image (#8879). Product image promoted from the
# scripts/lib/media-transcript-sandbox/Dockerfile proof: whisper.cpp
# whisper-cli built from the pinned v1.9.5 tag commit (static ggml/whisper
# libraries, CPU only, no ffmpeg/SDL/curl), the pinned ggml-base.en model,
# plus the statically built eshu-collector-media-import caller that execs
# the engine by absolute path.
#
# Layout (matches the whispercpp.Config the caller must pass):
#   /usr/local/bin/eshu-collector-media-import  caller (static, CGO)
#   /opt/whisper/bin/whisper-cli                engine binary
#   /opt/whisper/models/ggml-base.en.bin        model
#   <ldd-resolved readlink -f targets>           runtime libraries
#     (libstdc++, libgcc_s, libc; the ggml/whisper libraries are static)
#
# Build from the repository root (classic builder compatible: no
# BuildKit-only syntax such as COPY --chmod or ADD --checksum; permissions
# are set with RUN chmod). The binary digest is not reproducible across
# toolchains, so EXPECT_BINARY_SHA256 carries the reference-build value
# from specs/satellite-pins/transcription.<arch>.json; a rebuild with a
# different toolchain fails the gate until the reference is re-recorded.
# All three digest args are required; an empty one fails the gate closed:
#   docker build -f docker/satellites/transcription/Dockerfile \
#     --build-arg EXPECT_BINARY_SHA256=<binary.sha256> \
#     --build-arg EXPECT_LIBS_SHA256=<libraries_set_sha256> \
#     --build-arg EXPECT_MODEL_SHA256=<model.sha256> \
#     -t eshu-satellite-transcription:<arch> .
#
# The image runs one-shot under the gate-2 sandbox profile (no network,
# read-only root, dropped capabilities, non-root user, memory/PID caps);
# see docker/satellites/README.md.

ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
ARG ESHU_GO_IMAGE=golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca
ARG WHISPER_CPP_TAG=v1.9.5
ARG WHISPER_CPP_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
# The ggml model is architecture independent, so its digest is a safe
# pinned default; the per-arch pin files repeat it as the record.
ARG WHISPER_MODEL_SHA256=a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002
ARG WHISPER_MODEL_REVISION=5359861c739e955e79d9a303bcbc70fb988958b1

# whisper: builds whisper-cli from the pinned tag and fails unless HEAD
# equals the pinned commit (same provenance check as the proof).
FROM ${ESHU_ALPINE_IMAGE} AS whisper
ARG WHISPER_CPP_TAG
ARG WHISPER_CPP_COMMIT
RUN apk add --no-cache build-base=0.5-r3 cmake=3.31.1-r0 git=2.47.3-r0
RUN git clone --depth 1 --branch "${WHISPER_CPP_TAG}" https://github.com/ggml-org/whisper.cpp /src \
    && test "$(git -C /src rev-parse HEAD)" = "${WHISPER_CPP_COMMIT}"
RUN cmake -S /src -B /build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
        -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF -DWHISPER_BUILD_TESTS=OFF \
        -DWHISPER_BUILD_SERVER=OFF -DWHISPER_SDL2=OFF -DWHISPER_FFMPEG=OFF -DWHISPER_CURL=OFF \
    && cmake --build /build --target whisper-cli -j "$(nproc)" \
    && install -D -m 0555 /build/bin/whisper-cli /out/whisper-cli

# model: fetches ggml-base.en.bin at the pinned Hugging Face revision and
# verifies its sha256 at build time (curl pipe instead of the BuildKit-only
# ADD --checksum so the classic builder stays supported).
FROM ${ESHU_ALPINE_IMAGE} AS model
ARG WHISPER_MODEL_SHA256
ARG WHISPER_MODEL_REVISION
RUN apk add --no-cache curl \
    && mkdir -p /out \
    && curl -fsSL --retry 3 --max-time 300 -o /out/ggml-base.en.bin \
        "https://huggingface.co/ggerganov/whisper.cpp/resolve/${WHISPER_MODEL_REVISION}/ggml-base.en.bin" \
    && echo "${WHISPER_MODEL_SHA256}  /out/ggml-base.en.bin" | sha256sum -c -

# go-caller: statically linked eshu-collector-media-import. The caller
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
    && go build -trimpath -ldflags="${LDFLAGS}" -o /go-bin/eshu-collector-media-import ./cmd/collector-media-import

# Runtime: pinned engine plus caller. The apk floors below are the
# fail-closed drift guard scripts/verify-apk-floors.sh checks with
# --dockerfile. The exact pins live in
# specs/satellite-pins/transcription.<arch>.json and the RUN digest gate
# enforces them. This stage MUST stay last: a plain `docker build -f`
# resolves to the last stage.
FROM ${ESHU_ALPINE_IMAGE} AS runtime
ARG EXPECT_BINARY_SHA256
ARG EXPECT_LIBS_SHA256
ARG EXPECT_MODEL_SHA256
LABEL eshu.satellite="transcription" eshu.satellite.engine="whisper.cpp" eshu.satellite.engine-version="v1.9.5"
# musl is named explicitly, not left transitive: the base layer ships its
# own musl and apk keeps it unless asked, so an unnamed musl floats with
# the base image instead of the floor set (r11 stayed installed while the
# index served r12, which the digest gate caught). Every ldd-resolved
# package must be a named floor.
RUN apk add --no-cache "libstdc++>=14.2.0-r4" "libgcc>=14.2.0-r4" "musl>=1.2.5-r12" \
    && mkdir -p /opt/whisper/bin /opt/whisper/models /data
COPY --from=whisper /out/whisper-cli /opt/whisper/bin/whisper-cli
COPY --from=model /out/ggml-base.en.bin /opt/whisper/models/ggml-base.en.bin
COPY --from=go-caller /go-bin/eshu-collector-media-import /usr/local/bin/eshu-collector-media-import
RUN chmod 0555 /opt/whisper/bin/whisper-cli /usr/local/bin/eshu-collector-media-import \
    && chmod 0444 /opt/whisper/models/ggml-base.en.bin
# Build-time digest gate over the engine binary, the ldd-resolved libraries
# as a set (same "<path> <hex>" sorted-lines formula the OCR engine
# records; kept here so the two satellite gates stay comparable), and the
# model. The whispercpp engine re-verifies the binary and model digests at
# construction; this gate fails the image build before that.
RUN set -eu; \
    [ -n "${EXPECT_BINARY_SHA256:-}" ] || { echo "GATE FAIL: EXPECT_BINARY_SHA256 build arg is required (see specs/satellite-pins/transcription.<arch>.json)" >&2; exit 1; }; \
    [ -n "${EXPECT_LIBS_SHA256:-}" ] || { echo "GATE FAIL: EXPECT_LIBS_SHA256 build arg is required (see specs/satellite-pins/transcription.<arch>.json)" >&2; exit 1; }; \
    [ -n "${EXPECT_MODEL_SHA256:-}" ] || { echo "GATE FAIL: EXPECT_MODEL_SHA256 build arg is required (see specs/satellite-pins/transcription.<arch>.json)" >&2; exit 1; }; \
    got="$(sha256sum /opt/whisper/bin/whisper-cli | awk '{print $1}')"; \
    [ "${got}" = "${EXPECT_BINARY_SHA256}" ] || { echo "GATE FAIL: whisper-cli sha256:${got} != pinned ${EXPECT_BINARY_SHA256}; the binary is toolchain-dependent, re-record the reference (see docker/satellites/README.md)" >&2; exit 1; }; \
    got="$(sha256sum /opt/whisper/models/ggml-base.en.bin | awk '{print $1}')"; \
    [ "${got}" = "${EXPECT_MODEL_SHA256}" ] || { echo "GATE FAIL: ggml-base.en.bin sha256:${got} != pinned ${EXPECT_MODEL_SHA256}; re-record specs/satellite-pins (see docker/satellites/README.md)" >&2; exit 1; }; \
    libs="$(ldd /opt/whisper/bin/whisper-cli | awk '{ if ($2 == "=>") print $3; else print $1 }' | xargs -n1 readlink -f | LC_ALL=C sort -u | xargs sha256sum | awk '{print $2" "$1}' | LC_ALL=C sort)"; \
    [ -n "${libs}" ] || { echo "GATE FAIL: ldd resolved no libraries for /opt/whisper/bin/whisper-cli" >&2; exit 1; }; \
    printf 'pinned runtime libraries:\n%s\n' "${libs}"; \
    got="$(printf '%s\n' "${libs}" | sha256sum | awk '{print $1}')"; \
    [ "${got}" = "${EXPECT_LIBS_SHA256}" ] || { echo "GATE FAIL: library set sha256:${got} != pinned ${EXPECT_LIBS_SHA256}; re-record specs/satellite-pins (see docker/satellites/README.md)" >&2; exit 1; }; \
    echo "GATE PASS: transcription satellite pins hold (binary, model, $(printf '%s\n' "${libs}" | wc -l) libraries)"
USER 65532:65532
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/eshu-collector-media-import"]
