# Seed image provenance (lane-built proof, NOT the window pin)

## Local proof build (2026-11-05, this lane)

- Dockerfile: `Dockerfile.seed` in this directory (production shape:
  `FROM <pinned release worker image>` + emitter layer).
- Local base override (proof only): `alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d`
  (public egress works; the private release image `ghcr.io/eshu-hq/eshu:0650a6a4`
  is not resolvable from this lane — registry auth stays a window step).
- Emitter binary: `CGO_ENABLED=0 go build -trimpath` from the lane tree,
  sha256 `9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495`.
- Build flags: `--provenance=true --sbom=true` (SLSA + SBOM attestations
  embedded by buildx), `--load` for local inspection.
- Local digest: `sha256:ab8179d648cf7184c8e8a206ee5d170b728d05d68b3267ead49669bc75487798`
  (rebuilt after the DS-0002 fix: `COPY --chmod=755` + `USER 10001:10001`;
  first local build was `caa7110c…`, superseded).
- In-image smoke: piped a synth request to
  `/usr/local/bin/eshu-collector-synth-zero` in the built image running as
  uid 10001 (`id` confirms `uid=10001 gid=10001`); got `unchanged` with the
  claim echoed and one complete status row, zero facts.

## Window assembly (dispatch `.github/workflows/seed-publish.yml`, NOT run in-lane)

1. Base is pinned in-tree: `Dockerfile.seed` `BASE_IMAGE` carries the full
   live digest re-read at lane build time (tag `sha-7b3d6999…`, digest
   `ae20f270…`; the Stage A tag `0650a6a4` / prefix `2eca6e9f` is superseded
   by the re-baseline — never trust that string, re-read kubectl).
2. Dispatch the workflow (workflow_dispatch only): it builds with
   `--provenance=true --sbom=true`, pushes `ghcr.io/eshu-hq/eshu-synth-zero`,
   runs the in-image smoke on the pushed digest as uid 10001, and emits the
   digest as a job output. If the push step is denied, stop and re-escalate.
3. Fill the emitted digest into `manifest.yaml` (artifacts) and
   `deployment.yaml` (image) in place of the zeros placeholders, re-run the
   pin suite, and commit the pin before any apply.

## Window assembly record (run 42049436432, 2026-11-05, SUCCESS)

- Pushed: `ghcr.io/eshu-hq/eshu-synth-zero@sha256:145dbbf95cc5971c5a6607dccf455fc742fb11f37c21a339972ca2c5a17c8a93`
  (tag `seed-969bc20940406612ebfe15f730d3c332e350fd58`, linux/amd64+arm64).
- Base resolved in-tree: `ghcr.io/eshu-hq/eshu:sha-7b3d6999ee3060257aa1b579e110b2efcb399f42@sha256:ae20f27008b28a206893d08e5c2901f7fe23dfb8cab0d128bc91e0ad65541ff3`
  — the live fleet digest, no new live drift.
- In-image smoke on the pushed digest as uid 10001: `unchanged`, one
  complete status row, zero facts (`"facts": []` on the wire).
- Attestation: subject-digest equals the pushed digest (run log).

The zeros placeholders keep the shape valid until then; `TestWindow*`
enforces the digest pattern so the fill-in is mechanical. The local image
was removed after the smoke (`docker rmi`); the digest above is the record.
