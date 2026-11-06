# Document-Import Satellite Images

Product images for the two #8879 document-import satellites, promoted from
the sandbox proof Dockerfiles. Each image pairs one pinned engine with its
disabled-by-default caller, so the binary the caller execs by absolute path
is the binary the image build verified.

| Satellite | Product image | Promoted from | Caller |
| --- | --- | --- | --- |
| OCR | `docker/satellites/ocr/Dockerfile` | `scripts/ocr-sandbox/Dockerfile` | `go/cmd/collector-document-import` (tesseract 5.5.0, `eng.traineddata`) |
| Transcription | `docker/satellites/transcription/Dockerfile` | `scripts/lib/media-transcript-sandbox/Dockerfile` | `go/cmd/collector-media-import` (whisper.cpp v1.9.5, `ggml-base.en.bin`) |

The proof images stay as they are: fixtures plus test binaries for the
sandbox proofs. The product images carry no fixtures and no test binaries;
they carry the caller.

## Layout

OCR runtime:

- `/usr/local/bin/eshu-collector-document-import` — caller, statically linked.
- `/usr/bin/tesseract` — engine binary.
- `/usr/share/tessdata/eng.traineddata` — model.
- The runtime shared-object set: the ldd closure plus the `.so` files of
  floored-but-unlinked packages (`libssl3`, `libcrypto3`), enumerated via
  `apk info -L` — a same-version byte reissue installs silently unless its
  bytes are in the set.

Transcription runtime:

- `/usr/local/bin/eshu-collector-media-import` — caller, statically linked.
- `/opt/whisper/bin/whisper-cli` — engine binary.
- `/opt/whisper/models/ggml-base.en.bin` — model.
- The runtime shared-object set: the ldd closure (`libstdc++`,
  `libgcc_s`, the musl loader; ggml/whisper are static) plus the `.so`
  files of floored-but-unlinked packages (`libssl3`, `libcrypto3`,
  `zlib`), enumerated via `apk info -L`.

Both run as `65532:65532` with a one-shot `ENTRYPOINT` (the caller) and no
port, no health check, and no default environment: without the enable flag
plus tier, directory, and every pin, the caller exits 0 with zero commits.
The caller binaries are built inside the Dockerfile from this source with
`CGO_ENABLED=1 -extldflags '-static'` (tree-sitter needs CGO), the same
shape as the repository-root image builder. The Dockerfiles use no
BuildKit-only syntax, so the classic builder stays supported.

## Pins

`specs/satellite-pins/<satellite>.<arch>.json` is the record of truth per
architecture (`arm64`/`amd64`, matching the prove scripts): binary, library
per-file plus set (linked closure plus apk-listed unlinked `.so` files),
and model sha256 digests with the exact apk versions (transcription also
records the model `revision` its fetch URL resolves). The set hash is
SHA-256 over the sorted `"<path> <hex>\n"` lines, the same formula the OCR
engine records as `libs=sha256:...`, so one hash covers the whole runtime
shared-object set.

## Building

From the repository root. Every digest build arg is required; an empty one
fails the gate closed. Values come from the pin file for the build arch.

```sh
# OCR, arm64 (values from specs/satellite-pins/ocr.arm64.json):
docker build -f docker/satellites/ocr/Dockerfile \
  --build-arg EXPECT_BINARY_SHA256=62a760cd1c23d95211e7c42dddc761c2c9cb9488628941e9be6cd9b6748e9c77 \
  --build-arg EXPECT_LIBS_SHA256=8c31dbcc54632daf9221abacd24f5daa49bb19548bf71b800c5913b4e215a160 \
  --build-arg EXPECT_MODEL_SHA256=daa0c97d651c19fba3b25e81317cd697e9908c8208090c94c3905381c23fc047 \
  --build-arg EXPECT_ENGINE_VERSION=5.5.0 \
  -t eshu-satellite-ocr:arm64 .

# Transcription, arm64 (values from specs/satellite-pins/transcription.arm64.json):
docker build -f docker/satellites/transcription/Dockerfile \
  --build-arg EXPECT_BINARY_SHA256=85e9b7b0771b3c402d89d2969d6f39ccfee463cd22eff3fb4ab919249cd0a099 \
  --build-arg EXPECT_ENGINE_VERSION=v1.9.5 \
  --build-arg EXPECT_LIBS_SHA256=4e3a24b592594204cf6a1e23c36a4a1829d313dcf19e8d9a249c0f771a10349f \
  --build-arg EXPECT_MODEL_SHA256=a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002 \
  --build-arg WHISPER_MODEL_REVISION=5359861c739e955e79d9a303bcbc70fb988958b1 \
  --build-arg WHISPER_MODEL_SHA256=a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002 \
  -t eshu-satellite-transcription:arm64 .
```

## Build-time gate

Each Dockerfile ends its runtime stage with a `RUN sha256sum -c`-shaped
gate (written as explicit comparisons so the failure names the stale pin):
it re-hashes the engine binary, re-resolves the runtime shared-object set
through `ldd` plus `readlink -f` for linked libraries and
`apk info -L <pkg>` for floored-but-unlinked packages, recomputes the set
hash, re-hashes the model, and compares all three (plus the reported
engine version — exact match for tesseract, `whisper.cpp version: <tag>`
prefix match for whisper-cli, verified against the upstream
`WHISPER_CPP_TAG` convention) against the build args. Transcription also
gates the model revision against `WHISPER_MODEL_REVISION` (fail-closed
against a force-pushed tag). Any mismatch fails the build with the re-pin
pointer. The gate
also prints the resolved per-file library list, which is the operator's
source for `ESHU_DOC_IMPORT_TESSERACT_LIBRARIES`. The
`org.opencontainers.image.version` label derives from the engine version
arg the gate just verified, never a hand value.

## Apk floors

The final-stage `apk add` lines carry `name>=version` floors, the same
fail-closed guard as the repository-root image. Check them with the existing
interface (no script change was needed):

```sh
scripts/verify-apk-floors.sh --dockerfile docker/satellites/ocr/Dockerfile
scripts/verify-apk-floors.sh --dockerfile docker/satellites/transcription/Dockerfile
```

The floors float within the Alpine branch on purpose: a security update
installs, and the digest gate then fails the build until the pin files are
re-recorded. Floors are the drift signal; digests are the enforcement.
Every runtime-stage package must be a named floor, linked or not: apk
keeps the base layer's musl unless asked, so an unnamed musl floats with
the base image instead of the floor set (this exact drift failed the
transcription gate once, with r11 installed while the index served r12) —
and an unnamed `libssl3` would install unhashed, defeating the gate
(same-version reissue rule above).

## Re-pin ritual

When the gate or the floor script fails after a repository move:

1. Raise the stale floor to the served version
   (`scripts/verify-apk-floors.sh --dockerfile ...` confirms).
2. Build the image stages and re-measure: `sha256sum` the binary and
   model, list the linked libraries with
   `ldd <binary> | awk '{ if ($2 == "=>") print $3; else print $1 }' | xargs -n1 readlink -f | LC_ALL=C sort -u | xargs sha256sum`,
   list every `.so*` file of each floored-but-unlinked package with
   `apk info -L <pkg>`, hash both lists together, and recompute the set
   hash over the sorted `"<path> <hex>"` lines. Skipping the `apk info -L`
   half leaves same-version reissues invisible to the gate.
3. Write the new digests plus the installed exact apk versions into
   `specs/satellite-pins/<satellite>.<arch>.json` (transcription: also the
   model `revision` the fetch URL now resolves, and keep
   `WHISPER_CPP_TAG` pinned to the `WHISPER_CPP_VERSION` default unless the
   engine itself is upgraded — whisper.cpp has no separate tag; the
   `version: <tag>` the gate matches is compiled from that same
   `WHISPER_CPP_VERSION`, so a tag/version skew fails the build by design).
4. Rebuild with the new args: the gate must print `GATE PASS`.
5. Re-run both `verify-apk-floors.sh --dockerfile` invocations above.

Transcription amd64 now records its binary reference in
`transcription.amd64.json` (#10072): the `whisper` stage was built for that
arch, `/out/whisper-cli` hashed, and the full build gated PASS. A future
re-pin repeats the ritual above; until the new digests land, the gate fails
closed for that arch by design. A model re-pull whose bytes moved without a
revision change is the same case: record the new digest and revision, then
rotate any stored model content downstream.

## Running

The images run one-shot under the gate-2 sandbox profile from the engine
READMEs: `--network none --read-only --cap-drop ALL --security-opt
no-new-privileges:true --user 65532:65532` with memory and PID caps, a
writable noexec `/tmp` mount (the OCR caller scratches there), the artifact
directory mounted read-only, and the enable flag plus tier, directory, and
every engine pin passed as environment. The engines re-verify every digest
at construction; the read-only root is what keeps the verified files from
changing afterwards.

## Trivy

Wiring note (no scan configuration changes in this lane):

- `trivy-fs` already covers these Dockerfiles: it scans the repository
  filesystem on every PR, including Dockerfile misconfiguration checks, and
  this directory is not in `specs/trivy-skip-dirs.txt`. No workflow change
  was needed for that coverage.
- `trivy-image` does NOT cover these images: that job scans only the
  published `ghcr.io/eshu-hq/eshu` image after `docker-publish` runs, and
  these satellites are not published from any workflow yet (the satellites
  lane in `docker-publish.yml` is build-only proof; the selector does match
  `docker/satellites/**`). Wiring image scans needs a publish lane plus a
  `security-scan.yml` entry, which belongs with the deployment lane that
  first publishes a satellite, not here.
- Until then, scan locally before publishing:
  `trivy image --severity CRITICAL,HIGH eshu-satellite-ocr:<arch>` (and the
  transcription tag), with `--exit-code 1` to gate promotion.

## Deliberately not in this lane

No chart, no IaC reference, no image-pin change, no publish lane, no gate
flip. The shared runtime image (repository-root `Dockerfile`) and
`scripts/verify-runtime-image-tools.sh` (`REQUIRED_TOOLS`) are untouched:
the engines ship only in these satellites, never in the shared image, per
the whispercpp packaging decision and the OCR proof header.
