# golden-corpus-gate — Agent Instructions

LLM-assistant companion to `README.md`. Read this before editing any file in
`go/cmd/golden-corpus-gate/`.

## Read first

- `README.md` — what the phases (drains, graph, query, demo-answers, timing)
  assert and how node/edge counts + query shapes are asserted.
- `doc.go` — the godoc contract.
- `testdata/golden/e2e-20repo-snapshot.json` (repo root) — the B-12 contract this
  command diffs against. Its keys drive the typed structs in `snapshot.go`.
- `scripts/verify-golden-corpus-gate.sh` — the orchestrator that runs the
  pipeline and invokes this command. Changes here often need a matching change
  there.

## Invariants

- **The pure assertion core lives in `go/internal/goldengate`, not here.** The
  snapshot contract (`snapshot.go`), the `Finding`/`Report` accumulator
  (`report.go`), and every `Evaluate*` function (`evaluate.go`) moved into that
  importable package (#4112 / R-10) so the out-of-tree contributor conformance
  suite (`go/conformance`) asserts against the *same* logic with no forked copy.
  `shared.go` re-exports those symbols under the original package-local names via
  aliases, so the gate's call sites read unchanged. Edit the assertion rules in
  `internal/goldengate`, and keep them I/O-free there (no Postgres / Bolt / net).
  This command package keeps only the I/O-and-orchestration layer
  (`graph.go`, `drains.go`, `query.go`, `mcp.go`, `demoanswers.go`, `runner.go`,
  `timing.go`, `main.go`). The demo-answers phase (`demoanswers.go`, #4776)
  reuses the pure `EvaluateQueryShape` core — it adds no new evaluator, only the
  I/O to execute each `specs/demo-first-answers.v1.yaml` question live (its
  `surface`, or a playbook's `surface.execute` target) and assert the answer is
  populated.
- **Drain semantics are a contract, not a style choice.** `fact_work_items`
  residual = `status NOT IN ('succeeded','superseded')`; nonterminal
  `shared_projection_intents` = `completed_at IS NULL`. The `repo_dependency`
  subset is reported because B-13 (#3859) made it the primary drain signal. If
  the queue contract changes in `go/internal/storage/postgres`, update the SQL in
  `drains.go` and its rationale comment.
- **The residual breakdown prints the error text, and the bound is the reason it
  can.** `failure_class` is a triage bucket ("projection_bug"), not the failure —
  a real reducer defect and a machine-contention timeout land in the same one, so
  a count alone leaves a red run unattributable once the stack is torn down
  (#6306). `residualBreakdownSQL` therefore also selects `failure_message`, and
  `formatResidualBreakdown` prints it for at most `maxResidualMessageGroups`
  groups, each cut to `residualMessageMaxLen` runes and flattened onto one line.
  Keep all three bounds if you touch this: a 624-row residual must not become 624
  messages, and error text must never emit something that reads as another gate
  line. The message aggregates *within* the existing grouping rather than joining
  `failure_message` into the `GROUP BY`, so the returned row count is unchanged —
  `ResidualWorkItems` hands the same rows to the zero-correlation diagnosis, which
  a finer grouping would have quietly rewritten.
- **Two details of that query are load-bearing, and neither is visible from the
  Go side.** The aggregate carries its own `ORDER BY` over the message
  expression, because without one the concatenation order of a group holding
  several distinct causes is unspecified — two runs over identical data could
  then blame different things, which is worse than blaming nothing. Postgres
  rejects an ordinal there (`in an aggregate with DISTINCT, ORDER BY expressions
  must appear in argument list`), so the expression is repeated verbatim. And
  the query returns `residualMessageFetchLen` = budget **+ 1** characters, after
  collapsing whitespace *in SQL*: the extra character is how
  `truncateResidualMessage` tells a message that ended at the budget from one
  the database cut at it, and normalizing before the cut stops a Go-side flatten
  from shrinking an already-cut message back under the budget, where it would
  print with no truncation marker.
- **The residual breakdown runs only after the drain has already failed, so it
  needs its own live proof.** `make pre-pr` passing means the drain succeeded,
  which means this query never ran. `drains_residual_breakdown_live_test.go`
  executes the real `residualBreakdownSQL` against a disposable Postgres
  (`ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DSN` +
  `ESHU_TEST_DRAIN_RESIDUAL_POSTGRES_DISPOSABLE=1`, run command in the test's
  doc comment) over NULL, empty, multiple-distinct, over-budget, and multi-line
  messages, and differentials the first four columns against
  `residualBreakdownCountsSQL()` — the pre-message query, *derived* from the
  shipped halves rather than hand-copied. Asserting substrings of the SQL string
  is not execution: a wrong column name or a rejected aggregate clause passes
  every such test.
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
- **`pollUntilDrained` reports progress, not only the final bound.** Its
  `progress`/`progressEvery` parameters write a residual line to the given
  writer (`runDrains` wires `stderr` at a 15s cadence) whenever
  `time.Since(lastProgress) >= progressEvery`, so a human tailing `--keep`
  output can tell "still draining" (residual shrinking across lines) from
  "wedged" (identical residual repeated) before the drain-timeout bound, not
  only after it. Keep the printed field set identical to `runDrains`'s own
  timeout message so the vocabulary matches. Do not print once per poll — the
  default 2s poll interval against the default 10-minute timeout would be up
  to 300 lines. A nil writer or non-positive `progressEvery` disables this
  entirely; every unit-test call site does so deliberately.

## Tests

- The pure evaluators are unit-tested in their new home,
  `go/internal/goldengate` (`evaluate_test.go`, `report_test.go`,
  `property_test.go`, `snapshot_test.go`). The gate's own `*_test.go` cover the
  snapshot loader against the real committed snapshot, the drain poll loop (fake
  querier), the graph checker (fake counter), and the query client (httptest).
  Run both:
  `cd go && go test ./internal/goldengate/... ./cmd/golden-corpus-gate/ -count=1`.
- When you add a phase or assertion, add a focused test for its pure evaluator in
  `internal/goldengate` before wiring the I/O here.

## Out of scope here

- Bringing up Postgres / the graph / the API, replaying cassettes, and draining
  the reducer all live in `scripts/verify-golden-corpus-gate.sh`. This command
  assumes those are already running.
