---
name: eshu-golden-corpus-rigor
description: Use when changing anything the B-7 golden-corpus gate asserts — collector fact emission, reducer/projector graph writes, correlation/materialization output, query or MCP tool response shapes, fact-kind or schema constants, or a new verb/edge/node/correlation — and when editing the cassettes (testdata/cassettes/), the B-12 snapshot (testdata/golden/e2e-20repo-snapshot.json), or scripts/verify-golden-corpus-gate.sh, or when the golden-corpus gate goes red. The cassettes plus the snapshot are Eshu's golden standard; a code change that alters projected truth without updating them in the same change fails the gate.
---

# eshu-golden-corpus-rigor

The B-7 golden-corpus gate is Eshu's golden standard: it replays fixed inputs
through the real pipeline (`sync -> discover -> parse -> collect -> reduce ->
project -> query`) and asserts the resulting graph and query truth against a
committed contract. Two artifacts ARE that contract:

- **Cassettes** — `testdata/cassettes/<collector>/supply-chain-demo.json`: the
  recorded facts each credentialed collector replays (credential-free).
- **B-12 snapshot** — `testdata/golden/e2e-20repo-snapshot.json`: the required
  correlations, node/edge count tolerances, and HTTP + MCP query shapes the gate
  diffs against.

Both are driven by `go/cmd/golden-corpus-gate/` and run by
`scripts/verify-golden-corpus-gate.sh`.

## Operating rule

**If your change alters what the pipeline projects, update the cassette and/or
the snapshot in the SAME change, and prove it with the gate.** The gate exists to
fail when code and the golden fixtures drift — that is the feature, not a
nuisance. Do not "fix" a red gate by loosening an assertion; fix the code or
update the fixture to the new, correct truth under review.

## Does my change touch the golden corpus?

Ask this before you finish any change to these surfaces:

- **Collector fact emission** (`go/internal/collector/*`): new/changed fact kind,
  payload field, or evidence kind → the matching cassette must carry the new
  shape, or the gate runs against stale recorded facts.
- **Reducer / projector graph writes** (`go/internal/reducer/*`, `projector/*`,
  `storage/cypher/*`): new/changed node label, edge type, correlation, or
  property → add or adjust the snapshot's `required_correlations` / `node_counts`
  / `edge_counts` / required-node-property assertions.
- **Query or MCP tool response shape** (`go/internal/query/*`,
  `go/internal/mcp/*`, `cmd/api`/`cmd/mcp-server` wiring): new/renamed response
  field, a new tool, or a new mounted route → update `query_shapes.http` /
  `query_shapes.mcp`.
- **A new verb/ecosystem** (parser + collector + correlation): add a fixture
  and/or cassette that produces its edge, plus a required `rc-NN` that isolates
  it (see Cassette vs fixture, and the evidence-kind predicate below).

If yes to any, the change is not done until the gate is green with the updated
fixture.

## Cassette vs fixture (which input to update)

- **Live-collector edges** (anything from the 9 credentialed collectors: cloud,
  k8s, vault, OCI, package, terraform-state, prometheus) come from **cassettes**.
  Change the recorded facts in `testdata/cassettes/<collector>/`.
- **Static-parse edges** (code, gitlab, atlantis, terragrunt, kustomize, ...)
  come from **fixture repos** under `tests/fixtures/ecosystems/`. Add/adjust the
  fixture file; there is no cassette for these.

## Cassette contract

- **`fact_kind` is replayed verbatim** (`collector/cassette/source.go` sets the
  envelope kind to the JSON string as-is — no namespace transform). It must be
  byte-equal to the constant the consuming reducer matches (some families match
  an UNPREFIXED kind, e.g. `facts.VaultAuthRoleFactKind == "vault_auth_role"`). A
  mismatched/namespaced kind is silently **inert** — no error, zero nodes.
- **Cross-fact join keys are matched literally**, not re-derived. Linked facts
  (e.g. a vault role bound to a k8s service account) join on string-equal keys
  (`service_account_join_key`, `role_join_key`, ...). Use consistent synthetic
  values across the related facts; they are HMAC fingerprints in live collection
  but only need to be self-consistent strings in a cassette.
- **Synthetic/redacted data only.** NEVER commit real ARNs, account IDs, IPs,
  hostnames, or employer identifiers. The collector's redaction layer is why live
  recordings look synthetic; a hand-authored cassette must match that posture.
- A **0-node gate result is almost always a `fact_kind` or join-key mismatch**,
  not a graph-writer bug. Verify the projection emits nodes before blaming the
  writer.

## Snapshot contract

- **`required_correlations` (rc-NN)** are existence assertions; node/edge **count
  ranges** are cardinality assertions; **`query_shapes`** assert response truth.
- A new required correlation must be BOTH in the snapshot AND in the verify
  script's `-required-correlations` list, or it is silently not asserted (a
  **false green**). Shared edge types (DEPENDS_ON, DEPLOYS_FROM) need an
  `evidence_kinds` predicate to isolate a verb, not a per-tool edge type.
- **Calibrate count ranges to the real deterministic corpus**, not aspirations:
  floors that catch a major projection drop (e.g. #4019), ceilings wide for
  parser growth. Tolerances are asserted as **required** in full mode
  (`-graph-required-only=false`); an advisory tier is never actually validated.
- **MCP query shapes** are asserted live: the gate unwraps the MCP truth envelope
  `{data, truth, error}` and checks the payload under `data`. A tool whose route
  is not mounted on the MCP server returns `isError`/`HTTP 404` even though it is
  advertised — mount the route (mirror `cmd/api/wiring.go`), do not drop the
  shape. See `eshu-mcp-call-rigor`.
- **Governance-gated families assert `max: 0`.** The SecretsIAM graph projection
  is OFF by default (`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`, ADR
  #1314); never enable a governed feature just to satisfy a count.

## Validate

```bash
# unit + static contract (fast, no Docker)
cd go && go test ./cmd/golden-corpus-gate/ -count=1
bash scripts/test-verify-golden-corpus-gate.sh
# full live run (Docker): bootstrap + replay cassettes + drain + diff snapshot
bash scripts/verify-golden-corpus-gate.sh
```

The full run is the proof. Terminal shape is `N pass, 0 required-fail,
0 advisory-warn` plus `PASS: B-7 golden corpus gate green`. Do not call a
pipeline change done until the gate is green with your fixture update. Never
`docker compose down -v` while a gate run is live (it kills the run's backends).

## Out of scope / related

- Editing the gate command itself or its evidence: see
  `go/cmd/golden-corpus-gate/AGENTS.md`.
- Cassette replay internals: see `go/internal/collector/cassette/AGENTS.md`.
- Hot-path Cypher/perf evidence on graph writes: add `cypher-query-rigor`.
- Correlation/materialization truth: add `eshu-correlation-truth`.
