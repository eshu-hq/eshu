# #7165: drain superseded-generation intents covered by an emitted full successor

## Problem

About 1.18M pending `code_calls` shared-projection intents on ops-qa sit on
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
  If a successor generation quarantines a file, its repo-wide retract deletes
  that file's last-valid edges with nothing re-emitting them — the same end
  state as the replay order; last-known-good semantics are deferred to #7736.
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
- Accepted residual (review R1 F5, deferred to #7736 with arbiter approval):
  a pure-drain cycle can skip the successor retract in a narrow cross-cycle
  conjunction, letting a crash-window partial write linger until a later
  retract-forcing cycle. Same-cycle safety (retract forced when in-cycle
  stale IDs exist) is unaffected.

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
since removed): one fat scope at the ops-qa mean shape (1,500 intents across
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
no graph touch. The lane tests assert zero retract/write calls for drained
rows and normal projection for kept rows.

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
  with zero retract/write calls; keep-tests pin normal projection.
- `TestSplitCoveredByFullSuccessorRows*`: split contract (mixed rows, empty,
  no-port passthrough, error propagation).
- `TestCoveredByEmittedFullSuccessorIDs*`: SQL shape pins and round-trip/
  empty/error unit proofs.
