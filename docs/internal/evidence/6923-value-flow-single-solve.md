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

Re-measured after review round 1 dropped `failed`/`dead_letter` from the
fence status set (`valueFlowInputsFenceStatusList`), same database, same
seed (the 165 seeded nonterminal rows are `failed`/`dead_letter`, so they are
now outside the status set as well as on superseded generations):

| state | rows returned | exec time |
| --- | --- | --- |
| S0 drained | 0 | 0.21 ms |
| S0b one `dead_letter` iam_can_perform row on an ACTIVE generation (must not hold) | 0 | 0.19 ms |
| S1 one `retrying` row on an ACTIVE generation (refuses) | 1 | 0.18 ms |
| S3 heavy: 450 pending on active gens + 5000 open runs_in intents | 10 (5+5) | 3.0 ms |

Same plan shapes (index range on `fact_work_items_stage_domain_status_idx`,
partial pending index on `shared_projection_intents`), no Seq Scan on either
large table (`rg -c 'Seq Scan on (fact_work_items|shared_projection_intents)'`
over the plans = 0).

Migration number correction from the initial arbiter draft: the shipped
convergence migration is 120, not 118 — origin/main carried 118 (#6892's
index) when this branched and merged 119 (#6946's liveness stats) before
this landed, so the file was renumbered on rebase.

### PR review fix: the shared-intents half is scoped to the active generation

The PR review on #6966 found the fence's `shared_projection_intents` half
refused on any open `runs_in`/`invokes_cloud_action` intent regardless of
generation, so an orphaned intent on a superseded generation could hold the
global singleton for the full bound on every reopen. Both halves now join
`ingestion_scopes` on `active_generation_id`, and each pending description
carries `scope_id/generation_id` so the deferral log names the holder.

Proof on the scratch database (plain queries, one open `runs_in` intent
seeded on scope-7): on the SUPERSEDED generation `gen-7-2` the previous
statement returned `shared|runs_in:p1` (held) and the joined statement
returned no row; the same intent moved to the ACTIVE generation `gen-7-6`
returned `shared|scope-7/gen-7-6=runs_in:p1` (held, with the holder named).
`TestValueFlowInputsLivenessFenceSharesSnapshot` pins the superseded-intent
case (does not hold) next to the active-intent case (refuses).

Final statement `EXPLAIN (ANALYZE, BUFFERS)` on the same seed:

| state | exec time |
| --- | --- |
| S0 drained | 0.23 ms |
| S3 heavy: 450 pending on active gens + 5000 open runs_in intents on active gens | 2.0 ms |

Plans: `fact_work_items_status_idx` and `shared_projection_intents_acceptance_partition_pending_idx`
range scans joined to `ingestion_scopes_active_generation_idx`; no Seq Scan
on either large table.

### Replacement review fixes: guard covers node writers; migration 120 converging branch executed

The independent replacement review found the fence-domain guard only derived
relationship-type writers, so dropping `aws_resource_materialization` (the
`CloudResource` node writer) or `code_function_summary` (the `Function` node
writer) from the fence list left it green. The guard now also extracts every
node label the two probe statements traverse (`Function`, `CloudAction`,
`Workload`, `WorkloadInstance`, `CloudResource`), maps each to its writer
domain, fails on an unmapped label, and asserts `code_call_materialization`
(the shared-intent enqueuer) stays fenced. Seeded violation, orchestrator-run
with the mutation asserted unique: replacing `reducer.DomainAWSResourceMaterialization,`
in the fence list with a comment ->
`--- FAIL: TestValueFlowInputsFenceDomainsCoverCloudSinkChain` /
`node label CloudResource writer domain "aws_resource_materialization" is not
in the fence's declared domain set`; restored -> `ok`.
`TestCloudSinkChainNodeLabelsRejectsUnmappedLabel` pins the label extraction
and an unmapped synthetic label.

Migration 120's converging branch (the `NOT LIKE '%code_function_summary%'`
true path) executed on the scratch database, which held the 093/112
six-domain CHECK and trigger and 108,001 `fact_work_items` rows: before,
`pg_get_constraintdef` listed six domains and the trigger definition did not
contain `code_function_summary`; after applying 119 then 120 (both exit 0)
the CHECK lists seven domains and the trigger contains it, with the row count
unchanged; applying 120 a second time exited 0 and left the constraint and
trigger OIDs unchanged (no-op); an insert into `cross_scope_completion_events`
with `producer_domain = 'code_function_summary'` was accepted.

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

### Bracket-count guard (review round 2)

The guard added in round 1 for unparsed relationship hops is now a plain
regression test rather than a manual mutation:
`TestRelationshipTypesFromStatementsRejectsUnparsedHop` feeds the extraction
helper a statement with a `[:RUNS_IN|INSTANCE_OF]` hop and asserts the
"2 bracketed relationship patterns but the extraction regex understood 1"
error, while the production statements parse cleanly. The fence span
contract is likewise pinned by `TestHandlerFenceSpanCarriesOutcome`
(proceed / deferred / abandoned / error outcomes and `pending_input_count`
on an in-memory span recorder).

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
  re-reads both statements each run); a pre-fix leg's reducer log
  (2026-09-21, `golden-corpus-gate.XXXXXX.VqBnLf5yhF`) shows 29 `value-flow
  fixpoint evidence loaded` lines for 33 `code function summary persistence
  completed` lines. AFTER, measured on the four-leg run below: one converged
  solve per reducer drain window (the gate runs four reducer processes per
  leg), 4-5 executions of each loader statement per leg, every one carrying
  the converged digest, plus 5 `value_flow_inputs_not_ready` deferrals and 0
  abandonments in the one leg whose reducer log was snapshotted. The design
  target "exactly one" was too strong; see the design doc's 2026-09-22
  update for why one redundant converged re-solve per late completion event
  is expected and harmless to the oracle.
- `eshu_dp_reducer_readiness_waits_total{domain="code_value_flow_refresh"}`:
  new label value on the existing #6785 counter (`ReducerReadinessWaits`),
  reused rather than a new instrument — see Observability Evidence.
- Reducer retry delay bound: the added latency after the last chain writer
  lands is at most one `ESHU_REDUCER_RETRY_DELAY`-based defer
  (`reducer_queue_helpers.go`'s visible_at scheduling), which the fence's
  sub-2ms cost does not meaningfully add to.

### Four-leg capture, run A (tree 989ce794fe, pre-rebase, before review-round-1 fixes)

Driver: two pairings x two backends, each leg `scripts/verify-golden-corpus-gate.sh`
with `ESHU_DIFFERENTIAL_CAPTURE=1`, wiped corpus and capture dirs between legs,
isolated ports (`ESHU_POSTGRES_PORT=15635 NEO4J_BOLT_PORT=7792 NEO4J_HTTP_PORT=7679
GATE_API_PORT=18085 GATE_MCP_PORT=18095`), default NornicDB image pin, 2026-09-22
16:11-16:50 UTC on the shared macOS host.

| leg | gate | seconds | capture records |
| --- | --- | --- | --- |
| pair 1 nornicdb | PASS (569 pass, 0 required-fail, 3 advisory-warn) | 514 | 2641 |
| pair 1 neo4j | PASS | 858 | 2655 |
| pair 2 nornicdb | PASS | 516 | 2651 |
| pair 2 neo4j | PASS | 413 | 2571 |

Reducer-side executions of the two loader statements per leg (jq over the
capture JSONL, `reducer-*.jsonl` only; before the fix a leg carried 8-13
executions including 0-row pre-convergence answers, see shim 1):

| leg | `CloudSinkTargetsByPairCypher` | `CloudSinkWorkloadRowsCypher` (params `e_0c647bff8...`) |
| --- | --- | --- |
| pair 1 nornicdb | 5 executions, all 2 rows, digest `e4f609c95be6` | 5 executions, all 1 row, digest `cc2ae0441cb4` |
| pair 1 neo4j | 4 executions, all 2 rows, `e4f609c95be6` | 4 executions, all 1 row, `cc2ae0441cb4` |
| pair 2 nornicdb | 4 executions, all 2 rows, `e4f609c95be6` | 4 executions, all 1 row, `cc2ae0441cb4` |
| pair 2 neo4j | 4 executions, all 2 rows, `e4f609c95be6` | 4 executions, all 1 row, `cc2ae0441cb4` |

Every execution on every leg carries the converged digest; the digest SET
per fingerprint is a singleton and identical across all four legs. The
pre-fix zero-row fingerprint for the `e_58b8...` function batch never
executes any more (that batch is now only ever read inside a converged
solve). Executions per leg are 4-5, not 1: the gate runs seven reducer
processes per leg across its drain stages and each drain window that
changes an input ends in one solve; pair 1 nornicdb shows one process
solving twice with the same digest (the late-completion-event re-solve the
design doc describes).

Reducer logs snapshotted from three of the four legs' gate work dirs
(`reducer-config-state-drift-history.log`, which accumulates every drain):

| leg dir | `value-flow refresh completed` | `value-flow fixpoint evidence loaded` | deferred (`value_flow_inputs_not_ready`) | abandoned |
| --- | --- | --- | --- | --- |
| cAT2QE6T4p (pair 1 neo4j) | 4 | 4 | 5 | 0 |
| KzHzdItpfs (pair 2 nornicdb) | 4 | 4 | 2 | 0 |
| lQBw4viUia (pair 2 neo4j) | 4 | 4 | 3 | 0 |

(The pre-fix leg of 2026-09-21 had 29 `loaded` for the same 33 summary
persists.)

Compare (`golden-corpus-gate -phase=backend-diff`, both pairings,
`specs/backend-divergence-allowlist.v1.yaml`): with the committed allowlist
the run stops on a STALE entry: entry 36, the `TAINT_FLOWS_TO {evidence_uid}`
fixpoint writer whose reason was "taint fixpoint rows emitted under
timing-dependent scope sets" — the N+1-solve mechanism this change removes,
so that entry is retired in this PR. Entry 44 (a `Repository -> File`
language count read) also reported stale in this run; it is unrelated to
this change and is the known results-tier flap the differential job already
tolerates, so it is left in place. With those two set aside on a temporary
copy the compare reports: `summary: 2 pass, 0 required-fail, 1
advisory-warn` — `nornicdb_vs_neo4j_quorum: PASS (recordings agree across
both pairings)`, `nonreproducing: 82 pairing-local divergences dropped`,
`executions: 9 execution-count divergences with agreeing results held
advisory (scheduling noise)`. Neither loader statement has a results-kind or
missing divergence in either pairing; pairing 1 records a pairing-local
execution-count divergence on both of them (5 vs 4 executions, identical
digests), dropped by quorum as non-reproducing because pairing 2 is 4 vs 4 —
the redundant converged re-solve the design doc describes. (The compare log
prints only the first 20 divergences per pairing; this statement rests on
the per-leg recount above, not on the printed list.)

### Four-leg capture, run B (final runtime tree)

Same driver, same ports, 2026-09-22 16:52-17:23 UTC, on the tree carrying the
rebase onto main 5d26325ee2 and the review round-1 fixes (fence status set
without `failed`/`dead_letter`, span outcome, at-least-one-write replace
accounting). Round 2 changed only tests, docs and a contract comment, and
the final rebase onto 4a04039dbb touched no file this branch touches, so
run B covers the runtime code that ships.

| leg | gate | driver wall seconds (incl. build and teardown; gate `elapsed` 288/336/316/323) | capture records |
| --- | --- | --- | --- |
| pair 1 nornicdb | PASS | 438 | 2622 |
| pair 1 neo4j | PASS | 486 | 2589 |
| pair 2 nornicdb | PASS | 445 | 2633 |
| pair 2 neo4j | PASS | 464 | 2571 |

Reducer-side executions of the two loader statements per leg (seven
reducer processes per leg):

| leg | `CloudSinkTargetsByPairCypher` | `CloudSinkWorkloadRowsCypher` |
| --- | --- | --- |
| pair 1 nornicdb | 4 executions, all 2 rows, `e4f609c95be6` | 4 executions, all 1 row, `cc2ae0441cb4` |
| pair 1 neo4j | 5 executions, all 2 rows, `e4f609c95be6` | 5 executions, all 1 row, `cc2ae0441cb4` |
| pair 2 nornicdb | 4 executions, all 2 rows, `e4f609c95be6` | 4 executions, all 1 row, `cc2ae0441cb4` |
| pair 2 neo4j | 4 executions, all 2 rows, `e4f609c95be6` | 4 executions, all 1 row, `cc2ae0441cb4` |

Every execution converged; digest sets are singletons and identical across
all four legs and identical to run A. The at-least-one-write replace rule
did not raise the execution count (4-5 per leg, as in run A).

Reducer logs snapshotted from three legs (`reducer-config-state-drift-history.log`):

| leg dir | completed | loaded | deferred | abandoned | fence `error` outcome |
| --- | --- | --- | --- | --- | --- |
| bYPlPDOW2r | 4 | 4 | 4 | 0 | 0 |
| JFSvGdzAjO | 4 | 4 | 4 | 0 | 0 |
| XDfdwvB90b | 5 | 5 | 3 | 0 | 0 |

Compare, run twice: (1) inside the driver with the binary built from the
run-B tree, committed allowlist (entry 36 retired): exit 0. (2) After the
final rebase, with `golden-corpus-gate` rebuilt from head f3cecb1bb9 (which
carries #6941's comparison changes) and CI's
`-diff-executions-advisory-max=200`: exit 0, `summary: 2 pass, 0
required-fail, 1 advisory-warn`, `nornicdb_vs_neo4j_quorum: PASS`,
`nonreproducing: 87 pairing-local divergences dropped`, `executions: 5
execution-count divergences with agreeing results held advisory`, no stale
entry (entry 44 did not flap in this run), and no `TAINT_FLOWS_TO` `missing`
divergence now that entry 36 is unexcused. Neither loader statement has a
results-kind or missing divergence in either pairing; pairing 1 records a
pairing-local execution-count divergence on both (4 vs 5 executions,
identical digests, printed at `compare-final.log:7` for the workload-rows
statement), dropped by quorum as non-reproducing because pairing 2 is 4 vs
4. The advisory-max flag was passed on the command line; the tool does not
echo its argv and emits a finding only when the ceiling is exceeded, so the
log carries no trace of it. Logs: `/tmp/6923-legs-runB/compare.log`,
`/tmp/6923-legs-runB/compare-final.log`; captures
`/tmp/diff-capture-6923-runB/pair{1,2}/{nornicdb,neo4j}`.

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
