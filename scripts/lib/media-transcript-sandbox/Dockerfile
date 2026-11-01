# syntax=docker/dockerfile:1.7
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
#   fixture  - synthesizes the speech fixture offline with espeak-ng. The text
#              carries a password canary that redaction must remove.
#   proof    - alpine runtime with the binary, model, fixture, and the static
#              Go test binary compiled with -tags whispercpp_sandbox.

ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

FROM ${ESHU_ALPINE_IMAGE} AS whisper
ARG WHISPER_CPP_TAG=v1.9.5
ARG WHISPER_CPP_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
RUN apk add --no-cache build-base cmake git
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
RUN apk add --no-cache espeak-ng
RUN mkdir -p /out && espeak-ng -v en-us -s 140 -w /out/speech.wav \
	"Please restart the payments worker before noon. The password is swordfish."

FROM ${ESHU_ALPINE_IMAGE} AS proof
RUN apk add --no-cache libstdc++ libgcc \
	&& mkdir -p -m 0755 /opt/whisper/bin /opt/whisper/models /opt/whisper/fixtures /opt/whisper/proof
COPY --from=whisper /out/whisper-cli /opt/whisper/bin/whisper-cli
COPY --from=model --chmod=0444 /out/ggml-base.en.bin /opt/whisper/models/ggml-base.en.bin
COPY --from=fixture --chmod=0444 /out/speech.wav /opt/whisper/fixtures/speech.wav
COPY --chmod=0555 whispercpp.test /opt/whisper/proof/whispercpp.test
USER 65532:65532
ENTRYPOINT ["/opt/whisper/proof/whispercpp.test"]
