# Seed image provenance (integration builds, NOT the release pin)

## Integration proof builds (2026-11-06)

Built from the integration branch with pin-derived expect args, both gates
PASS per arch (22 libraries, GATE PASS lines in the build logs):

- `seed-document-import:amd64-prove`:
  `sha256:95b253e6df0ab75af737209fe7667ebf3855381ea47fd91f9b0837c5911b7ed8`
- `seed-document-import:arm64-prove`:
  `sha256:3926a2cabd8d5c608a21d7ca42d038ecfc916e04fdc08836807c9257731d6404`

Dockerfile: `Dockerfile.seed` in this directory. Production shape:
a rebuild of the OCR satellite runtime (`docker/satellites/ocr/Dockerfile`)
plus the seed vehicle (`/usr/local/bin/collector`, the legacy
`/usr/local/bin/eshu-collector-document-import` and `/collector`
symlinks, and `/manifest.yaml`). Rebuilt rather than FROM-pulled
because the OCR product image is not registry-published from this lane;
byte-equivalence comes from the same base pins, apk floors, pin-gate,
and static caller build flags.

Base pins: `alpine:3.21@sha256:48b0309c…` and
`golang:1.26.11-alpine@sha256:ae52b5bc…` (identical to the satellite
Dockerfile). Engine pins per arch from
`specs/satellite-pins/ocr.{amd64,arm64}.json` (binary, 22-library set,
model, version `5.5.0` — all asserted by the in-image gates).

Caller binary: `CGO_ENABLED=1 go build -trimpath` with
`-extldflags '-static'` from the integration tree (includes the `seed`
and `--sdk-stdio` branches).

Image naming: the seed images publish as
`ghcr.io/eshu-hq/eshu-collector-document-import` (the `eshu-collector-*`
sibling convention shared with the loki/tempo activations), superseding
the lane-local `eshu-document-import` name. `manifest.yaml` pins one
digest-pinned artifact entry per platform, filled with the digests
above.

In-image smoke: `/collector --version` and the legacy path resolve to
one binary; `/collector seed --dest` writes executable `collector` plus
a `manifest.yaml` stamped with the passed digest (exit 0); a tag-only
`--image-ref` fails closed (exit 1).

## Republish builds (2026-11-07, merged tree)

Built from `origin/main` at the #10319 merge (`791f45842c`), which adds the
seeded engine payload (bin, flat lib, tessdata) to the seed. Same
pin-derived expect args and `Dockerfile.seed`; all four in-image gates
PASS (22-library set per arch). Published tags
`:seed-791f45842c-amd64` / `-arm64`:

- `seed-document-import:amd64`:
  `sha256:e8d2297bfeacea416d1bb0ec9ccb09d5a27060591042f5572225cf34502efead`
- `seed-document-import:arm64`:
  `sha256:c1bf5ce2fd46038d82db5823d76429137d258fe1d276dd98b94917b04d613db4`

`manifest.yaml` in the repository is re-pinned to the digests above (media
lane: `sha256:68636a8e8bf2c4f3ca2b037505e8d2ee5d28cc0050eae0f29d1554dbd128111a`
amd64,
`sha256:825c7b3acef7617f14787c6fd007296902f5716f299012e9b1b33530c2c5b688`
arm64, same build convention). Do NOT rebuild after this fill.

## Republish builds (2026-11-07, merged tree at #10329)

Built from `origin/main` at the #10329 merge (`ba038ecaf2`), which adds the
stale soname-link sweep on re-seed. Same pins, Dockerfiles, and gate
convention as the #10319 republish; all in-image gates PASS per arch
(whisper/media stages cache-hit on identical inputs; caller stage rebuilt
with the sweep). Published tags `:seed-ba038ecaf2-amd64` / `-arm64`:

- `seed-document-import:amd64`:
  `sha256:2e813571b2e23367cbf09e4fb8c1bff6028a81567c8d55001511a8e8f8a40ca5`
- `seed-document-import:arm64`:
  `sha256:e15c3b8f1597537ca2661e6fbf5a26c2515a3c818be77e2f9e0e8770ae0e9d5d`

Media lane (same build convention, tags `:seed-ba038ecaf2-amd64` / `-arm64`):

- `seed-media-import:amd64`:
  `sha256:79db3b3d7f044e266a631d4d838ffce4d66dced25f9bc9102fb32d3ac090b801`
- `seed-media-import:arm64`:
  `sha256:c8dc6397f47e27602e2ec57a87c152f92d9b278edcf890a6e784e3b6956d7dd3`

Artifact smoke (2026-11-07): `collector seed` from the pushed arm64 doc
image exits 0 and the seeded `engine/lib` carries relative soname links
(`libtesseract.so.5 -> libtesseract.so.5.0.5`,
`libleptonica.so.6 -> libleptonica.so.6.0.0`); tag digests verified with
an independent registry read. Do NOT rebuild after the pin fill.

## Republish builds (2026-11-07, merged tree at #10336)

Built from `origin/main` at the #10336 merge (`dce4ab7ef5`), which seeds the
whisper runtime libs (`libstdc++.so.6.0.33`, `libgcc_s.so.1`) into the media
payload. Media images rebuilt per arch with all in-image gates PASS (arm64
against re-recorded libz 1.4 pins per #10339; amd64 likewise after its own
re-record in the same PR). Published tags `:seed-dce4ab7ef5-amd64` / `-arm64`:

- `seed-media-import:amd64`:
  `sha256:bae515ec1b7543696a1641eccbfa7c5e6c73220702573c54690f771eee455ce4`
- `seed-media-import:arm64`:
  `sha256:348d7ae76d875e52eb95ebfc25ede0599dc959e7d4fd74a0a2c3efdba98bfb22`

Tag digests verified with independent registry reads. Doc images unchanged
(`:seed-ba038ecaf2` still current). Do NOT rebuild after the pin fill.

## Pin-fill protocol (read before rebuilding)

`manifest.yaml` in the repository pins the digests above. The in-image
`/manifest.yaml` copy keeps the pre-pin bytes by construction: an image
cannot contain its own digest. Only the repository file and the copies
`collector seed` stamps at deploy time carry the authoritative pin. Do
NOT rebuild after the pin fill; a rebuild voids the recorded digests. A
release publish rebuilds from identical inputs (same source, same
pre-pin manifest bytes) and re-pins from its own dispatch run if any
byte differs.
