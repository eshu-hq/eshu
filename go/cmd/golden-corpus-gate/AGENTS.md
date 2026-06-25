# golden-corpus-gate — Agent Instructions

LLM-assistant companion to `README.md`. Read this before editing any file in
`go/cmd/golden-corpus-gate/`.

## Read first

- `README.md` — what the four phases assert and why node/edge counts are advisory.
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
  gate; advisory findings only warn. Node/edge count tolerances are advisory
  while the gate runs a minimal corpus. Do not promote them to required without
  also running the full 20-repo corpus, or the gate will flake.
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
