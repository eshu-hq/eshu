# Seed image provenance (lane-built proof, NOT the release pin)

## Local proof build (2026-11-06, this lane)

- Dockerfile: `Dockerfile.seed` in this directory. Production shape:
  a rebuild of the OCR satellite runtime (`docker/satellites/ocr/Dockerfile`)
  plus the seed vehicle (`/usr/local/bin/collector`, the legacy
  `/usr/local/bin/eshu-collector-document-import` and `/collector`
  symlinks, and `/manifest.yaml`). Rebuilt rather than FROM-pulled
  because the OCR product image is not registry-published from this lane;
  byte-equivalence comes from the same base pins, apk floors, pin-gate,
  and static caller build flags.
- Base pins: `alpine:3.21@sha256:48b0309c…` and
  `golang:1.26.10-alpine@sha256:9ce231b2…` (identical to the satellite
  Dockerfile). Engine pins from `specs/satellite-pins/ocr.arm64.json`:
  binary `62a760cd…`, library set `8c31dbcc…` (22 libraries, GATE PASS),
  model `daa0c97d…`, version `5.5.0`.
- Caller binary: `CGO_ENABLED=1 go build -trimpath` with
  `-extldflags '-static'` from the lane tree (includes the `seed` and
  `--sdk-stdio` branches), 26077760 bytes in-image.
- Local digest: `sha256:321c34d944a59d788e260041b912c6348de7def58fd9ad24e88c36b426458519`
  (platform `linux/arm64`; corroborated via `docker image inspect`
  `.Id` plus `RepoDigests` plus the build log's manifest-list line).
- In-image smoke as uid 10001: the three entry vectors resolve to one
  binary (`/collector --version` and the legacy path both report
  `eshu-collector-document-import dev`); `/collector seed --dest`
  writes executable `collector` plus a `manifest.yaml` stamped with the
  digest above (exit 0); a tag-only `--image-ref` fails closed with
  `artifact image must be digest pinned with sha256 and 64 hex
  characters` (exit 1).

## Pin-fill protocol (read before rebuilding)

`manifest.yaml` in the repository pins the digest above. The in-image
`/manifest.yaml` copy keeps the pre-pin bytes by construction: an image
cannot contain its own digest. Only the repository file and the copies
`collector seed` stamps at deploy time carry the authoritative pin. Do
NOT rebuild after the pin fill; a rebuild voids the recorded digest. A
release publish rebuilds from identical inputs (same source, same
pre-pin manifest bytes) and re-pins from its own dispatch run if any
byte differs; the `linux/amd64` publish additionally re-pins the
platform and the arch-specific engine pins.
