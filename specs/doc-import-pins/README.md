# Document-Import Job Image Pins

Single source for the `ESHU_DOC_IMPORT_TESSERACT_*` values consumed by
`go/cmd/collector-document-import` (Event-A one-shot Job, gate
[#9382](https://github.com/eshu-hq/eshu/issues/9382), drive #8879). Closes
the "Pins in the product image" residual in
`docs/internal/design/9728-ocr-engine-backend.md` §6: the
`job-document-import` Dockerfile target verifies these pins at build time
(`RUN sha256sum -c` over the binary, the 15 libraries, and the model), and
the engine re-verifies them at startup. Nothing here is duplicated anywhere
else: the Dockerfile carries only the apk package versions, never digests.

## Files

| File | Content |
| --- | --- |
| `tesseract-5.5.0.arm64.sha256` | `sha256sum -c` input for `linux/arm64`: binary + 15 libraries + model |
| `tesseract-5.5.0.amd64.sha256` | `sha256sum -c` input for `linux/amd64`: binary + 15 libraries + model |

Each file holds 17 lines: `/usr/bin/tesseract`, the 15 shared objects
`ldd` lists for it (resolved, `LC_ALL=C sort -u`), and
`/usr/share/tessdata/eng.traineddata`. No comments or blank lines:
BusyBox `sha256sum -c` is the consumer and comment handling is not portable.

## Provenance

Engine release `5.5.0`, from the digest-pinned Alpine 3.21 base
(`alpine:3.21@sha256:48b0309c…`) plus these exact, Alpine-signed apk
versions (same 16 packages as `scripts/ocr-sandbox/Dockerfile`, same
versions on both arches):

`tesseract-ocr=5.5.0-r0`, `tesseract-ocr-data-eng=5.5.0-r0`,
`leptonica=1.84.1-r0`, `libwebp=1.4.0-r0`, `libwebpmux=1.4.0-r0`,
`libsharpyuv=1.4.0-r0`, `musl=1.2.5-r12`, `libgcc=14.2.0-r4`,
`libgomp=14.2.0-r4`, `libstdc++=14.2.0-r4`, `giflib=5.2.2-r2`,
`libjpeg-turbo=3.0.4-r0`, `libpng=1.6.59-r0`, `tiff=4.7.2-r0`,
`zlib=1.3.2-r1`, `zstd-libs=1.5.6-r2`.

Digests were observed from a fresh install of exactly those packages and
cross-checked against the independent oracle in
`scripts/prove-ocr-sandbox.sh`: binary and model digests match its
`EXPECTED_*_SHA256` entries per arch, and the SHA-256 over sorted
`<path> <sha256>` library lines reproduces its `EXPECTED_LIBS_SHA256`
(`012ca858…` arm64, `74ed1eae…` amd64). Those two aggregates are derived,
not pinned: recompute them from the `.sha256` files, never the reverse.

## Environment mapping

The operator exports these from the pin file for the Job's arch
(`ARCH` is `arm64` or `amd64`; `PIN=tesseract-5.5.0.${ARCH}.sha256`):

| Variable | Value from the pin file |
| --- | --- |
| `ESHU_DOC_IMPORT_TESSERACT_BINARY_PATH` | `/usr/bin/tesseract` (first line's path) |
| `ESHU_DOC_IMPORT_TESSERACT_BINARY_SHA256` | first line's digest |
| `ESHU_DOC_IMPORT_TESSERACT_LIBRARIES` | library lines as `path=digest`, `;`-separated (swap the two columns, join with `;`) |
| `ESHU_DOC_IMPORT_TESSERACT_TESSDATA_DIR` | `/usr/share/tessdata` (model line's directory) |
| `ESHU_DOC_IMPORT_TESSERACT_MODEL_SHA256` | model line's digest |
| `ESHU_DOC_IMPORT_TESSERACT_ENGINE_VERSION` | `5.5.0` |

`ESHU_DOC_IMPORT_ENABLED=true`, `ESHU_DOC_IMPORT_TIER=qa`,
`ESHU_DOC_IMPORT_ARTIFACT_DIR`, and the optional scope/source-system/cap
variables stay operator-owned: they are deployment decisions, not pins, and
are documented in `go/cmd/collector-document-import/README.md`.

Deriving `ESHU_DOC_IMPORT_TESSERACT_LIBRARIES` from a pin file:

```sh
ARCH=arm64
awk 'NR > 1 && $2 != "/usr/share/tessdata/eng.traineddata" \
  { printf "%s%s=%s", (NR > 2 ? ";" : ""), $2, $1 } END { print "" }' \
  "tesseract-5.5.0.${ARCH}.sha256"
```

## Verification

- Build time: `Dockerfile` target `job-document-import` runs
  `sha256sum -c` on the arch-matching file; any drift fails the build.
- Index drift: `scripts/verify-apk-floors.sh --stage job-document-import`
  audits the exact pins against the served Alpine index.
- Well-formedness: `scripts/verify-doc-import-pins.sh` asserts both arch
  files hold exactly 17 portable `sha256sum -c` records, so a malformed
  pin file fails fast on the PR without a build. Structure only: digest
  correctness is proven by the main/tag dual-arch image build.
- Tools: `scripts/verify-runtime-image-tools.sh` with
  `ESHU_REQUIRED_TOOLS="tesseract eshu-collector-document-import"` asserts
  the exported rootfs ships both.
- Reproducibility: the `doc-import-builder` stage declares
  `ARG SOURCE_DATE_EPOCH` and builds with the same `-trimpath`,
  `-buildid=`, and `xx-go` flags as the main builder.
