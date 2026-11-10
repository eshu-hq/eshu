# C9 release provenance record — eshu-workflow v0.1.1 (Refs eshu-hq/eshu#8875)

Image: `ghcr.io/eshu-hq/eshu-workflow`, tag `v0.1.1`,
digest `sha256:e1b564afa5f72167e3911aea7c881973c74bab123183c03d9d6af5a2d80e47a4`.

## Provenance

- SLSA v1 provenance, builder
  `image-publish.yml@refs/tags/v0.1.1` in `eshu-hq/eshu-workflow`
  (file verified present at the tag), source commit `8311ed69`.
- Build steps at the tag: `attest-build-provenance`, cosign sign,
  in-pipeline cosign verify, SBOM + SBOM attestation.
- Level wording: hosted-builder (GitHub-hosted OIDC). No SLSA level-3
  claim is made anywhere in tree (verified by grep; see proof).
- `v0.1.0` resolves `MANIFEST_UNKNOWN` and is barred from all pins.

## Verification

- `gh attestation verify oci://ghcr.io/eshu-hq/eshu-workflow:v0.1.1
  --repo eshu-hq/eshu-workflow` → exit 0 (2026-11-05 and 2026-11-10).
- Per-digest verify on `sha256:e1b564af…` → exit 0 (2026-11-10),
  including separate `--predicate-type` runs for
  `https://slsa.dev/provenance/v1` (exit 0) and
  `https://spdx.dev/Document/v2.3` (exit 0).
- Host cosign re-verify: NOT_CHECKED (no cosign binary on the drive
  host); the pipeline's own cosign verify step covers the signature
  in CI. A host with cosign should re-run
  `cosign verify` against the digest above before pinning elsewhere.

## Staleness

Re-verify per digest on any image, tag, or pipeline change. A new
digest needs a new row in this record, not an edit of this one.
