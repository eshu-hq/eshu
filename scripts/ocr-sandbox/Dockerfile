# OCR sandbox proof image (#9728). Not a product image: the runtime image in
# the repository-root Dockerfile does not ship tesseract.
#
# Stage "fixtures" renders the proof images. Stage "proof" holds only the
# pinned tesseract binary, the pinned eng.traineddata, the fixtures, and the
# statically linked engine test binary built by scripts/prove-ocr-sandbox.sh.
# Network is used only while building; the proof run uses --network none.

ARG ESHU_ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

FROM ${ESHU_ALPINE_IMAGE} AS fixtures
RUN apk add --no-cache "imagemagick=7.1.1.41-r0" "imagemagick-jpeg=7.1.1.41-r0" \
        "imagemagick-webp=7.1.1.41-r0" "font-dejavu=2.37-r5"
# Mixed-case proportional text (DejaVu Sans) plus two synthetic PII canaries
# that ocr.Extract must redact. Keep these lines in sync with
# liveFixtureLines in go/internal/collector/document/ocr/tesseract/sandbox_live_test.go.
RUN mkdir /fixtures && cd /fixtures \
    && magick -size 900x200 xc:white -font /usr/share/fonts/dejavu/DejaVuSans.ttf \
        -pointsize 28 -fill black \
        -annotate +20+50 "Deploy the Checkout Service to staging" \
        -annotate +20+100 "password: Hunter2CanarySecret" \
        -annotate +20+150 "Contact alice.canary@example.com today" \
        base.png \
    && magick base.png -strip fixture.png \
    && magick base.png -strip -quality 92 fixture.jpg \
    && magick base.png -strip fixture.gif \
    && magick base.png -strip -define webp:lossless=true fixture.webp \
    && rm base.png

FROM ${ESHU_ALPINE_IMAGE} AS proof
RUN apk add --no-cache "tesseract-ocr=5.5.0-r0" "tesseract-ocr-data-eng=5.5.0-r0" \
        "leptonica=1.84.1-r0" "libwebp=1.4.0-r0"
COPY --from=fixtures /fixtures/ /ocr/fixtures/
COPY ocr-sandbox.test /ocr/ocr-sandbox.test
USER 65532:65532
WORKDIR /ocr
ENTRYPOINT ["/ocr/ocr-sandbox.test"]
