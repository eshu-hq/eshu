# 7209: heal the active generation after a zombie refusal — evidence note

The #7209 fix re-opens the scope's current active projector row when
Heartbeat or Ack refuses a zombie whose generation may have written
(`ProjectorQueue.healZombieRefusal`,
`go/internal/storage/postgres/projector_queue_zombie_heal.go`, hooked from
`refuseSupersededAck` and the already-superseded Heartbeat branch in
`projector_queue_scan.go`). A zombie that marked before its first write and
was refused after a successor published may have retracted the successor's
canonical nodes; the re-opened row lets a fresh worker re-project over that
damage through the unchanged claim/mark/write/ack path.

Gate (arbiter Option B, recorded on the issue): the refused generation
must have `projection_write_started_at IS NOT NULL`, checked atomically in
the heal statement. The marker is set before the first graph/content write,
a retired generation can never mark afterwards, and no supersede clears
it, so NULL means the generation provably never wrote. This dominates the
issue's "active when claimed" proxy on both recall (a pending-at-claim
writer can damage) and precision (an active-at-claim non-writer cannot),
and it excludes the Heartbeat replaced-by-newer path structurally (that
trigger requires a NULL marker). The heal additionally requires a
different, still-active current generation, and the ON CONFLICT DO UPDATE
carries the liveness write-time in-flight guard, so a live claim or a
prior heal wins and concurrent heals converge on one open row
(EvalPlanQual rechecks the guard on the locked conflict row).

The statement locks no scope or generation row: a blocking scope lock
would stall the refusing Heartbeat/Ack behind ingestion commits that hold
the scope row while streaming facts, and SKIP LOCKED would silently drop a
heal the dead zombie never retries. Residual TOCTOU: an Ack committing
mid-statement can move the pointer after the statement's snapshot,
reopening a just-superseded generation (swept back by claim maintenance)
or missing a just-published one — today's behavior plus a swept row.
A second residual: a live worker that already wrote before the damage
keeps its claim (`skipped_in_flight`) and nothing re-drives after its Ack;
that interleaving needs a live projection overlapping the sub-heartbeat
damage window, and the fix strictly shrinks the hole (before, every case
was a hole).

## No-Regression Evidence:

One new statement, only on the refusal path: a PK-driven upsert plus PK
lookups, no new locks held across statements, no claim/lease/order change,
no new index (all access is by primary key). EXPLAIN (ANALYZE, BUFFERS)
on the seeded proof shape shows Index Scans on `scope_generations_pkey`
(refused marker probe) and `ingestion_scopes_pkey` (active pointer),
Insert on `fact_work_items` with Conflict Resolution UPDATE arbitrated by
`fact_work_items_pkey` and the in-flight guard as the conflict filter,
10 shared buffer hits total, sub-millisecond. The steady-state Heartbeat
adds one `rows.Close()` earlier (connection hygiene for
single-connection pools; the deferred Close stays) and no new query. The
re-drive itself reuses the existing claim/mark/write/ack path, so
canonical-write behavior is unchanged: B-7 golden-corpus and B-12 snapshot
updates are not needed (no cassette or pinned result changes; replay has
no zombie refusals). Baseline: clean origin/main — refused zombie Ack
and Heartbeat leave gen-new `succeeded`, RED at
`TestProjectorRefusalHealsMarkedSupersededGeneration` and
`TestProjectorZombieHealRestoresCanonicalNodesLive`. After: the refusal
logs `projector zombie refusal healed the active generation
scope_id=scope-zh refused_generation_id=gen-old
healed_generation_id=gen-new outcome=healed`, re-opens exactly one
pending row (`zombie_heal_count=1`), repeated refusals converge
(`skipped_already_open`), and the Neo4j proof shows gen-new files 2 → 0
under the zombie retract → 2 after the heal drains, gen-old 1 → 0.

## Observability Evidence:

New counter `eshu_dp_projector_zombie_heal_total` by closed `outcome`
(`healed`, `skipped_in_flight`, `skipped_already_open`,
`skipped_no_write_marker`, `skipped_no_active_generation`, `error`),
counted once per Ack or already-superseded Heartbeat refusal from
`healZombieRefusal`; nil instruments are a no-op. Each heal logs INFO
with `scope_id`, `refused_generation_id`, `healed_generation_id`;
skips log DEBUG with `scope_id`, `refused_generation_id`, `outcome`;
statement failures log ERROR (`... may be missing nodes`) and count
`error`. Coverage row extended in
`docs/public/observability/telemetry-coverage.md`, reference in
`docs/public/reference/telemetry/metrics.md`. An operator joins a heal
to its refusal by `scope_id` + the fence counter's `failure_class`
(`projector_ack_generation_superseded` /
`projector_heartbeat_generation_superseded`); a nonzero `error` or
`skipped_in_flight` rate is the follow-up signal.
