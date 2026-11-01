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
# liveFixtureLines in go/internal/collector/document/ocr/tesseract/sandbox_proof_test.go.
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
# Two form layouts for the label and value rule in ocr.Extract (#9782). In
# form-stacked.png the value is on the line below its label. In
# form-columns.png tesseract reads each column as a block, so the value lands
# five regions after its label, past the reading-order window, on the same
# row. Tesseract keeps a three-row version of this layout on one line per row,
# so it takes the five rows. Keep both in sync with liveFormFixtures in
# sandbox_proof_test.go.
RUN cd /fixtures \
    && magick -size 900x300 xc:white -font /usr/share/fonts/dejavu/DejaVuSans.ttf \
        -pointsize 28 -fill black \
        -annotate +20+50 "Deploy the Checkout Service to staging" \
        -annotate +20+100 "Password:" \
        -annotate +20+150 "Hunter2CanarySecret" \
        -annotate +20+200 "Rotate the staging keys every week" \
        -annotate +20+250 "Ask the platform team for access" \
        -strip form-stacked.png \
    && magick -size 1200x240 xc:white -font /usr/share/fonts/dejavu/DejaVuSans.ttf \
        -pointsize 22 -fill black \
        -annotate +20+40 "Credentials for the staging cluster live" \
        -annotate +20+75 "in the shared vault and rotate weekly" \
        -annotate +20+110 "Password:" \
        -annotate +20+145 "Owner:" \
        -annotate +20+180 "Region:" \
        -annotate +640+40 "The checkout team owns these and they" \
        -annotate +640+75 "expire at the end of each quarter so" \
        -annotate +640+110 "Hunter2CanarySecret" \
        -annotate +640+145 "platform" \
        -annotate +640+180 "useast" \
        -strip form-columns.png

FROM ${ESHU_ALPINE_IMAGE} AS proof
# Every package that owns an object ldd lists for /usr/bin/tesseract (15
# objects, 15 packages, from `apk info -W`), plus the model package, at its
# exact Alpine-signed version. The same versions install on arm64 and amd64.
# A version that leaves the Alpine 3.21 index fails the build instead of
# floating. Keep these in step with the digests in scripts/prove-ocr-sandbox.sh.
RUN apk add --no-cache \
        "tesseract-ocr=5.5.0-r0" "tesseract-ocr-data-eng=5.5.0-r0" \
        "leptonica=1.84.1-r0" "libwebp=1.4.0-r0" "libwebpmux=1.4.0-r0" \
        "libsharpyuv=1.4.0-r0" "musl=1.2.5-r11" "libgcc=14.2.0-r4" \
        "libgomp=14.2.0-r4" "libstdc++=14.2.0-r4" "giflib=5.2.2-r2" \
        "libjpeg-turbo=3.0.4-r0" "libpng=1.6.59-r0" "tiff=4.7.2-r0" \
        "zlib=1.3.2-r0" "zstd-libs=1.5.6-r2"
COPY --from=fixtures /fixtures/ /ocr/fixtures/
COPY ocr-sandbox.test /ocr/ocr-sandbox.test
USER 65532:65532
WORKDIR /ocr
ENTRYPOINT ["/ocr/ocr-sandbox.test"]
