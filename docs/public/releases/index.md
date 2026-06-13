# Release Log

This log records Eshu releases that change the CLI, MCP/API behavior, graph
runtime, deployment defaults, or published artifacts.

## Releases

- Current prerelease train: [`v0.0.3-pre-release-*`](../roadmap.md). Stable
  `v0.0.3` has not been cut yet; the public roadmap lists the gates that still
  need runtime, collector, API/MCP, deployment, and performance proof.
- [v0.0.2](v0.0.2.md) - Dead-code finding matures for Go and Node/TypeScript,
  local runtime behavior gets faster again, and release artifacts move to the
  `v0.0.2` tag.

## Artifact Verification

Release container images published by `.github/workflows/docker-publish.yml`
are keylessly signed with Sigstore cosign. The workflow also publishes build
provenance and SPDX SBOM attestations for the pushed image digest, and tag
releases attach the generated image SBOM to the GitHub Release.

For a tag release, replace `<tag>` with the exact release tag:

```bash
TAG=<tag>
IMAGE="ghcr.io/eshu-hq/eshu:${TAG}"
REPO=eshu-hq/eshu
WORKFLOW_IDENTITY="https://github.com/eshu-hq/eshu/.github/workflows/docker-publish.yml@refs/tags/${TAG}"

cosign verify "${IMAGE}" \
  --certificate-identity "${WORKFLOW_IDENTITY}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com"

gh attestation verify "oci://${IMAGE}" -R "${REPO}"

gh attestation verify "oci://${IMAGE}" \
  -R "${REPO}" \
  --predicate-type https://spdx.dev/Document/v2.3

gh release download "${TAG}" \
  --repo "${REPO}" \
  --pattern 'eshu-image-sbom.spdx.json'
```

For non-tag images published from `main`, use
`refs/heads/main` in `WORKFLOW_IDENTITY`.
