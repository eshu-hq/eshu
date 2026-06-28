# golden-corpus-gate — Agent Instructions

LLM-assistant companion to `README.md`. Read this before editing any file in
`go/cmd/golden-corpus-gate/`.

## Read first

- `README.md` — what the four phases assert and how node/edge counts + query
  shapes are asserted.
- `doc.go` — the godoc contract.
- `testdata/golden/e2e-20repo-snapshot.json` (repo root) — the B-12 contract this
  command diffs against. Its keys drive the typed structs in `snapshot.go`.
- `scripts/verify-golden-corpus-gate.sh` — the orchestrator that runs the
  pipeline and invokes this command. Changes here often need a matching change
  there.

## Invariants

- **The pure evaluators in `evaluate.go` must stay I/O-free.** They are the
  unit-tested correctness core; every assertion rule is tested there without a
  database, graph, or HTTP server. Do not reach into Postgres / Bolt / net from
  `evaluate.go`.
- **Drain semantics are a contract, not a style choice.** `fact_work_items`
  residual = `status NOT IN ('succeeded','superseded')`; nonterminal
  `shared_projection_intents` = `completed_at IS NULL`. The `repo_dependency`
  subset is reported because B-13 (#3859) made it the primary drain signal. If
  the queue contract changes in `go/internal/storage/postgres`, update the SQL in
  `drains.go` and its rationale comment.
- **Required vs advisory is the safety boundary.** Required findings fail the
  gate; advisory findings only warn. Node/edge count tolerances are now **required**
  (`-graph-required-only=false`, #3866) because the orchestrator runs the full
  20-repo corpus. An advisory tier is never actually validated — promoting it to
  required is what surfaces drift, so prefer required once the corpus produces the
  value.
- **Calibrate count ranges to the real deterministic corpus, not aspirations.**
  The corpus is fixed (same fixtures + cassettes), so each count is deterministic.
  Set floors that catch a major drop (e.g. the #4019 nested-file loss) and keep
  ceilings wide for parser growth; do not copy an idealized range the corpus does
  not actually produce, or the required gate fails. When a count legitimately
  changes, update the snapshot range under review — that is the golden standard
  working, not a nuisance.
- **Governance-gated families assert `max: 0`.** The SecretsIAM graph projection
  is OFF by default (`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`, ADR
  #1314); enabling it without a target-deployment activation record is a rule
  violation, not a config choice. So the SecretsIAM* node/edge counts are pinned
  to `max: 0` — a nonzero count means the gate enabled a governed feature. Never
  enable the toggle just to satisfy a count.
- **MCP query shapes are asserted live through the tool layer.** `checkMCPQuery`
  invokes each tool via `POST /mcp/message` (served standalone, no SSE) and
  unwraps the MCP truth envelope `{data, truth, error}` — the payload is under
  `data`, so the shape is asserted against `data`, not the envelope. A tool whose
  route the MCP server does not mount returns `isError`+`HTTP 404` even though it
  is advertised; fix the route (mirror `cmd/api/wiring.go`), do not drop the
  assertion. Tools needing a selector pass it in `arguments` (`get_repo_summary`
  → `repo_name`; `list_kubernetes_correlations` → `cluster_id`).
- **`graph.go` is content-flagged by the perf-evidence gate** (it holds the
  scalar-count Cypher). Any edit to it — even a comment — needs a tracked
  `evidence-*.md` (No-Regression + No-Observability-Change is fine when no
  Cypher/perf/telemetry changed). The verifier diffs `HEAD~1` locally but
  `origin/main` in CI, so reproduce a CI failure with
  `ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh`.
- **Labels and relationship types are interpolated into Cypher** (they cannot be
  parameterized). `graph.go` validates them against `identRE` first. Keep that
  guard on any new graph query.
- **An empty report is a failure.** `Report.Failed()` returns true when nothing
  ran — a gate that asserted nothing has proven nothing. Preserve this.
- **Drain is populated-then-drained, not just drained.** `pollUntilDrained` must
  not accept a `0/0` reading until it has observed the reducer emit the
  require-populated domains (`-require-populated-domains`, default off in the
  binary, `repo_dependency` in the orchestrator). The reducer runs in the
  background, so a poll that fires before it starts would otherwise read an empty
  queue and pass on an unreduced pipeline. Do not weaken this to "queue empty".

## Tests

- `*_test.go` cover the pure evaluators, the snapshot loader against the real
  committed snapshot, the drain poll loop (fake querier), the graph checker (fake
  counter), and the query client (httptest). Run:
  `cd go && go test ./cmd/golden-corpus-gate/ -count=1`.
- When you add a phase or assertion, add a focused test for its pure evaluator
  before wiring the I/O.

## Out of scope here

- Bringing up Postgres / the graph / the API, replaying cassettes, and draining
  the reducer all live in `scripts/verify-golden-corpus-gate.sh`. This command
  assumes those are already running.
