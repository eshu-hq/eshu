# In-Tree Collector Removal: GCP Cloud (#8856)

Date: 2026-11-02. Branch: `drive/8856-remove-in-tree`, base `origin/main`.

## Scope

The production producer is the extracted satellite
(`eshu-hq/eshu-collector-gcp-cloud`), live with parity proven over two
complete plan windows (see #8856). This change deletes the superseded
in-tree producer and repoints every registry, contract, spec, and doc that
named it. Core-owned kernels stay: every file under
`go/internal/collector/gcpcloud/` is still imported by live core code
(reducer, projector, query, replay, contract tests), so those packages are
untouched except for two stale comments.

Deleted in-tree producer:

- `go/cmd/collector-gcp-cloud/` (binary, claimed/file config, service,
  status committer, tests, CAI fixture pages, offline config, redaction
  key fixture)
- Product-image builds: `Dockerfile` (1 line),
  `scripts/install-local-binaries.sh` (1 line), `go/cmd/README.md` (1 line)

Repointed (satellite owns the producer; kinds, projection, and read
surfaces stay core-owned): fact producer record (`gcp.go`),
fact-kind family shards (`families/gcp.md`, `families/gcp.yaml`), surface
inventory (owner, notes, proof gates) and the generated capability
inventory, golden-corpus gate (spec row removed, `gcp-cassette-replay`
pagerduty alias added) and its static test, env registry entries and
generated docs, Helm contract test (binary-doc test deleted), and the
service/collector/contract/install docs plus the extraction census status.

## Verification matrix

No-Regression Evidence: this change is strictly subtractive — it deletes
the in-tree producer and repoints registries/specs/docs, adding no Cypher,
worker claim, lease, batching, concurrency, or runtime compose/Helm path.
Baseline is the pre-removal tree at the branch base; after-measurement is
the removal head. Backend/version: local Go toolchain, no backend
input-shape change (no new facts, scopes, or queue shapes). Terminal
counts: `go build ./...` clean, `go vet` on touched packages clean,
`go test` and `go test -race` on changed packages PASS, fact-kind registry
`verify` clean, golden-corpus static test PASS, Helm GCP tests PASS with
`ESHU_DEPLOY_CHECKOUT` at the pinned commit, docs build plus citations
and CLI env-refs gates OK, factschema-diff, payload-usage-manifest, and
contracttest gates OK. Safe because every deleted code path is gone from
the product image with no live callers after the cutover, every kernel
change is comment-only, and the `gcpcloud` cassette still replays in the
gate through the pagerduty alias, so the projected corpus is unchanged.

Observability Evidence: the in-tree command's operator status server and
metrics endpoint leave with the deleted binary; no other in-tree emitter
used them. All runtime collector instruments live in the kept
`gcpcloud`/`gcpruntime` kernel and their call sites are unchanged. No
metric, span, or status-signal name changed. The satellite carries its own
claim/generation telemetry, exercised in the two live parity windows.

| Check | Result |
| --- | --- |
| `go build ./...` at removal head | clean |
| `go vet` + `gofumpt` + lint on changed packages | clean |
| `go test`, `go test -race` (changed packages) | PASS |
| Fact-kind registry `verify` | clean (generator output committed) |
| Golden-corpus static test | PASS |
| Helm GCP collector tests | PASS |
| Full Docker golden-corpus run | NOT_CHECKED (needs live-gate mutex and backends) |

## Live-source status (2026-11-02)

- gcp-cloud satellite v0.1.4: two consecutive complete runs, 105
  `gcp_cloud_resource` nodes with 27 relationships and 4 tags per run
  (136 facts/run), API/graph agreement on both project ID forms.
- Operator activation PR merged and synced; worker on the 0.1.4 producer
  version since 19:40 UTC.

## Follow-up

- Close eshu-hq/eshu#8856 on merge with the two-window parity evidence.
- Rollback is roll-forward: the satellite is already the production
  producer; the in-tree binary had no live callers.
