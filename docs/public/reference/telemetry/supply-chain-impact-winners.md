# Supply-Chain Impact Winners Resweep

The reducer keeps a read model of one winning finding per canonical key, so the
impact-findings list does not sort every finding at read time. A maintainer
rebuilds that read model in a loop under a single-owner lease, waiting 30 seconds
between resweeps. The 30 seconds is the pause, not the period: the tick is the
resweep time plus the pause. A keyed tick that leaves dirty-log rows queued
re-ticks immediately instead of waiting out the pause (#10706), so a backlog
drains as short spaced ticks. These signals say what each rebuild wrote and how
far behind it runs.

## How a resweep writes

A full resweep (stored contract missing, NULL or lower), a keyed tick that
passes the cap, and the audit's read recompute every winner from the active
facts; an ordinary keyed tick recomputes only the dirty-log keys (see "Keyed
resweeps and the audit"). Any resweep writes only new or changed rows,
deletes rows that are no longer winners, and stamps the read watermark: the
DB-clock tick stamp when the dirty log drained, or the horizon over queued
rows (#10706) — earliest queued capture time minus one microsecond, so every
capture at or under the watermark is applied except the documented one-tick
late-commit residual, which the next tick applies. A full resweep also
consumes what it covers (#11117): in its own transaction it reads the DB
clock, deletes the visible dirty-log and staged marks, rebuilds, and stamps
that reading capped under any still-queued mark; a later-committed mark stays
queued for the next keyed tick. An unchanged winner row is not locked,
versioned, or WAL-written. Freshness comes from the singleton watermark,
which every committed resweep stamps, so even a no-op resweep tells readers
the model is current.

Only one resweep runs at a time across reducer instances. The lease is not
renewed during a resweep, so its 60 second TTL can lapse under a long
statement and let a second instance start; that instance finds the first
one's lock, skips, and stamps nothing (comparing against an uncommitted
table could watermark over a missing winner). The next tick runs normally.
If the backend running a resweep dies, the lock goes with it.

On a winner row, `materialized_at` means the last content change. Do not use it
for freshness; read the watermark.

## Keyed resweeps and the audit

A full resweep recomputes every winner and records the winners contract
version and hash with the watermark (#11117, migration 329). Every maintainer
tick reads that row first. With no watermark row, or a stored version that is
NULL or below this binary's, the tick runs one full; every instance tries, and
the resweep lock lets exactly one commit. With an equal version the tick is
keyed and the audit runs, so a reducer roll with an unchanged contract runs no
full. With a higher stored version (a newer binary already committed) this
binary stands down: no full, keyed tick or audit. Rolling back to a binary
that predates the contract, or across a contract version bump, must set both
contract columns to NULL as a runbook step, so the next binary that reads them
runs one full. A keyed tick works this way: changes that can move a winner
append the canonical keys they touched to a dirty log, in the same
transaction as the change. Two captures exist: the impact writer names its
own changes in application code (inserted, rewritten or retracted findings),
and database triggers (migration 272, plus 272a for scanner tables) cover
the rest — a move of `ingestion_scopes.active_generation_id` (projector Ack
and Fail, collector scope upsert, retirement fence, operator suppression
store), a delete of an `ingestion_scopes` row (retirement purge), and
inserts, updates and deletes on the scanner classification and association
tables. The delete trigger runs before foreign keys cascade the scope's
facts away and appends every finding identifier the scope held, so a
retirement that fences and purges a scope before the next tick still repairs
its winners. A keyed tick consumes the log and recomputes only those keys;
cost follows changed keys, not table size. An empty tick only stamps the
watermark. Consumed keys past a cap (50,000 identifiers, or half the stored
winners) run the full rebuild in the same transaction instead, sampled
under `path="full"`.

One keyed tick is chunk-bounded (#9805, Dec 6(ii)): it reads the DB-clock tick stamp,
admits the 5,000 dirty-log rows with the oldest capture times (ordered by
`captured_at`, then `seq`, #11075) as candidates, then admits the oldest-first
budgeted prefix of the post-expansion identifiers: at most 200 identifiers
and at most 10,000 pre-filter scanned index tuples (the primary bound against
scope-expanded fan-out; #11120 redefined the unit from heap copies to index
tuples consumed, still bounding both heap terms). The tick probes identifiers
one at a time and stops admitting new ones once the budget is spent (the
current identifier is always admitted and never split, completing iff its
probe stays within the 120 s statement guard — see #11442), then deletes
exactly the rows it applied. Admission follows capture time, not insert order,
because the horizon is the oldest remaining capture: a converted scope
remainder keeps its old capture time on a new `seq`, and admitting by `seq`
left it behind every newer row while the watermark stood still. A tick that
stopped at either bound leaves the rest queued, stamps the horizon over them,
and counts `eshu_dp_supply_chain_impact_winners_chunked_ticks_total`, so a
backlog drains as short spaced ticks and the watermark advances on every
tick. The span and the commit log
line carry `bounded`, `remaining`, `remaining_upper_bound`, `chunk_identifiers`,
`chunk_kept_rows`, `chunk_scanned_copies`, and `chunk_truncated_by`
(`identifiers`, `scanned_copies`, or both; empty on a drained tick) for the
chunk the tick probed, kept, and scanned pre-filter (#11308): since #10880
PR-C1 the count reads the narrow temp table persisting the supports probe,
so the count attributes a tick's build cost to cost-per-copy against
kept-rows-per-identifier, and a budget trip stays distinguishable from an
identifier-bound trip in the log; since #11120 the span and log line also carry `probe_variant` (`top1` for the marker-gated TOP-1 probe, `fallback` for the pre-marker text).

Every keyed tick transaction pins a statement-timeout guard (Dec 7):
`statement_timeout` is set to 120 s, so a runaway statement aborts with the
distinct `statement_timeout` failure class instead of holding the
single-flight lock. The guard is sized from the post-fix tick maximum and is
never the fix; the over-cap full fallback lifts it. The guard also bounds
the hot-identifier promise: the first identifier is always admitted and
never split, but completes only within the guard — past ~401k warm
(~124k cold) copies for one identifier the tick aborts with no progress and
re-ticks identically, and the over-cap fallback never rescues it (#11442).
At 3 AM a climbing watermark with `build_active_facts` at the guard plus a
`statement_timeout` rate means diagnosing the probe plan on the oldest
identifier. The timeout is a store field only; there is no env knob.

No-Regression Evidence: the scanned-copies probe was measured read-only on
the QA replica (PostgreSQL 18.6, 86,765 canonical winners) with 30
identifiers sampled in canonical-key order. `EXPLAIN (ANALYZE, BUFFERS)`
shows an Index Scan on `fact_records_supply_chain_impact_winners_keyed_idx`
with no sequential scan: 314 copies in 93.9 ms total (about 3.1 ms per
identifier, 277 shared hits plus 195 reads). The statement text matches the
shipped probe's five equalities; a VALUES list stood in for the tick's temp
idents table, which only feeds those same equalities. At 3 ms per identifier
against a 17.1 s p95 build on 200 identifiers the extra COUNT is noise
(during primary migration load, so conservative), and the kept-row contract
is unchanged (the phase test pins kept rows while the count varies).

Observability Evidence: `chunk_scanned_copies` rides the existing resweep
span attribute and tick log line beside `chunk_kept_rows`; the
`build_active_facts` phase histogram covers the added read with no new
metric or stage. No staging
statement in a tick reads more of the staging queue than one collapse budget
(#11129): the collapse reads its bound from at most 50,001 seq-index entries
and folds only the seq range it saw, and the residual read counts the queue
exactly up to 50,000 rows. Past that, `remaining` is the visible seq span, an
upper bound, and `remaining_upper_bound` is `true`. The oldest capture time the
stamp uses is always exact.

When the chunk applies only part of an activation's scope row, the tick turns
the unapplied rest into fact rows and deletes the scope row in the same
transaction (#11145), each converted row keeping the scope row's capture time
(merging into an already-queued fact row keeps the earlier time). The horizon
stays under that activation's scope mark until every one of its identifiers
is applied. A conversion does not wake a later-enqueued `winners_watermark`
waiter early (separate late-commit gap in the staging collapse, #11149), and
`dirty_log_oldest_age_seconds` keeps reporting the activation's real age
while it drains. The commit log line carries `remainder_rows_converted`, and
`eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total`
counts the rows.

An audit runs beside the keyed ticks every
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL` (default 10 minutes; `0`
turns it off). It recomputes the whole table without taking the resweep lock,
verifies each differing key under the lock, and queues the stale ones for the
next keyed tick: the backstop for a change that left no dirty-log row, so its
repair count should stay at zero. With
`ESHU_POSTGRES_READ_ROUTING=staged-audits` the audit sweeps identifier
ranges with a persisted cursor (#11131): each range is one standby statement
over the next `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_RANGE_BUDGET`
keyed-index rows (default 10,000) plus one writer transaction verifying the
range's differing keys and advancing the cursor. Ranges run back to back for
up to `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_TICK_BUDGET` (default 5
minutes), so the sweep spans ticks and resumes after a roll; after a
lock-taking range, the next one waits for a keyed commit (moved watermark,
at most one keyed interval), so locked ranges cannot starve keyed ticks.
`audit_last_success_age_seconds` restarts only on a completed sweep.
Watch `audit_sweep_ranges_total` (flat while allowed means stuck),
`staged_audit_deferrals_total{reason}`, and
`audit_sweep_last_completed_age_seconds` against the 24 h bar.

Role-bound logins need grants for the application-code capture only: the
pointer, scope-delete and scanner trigger functions run as their owner
(`SECURITY DEFINER` with a pinned `search_path`), so the projector,
collector, retirement fence, suppression store and scanner publisher logins
need no privilege on the dirty log or staging table. A least-privilege
database grants the impact-writer (reducer) login `INSERT` on
`supply_chain_impact_winners_dirty_staging` plus `USAGE` on
`supply_chain_impact_winners_dirty_staging_seq`, and the winners maintainer `SELECT`/`INSERT`/`DELETE` on
the staging table plus the same sequence `USAGE`, keeping its
`SELECT`/`INSERT`/`DELETE` on `supply_chain_impact_winners_dirty_log`
(collapse merge-upsert and settle drain both write it) and needing
`SELECT`/`INSERT`/`UPDATE`/`DELETE` on
`supply_chain_impact_winners_dirty_tally`: its four drain statements keep
per-minute buckets, held for three hours, of the deleted NVD-proxy,
GHSA-proxy (#11078) and NVD scope rows, so the trip sampler (coordinator
login) reads inflow instead of survivors and needs `SELECT` on that table
and the dirty log (#10767, #11078). Each tally row is one capture minute:
`nvd_proxy_rows` the CVE-% fact marks, `ghsa_proxy_rows` the GHSA-% fact
marks, `nvd_scope_rows` the NVD scope marks deleted that minute, each
summed over `dirty_version`. The keyed tick and audit also create
`ON COMMIT DROP` temp tables, so the maintainer needs `TEMPORARY` on the
database (`PUBLIC` holds it by default; revoked means the tick fails with
`permission denied`). Without the writer grant the impact write fails with
`permission denied` and the reducer retries; nothing is skipped silently.

The winners path is forward-only across the staging change (#10837,
migrations 309-311): capture triggers stage every mark and only the new
maintainer tick collapses staging into the dirty log. An older binary against
the new schema writes fact captures straight to the log but its tick reads
only the dirty log, so staged trigger marks sit invisible until re-upgrade
(the audit re-queues some via the log path, not all). Never run an old
maintainer or old triggers against the new schema except transiently during
the roll; roll back by re-upgrading and letting the new tick collapse the
backlog, not by downgrading the binary alone.

## Enrichment freshness

A KEV listing or EPSS rescore does not edit winner rows directly. The
enrichment scope's next impact pass re-derives its own copies from the new
vintage, the changed CVE re-triggers every other scope holding a copy (the
holder fan-out, counted by `eshu_dp_supply_chain_impact_fanout_total` with
`cause="epss"` or `cause="kev"`), and the next resweep tick reflects the
converged copies. Until that drain finishes, the winner can still be an
older copy whose stale vintage outscores the fresh one. The freshness bound
is queue latency plus one holder pass per holder scope plus one resweep
tick; `eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds`
and the watermark age show how much of that bound is currently used. An
unchanged KEV or EPSS collection creates no generation at all (the
collector content-digest skip), so a flat `cause="epss"`/`"kev"` rate means
no enrichment changed, not that re-triggers stopped.

## Retraction freshness

A source record rejected or withdrawn is re-derived per holder scope: each scope
that holds an active copy of the finding rewrites its own copy as
`advisory_retracted` on its next pass. Winner selection ranks by
`priority_score` before anything else and does not look at the suppression
state, so until a stale holder copy re-derives, the default findings view can
return that stale active copy instead of the retracted one. The bound is the
holder fan-out (a changed or retracted write reopens every holder scope, the
operator suppression scope included, at most 256 per claim with the owed
remainder carried to the next claim), then one holder pass, then one resweep
tick. The bound holds only while every holder's item is reopenable and the originating pass finishes its fan-out. A dead-lettered holder, or a dead-lettered pass that still owes a fan-out, pins its copies until it is reopened. Queue latency is part of the bound. One residual is open: a running holder that a fan-out flags and that then dead-letters loses the flagged change, because the dead-letter transition clears the flag, until another change reaches it (#11159). A second residual is open: a genuine pass flagged while running that then fails in a non-counting class retries at attempt 1 with the origin stamp, so it runs as a CVE-scoped one hop and hides the genuine pass (#11173). The winners audit (every 10 minutes by default,
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL`) is not part of that bound: it
keeps the winners table equal to `fact_records`, and a stale holder copy is an
ordinary active fact there, so the audit confirms it as the winner and does not
repair it. Only the fan-out and the holder's own next pass re-derive it. A
holder the fan-out does not reach keeps its stale copy until that pass: a
dead-lettered holder item at the active generation is reopened as a full pass
unless its failure class is unsafe to replay (#11147; read
`eshu_dp_supply_chain_impact_active_dead_letters` and its `_owing_fanout`
subset for what is still pinned), a holder past the 256 cap
waits for a later claim, and a suppression that names no CVE, advisory or group
id gives the fan-out nothing to look up (counted by
`eshu_dp_supply_chain_impact_suppression_no_identity_scope_total`). The winner
ordering deliberately does not prefer a retracted copy: that would let a stale
retracted copy hide a restored, real vulnerability. Read
`eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}` for how often
a pass flips a copy across the retraction boundary, as `active_to_retracted` (a
retraction reached the copy) or `retracted_to_active` (a restored record
returned it). Each point is one retraction-state flip the pass wrote, and the
scope that first derives the retraction counts its own flip, so the counter does
not isolate what the fan-out repaired and says nothing about how many copies are
still stale. That remaining count is not measured today. For a directional estimate of how many stale copies remain over a window, aggregate both counters (they carry different label sets, so a bare subtraction matches nothing) and floor at zero: `clamp_min(sum(increase(eshu_dp_supply_chain_impact_fanout_total{outcome=~"reopened|deferred",cause="advisory"}[24h])) - sum(increase(eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition="active_to_retracted"}[24h])), 0)`. It is a heuristic, not a measurement, and there is no gauge for it, because counting the copies directly would add an unmeasured read to a hot path for a window that is already bounded, and a holder deferred then reopened inside the window shows under both outcomes. "Active" there means
any state other than `advisory_retracted`. Read the counter beside
`eshu_dp_supply_chain_impact_fanout_total` and the watermark age to tell a
working fan-out from a stalled one: with a rejected record in flight, a flat
counter means no pass has flipped a copy yet, not that the holders converged.

Before the fan-out slices its holder list to the 256 cap, one set read drops the
holders whose supply-chain impact item is already `pending` or `retrying` with a
next pass that covers the change: a full pass, or a scoped one whose stamp
already names every changed id (#10972).
`eshu_dp_supply_chain_impact_fanout_total{outcome="prefiltered_pending"}`
counts them. A queued holder stamped for another change is kept, and the reopen
merges the change into its stamp without moving its place in the queue,
counted as `outcome="stamp_widened"`. A succeeded holder whose watermark
postdates the origin reports `outcome="shadow_would_skip"` while still going
pending (#11613 PR1 counts the would-skip for PR2). A failing prefilter read counts
`read_error` or `read_timeout` with `site="holder_prefilter"`, and the pass
retries; every read is timed under
`eshu_dp_supply_chain_impact_fanout_read_duration_seconds{kind="holder_prefilter"}`.
A rising `prefiltered_pending` with `deferred` falling is the prefilter working,
and `stamp_widened` should fall as stamps converge; a rising `deferred` with
`prefiltered_pending` flat means the holders are not queued and the cap is
doing real work.

A manifest or lockfile change converges the same way (#9874): the git
generation's marker pass reopens every scope holding advisory evidence for a
changed package, counted under `cause="manifest_dependency"`. A marker item
whose generation has not activated yet is deferred by the reducer runtime
before the handler runs, under the shared non-counting failure class
`generation_activation_not_ready` (see
`eshu_dp_reducer_retry_surge_total{failure_class}`); it retries until the
generation activates or is superseded. Two marker results matter at 3 AM, both
log fields on the reducer success line rather than metrics:
`sub_signal_manifest_trigger_prior_base_missing=1` means a delta in the diffed
chain had an unreadable base, so a removal its base alone held was invisible
to the package-keyed diff, and `sub_signal_manifest_trigger_baseline_degraded=1` means the chain was
over its 32-generation cap (the current generation counted) or its baseline was pruned, so every current pin fanned
out and removals in that window were missed by the diff. Either case, or a
skipped pruned intermediate (`sub_signal_manifest_trigger_chain_pruned=1`),
makes the pass reopen every scope holding an active finding for the
generation's repository (#10059), counted on
`sub_signal_manifest_trigger_repository_holders`, so the stale finding is
re-derived without waiting for the package's advisory scope to rerun.

An advisory group merge or split converges the same way (#10408): the
committing pass re-keys its findings under the new grouping, the touched
groups reopen every scope holding a copy through the widened holder lookup,
and the next resweep tick reflects the converged copies. Until that drain
finishes, a holder can still serve the pre-merge copy. The freshness bound
is the same — queue latency plus one holder pass per holder scope plus one
resweep tick — and the wave is separable on
`eshu_dp_supply_chain_impact_rekey_total{outcome}` (`merge`, `split`,
`member_only`, `retire_only`; only a genuine identity change counts — a pass
that re-derives identical groups counts nothing unless it dropped
suppression coverage, which reports `split`, so the steady state is
silence). A `split` rate with a rising
`eshu_dp_supply_chain_impact_suppression_orphaned_by_split_total` means a
split dropped suppression coverage the operator must re-issue; a `merge`
rate with only
`eshu_dp_supply_chain_impact_suppression_carried_via_member_total`
converges on its own. Per-pass magnitudes ride the reducer success line as
`sub_signal_rekey_groups_touched`, `sub_signal_rekey_findings_rekeyed`,
and `sub_signal_rekey_rows_tombstoned`.

## Signals

| Signal | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_winners_resweep_duration_seconds` | histogram | `path` | Wall time of one committed resweep statement. `path` is `full` (a full run for a missing, NULL or lower stored contract, or a keyed tick the cap sent to the full rebuild), `keyed` or `audit` (the full-recompute read). The full recompute is O(findings); a keyed tick follows the changed keys. A failed or skipped resweep records no sample. |
| `eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds` | histogram | `phase` | Wall time of each phase of one keyed tick (#9975), so a slow `keyed` duration reads as a slow read or a slow write. `phase` is `claim` (transaction begin and the resweep try-lock), `collapse` (folding the staging queue into the dirty log with one key-ordered GROUP BY merge-upsert per kind plus the seq-bounded delete, #10837), `consume` (reading the DB-clock tick stamp, then admitting the oldest dirty-log window into the consumed temp table), `build_idents` (the identifier set, its `ANALYZE` and the cap check), `build_active_facts` (the identifier chunk, the keyed active-facts temp table built from the supports probe since #10880 PR-C1, plus the operator facts and its `ANALYZE`), `apply` (deleting exactly the admitted rows the chunk applied with their per-source tally, plus the winner select and row writes), `full_rebuild` (the whole-table rebuild a tick ran instead of `build_active_facts` and `apply` because it went over the cap), `stamp` (the watermark stamp of a tick that consumed nothing) and `commit`. A tick records one sample per phase it ran, so a quiet tick records `claim`, `collapse`, `consume`, `stamp` and `commit`, and a tick that lost the lock records only `claim`. A phase whose statement failed still records the time it spent, which `resweep_duration_seconds` does not. Buckets are the resweep buckets (0.05 to 300 s), so a phase under 50 ms reads as the first bucket. |
| `eshu_dp_supply_chain_impact_winners_keys_per_sweep` | histogram | none | Canonical keys one keyed tick recomputed. A value near the cap is the warning before a tick falls back to the full rebuild. |
| `eshu_dp_supply_chain_impact_winners_identifiers_consumed_total` | counter | none | Dirty-log rows keyed ticks consumed. |
| `eshu_dp_supply_chain_impact_winners_chunked_ticks_total` | counter | none | Keyed ticks that stopped at the identifier chunk bound with dirty rows still queued, so committed their chunk while stamping the horizon over the rows left queued (#10706). A rising counter with a falling dirty-log depth is a backlog draining in bounded ticks; a rising counter with a flat or climbing depth means arrivals outpace the bound. |
| `eshu_dp_supply_chain_impact_winners_capture_rows_total` | counter | `source` | Staged rows appended, by capture source: `fact_upsert`, `fact_retract`, `activation`, `operator`, `scanner` or `audit` (#10837: every capture owner stages one row per mark into the shared queue; the tick collapse folds them into the dirty log). The impact writer counts `fact_upsert` and `fact_retract` from the capturing statements' row counts, but only after its transaction commits: a rolled-back attempt, or a commit that fails, adds nothing, so those two sources match the log rows that exist, apart from a commit whose outcome is unknown (a connection lost after the server applied it), which leaves its rows uncounted. Database triggers write `activation`, `operator` and `scanner` rows and cannot emit a metric, so the maintainer counts those when a keyed tick or a full resweep (run when the stored contract is missing, NULL or lower) consumes them. `audit` is counted when the audit queues a repair. A source that goes flat while its writes continue is a capture path that stopped. The scope-delete trigger writes its rows as `activation` (or `operator` for the suppression scope), the same sources as a pointer move, so a stopped delete capture does not show as a flat source: the `fact` rows it writes are the only difference. |
| `eshu_dp_supply_chain_impact_winners_backstop_repairs_total` | counter | none | Stale keys the audit found and queued. Steady state is zero. Any rate means some change left no dirty-log row. |
| `eshu_dp_supply_chain_impact_stale_copy_repairs_total` | counter | `transition` | Retraction-state flips a pass wrote against its stored baseline (#10765): `active_to_retracted` or `retracted_to_active`. The scope that first derives the retraction counts its own flip too, so a point is not necessarily a copy the holder fan-out repaired, and the counter does not say how many stale copies are still waiting: that number is not measured. Counted once per rewrite, after the writer commit. Quiet when no record is being rejected or restored. |
| `eshu_dp_supply_chain_impact_winners_audit_skipped_total` | counter | none | Audits that found candidate stale keys but could not take the resweep lock, so verified nothing and queued nothing. A skipped audit is the backstop missing a run, not a failure. Alert on a streak: no `path="audit"` sample in `eshu_dp_supply_chain_impact_winners_resweep_duration_seconds` while this counter rises, for example `sum(increase(eshu_dp_supply_chain_impact_winners_audit_skipped_total[1h])) >= 3` at the default 10 minute interval, means the audit keeps losing the lock and has not verified its candidates for a while. Sum across reducer pods, because the audit lease can move between them. One skip is expected and harmless. `increase()` does not count the first increment of a series that did not exist yet. An audit that read no candidates returns early and neither skips nor counts here. |
| `eshu_dp_supply_chain_impact_winners_dirty_log_depth` | gauge | none | Rows waiting in the dirty log at the last tick this instance sampled, plus the staged rows (the gauge unions both queues). |
| `eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds` | gauge | none | Age of the oldest waiting row across both queues, 0 when both are empty. It bounds how far a winner can lag the facts. |
| `eshu_dp_supply_chain_impact_winners_staging_depth` | gauge | none | Rows waiting in the staging queue at the last tick this instance sampled. A depth that keeps climbing means captures outpace the per-tick collapse. |
| `eshu_dp_supply_chain_impact_winners_staging_oldest_age_seconds` | gauge | none | Age of the oldest staged row at the last tick this instance sampled, 0 when the queue is empty. It bounds how long a capture can sit before the collapse folds it in. |
| `eshu_dp_supply_chain_impact_winners_staging_depth_upper_bound` | gauge | none | 1 when the last `staging_depth` sample, and the `dirty_log_depth` that includes it, is an upper bound: past one collapse budget (50,000 staged rows) the sample reports the visible seq span instead of counting the whole queue (#11129). 0 when the depth is exact. The age gauges are exact either way. |
| `eshu_dp_supply_chain_impact_winners_staging_collapsed_rows_total` | counter | none | Staged rows one keyed tick collapsed into the dirty log: the seq-bounded delete count, so one collapsed group counts every mark it combined. |
| `eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total` | counter | none | Dirty-log fact rows a keyed tick wrote (inserted, or merged into a queued row) when it converted the unapplied remainder of a partly applied scope row (#11145). The rows keep the scope row's capture time, so while any is queued the watermark stays under that activation's scope mark. Recorded before the tick commits, so it includes a tick that later rolls back. |
| `eshu_dp_supply_chain_impact_winners_settle_skipped_rows_total` | counter | `reason` | Admitted dirty-log rows the applied drain left queued, per keyed tick (#9975 drain discriminator). `reason` is `admitted_not_in_chunk` (the chunk did not apply them: the bounded-tick remainder) or `in_chunk_version_mismatch` (the chunk applied them but their `dirty_version` moved after admission, so the version recheck kept them). A dominant `admitted_not_in_chunk` share with a flat watermark means the chunk bound holds the drain; a rising `in_chunk_version_mismatch` with a rising converted rate names convert self-bump as the suppressor. Zero when a tick drains its window. |
| `eshu_dp_supply_chain_impact_winners_write_conflicts_total` | counter | `site`, `failure_class` | Writer and tick statements the database aborted with a deadlock (`40P01`) or a serialization failure (`40001`) (#10851). `site` is `writer_capture` (the capture statement), `writer_commit` (the writer transaction commit) or `tick` (any keyed-tick phase, including the collapse, the settle and the tick commit); `failure_class` is `deadlock` or `serialization_failure`. The counter classifies the victim only: nothing retries, the transaction rolls back and the queue re-drives it. Steady state is zero; a rising rate names the contending site. |
| `eshu_dp_supply_chain_impact_winners_rows_total` | counter | `action` | Rows a committed resweep handled. `action` is `inserted`, `updated`, `deleted` or `unchanged`. Only the first three write a row version. |
| `eshu_dp_supply_chain_impact_winners_resweep_skipped_total` | counter | none | Resweeps that found another in flight, so wrote and stamped nothing. A resweep can only overlap another after outlasting the lease TTL, so any rate means statements run past 60 seconds. |
| `eshu_dp_supply_chain_impact_winners_watermark_age_seconds` | gauge | none | Seconds since the read watermark was stamped, computed at scrape time from the last watermark the instance saw. Every instance reports it, whether or not it holds the lease. It rises between resweeps, drops on each commit, and keeps climbing through a stall. It has no value until the instance first sees a watermark. |
| `supply_chain_impact.winners_resweep` | span | none | One resweep or audit. Attributes `path`, `keys`, `inserted`, `updated`, `deleted`, `unchanged`, or `skipped`; the audit span carries `repairs`, or `skipped` when it could not take the lock; a staged sweep range (#11131) adds `outcome` and `partial` (true until the range that completes the sweep). A failed resweep records the error on the span. |
| `eshu_dp_supply_chain_impact_winners_resweep_in_flight_age_seconds` | gauge | `path` | Seconds the resweep in flight on this instance has run, by `full`, `keyed` and `audit`; 0 when none is in flight (#11117). |
| `eshu_dp_supply_chain_impact_winners_last_commit_timestamp_seconds` | gauge | `path` | Unix time of the last resweep this instance committed, by path. It is the commit time, not the watermark value the resweep stamped. No value until the path first commits. |
| `eshu_dp_supply_chain_impact_winners_audit_last_success_age_seconds` | gauge | none | Seconds since this instance last completed an audit pass, repairs found or not. It has no value until the first completed pass in this process and resets on a roll, so a backstop that never runs shows as an absent series, not a climbing one: alert on its absence together with `staged_audit_deferrals_total{reason="budget"}`. Under the staged sweep (#11131) a pass is a completed sweep: ranges alone do not reset it. |
| `eshu_dp_supply_chain_impact_winners_contract_version` | gauge | `kind` | The stored contract version (`stored`, 0 when none is recorded) and this binary's (`binary`), from the last contract read. |
| `eshu_dp_supply_chain_impact_winners_full_resweeps_total` | counter | `reason` | Fulls this instance committed, by start reason (a tick that did not claim the lease, or was skipped or found current, adds nothing, #11177): `no_watermark` (no resweep had committed), `contract_version` (stored version NULL or lower), `keyed_cap` (a keyed tick fell back past the cap), `supports_ready` (the supports table became ready after the last committed resweep, #10880 PR-C1). |
| `eshu_dp_supply_chain_impact_winners_stand_downs_total` | counter | none | Ticks this instance stood down because the stored contract is newer than its binary, at the tick's decision read or, for a keyed tick or a `supports_ready` full, at the re-read under the resweep lock. Any rate means mixed binaries. |
| `eshu_dp_supply_chain_impact_winners_supports_not_ready_total` | counter | `path` | Maintainer ticks and audit ticks refused because the supports table the maintainer's inputs read is not ready (the backfill marker incomplete, a marker for a higher contract version, or the old-writer fence set), by path `full`, `keyed` or `audit` (#10880 PR-C1). A refused tick claims no lease, writes nothing and leaves the watermark alone. A count that keeps rising after a roll means the supports backfill has not completed; the first ready tick after such a period runs one full (`full_resweeps_total{reason="supports_ready"}`). |
| `supply chain impact winners maintainer waiting` | warn log | none | Logged once when the maintainer starts waiting for the supports table (the refused ticks above); a matching info log marks the resume. |
| `supply chain impact winners maintainer standing down` | warn log | none | Logged once when an instance starts standing down, with `stored_version` and `binary_version`; a matching info log marks the resume. |
| `eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total` | counter | `reason` | Staged-audit sweep ranges (routing `staged-audits`) that committed nothing, by reason (#11117, #11131): `timeout` (a one-identifier range still hit the 25 s statement guard), `null_key` (a source finding fact has a NULL package, repository or digest the ranges cannot reach; at sweep start nothing runs, at sweep end the sweep restarts uncounted), `pipe` (a range fact has `\|` in an identifier field; the sweep stops at that range), `lock_busy` (the waiting resweep-lock claim hit its 20 s `lock_timeout`), `cursor_race` (another instance advanced the range first; this one rolled its repairs back), `keyed_yield` (after a verify that took the resweep lock, the next range that needs the lock waited for a keyed commit, a moved watermark, or one keyed interval, so the sweep cannot starve keyed ticks; it then commits in the same pass), `conflict_exhausted` (a standby recovery conflict), `batch_abort` (any other read failure). `budget` no longer occurs under the sweep. A steady `timeout`, `null_key` or `pipe` rate means the sweep is not advancing. |
| `eshu_dp_supply_chain_impact_winners_staged_audit_ticks_total` | counter | none | Completed whole-corpus staged-audit read phases. The range sweep (#11131) replaced that read, so this stays at 0; read `audit_sweep_ranges_total`. |
| `eshu_dp_supply_chain_impact_winners_staged_audit_last_batches` | gauge | none | Standby batches (one per active scope generation) the last completed staged-audit tick read. Absent until a pass completes; a budget-deferred tick records nothing here. Retired by the range sweep (#11131): always absent. |
| `eshu_dp_supply_chain_impact_winners_staged_audit_last_max_batch_seconds` | gauge | none | The last completed tick's slowest standby batch; each batch has a 25 s statement timeout. Absent until a pass completes; a budget-deferred tick records nothing here. Retired by the range sweep (#11131): always absent. |
| `eshu_dp_supply_chain_impact_winners_staged_audit_last_staged_facts` | gauge | none | Facts the last completed staged-audit tick staged (cap 1,000,000). Absent until a pass completes; a budget-deferred tick records nothing here, so read `staged_audit_deferrals_total{reason="budget"}` instead. Retired by the range sweep (#11131): always absent. |
| `eshu_dp_supply_chain_impact_winners_staged_audit_last_distinct_keys` | gauge | none | Distinct keys the last completed tick compacted (cap 250,000). Absent until a pass completes; a budget-deferred tick records nothing here. Retired by the range sweep (#11131): always absent. |
| `eshu_dp_supply_chain_impact_winners_audit_range_duration_seconds` | histogram | `outcome` | Wall time of one staged audit sweep range, standby read through writer commit (#11131). `outcome` is `advanced`, `completed` (the range that completed the sweep), `skipped` (the try-lock lost), `lock_busy`, `cursor_race` or `deferred`. The standby statement has a 25 s statement guard. The writer verify has no enforced timeout: under 30 s per verify is the QA acceptance bar (#11120). On the `staged winners audit range` log line, `range_seconds` minus `read_seconds` is an upper bound on the verify time: it also counts the end NULL probe and any keyed-yield wait. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_total` | counter | none | Sweep ranges committed (cursor advanced). Flat while the audit is allowed means the sweep is stuck: read the deferrals by reason. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_locked_ranges_total` | counter | none | Committed ranges whose verify took the resweep lock (a candidate nothing had queued). Each one makes the next lock-needing range yield to a keyed commit; read it beside `staged_audit_deferrals_total{reason="keyed_yield"}`. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_range_retries_total` | counter | none | Range reads retried at half budget after the 25 s statement guard expired. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_facts_total` | counter | none | Impact facts the committed ranges recomputed. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_candidates_total` | counter | none | Identifiers whose recomputed winners differed from the stored rows in committed ranges. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_pending_total` | counter | none | Candidates already queued in the dirty log or staging, dropped before the resweep lock (a keyed tick recomputes them). |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_repairs_total` | counter | none | Stale identifiers committed ranges queued for the next keyed tick (capture holes). Steady state is zero. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_discards_total` | counter | none | Candidates that were not repaired: already queued, or clean under the lock. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done` | gauge | none | The sweep position: ranges the sweep in progress has committed, read from the persisted cursor row, so it survives a roll. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_last_completed_age_seconds` | gauge | none | Age of the last completed sweep from the persisted row; the bar is 24 h. No value before the first completion. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_last_seconds` | gauge | none | Duration of the last completed sweep, first range commit to last. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_last_ranges`, `..._last_candidates`, `..._last_repairs`, `..._last_discards` | gauge | none | Totals of the last completed sweep, from the persisted row. |
| `eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_seconds`, `..._last_range_budget` | gauge | none | The last committed range in this process: wall time and the row budget after any timeout halving (below the configured budget means timeouts halved it). |
| `staged winners audit range` | info log | none | One line per range with `outcome`, `sweep_id`, `ranges_done`, `last`, `oversize`, `budget`, `facts`, `candidates`, `pending`, `repairs`, `discards`, `locked`, `read_seconds`, `range_seconds` and the new `cursor`. |
| `staged winners audit sweep completed` | info log | none | One line per completed sweep with `sweep_id` and `sweep_seconds`. |
| `staged winners audit deferred` | info log | none | One line per deferred range with `reason`, `read_seconds` and the `error`. |
| `supply chain impact winners audit completed` | info log | none | One line per completed audit pass, zero repairs included, with `repairs`, `read_seconds` and `duration_seconds`. |
| `supply chain impact winners full resweep recorded the contract` | info log | none | This instance claimed the lease and committed a full that recorded this binary's contract, with `reason` and the stored and binary versions it replaced. A tick that did not claim the lease, was skipped behind another instance or found the contract already current logs nothing (#11177). To see that the contract is in place, read the winners watermark row (`contract_version` set) or the `supply chain impact winners resweep committed` line with `path: full`. |
| `supply chain impact winners resweep committed` | info log | none | The same four counts plus `path`, `identifiers`, `keys`, `duration_seconds` and `previous_watermark_age_seconds` (`-1` for the first resweep). |
| `supply chain impact winners audit found stale keys` | warn log | none | The audit queued `repairs` stale keys, with `read_seconds` and `duration_seconds`. Carries no key, scope or CVE. |
| `supply chain impact winners dirty log stats read failed` | warn log | none | The read behind the two dirty-log gauges failed; they keep the last sample. |
| `supply chain impact winners resweep skipped: another resweep is in flight` | info log | none | The skip the counter counts. |
| `supply chain impact winners resweep failed` | error log | none | The wrapped cause: lease claim, statement, commit, or lease release. |
| `supply chain impact winners watermark read failed` | warn log | none | The read behind the age gauge failed. The gauge keeps aging the last watermark it saw. |

No label carries a canonical key, scope, repository, or CVE.

## Reading them at 3 AM

- Start with the watermark age, as `min()` across instances, because an instance
  sees another's commit only on its next tick. The API reports the read stale
  once it passes 120 seconds. Stamped before the statement runs, the age peaks at
  about twice the resweep time plus 30 seconds just before a commit. A value that keeps
  climbing past that means no instance is completing resweeps: check
  `rate(eshu_dp_supply_chain_impact_winners_resweep_duration_seconds_count[5m])`
  at zero, the failure log line, and the `supply_chain_impact_winners` lease.
- Then read `rows_total{action="unchanged"}` against the other three. On a
  quiet system `updated`, `inserted` and `deleted` stay near zero and
  `unchanged` equals the winner count. A sustained high `updated` rate means
  findings really are changing; check the findings counters before blaming the
  maintainer.
- Then read the resweep duration by `path`. A keyed tick follows the number of
  changed keys. Scale anchors do not compare across corpora (#9805 synthetic:
  0.36 s keyed, 5.6 s full; #9774 QA baseline: ~40 s per statement, nearly
  all read). What carries over is the gap: `full`/`audit` is expected long
  (reads every finding); `keyed` near full-resweep time is an incident. Look
  for a plan change and `keys_per_sweep` near the cap, not the cadence.
- When a keyed tick is slow, split it by phase with
  `eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds{phase}`.
  `build_idents`/`build_active_facts` is the read side (the supports probe
  since #10880 PR-C1): check buffer reads and CPU throttling.
  `apply`/`commit` is the write side; `collapse` is the staging drain (see
  its staged-row rate and budget); `consume` points at the dirty-log delete.
  `full_rebuild` means over the cap; rising `claim` means a slow begin or
  try-lock round trip, not slow work. Match a slow tick to its phase by the
  jump time in the bucket counts. The `build_active_facts` cost model is one
  index descent plus the kept facts' heap rows per dirty identifier (~13 kept
  facts per identifier on QA), so slowness at ordinary counts means a lost
  index (check the plan) or a grown kept set. Peak-watermark verdicts read
  the maximum over the window of the per-instant `min()` across instances,
  never one instance's maximum.
- Then read the dirty log. `dirty_log_depth` should return to near zero after
  each keyed tick, and `dirty_log_oldest_age_seconds` bounds how far a winner
  can lag its facts. Every tick admits the oldest captures first, so while
  ticks run, `dirty_log_oldest_age_seconds` and `watermark_age_seconds` should
  fall or hold, not climb one second per second. If either keeps climbing, no
  instance is running keyed ticks: check the failure log line, the `supply_chain_impact_winners` lease, and
  the grants listed above (a missing maintainer grant fails the tick with
  `permission denied` and logs it). When the log is empty but work is pending,
  read `staging_depth`: captures stage there first, and a climbing staging
  depth with a flat log means the per-tick collapse budget is smaller than the
  capture rate. While `staging_depth_upper_bound` is 1 the depth is the seq
  span of the queue, not a count: read its trend, not its exact value.
- If the `collapse` and `stamp` phase times rise while `staging_depth` stays
  flat, suspect a held snapshot. The collapse and residual reads start at the
  oldest index entry, and a transaction older than the deleted rows keeps their
  index entries from being cleaned, so each read walks them. Find the holder
  with `SELECT pid, backend_xmin, xact_start, state, left(query, 80) FROM
  pg_stat_activity WHERE backend_xmin IS NOT NULL ORDER BY age(backend_xmin)
  DESC;`. At 2.5M deleted rows behind a held snapshot each of the three walks
  measured about 0.5 s; they return to tens of milliseconds once the snapshot
  ends and vacuum runs.
- Read `backstop_repairs_total`. Steady state is zero. A nonzero rate means the
  audit found a winner that no dirty-log row had queued, so a capture path is
  missing. Check `capture_rows_total` by `source`: a source that goes flat while
  its writes continue is the path that stopped. A repair is safe, because the
  audit queues the key for the next keyed tick, but it is a defect to chase.
- `supports_not_ready_total` rising means the maintainer is waiting for the
  supports backfill and writing nothing; read it with the supports backfill
  gauges. After the table becomes ready expect one `full_resweeps_total` sample with
  `reason="supports_ready"`, then keyed ticks.
- `full_resweeps_total` says why each full ran. `no_watermark` and
  `contract_version` should appear once per contract change, not once per roll;
  after a roll with an unchanged contract, expect no increase and a keyed
  `last_commit_timestamp_seconds` within the lease TTL plus two intervals. A
  `contract_version` full on every roll means the stored version is being reset
  (a rollback runbook step, or a binary below the stored version writing it).
  A non-zero `stand_downs_total` means an older binary runs beside a newer one;
  `contract_version{kind}` shows both versions.
- `resweep_in_flight_age_seconds{path="full"}` climbing toward the usual full
  duration is a full in progress. A keyed in-flight age past a few minutes is a
  stuck tick, before it commits or fails.
- A `path="full"` sample with `full_resweeps_total{reason="keyed_cap"}` means a
  keyed tick passed the cap and fell back. Read `keys_per_sweep` to see how many keys
  it held, and check what wrote that many changes at once (for example a mass
  re-ingest or a retirement purge).
- A rising `eshu_dp_supply_chain_impact_winners_chunked_ticks_total` with a
  falling `dirty_log_depth` is a backlog draining in bounded ticks: healthy. A
  rising chunked-ticks counter with a flat or climbing depth means arrivals
  outpace the 200-identifier bound; look for what writes so many changes (and for
  rewrites that change nothing) before touching the bound, and do not chase a
  faster tick. A bounded tick stamps the horizon over the
  rows it leaves queued (#10706), so the watermark age stays bounded by the
  tick cadence while the drain runs; if the watermark age keeps climbing while
  the depth falls, ticks are not committing (check the failure log line and the
  `supply_chain_impact_winners` lease) — that climb is a stall, not the drain.
- A rising `scope_remainder_converted_rows_total` with a flat watermark and a
  `dirty_log_oldest_age_seconds` that keeps growing means a large activation is
  draining over bounded ticks. The watermark is held at that activation's scope
  mark on purpose (#11145): advisory waits enqueued after the activation stay
  parked until its winners are applied. If `dirty_log_depth` falls tick by tick,
  this is the drain, not a stall.
- A dominant `settle_skipped_rows_total{reason="admitted_not_in_chunk"}` share means the
  chunk bound holds the drain (#9975): each tick applies only its chunk. A rising
  `{reason="in_chunk_version_mismatch"}` with a rising converted rate means the tick's
  own convert bumps rows it then fails to delete; the per-tick log fields carry the same split.
- During a mixed-version rollout, an older reducer pod does not capture the
  impact writer's own changes (`fact_upsert`, `fact_retract`), while the
  database triggers still capture pointer, delete and scanner changes. A winner
  changed only by an older pod stays stale until the next audit (default 10
  minutes), then shows as a one-time `backstop_repairs_total` increase. An older
  binary does not read the dirty log, but it recomputes every winner on each
  tick, so after a rollback the winners stay current and the log rows wait until
  a newer binary runs again. Do not treat a one-time increase after a rollout as
  a capture defect unless it repeats once every pod runs the new binary.
- Read `audit_skipped_total` beside `backstop_repairs_total`. A flat repair
  count is only good news while audits complete. A streak of skips, with no
  `path="audit"` duration sample in the same window, means the audit loses the
  resweep lock every time it tries: a long keyed or full tick holds it. Look at
  the `keyed` and `full` durations before touching the audit interval.
- A nonzero skip rate means statements run past the lease TTL. Skips are safe,
  but they are the signal that headroom is gone.
- To confirm write volume, compare `pg_stat_user_tables` for
  `supply_chain_impact_canonical_winners` against the counter: `n_tup_upd`
  follows `updated`, `n_tup_ins` follows `inserted`, and `n_tup_del` follows
  `deleted`, none of them the winner count.
