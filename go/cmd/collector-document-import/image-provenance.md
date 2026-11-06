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
`golang:1.26.10-alpine@sha256:9ce231b2…` (identical to the satellite
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

## Pin-fill protocol (read before rebuilding)

`manifest.yaml` in the repository pins the digests above. The in-image
`/manifest.yaml` copy keeps the pre-pin bytes by construction: an image
cannot contain its own digest. Only the repository file and the copies
`collector seed` stamps at deploy time carry the authoritative pin. Do
NOT rebuild after the pin fill; a rebuild voids the recorded digests. A
release publish rebuilds from identical inputs (same source, same
pre-pin manifest bytes) and re-pins from its own dispatch run if any
byte differs.
