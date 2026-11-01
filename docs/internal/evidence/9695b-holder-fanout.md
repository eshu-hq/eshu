# 9695b: supply-chain impact holder fan-out

Issue #9695, item 1.5b of epic #9209. This note records the design proof, the
performance measurements, and the operator signals for the one-hop holder
fan-out. It builds on [9695-advisory-retraction.md](9695-advisory-retraction.md)
(1.5a), which states the convergence gap this change closes. Figures come from a
private native Postgres 18.6 on macOS arm64 with `go1.26.10`; they are one-sample
measurements on a developer host (load 9 to 16 while benchmarking), not a CI
baseline.

## What the change does

A `supply_chain_impact` pass writes a replace-set of finding rows for its own
scope. Another scope that holds an active copy of the same CVE's finding keeps a
stale copy until something reruns that scope. After the writer commit and before
the queue acknowledgement, a pass that changed or retracted rows now reopens the
`supply_chain_impact` item of every scope that holds an active copy of those CVEs.
The reopened pass re-derives its copy from current evidence. There is no
precedence rule in the winners query: convergence is re-derivation.

- **Which CVEs.** The writer reads the active finding rows of the generation it is
  writing and of the scope's previous generation inside its transaction (after the
  conflict-domain lock and the fencing admission, before the upsert) and compares
  each row's `provenance.content_digest` with the new row's. A finding's fact id
  embeds its generation, so the first pass of a new generation finds none of its
  own rows; that pass is judged against the previous generation, matched by stable
  key, and a rerun is judged against the generation's own rows by fact id. A row
  that is new, revived, different, or has no stored digest is changed. A finding
  the baseline had and this pass lacks is changed (a stored row the retraction will
  tombstone, or a previous-generation row with no counterpart), except in a
  partial-evidence pass, which proves no removal. The digest hashes the typed
  payload minus `intent_id`, `cause`, `generation_id`, and `evidence_fact_ids`
  (a rescan mints new source fact ids for identical records);
  `suppression_state` is inside it, so an operator copy flipping between
  `accepted_risk` and `advisory_retracted` counts.
- **Which scopes.** One exact indexed read per chunk of 200 changed CVEs:
  `payload->>'cve_id' = ANY($1)` over active finding rows at the scope's active
  generation, minus the asking scope. The operator suppression scope
  (`operator:vulnerability_suppressions`) is a holder like any other. No new index:
  the read uses `fact_records_supply_chain_impact_lookup_idx`. A second read adds
  the scopes whose item at the scope's active generation is claimed or running
  (`fact_work_items_stage_domain_status_idx`, no new index). A pass in flight may
  have loaded its evidence before this pass's facts were visible, and a scope whose
  new generation has no finding rows yet is invisible to the row read; flagging a
  running item makes it rerun once after its acknowledgement, and a pending or
  retrying item loads fresh evidence anyway. Dropping the active-generation join
  instead would also reach superseded generations' rows and add about 19 percent
  more reopens (857 against 722 holders in the synthetic data) for nothing the
  in-flight read does not already cover.
- **How.** One autocommit statement per holder, in sorted order, holding one row
  lock: a succeeded item becomes pending, a claimed or running item gets the
  durable replay bit the migration 093 trigger turns into one rerun, a queued item
  is left alone without a row version, and a holder whose active generation has no
  item gets one enqueued. The statement stamps `fanout_origin` on the item it
  writes.
- **One hop.** Only the attempt a fan-out created is one hop. A reopen of a
  succeeded item and the replay of a flagged one both reset `attempt_count` to 0, so
  that claim is attempt 1: the pass re-derives its copy, fans out nothing, and
  consumes the stamp with a lease-fenced update on its own item (only while the
  stored stamp still equals the one it saw). A stamp that reaches a later attempt was
  written while a genuine pass was running and that pass failed, so the retry is a
  genuine pass again and fans out its own change. An owed marker also makes a pass
  genuine, whatever stamp it carries. A continuation is not a hop: a stamped drain
  claim drains the stored list, and clearing the list is an explicit field.
- **Bounded claims.** A claim reopens at most 256 holders. The rest is stored on
  the committing item as `fanout_remaining` (the same lease-fenced update also marks
  the item for one rerun), and the next claims drain it.
- **At-least-once.** When a commit changed rows, the writer sets the constant
  marker `fanout_owed` on the pass's own item inside the same transaction, fenced
  on the lease owner, a live lease, and the claimed attempt; zero rows affected is a
  retryable lost-lease error and the write rolls back, so a zombie pass cannot
  commit. The fan-out clears the marker in the same fenced update that finishes the
  last holder (it stays while a continuation list is outstanding). A retry that
  finds the marker knows an earlier attempt committed: it asks the writer for every
  CVE the scope's rows name, tombstoned rows included, and fans out for all of them.
  A retry after a failure before the commit has no marker and fans out only its own
  changes. A failed holder read, in-flight read, reopen, or state update fails the
  pass with a retryable error of failure class
  `supply_chain_impact_fanout_failed`; the queue retries it up to its attempt budget
  and then dead-letters it, which is loud and replayable.

## Defects found while proving the first draft

The unit tests with a stub port could not see the first one, and the live tests
as first written (they rerun one generation) could not see the fourth. The review
of that draft found five more, each reproduced as a failing live test on real
Postgres first. All are fixed and covered.

- **A fan-out failure dead-lettered the pass.** The queue dead-letters any error
  that does not report `Retryable() == true`. The first draft returned plain
  errors, so one transient holder-read timeout, after the writer had already
  committed, would have dead-lettered the item on its first attempt.
  `TestFanOutFailuresAreRetryableAndSelfClassified` and
  `TestFailedHolderReadRetriesTheWholePassAndStillConvergesLive` pin it.
- **The reopen statement omitted the identity authorization columns.** Found by
  reading the existing replay query before the store was written: the shim
  statement did not reset `container_image_identity_v2/v3_authorized_status` the way
  `scheduleCrossScopeMaterializationReplayQuery` does. The store now carries the
  same `SET` list.
- **`SELECT DISTINCT` with `ORDER BY ... COLLATE "C"`** is a Postgres error
  (SQLSTATE 42P10). The handler sorts holders in Go, so the query orders by the
  column.

- **Every rescan fanned out for every CVE.** The first draft read only the
  generation being written. A fact id embeds the generation, so the first pass of
  every new generation found no rows of its own, saw every row as new, and would
  have reopened every holder of every CVE on every rescan (KEV about every 30
  minutes, EPSS daily). The change set is now judged against the previous
  generation by stable key, and the digest drops the generation id and the cited
  source fact ids. `TestNewGenerationWithIdenticalContentReportsNoChange` is the
  unit guard; `TestRescanWithIdenticalContentFansOutNothingLive` runs it on the real
  queue and `TestRescanThatRejectsTheRecordFansOutLive` is its other half.

- **A stamp written while a pass was running suppressed that pass's retry.** The
  reopen statement stamps a claimed or running item it flags. If that item was a
  genuine pass that then failed, its retry carried the stamp, was treated as one
  hop, and never fanned out the pass's real change.
  `TestFanOutRaceStampOnRunningPassKeepsItsRetryFanOutLive`.
- **Consuming a stamp deleted the continuation list.** An empty list meant "clear"
  to the store, so a stamped drain claim silently dropped the holders still owed.
  The list is now rewritten only under an explicit `ReplaceRemaining`.
  `TestFanOutRaceStampDuringDrainKeepsTheContinuationLive`.
- **A holder whose new generation had no rows yet was invisible.** The row read
  joins the active generation; a holder whose pass for a newly active generation had
  loaded evidence and not yet written was missed and then committed the stale copy.
  The in-flight read closes it, given the order of the two reads (next bullet).
  `TestFanOutRaceInFlightFirstPassOfNewGenerationIsFlaggedLive` and its siblings.
- **The two reads in the wrong order left a gap.** With the row read first, a holder
  with no rows that committed and acknowledged between the two reads was missed by
  both (no rows at the first, already succeeded at the second).
  `TestFanOutRaceHolderCommitsAndAcksBetweenTheTwoReadsLive` failed on that order.
  Order, with t1 the in-flight read and t2 the row read: a holder claimed or running
  at t1 is flagged (if it acknowledged before the reopen statement, the reopen sees
  `succeeded` and reopens it). A holder that acknowledged before t1 committed its rows
  first (a pass commits before it acknowledges), so t2 sees them. A holder claimed
  after t1 loads its evidence after t1, which is after the source's facts were visible
  (the source's pass ran after its generation activated) and after the source's
  commit, so it needs no flag. There is no fourth state, and the invariants it rests
  on are that a pass commits before it acknowledges and that evidence is loaded after
  the claim. A scope that loaded its evidence before the facts were visible, derived no
  row, and acknowledged before t1 is the separate pre-existing limit below.
- **The fan-out span ended the caller's span.** With no tracer wired, the fallback
  returned the context's own span and the deferred `End` closed `reducer.run`. The
  tracer is wired and the fallback is a no-op.
- **Every counting retry fanned out for every CVE.** The `AttemptCount > 1`
  trigger could not tell a retry after a commit from one before it. The owed marker
  replaces it.

## Performance Evidence:

**Conflict domain.** Per `fact_work_items` row (one holder item), taken with
`FOR NO KEY UPDATE` for the length of one autocommit statement; and per
`(scope, generation)` finding set, the existing advisory lock the writer already
takes. The fan-out holds no lock across statements and no lock while the writer
transaction is open: it starts after the commit.

**Worker and lease settings.** The fan-out runs inside the claiming worker's pass.
Its own-item update fences on the work queue's lease owner (`reducer`, one
constant shared with the queue), a live `claim_until`, and `last_attempt_at`, the
same fence the acknowledgement uses. No worker count, batch size, lease duration,
or retry setting changed.

**Holder read.** Synthetic table of 500,000 finding rows over 900 scopes (80
percent at the active generation, 15 percent at an older generation, 5 percent
tombstoned; 25,000 CVEs with a skewed spread), the real partial indexes, the real
query text. `EXPLAIN (ANALYZE)`, best of three:

| Changed CVEs | Holders returned, no operator rows | Execution, no operator rows | Holders returned, operator scope seeded | Execution, operator scope seeded |
| --- | --- | --- | --- | --- |
| 1 | 701 | 5.2 ms | 703 | 4.3 ms |
| 10 | 720 | 11.6 ms | 722 | 10.5 ms |
| 50 | 720 | 47.3 ms | 722 | 50.8 ms |
| 100 | 720 | 61.7 ms | 722 | 61.0 ms |
| 200 | 720 | 77.5 ms | 722 | 77.7 ms |

The operator scope was seeded with 3,000 finding rows over 1,000 CVEs (the shape
of 1,000 suppressions with three copies each) and is in every result. The plan at
200 CVEs is a Bitmap Index Scan on `fact_records_supply_chain_impact_lookup_idx`
(43,993 rows), a Bitmap Heap Scan, a Hash Join to `ingestion_scopes`, and a
HashAggregate; the heap fetch is the cost because `scope_id` is not in the index.
Adding the operator rows moved no timing outside the run-to-run noise. The read
runs once per chunk of 200 CVEs, only on a changed write, a retry, or a
continuation.

**Reopen count per suppression mutation.** In that synthetic data, 50 changed CVEs
reach 722 of 902 scopes, and the single most common CVE has 703 holders; a rare
CVE (CVE-2026-020000) has 9. The skew is deliberately harsh. The cost of a hot CVE
is the holder reruns, not the read: a claim reopens at most 256 holders, so 703
holders take three claims of the committing item. A real operator mutation touches
the CVEs whose copies changed, so one mutation of N suppressions reopens the
distinct holders of at most N CVEs.

**In-flight read.** The real bootstrapped schema, 36,001 `fact_work_items` rows over
900 scopes and 40 generations each, 24 claimed: an Index Scan on
`fact_work_items_stage_domain_status_idx` (stage, domain, status), 3 buffers, 0.089 ms
execution, 24 rows. The set is bounded by the passes in flight at once.

**Fleet cost of the in-flight read.** During a burst of N changed passes about
every pass in flight at each commit is flagged, so the burst costs about 2N
passes and drains in about twice the time; continuous changed arrivals above
roughly a third of the domain's worker capacity (W=4) are not kept up with until
they stop; an idle scope and an unchanged rescan cost nothing.

*Measurement.* `TestFanOutRaceFleetBurstCostOfTheInFlightReadLive`, real Postgres
under `-race`: N=24 scopes, each holding a distinct CVE, so the row read finds no
holder and the in-flight read is the only fan-out path; all 24 have a status change
queued at once; 4 workers; a 400 ms pass; the in-flight read on, then replaced by a
wrapper that returns an empty set. Medians with the range in brackets:

| In-flight read | Runs | Passes (N=24) | Ratio | `replay_flagged` | `reopened` | Makespan |
|---|---|---|---|---|---|---|
| off | 7 | 24 (24 to 24) | 1.00 | 0 | 0 | 2.80 s (2.75 to 2.88) |
| on | 7 | 50 (50 to 50) | 2.08 | 26 | 0 | 5.67 s (5.61 to 5.77) |

Every run satisfied passes = N + `replay_flagged` + `reopened`: each flag is exactly
one rerun. A `reopened` of 1 is a holder that acknowledged between the in-flight
read and the reopen statement; it also costs one rerun (it appeared in some runs
before the rebase, not in these seven). The makespan ratio is 2.02. These runs are on
the rebased tree and the disposable-database harness, with the host load average
near 40 from other lanes: the pass counts do not depend on load and the makespans
are only as steady as that host. The measurement before the rebase (isolated schema,
11 runs on and 5 off) gave 50 (50 to 51) passes in 5.75 s against 24 in 2.74 s, a
ratio of 2.10.
The test fails above 2.5 passes per genuine pass, the arbiter's bound for building
the narrowing now, and below 1.5, where the read would have measured nothing.

*Model, not a measurement.* The arbiter's discrete-event model (W workers, oldest
first claim, every other in-flight item flagged on a changed commit, a flagged item
re-entering at the tail as a one-hop pass) gives, at W=4 and an 8 s pass, 2.08x,
2.00x and 2.00x passes for bursts of 40, 400 and 4,000, and a drain of 2.15x, 2.02x
and 1.99x of ideal. The measurement above agrees at N=24. The capacity ceiling
comes only from the model: continuous changed arrivals at 0.1 of the domain's
capacity cost 1.43x the work and keep up, 0.2 costs 2.14x and keeps up, 0.3 costs
2.84x and barely keeps up, and 0.4 or more costs about 3x with a backlog that grows
until arrivals stop. The ceiling is lower at higher W. It was not measured here.

Where the row read already reopens most holders (dense CVE sharing, as in the
reopen-count data above), the in-flight read's marginal cost is near zero. In the
sparse case measured above it is the whole 2x. The daily EPSS wave is one changed
source pass, so the in-flight read adds at most W-1 reruns to it.

*Mechanism, per scope.* A changed pass flags every scope in flight at that moment,
whatever CVEs it holds; a scope already flagged reports `pending_noop`, so a burst of
changed passes during one run causes one rerun. The rerun is a one-hop pass that
fans out nothing, so there is no cascade. It is itself in flight, so a changed pass
that commits during it flags it again, and while changed passes arrive faster than a
pass runs the scope reruns back to back and does not reach `succeeded`. The review's
scenario (one unrelated scope, a 400 ms pass, four workers, a changed pass every
`N`, 6 s of arrivals): `N` = 150 ms gives 14 reruns, 400 ms gives 13, 1 s gives 1,
3 s gives 1; a 4 s pass with arrivals every second gives 1 inside the window and
runs a third time once the arrivals stop (its rerun was flagged again). These count
reruns, not runs: the seeded first pass is excluded. The rate is bounded by
the lesser of the arrival rate and one rerun per pass time. An idle scope is never
touched.

*Consumers.* The only consumer found that reads an impact item's state is the
security-alert reconciliation of the same scope generation. It waits without
spending an attempt, up to `crossscope.ProducerReadinessMaxWait` (30 minutes) from
the newest in-flight activity of the item, and every rerun advances that anchor. A
storm therefore delays the reconciliation (outcome `deferred_in_flight`) and never
makes it give up and write unmatched rows. No other domain waits for this item to
succeed.

*Signals at 3 AM.* On `eshu_dp_supply_chain_impact_fanout_total` read
`replay_flagged` beside `reopened` and `pending_noop`; each set replay bit is one
rerun. Beside them put the rate of `eshu_dp_reducer_executions_total{domain="supply_chain_impact",queue="reducer"}`
and `eshu_runtime_domain_oldest_age_seconds{domain="supply_chain_impact"}`. A storm is
`replay_flagged` close to the execution rate, `reopened` flat, and the age rising.
The consumer-visible symptom is
`eshu_dp_security_alert_reconciliation_impact_wait_total{outcome="deferred_in_flight"}`
staying above its rate before the rollout.

**Narrowing: deferred, with a trigger.** Flagging only items claimed before our
commit does not help: the storm items were claimed before the later commits.
Flagging only scopes whose own facts name the CVE is unsound: the case the
in-flight read exists for is a holder with no rows, and scopes that expand CVEs
through packages (SBOM, OCI, package registry, repository) name no CVE in their own
facts. A scope-kind list is a static exception list. The only sound narrowing
carries the changed CVE ids on the flag (sorted, de-duplicated, capped, with a
sentinel above the cap that means unconditional) and lets the one-hop pass clear
the flag, fenced on the lease and on the stamp it saw, when the CVEs it derived are
disjoint from the set. It needs a conditional clear and a new race window with its
own live proof, so it is not part of this change. The arbiter accepted the cost as
built, on the condition that the fleet cost is measured and stated (above) and that
the narrowing is filed (#9740). The narrowing is built in this change instead if
the burst ever measures above 2.5 passes per genuine pass. Build the follow-up when,
after the rollout wave has drained, either holds over any rolling hour: the
`replay_flagged` rate is at least half the domain's execution rate and the domain's
oldest age rises for 30 consecutive minutes; or `deferred_in_flight` stays above its
pre-rollout rate.

**Owed marker.** Two item updates per changed pass, one inside the writer
transaction and one in the fan-out's final state update: 600 claimed items, mark
p50 178 µs and p95 300 µs, clear p50 156 µs and p95 269 µs. The marker is the
constant `true`; the largest item payload in that run was 234 bytes. With the stamp
and a continuation list (one scope id per holder, bounded by the number of scopes)
the payload stays under about 40 KiB in the worst case. The lock order is the
advisory lock, then the pass's own item row; every fan-out statement takes one item
row at a time and never the advisory lock, so no cycle can form
(`TestFanOutRaceConcurrentStressLive` runs 10 workers, 8 holders and the source,
16 rounds, under `-race`).

**Reopen statement.** 900 holder items on a private schema, sequential autocommit
statements, one sample:

| Case | Total | p50 | p95 | Max |
| --- | --- | --- | --- | --- |
| succeeded to pending (900 holders) | 125 ms | 124 µs | 227 µs | 1.13 ms |
| already pending, no-op (900 holders) | 64 ms | 71 µs | 78 µs | 0.22 ms |

A queued holder writes no row version (`TestReopenHolderTransitionsLive` compares
`xmin`). `TestMutualFanOutHasNoDeadlockLive` runs three concurrent sessions per
round over ten holders in ascending and descending order for 30 rounds under
`-race` with no deadlock and every holder ending pending.

**Stored-row read.** One scope with two generations of 20,000 active finding rows
each (40,000 rows read) among the 500,000, plus a 900-scope `scope_generations`
table: Bitmap Heap Scan driven by the `(scope_id, generation_id)` partial index,
8,366 buffers, 14.9 and 15.1 ms execution on the warm runs (28.0 ms on the first,
cold run). The previous-generation lookup is an InitPlan over
`scope_generations_scope_idx` and costs 0.02 ms. It runs once per write
transaction. The fact kind is a literal in the query so the partial-index
predicate holds under a generic plan; there is no array parameter.

**Privileges.** The writer path reads `scope_generations` inside the write
transaction (the previous-generation lookup above). The runtime-writer login
already needs SELECT on that table on main, for every reducer claim
(`reducer_generation_filter_sql.go:121-123`) and for the impact load
(`facts/impact/branches.go:82`), on the same pool as the writer. The protected
publish proof grants base tables explicitly, as a DBA pregrant outside the
migration set (243a); its set for this path is `fact_records`,
`supply_chain_impact_write_admission` and `scope_generations` (inside the
transaction), and `fact_work_items` and `ingestion_scopes` (the fan-out, outside
the transaction). There is no change to the protected role inventory. A
read-only probe of the reducer login in the QA environment (2026-11-01) found a
non-superuser role that owns `scope_generations`, with SELECT on
`scope_generations`, `fact_records` and `ingestion_scopes` and INSERT on
`fact_records` and `supply_chain_impact_write_admission`: that deployment uses one
credential for schema and runtime, so the new read needs no grant there, and a
role-bound deployment already needs the same pregrant for every reducer claim.

**Write-path CPU.** `BenchmarkWritePayloadWithoutDigest` against
`BenchmarkWritePayloadWithDigest` (1,000 findings, six runs each, `-benchtime=20x`):

| | ns per 1,000 findings | bytes | allocations |
| --- | --- | --- | --- |
| payload build, encode, marshal | 11.2 to 11.7 ms | 16.2 MB | 183,025 |
| plus the content digest | 13.5 to 13.9 ms | 19.1 MB | 196,030 |

That is about 2.3 ms more per 1,000 findings (about 20 percent of the payload
step, not of the pass), so a 20,000-finding scope pays about 46 ms of CPU per
pass. The digest marshals the typed payload a second time; hashing the already
encoded payload would remove most of it but would include `intent_id` and `cause`.

**What the first rollout does.** Rows written before this change carry no digest,
so the first pass of each scope after the rollout reads every row as changed and
fans out for every CVE it holds. That is intended: it heals copies written under a
1.5a-only image, which the changed-write rule alone would not. Its cost is bounded
by the holder cap per claim and by the no-op reopen of already queued holders. It
was not measured at deployed scale.

## Observability Evidence:

- Counter `eshu_dp_supply_chain_impact_fanout_total`, labels `domain` and
  `outcome`: `reopened`, `replay_flagged`, `pending_noop`, `enqueued` (per holder),
  `deferred` (holders carried to a later claim), `crash_recovery` (a pass that fanned
  out only because its attempt count was above 1), `read_error`, `read_timeout`.
- Span `supply_chain_impact.fanout` with `scope_id`, `generation_id`, and the
  holder, reopened, replay-flagged, and deferred counts; read and reopen errors are
  recorded on it.
- One info log line per pass (`supply chain impact holder fan-out`) with source
  scope, generation, intent, and the counts; a debug line per holder names the
  holder scope and outcome.
- Reducer result sub-signals `fanout_holders`, `fanout_reopened`,
  `fanout_replay_flagged`, `fanout_deferred`.
- A persistent failure is visible as the `read_error` or `read_timeout` outcome, a
  retrying row with failure class `supply_chain_impact_fanout_failed`, and finally a
  dead letter with that class.
- At 3 AM: a rising `deferred` with a flat `reopened` means a hot CVE is draining
  across claims; `crash_recovery` above zero means passes are being retried;
  `pending_noop` dominating means holders were already queued.

## Residual risks

- A holder reopened by the fan-out reruns a full pass. A hot CVE with many holders
  multiplies reducer work by the holder count; the cap bounds one claim, not the
  total.
- The digest includes the EPSS probability, percentile, and score date. Advisory
  age is `updated - published`, not wall-clock, so it does not drift; the daily
  driver is the EPSS rescore, and each daily rescore fans out to every holder of
  every rescored CVE once EPSS targets exist (none in QA). That is the correct
  refresh, not a defect, but it is a standing daily wave.
- A generation whose first pass derived zero findings stays a "first pass" for
  change detection, so each unstamped rerun reports the previous generation's CVEs
  as changed again. Cost only.
- A scope that loaded its evidence before the source's facts were visible, derived
  no finding row for the CVE, and acknowledged before the source's in-flight read is
  not a holder, and nothing reruns it until its own next generation. This predates the change and can occur with current
  collectors (KEV, EPSS, and alert scopes derive their own copies); the source
  scope's pass derives the finding from the same cross-scope evidence, so the
  missing copy does not by itself hide the finding.
- After the rollout, watch `reopened`, `deferred`, and `pending_noop` on
  `eshu_dp_supply_chain_impact_fanout_total` and the domain's backlog age for an
  hour. QA reference: about 137 `supply_chain_impact` items per OSV pass drain in
  4 to 5 minutes (`9292-reopened-backlog-age.md`).
- A holder that holds no active copy yet is not reached; it derives its first
  copy from current evidence when its own pass first runs.
- A dead-lettered holder item is left to the operator.
- The deployed-scale cost of the rollout wave, the real distribution of holders per
  CVE, and the digest drift are NOT_CHECKED.
