# Reconciliation Sweep

The reconciliation sweep is the periodic full re-observation that retracts graph
drift the git delta path cannot see. It is part of the
[Incremental Freshness Model](incremental-freshness-model.md); this page
documents when the sweep forces a full snapshot, how it is bounded, and how an
operator watches it.

## Why the sweep exists

Even with a correct baseline, a delta sync can leave stale graph nodes: a missed
deletion or a retraction that failed after its delta was applied has no later
delta that mentions the path, so the delta path alone can never clean it. A
periodic reconciliation catches this drift.

Reconciliation re-uses the proven full-snapshot path rather than a bespoke
graph-diff. When a git scope has gone longer than `ESHU_REPO_RECONCILE_INTERVAL_HOURS`
(default 24) without a projected **full** observation, the next sync forces a
full snapshot for that scope regardless of any usable delta baseline. A full
generation re-emits every current file under a new `generation_id`, and the
canonical projector retracts every File/Directory/Entity node carrying a
different `generation_id` for the repository — so any path that disappeared
between the last full observation and now is deleted from the graph.

Each generation records whether it was a delta on `scope_generations.is_delta`.
The sweep reads `FullReconcileState` for the scope: the ingest time of the
newest **activated** full generation, and the newest full generation of any
status. A reconciliation snapshot keeps its content freshness hint and carries
its reason on the generation (#7978). It never stores a duplicate:

- When the snapshot's hint (v3: content plus collector, parser, and fact-schema
  version) and commit equal the **active** full generation's, the ingestion
  store re-opens that generation's projector work instead of storing a new
  generation. The projector re-projects the same `generation_id`, which
  retracts every canonical node and edge stamped with another generation id.
  It records `reconcile_reason` and `reconcile_requested_at` on the generation,
  and the Ack records `reconcile_projected_at`.
- When they equal a **pending** generation, the store stores nothing: that
  generation's own projection is still queued.
- Otherwise (new content, a new commit, a version change, an active delta, or a
  default-branch change) the store stores a new generation as before.

A re-projection does not retract graph state stamped with the active generation
id itself; only a projector defect or a manual graph edit can create such
state (ruling a9-117 Q1).

The obligation is measured on the last projected full generation, and the
throttle on the latest full attempt (#7288). With `Interval` =
`ESHU_REPO_RECONCILE_INTERVAL_HOURS`, the first matching row decides; the reason
is the bounded label on the sweep's counters and log:

| Scope state | Decision | Reason |
| --- | --- | --- |
| active full generation has a re-projection request younger than `Interval` and no Ack since | hold | `reconcile_in_flight` |
| the same request is `Interval` old, or its re-projection was dead-lettered, and no forced full is in flight or backing off | force; the store stores a generation unless one with the same identity is pending with live work (a pending full at the remote head with live work holds as `in_flight_expired_held`) | `reconcile_request_stale` |
| newest activated full generation (or its last re-projection Ack) is younger than `Interval` | hold | `fresh` |
| newest full generation is `pending`, younger than `Interval` | hold | `reconcile_in_flight` |
| newest full generation is `pending`, `Interval` old or older, at the remote head, with live projector work (pending, retrying, claimed, or running) | hold, checked after the fetch | `in_flight_expired_held` |
| newest full generation is `pending`, `Interval` old or older, and the head moved or its work is gone | force | `in_flight_expired` |
| newest full generation failed, or was superseded before it activated, and is younger than `Interval / 4` | hold | `reconcile_retry_backoff` |
| the same, `Interval / 4` old or older | force | `retry_after_unprojected` |
| no full generation ever activated | force | `never_reconciled` |
| otherwise | force | `interval_elapsed` |

A clock-skewed (future) ingest time counts as younger than every bound and holds.
A resolver read error holds as well, so a Postgres outage does not also trigger
a fleet of forced full snapshots.

Without the in-flight rule, a projector that ran behind the selection cycle
(about six minutes) made the sweep force a new full snapshot of the same commit
every cycle, and each one superseded the previous before it could activate.
Without the backoff, a full generation that failed or was superseded by a delta
before activating would make its scope due again on the next cycle. No single
generation suppresses the sweep for longer than `Interval`, and the per-scope
worst case is one forced full per `Interval` while fulls stay pending, or four
while they keep failing. The retry backoff is derived from `Interval`; there is
no separate setting. A sweep that stays ineffective because nothing ever
activates is a projection outage, visible through the counters below.

The sweep is bounded and scheduled, not a full re-index:

- **Scheduled**: it rides the normal sync cadence; a scope is re-observed fully
  once per interval, then resumes delta sync.
- **Bounded per cycle**: `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` (default 10) caps how
  many overdue scopes one selection cycle may force to full, so a fleet that all
  comes due together does not stampede into simultaneous full snapshots. The
  remainder are picked up on later cycles.
- **Disable**: set `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0`. A
  [reindex request](#reindex-requests) is still honored.

Cost is one full re-observation and projection per scope per interval — the same
cost as a first sync, paid on a documented cadence. For an unchanged repository
the collector skips the re-observation and keeps only the re-projection (see
[Skipping the parse](#skipping-the-parse-of-an-unchanged-repository)). Each forced reconciliation
increments `eshu_dp_collector_reconciliation_full_snapshots_total`, labeled by
`reason` (`never_reconciled`, `interval_elapsed`, `in_flight_expired`,
`retry_after_unprojected`, `graph_dirty`, `reindex_requested`,
`repository_reindex_requested`, `reconcile_request_stale`), and logs `git_reconcile_forced` with
`scope_id`, `reason`, `last_projected_full_at`, `latest_full_at`, and
`latest_full_status`. Any log of a scope that has a recorded reindex
watermark also carries `reindex_requested_at`, whatever the reason: it is the
watermark, not the reason, and watermarks are never cleared.
The log is WARN for `in_flight_expired`, `retry_after_unprojected`, and
`reconcile_request_stale` (which also logs `reconcile_requested_at`,
`reconcile_projected_at`, and `reconcile_dead_lettered_at`): repeats of any
mean projection is not keeping up with the sweep. Each evaluation the
sweep holds off increments `eshu_dp_collector_reconciliation_suppressed_total`,
labeled by `reason` (`reconcile_in_flight`, `reconcile_retry_backoff`,
`in_flight_expired_held`). A sustained suppression rate with few forced
reconciliations points at projector lag, not at the sweep.
`in_flight_expired_held` (ruling a9-126) means a full generation has been
pending for longer than `Interval` at the remote head and its projector work is
still live, so the sweep holds the scope instead of parsing it again: the
reducer or projector is behind on this scope. Read the projector queue for the
scope and the INFO log `git_reconcile_held` (`scope_id`, `forced_reason`,
`pending_commit_sha`, `latest_full_at`). A held scope takes no
`ESHU_REPO_RECONCILE_MAX_PER_CYCLE` slot. When the head moves or the work row
is gone (missing or terminal), the sweep forces the scope again.

The store side of each forced snapshot shows on
`eshu_runtime_refresh_skipped_by_reason_total{reason}`: `reconcile_reprojected`
(the active generation was re-opened, nothing stored), `reconcile_reprojection_pending`
(its work was already open or leased), `reconcile_superseded` (a newer
generation won the scope lock; the snapshot is dropped), and
`pending_same_identity` (a pending generation with live projector work already
holds that content; a pending match without live work is not a duplicate, so the
snapshot is stored and supersedes it). A
reconcile re-projection that is dead-lettered before its Ack leaves the
generation active and its scope pointer in place; the next sweep forces
`reconcile_request_stale`, which stores a generation. Any other dead letter of
a generation (a #7209 heal or liveness re-drive, a rebuild re-drive, or a
first projection) marks it failed and clears the pointer, as before #7978
(rulings a9-130, a9-130.1).

When the canonical graph writer applies that forced snapshot, successful stale
cleanup statements also increment
`eshu_dp_reconciliation_drift_retractions_total` with bounded labels:
`domain="canonical_graph"`, `write_phase`, and `kind="node"` or `kind="edge"`.
The full-snapshot counter answers "did reconciliation run?"; the drift
retraction counter answers "did it actually delete stale graph state?" A
re-projection of an already-active full generation (#7978 reconcile, #7209
heal of any collector, liveness re-drive, rebuild or refinalize) counts as a
reconciliation there; a first projection does not. Alert on
sustained nonzero retractions after the first reconciliation window, and use
projector logs/spans keyed by `scope_id`, `repo_id`, and `generation_id` for the
specific source. Those identifiers are intentionally not metric labels.

## Skipping the parse of an unchanged repository

Before the skip, a forced reconcile of an unchanged repository fetched, checked
out, discovered, parsed, and built the whole fact stream, and only then did the
store notice that nothing had changed and re-project the active generation
(#7978). Measured on one host: about 24 s wall and 43 s CPU for a 2,011-file
repository, and about 3.7 min wall and 6 min CPU for a 26,301-file one.

The collector now decides before the parse (ruling a9-143). After the fetch it
computes a **pre-parse identity**: the content version, the remote head, the
default branch, and every configuration input that changes emitted facts (the
discovery ignore and preserve globs, the [value-flow gate](value-flow-emission.md), `SCIP_INDEXER`,
`SCIP_LANGUAGES` and the stat of each SCIP indexer binary, and the bootstrap
dependency settings). Every stored generation and every re-open after a parse
stamps the identity on the generation. The first sweep after a deploy parses
and stamps; later sweeps compare. The ingester and collector-git run the skip on
both their scheduled and webhook selectors; bootstrap-index only stamps. A
snapshot that fell back from SCIP or could not read a file stamps no identity
(INFO `git_snapshot_preparse_identity_dropped` with the `reason`), so the next
sweep reads `identity_absent` and parses again.

| Condition | Result | `outcome` |
| --- | --- | --- |
| reason is not `interval_elapsed` or `graph_dirty` (a reindex, a stale request, a pending or failed full, no projected full) | parse | `reason_ineligible` |
| `ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS=0` | parse | `disabled` |
| no active generation, or the active generation is a delta | parse | `no_active_full`, `delta_active` |
| the remote head is not the active generation's commit | parse | `head_moved` |
| no stored identity yet, or it differs (content version or configuration moved) | parse | `identity_absent`, `identity_mismatch` |
| the repository has `.gitmodules` | parse | `submodules_present` |
| the scope already skipped N-1 times since its last parse | parse | `valve_due` |
| the checkout has untracked or modified files | parse | `dirty_tree` |
| the tree check or the store call fails, or the store answers with an unknown reason | parse | `read_error` |
| the store fence refuses the re-open (another generation is active, or the commit or identity changed) | parse in the same cycle | `reprojection_superseded` |
| the re-projection is already open or leased | no parse, nothing stamped | `reprojection_pending` |
| otherwise | re-project the active generation, no parse | `skipped` |

A skip still re-projects, so drift is retracted as before and the skip spends an
`ESHU_REPO_RECONCILE_MAX_PER_CYCLE` slot. The store re-open is fenced on the
active pointer, a full generation, the commit, and the stamped identity, under
the same scope lock and in-flight guard as the store's own re-open; it stamps
`reconcile_requested_at`, so an unacknowledged or dead-lettered re-projection
follows the same `reconcile_in_flight` and `reconcile_request_stale` rules.

`ESHU_REPO_RECONCILE_PARSE_MAX_SKIPS` (default 7) is the valve: an unchanged
repository parses at least every N intervals, so an input the identity does not
cover stays stale for at most N intervals; at the daily default that is one
parse per repository per week. Set a larger N to parse less often, or 0 to
always parse. A negative or non-integer value stops the collector at startup
with an error that names the variable and the value.

Each decision increments
`eshu_dp_collector_reconcile_parse_skip_decisions_total{outcome}` and adds a span
event `git.reconcile.parse_skip_decision` (`scope_id`,
`eshu.reconcile.parse_skipped`, `eshu.reconcile.parse_skip_outcome`). A skip or a pending re-projection logs INFO
`git_reconcile_parse_skipped` (`scope_id`, `reason`, `generation_id`,
`commit_sha`, `preparse_skips`, `config_digest`, `outcome`); an identity
mismatch, a fenced re-open, or a store error logs WARN
`git_reconcile_parse_skip_refused`. The store side still counts
`reconcile_reprojected` on `eshu_runtime_refresh_skipped_by_reason_total`. A
sustained `identity_mismatch` rate without a deploy that changed the content
version or the configuration needs a look at `config_digest`.

Submodules: the managed clone does not check out submodule content, so a
repository with `.gitmodules` always parses. Windows: the check uses `git status`
and file stats only, the same calls on every platform; the Windows-specific
files in this package serve filesystem copy mode, which has no commit and no
identity.

## Reindex requests

`POST /api/v0/admin/reindex` (or `eshu admin reindex`) forces a full re-parse
of every git repository, for example after an upgrade that changes what the
parsers emit (#7620). It records a fleet **reindex watermark**:
`runtime_ingester_control.reindex_request_requested_at` for the `repository`
ingester, stamped by Postgres and never moved backward. The response returns it
as `requested_at`.

Every git ingester shard reads the watermark once per sync cycle, one
primary-key read. A watermark is checked before the sweep and `graph_dirty`
(ruling a9-125): while it is pending it decides the scope, so the forced full
carries the reindex reason and stores the re-parse instead of re-projecting.

| Scope state | Decision | Reason |
| --- | --- | --- |
| newest activated full generation was itself stored (ingested) at or after the watermark; a re-projection Ack does not count | the sweep and `graph_dirty` decide as usual | `fresh` or a sweep reason |
| in-flight or recently failed full (the throttle rows above) | hold | `reconcile_in_flight` or `reconcile_retry_backoff` |
| newest full is `pending`, `Interval` old or older, at the remote head, with live projector work | hold, checked after the fetch (ruling a9-128) | `in_flight_expired_held` |
| otherwise, including a scope with no activated full | force | `reindex_requested` |

The hold covers an explicit reindex too (ruling a9-128). While a full of the
same commit is pending with live work, the store would skip any snapshot of
that commit as `pending_same_identity`, so a forced parse there could not
change anything. The re-parse waits one activation of that pending full: if
it activates and was ingested before the watermark, the next cycle forces
`reindex_requested` and the store keeps the re-parse; if it was ingested at or
after the watermark, it is the re-parse. A moved head, or work that is no
longer live, forces at once. A held scope logs `git_reconcile_held` with
`reindex_requested_at`.

A forced reindex takes no delta and a slot in
`ESHU_REPO_RECONCILE_MAX_PER_CYCLE`, so a large fleet re-parses over several
cycles. An explicit reindex request stores the re-parse as a new generation
even when the content identity (commit, hint, and content version) is
unchanged (ruling a9-125); the sweep, `graph_dirty`, webhook, and scheduled
sync re-project the active generation instead (#7978). A pending watermark
beats `interval_elapsed` and `graph_dirty`, and only a stored full ingested at
or after it satisfies it, so a re-projection never answers a reindex. The content-version
gate narrows, but cannot close, the window where a parser change does not
change the hint; an explicit reindex is the remedy. The request is never claimed and has no
completion status. It is satisfied scope by scope: a forced full is ingested at
the cycle's start time, which is at or after the watermark, so once it activates
the scope stays `fresh`. A later request moves the watermark forward and starts
a new pass.

Send the request only after every git ingester runs the new binary. The
watermark records when a full generation was ingested, not which parser
produced it. During a rolling upgrade, a shard still on the old binary can
produce fulls after the watermark, and those scopes then count as `fresh` with
the old parser output. An ingester clock that runs ahead of Postgres widens
this window by the skew. If that happens, send another request once the rollout
completes.

- A shard whose cycle started before the watermark (clock skew between the
  ingester and Postgres) skips it that cycle and logs INFO
  `git_reindex_watermark_deferred`. Otherwise every forced full would land
  before the watermark and be forced again.
- A failed read is ignored for that cycle and logs WARN
  `git_reindex_watermark_read_failed`.
- Each cycle with an active watermark logs DEBUG
  `git_reindex_watermark_active` with `repo_shard_index`, `repo_shard_count`,
  and `reindex_requested_at`. It is DEBUG because the watermark persists
  after the pass completes. Each forced scope logs INFO `git_reconcile_forced`
  with `reason=reindex_requested`.
- With `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` the watermark is still honored,
  with the throttle bounds of the 24-hour default. The interval reasons and
  `graph_dirty` stay off. The watermark is never cleared, so after the first
  request each cycle reads the per-scope state the default sweep reads, and a
  scope whose fulls keep failing is retried on the throttle bounds.
- A watermark stamped in the future (for example after the Postgres clock
  jumped forward) defers every shard until clocks pass it. To recover, set
  `reindex_request_requested_at` for `ingester = 'repository'` in
  `runtime_ingester_control` back to the intended time.
- A webhook-only ingester reaches only the repositories it is triggered for.
  Filesystem source mode does not read the watermark.

Watch progress with
`eshu_dp_collector_reconciliation_full_snapshots_total{reason="reindex_requested"}`
and the `git_reconcile_forced` log. When that counter stops rising while the
suppression counter is flat, no scope the sync visits is still due. A scope the
sync never visits is not covered (#7625).

### Per-repository reindex requests

`POST /api/v0/admin/reindex` with `"scope": "repository"` and a `repositories`
list of 1 to 100 selectors (or `eshu admin reindex --repository <selector>`,
repeated) re-parses only those repositories. The API resolves each selector to
exactly one git default-branch repository scope. If any selector matches no
repository, matches several, or resolves to a ref or non-git scope, it returns
400 naming every such selector and records nothing. Otherwise it upserts one
row per distinct scope into `repository_reindex_requests (scope_id,
requested_at)`, stamped by Postgres and never moved backward, and returns each
repository with its `scope_id` and `requested_at`.

Each shard reads the rows newer than the fleet watermark active for the cycle
(every row when none is active, including a deferred or unreadable fleet
watermark) once per sync cycle, right after it reads the fleet watermark. A
scope's effective watermark
is the later of the two, so a per-repository row at or before the fleet
watermark adds nothing. The decision table above applies unchanged; a forced
scope records reason `repository_reindex_requested` when its own row is the
later watermark, otherwise `reindex_requested`.

- Requested repositories are synced first in the cycle, so a fleet reindex
  cannot starve a targeted one behind `ESHU_REPO_RECONCILE_MAX_PER_CYCLE`. The
  budget itself is unchanged. Because rows are never deleted, a requested
  repository stays first in the sync order, even once satisfied, until a newer
  fleet request supersedes its row. Order matters elsewhere only for the
  fleet-wide pinned-ref worktree cap in
  [Ingestion and queue environment](environment-ingestion-queues.md).
- Each row is deferred on its own when it is later than the cycle start; a
  cycle with deferred rows logs INFO `git_repository_reindex_deferred` with
  `deferred_count`. A cycle with active rows logs DEBUG
  `git_repository_reindex_active` with `active_count`. A failed read is
  ignored for the cycle with WARN `git_repository_reindex_read_failed`; the
  fleet watermark still applies.
- With `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` and no fleet watermark, only
  scopes with a row read their per-scope state.
- Rows are never deleted, like the fleet watermark. The read is a sequential
  scan of a table with one row per repository ever requested, about 1 ms per
  10,000 rows. A webhook-only ingester honors a row only when the repository is
  triggered, and filesystem source mode does not read the table. Send the
  request after every git ingester runs a binary that reads the table: older
  ingesters ignore it.

A refinalize (`POST /api/v0/admin/recover-generations`,
`POST /api/v0/admin/refinalize`, or the runtime `/admin/refinalize`) writes the
same rows, with the same upsert, for each git default-branch scope it
re-projects through a delta generation (#7797). It does that inside its own
transaction, so the rows commit or roll back with the re-enqueue. A delta
restores only its changed files onto a wiped graph; the forced full restores the
rest. The response reports those scopes in `delta_active_scopes` and the rows
in `reindex_requests_written`. As with any per-repository row, a webhook-only
ingester honors it only when the repository is triggered. See
[Rebuild the graph from facts](../operate/graph-rebuild-from-facts.md#delta-active-scopes).

Watch progress with
`eshu_dp_collector_reconciliation_full_snapshots_total{reason="repository_reindex_requested"}`
and the `git_reconcile_forced` log for the requested `scope_id`.

## Delta baseline fence

A delta generation is the diff from the scope's active commit, read when the
collector starts its cycle, to the new remote head. Another generation can
activate while that delta is parsed, committed, or projected (#7319). The
collector therefore records the commit it diffed from on
`scope_generations.delta_baseline_commit_sha`, and the projector enforces one
invariant:

> A delta generation activates only while
> `active(scope).source_commit_sha == delta.delta_baseline_commit_sha`.

The projector checks it twice with the same decision table. The preflight check
runs before the work item loads facts or writes the graph. The Ack check runs
inside the Ack transaction as its own statement, after Ack has taken the scope
row, so it sees every Ack that committed while it waited. Only commits are
compared, never generation ids.

| Read | Outcome |
| --- | --- |
| full generation (no baseline) | proceeds, not counted |
| delta written before migration 148 (no baseline) | proceeds as `unfenced` |
| the delta's own generation is already active | proceeds as `already_active` |
| the delta's own generation is already superseded | proceeds, not counted, as `target_superseded` |
| active commit equals the baseline | proceeds as `matched` |
| active commit differs, or is empty | refused as `refused_active_differs` |
| no active generation | refused as `refused_no_active` |

A target already superseded by a newer generation's activation is not a fence
refusal: it is the normal, expected outcome of projector lag, already owned by
the pre-existing superseded-generation path (Ack's own activation predicate
refuses it there exactly as it would a raced full generation, logging at INFO
with `failure_class = projector_ack_generation_superseded`; a preflight read
proceeds and lets Ack or a heartbeat catch it, the same way an unfenced full
generation already does). The fence checks target-superseded before comparing
commits, so a delta that can never activate again is never scored against a
baseline it has no further claim on.

A refused delta's work row and generation become `superseded`, never `failed`,
so replay and dead-letter drains leave it alone. The work row carries
`failure_class = projector_delta_baseline_mismatch` when preflight refused it
(the graph was not touched) or `projector_delta_baseline_mismatch_after_projection`
when Ack refused it, and `failure_details` names the scope, generation, baseline
commit, active commit (or `none`), active generation, and phase. The next
collector cycle diffs from the new active commit, so recovery needs no trigger.

What the fence does not enforce on its own: that the graph content equals the
baseline commit. The write-through contract (#7389) closes that gap. Before
its first graph or content write, after it loads facts, a projector attempt
sets its generation's `projection_write_started_at` marker (the latest write
start, fenced on its claim). The heartbeat never supersedes a generation whose
marker is set for a newer one, so a writer runs to Ack; a newer delta diffed
from the same active commit is then refused at preflight before it writes, and the
collector's next delta is diffed from the writer's commit. A generation that
wrote and still never activated keeps its marker. The heartbeat never
abandons a started write to a newer generation, but other paths still retire
one: the claim path's stale-generation supersede and Ack's obsolete supersede
(a marked pending generation whose work was retrying), a dead letter, a lease
expiry, or an Ack refusal. Each is healed by a forced full: while such a
writer is uncovered, the sweep forces the scope with reason `graph_dirty` (WARN
log `git_delta_baseline_graph_dirty`), held off only by the in-flight and
retry-backoff throttle, never by a fresh full, and counted against
`ESHU_REPO_RECONCILE_MAX_PER_CYCLE` (a scope over budget stays on its delta and
is forced a cycle later). Setting `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` also
disables this heal. A marked writer is released only by its own completion,
failure, or a lease expiry after its process dies; on Neo4j
`ESHU_CANONICAL_WRITE_TIMEOUT` defaults to `300s` (#7471), so a hung write
aborts and requeues at the bound instead of freezing that scope's freshness
until the worker restarts (which now takes an explicit unbounded opt-out).
The heal is paced by that throttle: a pending heal full holds the scope for up
to the reconcile interval, and a heal full superseded or failed before
activation backs the scope off a quarter of the interval. The claim path
supersedes a pending full when a newer delta arrives before it is claimed, so a
hot repo under projector backlog repeats the heal in quarter-interval steps
rather than at the next sync (about 28 consecutive attempts at the defaults
before a 168-hour retention prune could remove the marker). Retention of an uncovered writer before its heal
is a tracked follow-up.

`eshu_dp_projector_delta_baseline_fence_total` counts decisions by `phase`
(`preflight`, `ack`) and `outcome`. Passes are counted once, at Ack. A
preflight `refused_active_differs` is expected whenever a newer generation
arrives while an older one is writing; its rate should not exceed the scope's
generation commit rate. A rising refusal share beyond that means the projector
lags the collector. Any `phase=ack` refusal
(`refused_active_differs` or `refused_no_active`) means two claims were valid
in one scope and logs at ERROR; a preflight refusal logs at WARN. This holds
because a target already superseded by a newer activation never reaches a
refused outcome here -- it is `target_superseded` (see above), so it cannot be
mistaken for the two-valid-claims signal. `unfenced` should fall to zero once
every collector writes the baseline. Commit SHAs appear in logs and
`failure_details`, never as labels. The collector's `git repository sync
completed` log carries `delta_baseline_commit_sha` for a delta sync.

Rollout order is migrations 148-150, then binaries. Deltas written by an older
collector, or in flight during the rollout, have no baseline and are not fenced.

## Related references

- [Incremental Freshness Model](incremental-freshness-model.md)
- [Telemetry](telemetry/index.md)
