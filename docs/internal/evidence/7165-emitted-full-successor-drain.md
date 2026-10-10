# #7165: drain superseded-generation intents covered by an emitted full successor

## Problem

About 1.18M pending `code_calls` shared-projection intents on the QA environment sit on
superseded scope generations (794 git scopes). Acceptance is keyed by source
run and advance-only per key, so nothing ever marks those rows stale; once the
quiescence gate stops wedging the lane (#7133), the code-call runner replays
them oldest-first as real retract/write cycles. Correct but slow.
`repo_dependency` has the same shape (~10k rows). Neither lane drained
superseded generations at all (only acceptance-mismatch stale).

## Rule

Drain a lane row when its generation G is all of:

1. `status = 'superseded'` (terminal-status keying, never "not active");
2. covered by a newer full F in the same scope: `F.is_delta = false` and
   `(F.ingested_at, F.generation_id) > (G.ingested_at, G.generation_id)`, the
   order activation, supersession, and acceptance advance in (#6686);
3. F emitted the lane's domain (a `shared_projection_intents` row carries
   `(F.scope_id, F.generation_id, domain)`);
4. G has no in-flight producer (#7121 NOT EXISTS clauses verbatim).

The newest full generation has no newer full generation, so it and every
generation after it are kept. The issue's four shapes fall out:

- (full superseded, delta active): no newer full exists, G1 kept;
- (full, full): G1 covered, drained;
- (full, delta superseded, full active): G1 and G2 covered, drained;
- (full, full, delta active): G0 covered, G1 (newest full) kept.

## Safety case

- Both lanes emit atomically per generation: one `UpsertIntents` call in one
  transaction (`Handler.Handle` for code_calls, `resolve()`'s mutually
  exclusive paths for repo_dependency). One intent row for F proves F covered
  every unit, so F's rows re-emit every drained edge when they project.
  Quarantined-file exception (closed in #7736 F4, see the follow-up below):
  if a successor generation quarantines a file, the file is absent from the
  successor's intents, so the drain's forced retract drops its last-valid
  edges until the next valid generation re-emits them. One-generation
  transient, accepted with an operator signal — not a bug, and no
  last-known-good repair path is planned.
- The emission check is domain-scoped. Acceptance units are repository ids in
  both lanes, so an unscoped check would let one lane's emission falsely cover
  the other's; the live proof pins a wrong-domain seed.
- A failed F that emitted still covers: the pending listings filter only
  `completed_at IS NULL`, never generation status, so F's rows still project.
  Completed F rows cover a fortiori (already re-emitted).
- repo_dependency gets the same rule: its emitters diff against existing edges
  and never re-emit the whole source set, so the re-emission escape hatch in
  the issue does not hold.
- The drain re-evaluates every pass; a lookup error fails the cycle instead of
  guessing; a reader without the port keeps byte-identical behavior.
- Accepted residual, shared with #7121: projector Ack reactivation of a
  superseded generation (#7130). Documented on the store SQL, not closed.
- Closed residual (review R1 F5, fixed by this #7736 PR): a pure-drain cycle
  now runs the forced retract on the drained scope before marking completed
  (both lanes), so a crash-window partial write ahead of the drain cannot
  linger. Same-cycle safety unchanged (retract still forced when in-cycle
  stale IDs exist); all-stale cycles with no drainable rows keep the old skip.

## Arbiter verdict

Muse Spark (arbiter on Muse): generation-level coverage with a domain-scoped
emission proof, rather than per-unit coverage or an activation-status guard.
Per-generation atomic emission makes F-emitted imply F-covers-all-units; the
status of F is irrelevant because pending rows project regardless of it.

Deferral ruling (review R1 F4/F5): "RULING: APPROVE deferring F4+F5 to #7736
— merge the #7165 PR without fixing them, subject to the conditions below.
Severity re-grade right exercised: both stay P2 ("edge case"); neither is
re-graded to P1." Conditions: link #7736 and quote the ruling in the PR body,
carry both finding texts with category "edge case", scope the re-emission
claim for quarantined files (done above), keep the promotion order intact, no
new telemetry here. Full text in the PR body.

## Performance Evidence:

Conflict domain: `shared_projection_intents` rows of one lane per selection
pass; one bounded lookup over the cycle's distinct generation ids, skipped
when the cycle has no rows. Worker/lease settings unchanged.

Prove-the-theory-first shim (disposable Postgres 18, `/tmp/7165-drain-shim.sql`,
since removed): one fat scope at the QA mean shape (1,500 intents across
two full generations) plus 49 thin (full, delta) scopes. The candidate lookup
returned the covered generation with 15 shared-buffer hits on the covering
side (index scans on `scope_generations_scope_latest_lookup_idx` and
`shared_projection_intents_acceptance_lookup_idx), no new index. Execution
0.413 ms is host-noisy on this shared box; buffers and plan shape are the
claim. No wall-time speedup is claimed (NOT_CHECKED on a quiet host): the win
is skipped retract/write graph cycles, visible as `covered_by_full_successor`
drain counts once deployed.

Before/after row counts: before, every pending row on a superseded generation
replays as a real cycle; after, covered generations' rows mark completed with
one bounded retract per pure-drain cycle and no write (#7736 F5, see the
follow-up below). The lane tests assert the forced retract plus zero writes
for drained rows and normal projection for kept rows.

## Observability Evidence:

- `eshu_dp_shared_projection_stale_intents_total{reason="covered_by_full_successor"}`
  counts drained rows per (`domain`, `runner`); `code_call_projection` and
  `repo_dependency_projection` runners emit it.
- INFO log `shared projection drained intents covered by an emitted full
  successor` with `stale_reason` and `stale_count` per cycle.
- `PartitionProcessResult.CoveredByFullSuccessorIntents` carries the per-cycle
  drain count (a subset of `StaleIntents`).
- Docs: `telemetry-coverage.md` row for the new file;
  `metrics-reducer-storage.md` documents the closed reason set.

## Proof

- `TestCoveredByEmittedFullSuccessorShapesAgainstPostgres` (live, reducer
  contention gate): the four issue shapes plus unemitted/wrong-domain
  successor, in-flight producer, live repair row, completed rows, failed and
  completed successors, and a non-generation id.
- `TestCodeCallProjectionRunnerDrainsCoveredGenerations` /
  `TestRepoDependencyProjectionRunnerDrainsCoveredGenerations` (RED before
  wiring: `drain lookups = 0, want exactly 1`): drained rows mark completed
  with zero writes; since #7736 F5 the pure-drain cycle runs the forced
  retract (see the follow-up below). Keep-tests pin normal projection.
- `TestSplitCoveredByFullSuccessorRows*`: split contract (mixed rows, empty,
  no-port passthrough, error propagation).
- `TestCoveredByEmittedFullSuccessorIDs*`: SQL shape pins and round-trip/
  empty/error unit proofs.

## Follow-Up: Quarantined-File Transient Loss (#7736 F4, Accepted)

When a successor generation quarantines a file fact (input_invalid: the file's
parsed body is missing), the file is absent from the successor's intents. The
drain then drops that file's last-valid edges — the forced retract wipes the
scope and the partial successor cannot re-emit the missing file — until the
next valid generation re-emits the file and its cycle restores the edges. This
transient loss is accepted, not a bug: the successor's emitted set is the
source of truth, and quarantine means the successor genuinely has no
replacement edges to offer. The window lasts at most one generation, and the
heal is structural (a normal retract/write cycle), not a repair path.

Operator signal: WARN log `code call file quarantined, edges excluded` with
`scope_id`, `generation_id`, `quarantined_files` (up to 32 `repo_id:path`
entries, fact-ID fallback when the payload lacks file identity) and
`quarantined_file_count` (true total), emitted by
`logCodeCallQuarantinedFiles` in
`go/internal/reducer/code/call/materialization/handler.go`. The existing
factdecode ERROR log carries only fact IDs; this signal names the paths so an
operator can correlate a transient edge loss with the quarantining generation
without a `fact_records` lookup.

Proof:

- `TestCodeCallMaterializationHandlerNamesQuarantinedFiles` (RED before the
  signal: missing quarantine file-identity log): one quarantined file fact
  produces the WARN naming its `repo_id:relative_path`.
- `TestCodeCallQuarantinedFileHealsOnNextValidGeneration` (characterization
  pin, GREEN on current code): drives write-G, drain-G, partial-F,
  healing-H through the real code-call runner against a stateful edge set and
  asserts file 2's edges are present, dropped, still absent, then restored.

## Follow-Up: Pure-Drain Forced Retract (#7736 F5)

A pure-drain cycle (nothing kept, drainable rows present) runs one bounded
`retractRepo` on the drained scope before marking completed, with no write
and no history check, in both lanes. This closes the R1 F5 residual above:
without it a crash-window partial write (write ok, mark failed) ahead of the
drain lingers, because the successor's own cycle has no stale IDs to force
its retract. The drain splits acceptance-filtered rows while the successor is
still unaccepted, so the forced retract always precedes the successor's write
and cannot wipe successor edges.

No-Regression Evidence: baseline is the #7165 pure skip (no graph touch);
after, one bounded retract per pure-drain cycle — a rare
replay/crash-window path, no new query shape (each lane reuses its existing
`retractRepo` → `RetractEdges` path with builder-produced rows identical in
shape to the normal path), no cardinality or queue-depth change, no index or
DDL. Safe because the retract is the lane's own idempotent pre-write retract,
run at most once per pure-drain cycle on a scope the successor rewrites next.

Observability Evidence: the F4 WARN `code call file quarantined, edges
excluded` (new operator signal, documented in
`docs/public/reference/telemetry/index.md`); existing drain counters/INFO
unchanged. `RetractedRows`/`RetractDurationSeconds` on the cycle result now
cover the forced retract.

Proof: `TestCodeCallPureDrainForcesRetract` /
`TestRepoDependencyPureDrainForcesRetract` (RED pre-fix:
`len(retractCalls) = 0`), skip-precision pins, mark-failure-retry tests;
prior keep/drain pins updated to the new contract (forced retract calls,
zero writes).
