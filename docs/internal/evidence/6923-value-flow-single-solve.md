# #6923 value-flow single-solve evidence

CAN_PERFORM sink probe row set varies run to run: the value-flow fixpoint had
two unfenced entry points to the same global solve (an inline solve in every
repo's `code_function_summary` handler, and the #6785 `eshu:global` refresh
singleton), so a B-7 leg ran it up to N+1 times and at least one run could
read the CAN_PERFORM/USES/RUNS_IN chain before it finished materializing.
Fix: `code_function_summary` becomes the refresh singleton's fifth producer
(stops solving inline), and the singleton refuses to solve while any
active-generation writer of the chain is nonterminal — see
`docs/internal/design/6785-value-flow-cloud-sink-refresh.md`'s 2026-09-22
update for the full mechanism.

## Theory shims (prove-the-theory-first)

### Shim 1: converged answers agree across backends; only pre-convergence executions differ

Source: `/tmp/diff-capture-local/{nornicdb,neo4j}` (local B-7 legs,
2026-09-21), grouped by capture file with `jq` over
`.record.Fingerprint.Statement`, `.record.RowCount`, `.record.Digest`.

`CloudSinkTargetsByPairCypher` (statement containing `sinkRel:CAN_PERFORM`),
reducer-side executions, params
`pairs=[{s3:putobject, content-entity:e_6e935a20d390, ...}]`:

| leg | file | executions | rows | digest |
| --- | --- | --- | --- | --- |
| nornicdb | reducer-nornicdb-18128 | 3 | 2 | e4f609c95be6 |
| nornicdb | reducer-nornicdb-23398 | 1 | 2 | e4f609c95be6 |
| nornicdb | reducer-nornicdb-24144 | 1 | 2 | e4f609c95be6 |
| nornicdb | reducer-nornicdb-6603 | 3 | 2 | e4f609c95be6 |
| neo4j | reducer-neo4j-1134 | 1 | 2 | e4f609c95be6 |
| neo4j | reducer-neo4j-4135 | 1 | 2 | e4f609c95be6 |
| neo4j | reducer-neo4j-73142 | 2 | 0 | e3b0c44298fc |
| neo4j | reducer-neo4j-73142 | 1 | 2 | e4f609c95be6 |
| neo4j | reducer-neo4j-90272 | 3 | 2 | e4f609c95be6 |

Digest sets: nornicdb `{e4f6}` vs neo4j `{e3b0, e4f6}` -> results-kind
divergence (the #6923 mechanism, opposite pairing to the issue's report).
Final execution per leg: `e4f6` on both. 8 reducer-side executions per leg of
a global solve whose answer needs one. (The mcp-server files also execute
the statement once each with different params, `limit`/`sink_rels`, 0 rows
on both legs — a different fingerprint, agrees.)

`CloudSinkWorkloadRowsCypher` (`INVOKES_CLOUD_ACTION]->(action:CloudAction)`),
reducer-side, per params prefix:

| leg | params | 0-row execs (e3b0) | 1-row execs (cc2ae0441cb4) |
| --- | --- | --- | --- |
| nornicdb | e_0c64... | 3 | 8 |
| nornicdb | e_58b8... | 12 | 0 |
| neo4j | e_0c64... | 8 | 8 |
| neo4j | e_58b8... | 13 | 0 |

Both legs carry the pre-convergence 0-row digest for `e_0c64`, so the sets
agree by luck (`{e3b0, cc2a}` both); the final execution per leg is `cc2a` on
both. Same mechanism.

Conclusion: the converged answer is backend-stable for both loader
statements; every divergence candidate is a pre-convergence execution.
Last-wins is NOT the fix (see the rejected "compare-side last-wins" shape in
the design doc); one solve per quiescent leg is.

### Shim 2: fence statement cost and correctness

Scratch `postgres:18-alpine` container `eshu-6923-pg` (127.0.0.1:15997),
schema = all 118 migrations applied in path order via `psql`, seeded: 300
scopes x 6 generations (gen 6 active), 60 reducer domains each = 108,001
`fact_work_items` (165 nonterminal rows in the six fence domains, ALL on
superseded generations), 500,000 `shared_projection_intents` (20 domains,
all completed). Statement: the fence text verbatim (see
`docs/internal/design/6785-value-flow-cloud-sink-refresh.md`'s 2026-09-22
update section 2, or `go/internal/storage/postgres/value_flow_refresh_ack.go`'s
`valueFlowInputsFenceSQL`).

| state | rows returned | plan (fact_work_items half) | plan (shared intents half) | exec time |
| --- | --- | --- | --- | --- |
| S0 drained (165 nonterminal on superseded gens) | 0 | Index Scan `fact_work_items_stage_domain_status_idx` (165 rows) -> Hash Join `ingestion_scopes` (301 rows, 5 buffers) -> Sort -> Limit | Index Only Scan partial pending idx, 0 rows | 0.40 ms |
| S1 one `running` iam_can_perform row on an active generation | 1 | same, 166 -> 1 | 0 rows | ~0.36 ms |
| S2 one open `runs_in` intent | 1 | 165 -> 0 | partial pending idx, 1 row | ~0.40 ms |
| S3 heavy: 450 pending on active gens + 5000 open runs_in | 10 (5+5) | 615 -> 450 -> top-N heapsort | Index Scan `generation_pending_idx` 5000 rows | 1.66 ms |
| S4 restored drained | 0 | 165 -> 0 | 0 rows | 0.88 ms (dead tuples, pre-vacuum) |

No Seq Scan on `fact_work_items` or `shared_projection_intents` in any state
(the 301-row `ingestion_scopes` seq scan is the hash-join build side, 5
buffers). Buffers: 194-711 shared hits. The `active_generation_id` join is
what keeps the 165 superseded-generation nonterminal rows from holding the
fence (S0 = 0 rows). Cost is three orders of magnitude under the reducer
retry delay, so the fence poll is not a scheduling cost.

Migration number correction from the initial arbiter draft: the shipped
convergence migration is 120, not 118 — origin/main carried 118 (#6892's
index) when this branched and merged 119 (#6946's liveness stats) before
this landed, so the file was renumbered on rebase.

### Live fence proof (implementation-time, this branch)

`TestValueFlowInputsLivenessFenceSharesSnapshot`
(`go/internal/storage/postgres`) re-proves the fence's correctness directly
against the same scratch Postgres (127.0.0.1:15997): baseline drained; an
open `runs_in` shared-projection intent refuses; completing it clears the
fence; a `dead_letter` `iam_can_perform_materialization` row on a
SUPERSEDED generation does not hold the fence; the same row on the ACTIVE
generation refuses. `go test -run TestValueFlowInputsLivenessFenceSharesSnapshot`
with `ESHU_VALUE_FLOW_REFRESH_LIVE=1` and
`ESHU_POSTGRES_DSN=postgres://eshu:change-me@127.0.0.1:15997/eshu?sslmode=disable`
exited 0.

### Seeded-violation guard proof

`TestValueFlowInputsFenceDomainsCoverCloudSinkChain`
(`go/internal/reducer/code/value`) derives the relationship types
`CloudSinkWorkloadRowsCypher`/`CloudSinkTargetsByPairCypher` traverse and
asserts every one resolves, through the materialized-edge family registry or
a hand-documented map for the two relationship types that registry does not
catalog (`CAN_PERFORM`, `INSTANCE_OF`), to a domain the fence declares.
Seeded violation (run manually during development, then reverted): removing
`reducer.DomainRunsIn` from `postgres.ValueFlowInputsFenceSharedDomains`
turned the test RED —

```
cloud_sink_fence_domains_test.go:125: relationship RUNS_IN owner domain
"runs_in" (family "runs_in") is not in the fence's declared domain set
--- FAIL: TestValueFlowInputsFenceDomainsCoverCloudSinkChain (0.00s)
```

— and restoring the entry turned it GREEN again.

### Orchestrator re-run of both guards (2026-09-22, head 989ce794fe)

Re-proven on the committed tree, not from the executor's manual run. A first
attempt with a `\s`-based BSD sed pattern never applied either mutation (empty
`git diff --stat`) and reported a vacuous green; a second attempt that deleted
the enrollment line failed the build (unused `crossscope` import), which is not
the guard firing. Both mutations below were asserted non-empty before the test
ran and compiled.

- Fence-domain guard: replacing `reducer.DomainRunsIn,` in
  `ValueFlowInputsFenceSharedDomains` with a comment ->
  `--- FAIL: TestValueFlowInputsFenceDomainsCoverCloudSinkChain` /
  `relationship RUNS_IN owner domain "runs_in" (family "runs_in") is not in
  the fence's declared domain set`; restored -> `ok`.
- Enrollment guard: replacing `crossscope.ValueFlowInputsNotReadyFailureClass,`
  in `nonCountingReducerRetryFailureClasses` with a duplicate
  `crossscope.ProducerNotReadyFailureClass,` (compiles, unenrolls the class) ->
  `--- FAIL: TestEveryReadinessFailureClassIsEnrolled` naming
  `value_flow_inputs_not_ready (crossscope/value_flow_inputs_readiness.go)` and
  `--- FAIL: TestReducerQueueFailDefersValueFlowInputsReadinessPastAttemptBudget`;
  restored -> `ok`.

## TDD RED/GREEN pairs

- `refresh.TestHandlerRefusesWhileInputsUndrained` /
  `TestHandlerSolvesWhenBoundExpired`: RED at compile time (`Handler` had no
  `InputsLiveness`/`Now` fields), GREEN after the fence and bound landed in
  `code/value/refresh/handler.go`.
- `summary.TestCodeFunctionSummaryHandlerDoesNotSolveInline` (replaces
  `TestCodeFunctionSummaryHandlerProjectsFixpointAfterPersistence`): RED
  (`CanonicalWrites = 0, want 2`) before the removed-rows accounting and
  `refresh_affected_repos` signal landed, GREEN after.
- `TestEveryReadinessFailureClassIsEnrolled`
  (`go/internal/storage/postgres`): RED the moment
  `crossscope.ValueFlowInputsNotReadyFailureClass`'s error type landed
  (unenrolled class reported), GREEN after enrolling it in
  `nonCountingReducerRetryFailureClasses`.
- `TestValueFlowInputsFenceDomainsCoverCloudSinkChain`: seeded-violation
  RED/GREEN pair above.

## Performance Evidence: fence cost and solve-count reduction

- Fence statement cost: shim 2 above, all states sub-2ms, index-range scans
  only, live-proved by `TestValueFlowInputsLivenessFenceSharesSnapshot`
  against the same scratch database's live schema.
- Global solve count per leg: BEFORE (local capture, shim 1) = 8-13
  reducer-side executions per leg per loader statement (a global solve
  re-reads both statements each run). AFTER (design target, this change):
  1 execution of each loader statement per quiescent replay, since every
  trigger — the four existing producers plus `code_function_summary` — now
  coalesces onto the one fenced singleton instead of `code_function_summary`
  running an independent inline solve per repo generation.
- `eshu_dp_reducer_readiness_waits_total{domain="code_value_flow_refresh"}`:
  new label value on the existing #6785 counter (`ReducerReadinessWaits`),
  reused rather than a new instrument — see Observability Evidence.
- Reducer retry delay bound: the added latency after the last chain writer
  lands is at most one `ESHU_REDUCER_RETRY_DELAY`-based defer
  (`reducer_queue_helpers.go`'s visible_at scheduling), which the fence's
  sub-2ms cost does not meaningfully add to.

PENDING: four-leg capture proof. The equivalence assertion the design doc's
Prove-The-Theory-First section specifies — two legs per backend with capture
on, every exploded group of both loader statements showing exactly ONE
reducer-side record per leg, digest equal across all four legs,
`CompareRecordings` reporting 0 `results`/`missing`/`failures` for these
fingerprints, the B-7 snapshot diff byte-identical to main, and the reducer
log showing exactly one `value-flow fixpoint evidence loaded` per leg plus
>=1 `value_flow_inputs_not_ready` refusal — has NOT been run yet on this
branch. The orchestrator will run it and update this section before
promotion; do not treat this PR as fully proven until this section is
filled in with the actual capture output.

## Observability Evidence: reused counter, new span, structured refusal log

- `eshu_dp_reducer_readiness_waits_total{domain="code_value_flow_refresh",
  outcome=deferred|abandoned}` (existing `ReducerReadinessWaits` instrument,
  `go/internal/telemetry/instruments.go`): the refresh handler's
  `recordReadinessWait` adds this label value alongside the #6785 producer
  domains already using the counter — no new instrument.
- `eshu_dp_value_flow_refresh_gate_evaluations_total{domain="code_function_summary"}`:
  not emitted — `code_function_summary` runs no graph gate read (its
  `refresh_affected_repos` signal is always explicit 1), so it never calls
  `affected.BeginRefreshGateEvaluation`. This is a deliberate scope
  narrowing versus the other four producers, documented in
  `code/function/summary/README.md`'s Telemetry section.
- New span `reducer.value_flow_inputs_fence`
  (`telemetry.SpanReducerValueFlowInputsFence`, added to the existing span
  const block in `go/internal/telemetry/contract.go` rather than a new file,
  which would have pushed `internal/telemetry` over the dirgate 40-file
  cap) wraps the fence read in `refresh.Handler.beginFenceSpan`, annotated
  with the refusal outcome.
- Structured refusal log ("value-flow refresh deferred: cloud-sink chain
  inputs not drained" / "... solving with undrained cloud-sink chain
  inputs: starvation bound reached"), keys: `scope_id`, `generation_id`,
  `failure_class`, `readiness_wait_outcome`, `pending_input_count`,
  `pending_input_sample`, `elapsed_since_cycle_anchor` (abandoned only),
  `max_wait`.
- `docs/public/observability/telemetry-coverage.md`: the value-flow refresh
  row now carries the counter and span (no longer No-Observability-Change)
  and lists `value_flow_refresh_ack.go` (fence SQL) and the summary handler
  as files that add no signal of their own; folded into the one existing row
  because the page is grandfathered at its current length and may not grow. `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
  scripts/verify-telemetry-coverage.sh` run and passing.
