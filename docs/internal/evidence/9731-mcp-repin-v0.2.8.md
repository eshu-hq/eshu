# MCP satellite re-pin to v0.2.8 (#9731)

Advances the Dockerfile `mcp-satellite` stage and the shared `ESHU_MCP_REF`
default (registry, installer, gate helper, reference doc) from v0.2.5 to the
published v0.2.8 satellite build (eshu-hq/eshu-mcp#27). v0.2.8 is satellite
commit `8d32de38472ee452e50834a4f6913a9b6d4c4a3c`, which contains
eshu-mcp#49 (opaque Bearer passthrough and core admission) and eshu-mcp#51
(SSE caps, canonical principals). The v0.2.5 pin fails the
`call-frontend / MCP-identity auth E2E` leg (initialize with a personal token
expected 200, got 401; oidcbearer denial outcomes missing from mcp-server
logs). Product runtime: string constants only, with no control flow, query,
queue, lease, schema, or graph-write change in this repo.

The same range also changes the E2E harness, not the product. On the v0.2.8
pin the leg still failed one step because the scripted-OAuth `tools/call`
proof was vacuous (it read `structuredContent.total` instead of
`data.total`, so an `unauthenticated` envelope looked like `total=0`), and
core had no `ESHU_AUTH_RESOURCE_URI` in the pinned E2E compose, so it could
not verify the JWT the satellite forwards (eshu#9760). The range makes that
step fail closed on an error envelope and wait for core to pick up the new
OIDC provider (60s budget, one attempt every 2s, against core's 30s
provider-snapshot TTL in `go/internal/oidcbearer/resolver.go`), and moves both
`frontend.yml` eshu-deploy checkouts to `1e306ec5`, the merge of eshu-deploy
PR #25 that sets the variable on the core service.

Pinned digest: `sha256:fb370fd3ff8ef8cf19804bf2d1535cfd06d4ef275a165766611b3e658b9cc489`
(`ghcr.io/eshu-hq/eshu-mcp` tags `0.2.8`, `0.2`, `0`, `latest`).

No-Regression Evidence: the edited Go line is one registry default string,
read at startup like every other `ESHU_*` default; no request, parse, emit,
or projection path branches on its value. Proven by
`go test ./internal/envregistry/ -count=1` and
`scripts/verify-env-registry-doc.sh` (the reference row derives from the same
default). The pinned digest was checked locally: the sha256 of
`docker buildx imagetools inspect ghcr.io/eshu-hq/eshu-mcp:<tag> --raw`
equals the pinned digest for tags 0.2.8, 0.2, 0 and latest, and the v0.2.8
Release body records the same digest. Attestations were not verified from
this host: the local `gh` (2.46.0) has no `attestation` subcommand. The
eshu-mcp release run for v0.2.8 (run 41273208189, a tag push) ran
`gh attestation verify` on the pushed image in its `push` job, step "Verify
image attestations", and that step succeeded for both the provenance and the
SBOM attestation. Full CI on the PR head re-runs the suite, including the E2E
legs that exercise the new satellite; those results decide whether the pin
fixes the MCP-identity leg.

No-Observability-Change (in-tree): this repo's edits add, remove, or rename
no metric, span, log line, status endpoint, or alert threshold; operators
keep the existing installer, gate, and E2E signals. Separately, the pinned
satellite runtime emits oidcbearer denial outcomes in its own logs (adopted
runtime behavior under test, owned by the satellite's gates), and those are
the signals the #9731 MCP-identity E2E leg inspects.
