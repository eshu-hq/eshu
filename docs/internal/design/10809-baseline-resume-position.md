# NVD baseline resume position and seed rotation (#10809, #10810)

Status: **Implemented in core**; the collector half (`eshu-collector-vulnerability-intelligence`
#199) stamps the two new verdict keys. Extends
[10728-baseline-pacing.md](10728-baseline-pacing.md) sections 1 and 3, which
cover year order and the checkpoint. Decisions 4 to 7 of the 2026-11-16 baseline
pace ruling.

## Seed rotation: forward resumes, rollback refuses at commit (#10810)

The old plan-time hold (the checkpoint's seed sha no longer matched the
manifest) is deleted. It could never be evaluated: the coordinator cannot read
the manifest, and the SDK claim carries only the scope. Instead the collector
stamps `nvd_baseline_year_generated_unix_us` (the manifest's `lastModified` for
the year) beside the sha, and the checkpoint writer runs inside the generation's
commit transaction. The guard is part of the same upsert (`DO UPDATE ... WHERE`):
a stored stamp newer than the candidate's, or equal under a different sha,
touches no row, the writer returns an error with failure class
`nvd_baseline_seed_rollback`, and the whole generation rolls back. Folding the
guard into the upsert evaluates it against the locked, latest row version, so a
rollback cannot slip past a newer commit under READ COMMITTED (the live test
holds two transactions open). Forward is safe because a record's `lastModified`
only moves later, so no record moves from ahead of the position to behind it; a
record changed after a chunk read it is the window path's job. A candidate with
no stamp (an older collector, or a manifest entry with no `lastModified`) skips
the guard and commits as before, and it never clears a stored identity: the
position and chunk identity advance, but the stored sha and stamp stay, so a
later candidate stamped with an older seed is still refused. The sha and stamp
always move together. A strictly newer stamp moves them at any position
(#10746): a newer-seed candidate behind the stored position has committed its
facts, so the stored identity adopts it while the position, chunk identity, and
counters stay where they were, and a later older-seed candidate at or past the
position is refused instead of being accepted against an identity that never
saw the newer seed. Any other candidate moves the pair only at an equal or
greater position. The stored seed stamp never decreases; after any stamped
checkpoint commit it is the newest stamp a committed checkpoint advance carried.
The guard orders checkpoint advances only: a held or not-covered chunk commits
its facts and never writes the checkpoint, so the stored identity does not learn
the seed that chunk read. After a behind-position adoption the stored identity
is not the seed of `last_generation_id`: the identity names the newest seed
committed, the chunk identity names the chunk that last moved the position. The
other option the issue named, refusing a behind-position newer seed under a
distinct failure class, was not taken: it adds a class to the closed set the
status leaf, the OpenAPI enum, and the metrics docs pin, and the refused claim
retries against a position only the other commit order moves.

A refused claim fails retryable under its own class `nvd_baseline_seed_rollback`.
The generation dead letter is recorded under `commit_failure`, and after
`MaxAttempts` the claim goes terminal as `attempt_budget_exhausted`, so a seed
that never catches up ends dead-lettered rather than retrying forever. The
planner re-mints the same position after a refused claim and waits the 30 minute
retry wait. The forward-rotation counter (reported only when the stored seed
identity actually moves: a strictly newer stamp under another sha, at any
position) and the committed-chunk INFO line fire only after the commit lands.
Both ride a stash of at most 64 settlements per host process, written in the
commit transaction and read by the post-commit hook. A full stash evicts its
oldest entry to take a new one (the earliest stash time; equal times fall to the
smaller work item id, so the choice never depends on map order), and that is
never silent (#10746): each eviction counts on
`eshu_dp_nvd_baseline_settlement_evictions_total`, which carries no label, and
logs WARN `nvd baseline settlement evicted at capacity, post-commit signals for
that chunk may be lost` naming the dropped work item. The stash cannot tell a
stale entry (a commit that never landed) from a chunk that committed and is
waiting for its hook, so the signal says "may". The evicted chunk's facts and
checkpoint are unaffected. If it had committed, only its forward-rotation count
and committed-chunk line are lost, so after a rise in the counter read the
stored seed identity, not the rotation counter, to see which seed a year is on.
Both signals are reported after the stash lock is released, because every commit
worker shares it.

## The `advance_to` contract (`nvd_baseline_advance_to_unix_us`)

The satellite stamps one `nvd_baseline_advance_to_unix_us` per chunk beside
`nvd_baseline_year_sha256`, `nvd_baseline_year_complete`, and
`nvd_baseline_should_advance`, and, when the position falls inside an instant,
`nvd_baseline_advance_to_cve_id`; it also stamps the seed generation
`nvd_baseline_year_generated_unix_us` (the section above). The two keys added by
#10809 and #10810 are optional on the wire so an older collector commits as
before; the shared SDK vector for them lands with the satellite. The host
commit policy (`NVDBaselineCommitPolicy`) decides from them:

- **Meaning.** The position `(advance_to, advance_to CVE ID)` names the
  **first record the chunk did not emit**, in `(lastModified, CVE ID)` order.
  It is not "the last committed record". The next plan starts at it, and the
  start is an inclusive lower bound, so a record is never skipped and never
  doubled. An empty CVE ID means "the first record of that stamp". A chunk may
  cut inside an instant by CVE ID, which is how a same-stamp group larger than
  one chunk drains across chunks.
- **Precision.** The stamp is whole UTC milliseconds, the same form as the
  scope id and the claim's `ModifiedStart`. The host floors a finer value
  (`NormalizeNVDWindowTime`) before it compares or stores it, so a stamp with
  sub-millisecond digits cannot make the stored resume, the planned start, and
  the scope id disagree. The planner floors a stored sub-millisecond stamp and drops its CVE ID,
  because an ID inside a floored sub-millisecond stamp names a position that no
  longer exists; flooring only re-reads, which replays idempotently. A verdict
  that names a CVE ID inside a stamp that is not a whole millisecond is held as
  `position_malformed`, never floored with the ID kept.
- **Progress.** An incomplete verdict (`year_complete` false) must move
  **strictly forward** from the chunk start `(start stamp, from CVE ID)` in
  that row order, comparing CVE IDs as bytes. A verdict that does not is held
  as `not_covered`: the facts commit, the checkpoint stays, and the planner
  re-mints the same scope after the retry wait. A chunker that cannot move
  forward must fail loudly instead of stamping a non-advance. A CVE ID that is
  not canonical, or a seed stamp that is not a non-negative integer, is held as
  `position_malformed` or `generated_malformed`.
- **Completion.** A complete verdict (`year_complete` true) is accepted at any
  position not behind the chunk start. The last chunk of a year can end exactly
  at the stream tail with `complete=false`; the next chunk resumes there, finds
  nothing left, and has no later position to name, so it stamps
  `complete=true` at its own start. That verdict must land `year_complete` or
  the lane stalls on the year. `year_complete` is sticky in the store. No
  verdict, complete or not, may move behind the chunk start.

## Per-year plan row (#10828, migration 312)

The planner's two per-year inputs, `NVDBaselineLastPlanned` and
`NVDBaselineInFlightYears`, need a durable source that is not the checkpoint
row. The checkpoint commits with a chunk's facts, so it can only say what a
finished chunk covered. A chunk that is planned, claimed, or failed moves no
checkpoint, and that is the state the pace wait, the retry wait, and the
one-chunk-per-year rule read. Binding ruling: 2026-11-17, sections 3 and 4.

`vulnerability_nvd_baseline_plans` holds one row per `(baseline_run_id, year)`:
`last_planned_scope_id` and `last_planned_at`. No foreign key (work-item
retention deletes terminal items), no secondary index (the primary key is the
only access path), and nothing on the checkpoint row.

- Writer. `NVDBaselinePlans` (coordinator `vulnerability` package) derives the
  plans from the items that survived tenant authorization, inside the shared
  `createWorkflowWork` step, so the in-tree instance and a vulnerability
  activation cannot differ. `WorkflowControlStore.CreateRunWithWorkItemsRecordingBaselinePlans`
  upserts them in the admission transaction, after the work-item insert and the
  slot move and before commit. Only a chunk the insert accepted gets a row: a
  terminal-run skip, the open-target guard, or a primary-key conflict drops the
  chunk and writes nothing, and the row never names a work item that does not
  exist. The store re-checks every plan against the admitted items and the scope
  id first, and refuses the admission (before it opens a transaction) when a plan
  disagrees with its scope or two plans name one year. If the rows written differ
  from the chunks accepted, the transaction aborts.
- Last writer wins. The pointer always names the chunk the latest admission
  inserted. The upsert has no `WHERE` guard: a compare on time or position could
  leave an older scope id in place after a newer insert, and `DO NOTHING` would
  freeze a year on its first chunk.
- Clock. `last_planned_at` is `clock_timestamp()`, taken once per statement under
  the per-instance advisory lock, never `now()`. `now()` is the transaction start,
  which can predate a previous lock holder's stamp and would move the pace wait
  backwards. A future-dated stamp from a stepped clock is the planner slice's
  concern, as it is for the trip latch.
- Locks. No new advisory lock and none held across planning. The write reuses the advisory
  lock admission already takes for `(collector_kind, collector_instance_id)`.
  Two instances that share one baseline run id are not serialized by it (the key
  carries the instance), so the plan row lock is the serializer between them. The
  writer sorts its rows by `(baseline_run_id, year)` so two such admissions take
  row locks in one order, which cannot deadlock as long as PostgreSQL
  processes the rows in array order (measured on 18.6, see the evidence note);
  the plan write is the last
  statement before commit, so the contended row is held for the shortest time.
- Reader. `ReadNVDBaselinePlans(ctx, runID)` returns the year plans of a run and,
  in one statement and one snapshot, whether each year's last planned scope still
  has a work item in the open statuses (`pending`, `claimed`,
  `failed_retryable`, `expired`). The probe is keyed by `(collector_kind,
  scope_id)`: the `collector_kind` equality is load-bearing, because no index
  leads with `scope_id` and without it the planner picks the broad queue-depth
  index. A read failure is returned, never an empty map. A completed or
  terminally failed item, or one retention deleted, is not in flight; the row then
  only paces the year.
- Telemetry. `eshu_dp_nvd_baseline_plan_rows_total` (label `outcome`=recorded or
  not_inserted), one point per admission that carried baseline chunks, after the
  commit. The coordinator logs the same split at INFO.
- Rollout and rollback. The migration creates a new table nothing else
  references, so the schema rollback is `DROP TABLE
  vulnerability_nvd_baseline_plans`. A binary older than 309 never touches it. A
  309 binary against a pre-309 schema fails a baseline admission loudly
  (undefined table) and never touches the table for any other admission. Nothing
  reads the rows and the planner mints no baseline chunk in production until
  #10746, so no row is written today.
- Not here. The planner wiring and the per-tick read (#10746), the adaptive
  interval (#10814), and row retention (#10831). Evidence:
  (#10828).
