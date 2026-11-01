# MCP satellite re-pin to v0.2.6 (#9731)

Advances the Dockerfile `mcp-satellite` stage and the shared `ESHU_MCP_REF`
default (registry, installer, gate helper, reference doc) from v0.2.5 to the
satellite build of eshu-mcp commit `083294b9f4` (v0.2.6, eshu-mcp#49). The v0.2.5
build refused opaque personal and scoped tokens at its edge under OIDC
(eshu-mcp#48), which failed the `MCP-identity auth E2E` leg on this repository's
`main`. String constants only: no control flow, no query, no queue, lease,
schema, or graph-write change.

The image is pinned by digest `sha256:6dd4397454033b63c6bf63538b8ddc7c001044dbfec25d1bfe288cde1b83d413`.
It is the `sha-083294b9f45fe5a6fd6a098c8bcd4329fcf6ba49`, `main`, and `latest`
image of `ghcr.io/eshu-hq/eshu-mcp` (one digest on all three). The semver tags
`0.2.6` and `v0.2.6` and the GitHub Release do not exist: eshu-mcp's `auto-tag`
workflow pushed the tag `v0.2.6` with the default workflow token, and GitHub
does not start the tag-triggered `release` run from such a push. The
main-branch `release` run (41266921894) built, scanned, and published the image
with the same gates. `ESHU_MCP_REF=v0.2.6` still resolves, because the git tag
exists on the same commit.

No-Regression Evidence: the edited Go line is one registry default string,
read at startup like every other `ESHU_*` default; no request, parse, emit,
or projection path branches on its value. Proven by
`go test ./internal/envregistry/ -count=1` green and
`scripts/verify-env-registry-doc.sh` green (the reference row derives from the
same default). The digest is identical on the three tags above. GitHub's
attestations API returns two attestations for the digest: an SPDX SBOM and SLSA
provenance whose resolved dependency is commit `083294b9f45fe5a6fd6a098c8bcd4329fcf6ba49`.
The attestation signatures were not verified here (NOT_CHECKED): the local `gh`
has no `attestation verify`. Full CI on the PR head re-runs the suite,
including the E2E legs that exercise the new satellite.

No-Observability-Change (in-tree): this repo's edits add, remove, or rename no metric, span, log line, status endpoint, or alert threshold; operators keep the existing installer, gate, and E2E signals. Separately, the pinned satellite runtime adds core admission of forwarded credentials (eshu-mcp#49): its `/healthz` reports `admission` counters (`admitted`, `denied`, `unavailable`, `cache_hit`), and it logs `oidc bearer token denied` with `iss` and `outcome`. That is the adopted runtime behavior under test, proven by the satellite's own gates, and those signals are what the #9731 E2E legs exercise.
