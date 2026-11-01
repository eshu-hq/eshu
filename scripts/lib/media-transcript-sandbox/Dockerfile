# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
#
# Proof-only image for the whisper.cpp media transcript engine sandbox proof
# (#9729). scripts/verify-media-transcript-sandbox.sh builds and runs it. It
# is not a runtime image and is not published.
#
# Stages:
#   whisper  - builds whisper-cli from the pinned v1.9.5 tag commit, static
#              ggml/whisper libraries, CPU only, no ffmpeg/SDL/curl.
#   model    - fetches ggml-base.en.bin at a pinned Hugging Face revision and
#              verifies its sha256 at build time.
#   fixture  - synthesizes the speech fixtures offline with espeak-ng. Both
#              carry a password canary that redaction must remove. speech.wav
#              says marker and value together. speech-split.wav puts an SSML
#              silence between them so the value lands in its own segment
#              (#9786).
#   proof    - alpine runtime with the binary, model, fixture, and the static
#              Go test binary compiled with -tags whispercpp_sandbox.
#
# Pins: the frontend and base image by digest, whisper.cpp by tag plus commit
# check, the model by sha256, and every apk package by exact version
# (resolved from the alpine 3.21 repository on 2026-11-01). Alpine drops
# superseded versions from a stable branch, so a security update makes this
# build fail at `apk add` instead of silently changing packages. Re-pin with:
#   docker run --rm <ESHU_ALPINE_IMAGE> sh -c 'apk update && apk policy <pkg>'
# The built whisper-cli digest is not reproducible across toolchains; the
# proof script hashes the binary it built and verifies that digest in New.

ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

FROM ${ESHU_ALPINE_IMAGE} AS whisper
ARG WHISPER_CPP_TAG=v1.9.5
ARG WHISPER_CPP_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
RUN apk add --no-cache build-base=0.5-r3 cmake=3.31.1-r0 git=2.47.3-r0
RUN git clone --depth 1 --branch "${WHISPER_CPP_TAG}" https://github.com/ggml-org/whisper.cpp /src \
	&& test "$(git -C /src rev-parse HEAD)" = "${WHISPER_CPP_COMMIT}"
RUN cmake -S /src -B /build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
	-DGGML_NATIVE=OFF -DGGML_OPENMP=OFF -DWHISPER_BUILD_TESTS=OFF \
	-DWHISPER_BUILD_SERVER=OFF -DWHISPER_SDL2=OFF -DWHISPER_FFMPEG=OFF -DWHISPER_CURL=OFF \
	&& cmake --build /build --target whisper-cli -j "$(nproc)" \
	&& install -D -m 0555 /build/bin/whisper-cli /out/whisper-cli

FROM ${ESHU_ALPINE_IMAGE} AS model
ADD --checksum=sha256:a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002 \
	https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/ggml-base.en.bin \
	/out/ggml-base.en.bin

FROM ${ESHU_ALPINE_IMAGE} AS fixture
RUN apk add --no-cache espeak-ng=1.51.1-r1
RUN mkdir -p /out && espeak-ng -v en-us -s 140 -w /out/speech.wav \
	"Please restart the payments worker before noon. The password is swordfish."
RUN espeak-ng -v en-us -s 140 -m -w /out/speech-split.wav \
	'The password is as follows. <break time="3s"/> Swordfish.'

FROM ${ESHU_ALPINE_IMAGE} AS proof
RUN apk add --no-cache libstdc++=14.2.0-r4 libgcc=14.2.0-r4 \
	&& mkdir -p -m 0755 /opt/whisper/bin /opt/whisper/models /opt/whisper/fixtures /opt/whisper/proof
COPY --from=whisper /out/whisper-cli /opt/whisper/bin/whisper-cli
COPY --from=model --chmod=0444 /out/ggml-base.en.bin /opt/whisper/models/ggml-base.en.bin
COPY --from=fixture --chmod=0444 /out/speech.wav /opt/whisper/fixtures/speech.wav
COPY --from=fixture --chmod=0444 /out/speech-split.wav /opt/whisper/fixtures/speech-split.wav
COPY --chmod=0555 whispercpp.test /opt/whisper/proof/whispercpp.test
USER 65532:65532
ENTRYPOINT ["/opt/whisper/proof/whispercpp.test"]
