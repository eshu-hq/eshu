# Supply-Chain Impact Winners Resweep

The reducer keeps a read model of one winning finding per canonical key, so the
impact-findings list does not sort every finding at read time. A maintainer
rebuilds that read model in a loop under a single-owner lease, waiting 30 seconds
between resweeps. The 30 seconds is the pause, not the period: the tick is the
resweep time plus the pause. These signals say what each rebuild wrote and how
far behind it runs.

## How a resweep writes

The startup resweep, a keyed tick that passes the cap, and the audit's read
recompute every winner from the active facts; an ordinary keyed tick recomputes
only the keys in the dirty log (see "Keyed resweeps and the audit"). Whichever
keys a resweep recomputes, it writes only the rows that are new or changed,
deletes rows that are no longer winners, and
stamps the read watermark. A winner row that did not change is not locked,
versioned, or written to the write-ahead log. Freshness comes from the
singleton watermark, which every committed resweep stamps, so a resweep that
wrote nothing still tells readers the model is current.

Only one resweep runs at a time across all reducer instances. The lease is not
renewed during a resweep, so its 60 second TTL can lapse under a long statement
and let a second instance start. That instance finds the first one's lock, skips,
and stamps nothing: comparing against a table the first resweep has not committed
could otherwise put a fresh watermark over a missing winner. The next tick runs
normally. If the backend running a resweep dies, the lock goes with it.

On a winner row, `materialized_at` means the last content change. Do not use it
for freshness; read the watermark.

## Keyed resweeps and the audit

The first resweep after a reducer starts recomputes every winner. Every tick
after it is keyed: the changes that can move a winner append the canonical keys
they touched to a dirty log, in the same transaction as the change. Two kinds of
capture exist. The impact writer names its own changes in application code, for
findings it inserts, rewrites or retracts. Database triggers (migration 272, plus 272a
for the scanner tables) cover the rest: a move of `ingestion_scopes.active_generation_id` (the projector
Ack and Fail, the collector scope upsert, the retirement fence, the operator
suppression store), a delete of an `ingestion_scopes` row (the retirement
purge), and inserts, updates and deletes on the scanner classification and
association tables. The delete trigger runs before the foreign keys cascade the
scope's facts away, and appends every finding identifier the scope held, so a
retirement that fences and purges a scope before the next tick still repairs
its winners. A keyed tick consumes the log and recomputes only those keys. Its cost follows the number of changed keys,
not the size of the table. A tick that consumed nothing only stamps the
watermark. A tick whose consumed keys pass a cap (50,000 identifiers, or half
of the stored winners) runs the full rebuild in the same transaction instead,
and records its sample under `path="full"`.

An audit runs beside the keyed ticks every
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL` (default 10 minutes; `0`
turns it off). It recomputes the whole table without taking the resweep lock,
verifies each key that differs under the lock, and queues the stale ones for the
next keyed tick. The audit is the backstop for a change that left no dirty-log
row, so its repair count should stay at zero.

Role-bound logins need grants for the application-code capture only. The pointer,
scope-delete and scanner trigger functions run as their owner (`SECURITY
DEFINER` with a pinned `search_path`), so the projector, collector, retirement
fence, suppression store and scanner publisher logins need no privilege on the
dirty log. A database that uses least-privilege writer logins must grant `INSERT`
on `supply_chain_impact_winners_dirty_log` and `USAGE` on its
`supply_chain_impact_winners_dirty_log_seq_seq` sequence to the login that runs
the impact writer (the reducer), and `SELECT`, `INSERT` and `DELETE` plus the
same sequence `USAGE` to the winners maintainer. The keyed tick and the audit
also create `ON COMMIT DROP` temporary tables, so the maintainer login needs
`TEMPORARY` on the database; `PUBLIC` holds it by default, and a database that
revoked it fails the tick with `permission denied`. Without the writer grant the
impact write fails with `permission denied` and the reducer retries it; nothing
is skipped silently.

## Signals

| Signal | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_winners_resweep_duration_seconds` | histogram | `path` | Wall time of one committed resweep statement. `path` is `full` (startup, or a keyed tick the cap sent to the full rebuild), `keyed` or `audit` (the full-recompute read). The full recompute is O(findings); a keyed tick follows the changed keys. A failed or skipped resweep records no sample. |
| `eshu_dp_supply_chain_impact_winners_keys_per_sweep` | histogram | none | Canonical keys one keyed tick recomputed. A value near the cap is the warning before a tick falls back to the full rebuild. |
| `eshu_dp_supply_chain_impact_winners_identifiers_consumed_total` | counter | none | Dirty-log rows keyed ticks consumed. |
| `eshu_dp_supply_chain_impact_winners_capture_rows_total` | counter | `source` | Dirty-log rows appended, by capture source: `fact_upsert`, `fact_retract`, `activation`, `operator`, `scanner` or `audit`. The impact writer counts `fact_upsert` and `fact_retract` when it appends them, from the capturing statement's own row count, before its transaction commits, so a rollback after the capture over-counts those two sources by that attempt's rows. Database triggers write `activation`, `operator` and `scanner` rows and cannot emit a metric, so the maintainer counts those when its keyed tick consumes them. `audit` is counted when the audit queues a repair. A source that goes flat while its writes continue is a capture path that stopped. The scope-delete trigger writes its rows as `activation` (or `operator` for the suppression scope), the same sources as a pointer move, so a stopped delete capture does not show as a flat source: the `fact` rows it writes are the only difference. |
| `eshu_dp_supply_chain_impact_winners_backstop_repairs_total` | counter | none | Stale keys the audit found and queued. Steady state is zero. Any rate means some change left no dirty-log row. |
| `eshu_dp_supply_chain_impact_winners_dirty_log_depth` | gauge | none | Rows waiting in the dirty log at the last tick this instance sampled. |
| `eshu_dp_supply_chain_impact_winners_dirty_log_oldest_age_seconds` | gauge | none | Age of the oldest waiting row, 0 when the log is empty. It bounds how far a winner can lag the facts. |
| `eshu_dp_supply_chain_impact_winners_rows_total` | counter | `action` | Rows a committed resweep handled. `action` is `inserted`, `updated`, `deleted` or `unchanged`. Only the first three write a row version. |
| `eshu_dp_supply_chain_impact_winners_resweep_skipped_total` | counter | none | Resweeps that found another in flight, so wrote and stamped nothing. A resweep can only overlap another after outlasting the lease TTL, so any rate means statements run past 60 seconds. |
| `eshu_dp_supply_chain_impact_winners_watermark_age_seconds` | gauge | none | Seconds since the read watermark was stamped, computed at scrape time from the last watermark the instance saw. Every instance reports it, whether or not it holds the lease. It rises between resweeps, drops on each commit, and keeps climbing through a stall. It has no value until the instance first sees a watermark. |
| `supply_chain_impact.winners_resweep` | span | none | One resweep or audit. Attributes `path`, `keys`, `inserted`, `updated`, `deleted`, `unchanged`, or `skipped`; the audit span carries `repairs`. A failed resweep records the error on the span. |
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
  changed keys. The synthetic scale harness measured about 0.36 seconds for one
  keyed tick at its 1x corpus, and about 5.6 seconds for the full resweep on the
  same corpus (`docs/internal/evidence/9805-winners-keyed-resweep-scale.md`). On
  the QA baseline, a different corpus on a different host, the full resweep took
  about 40 seconds per statement before this change (#9774), nearly all of it
  the read. Do not
  compare those two scales with each other. What carries over is the gap: a
  `full` or `audit` duration is expected to be long, because it reads every
  finding, and a `keyed` duration close to the full-resweep time is an incident.
  Look for a plan change on the winners query and for `keys_per_sweep` near the
  cap. Raising the cadence is not the fix.
- Then read the dirty log. `dirty_log_depth` should return to near zero after
  each keyed tick, and `dirty_log_oldest_age_seconds` bounds how far a winner
  can lag its facts. If either keeps climbing, no instance is running keyed
  ticks: check the failure log line, the `supply_chain_impact_winners` lease, and
  the grants listed above (a missing maintainer grant fails the tick with
  `permission denied` and logs it).
- Read `backstop_repairs_total`. Steady state is zero. A nonzero rate means the
  audit found a winner that no dirty-log row had queued, so a capture path is
  missing. Check `capture_rows_total` by `source`: a source that goes flat while
  its writes continue is the path that stopped. A repair is safe, because the
  audit queues the key for the next keyed tick, but it is a defect to chase.
- A `path="full"` sample after the first resweep after startup means a keyed
  tick passed the cap and fell back. Read `keys_per_sweep` to see how many keys
  it held, and check what wrote that many changes at once (for example a mass
  re-ingest or a retirement purge).
- During a mixed-version rollout, an older reducer pod does not capture the
  impact writer's own changes (`fact_upsert`, `fact_retract`), while the
  database triggers still capture pointer, delete and scanner changes. A winner
  changed only by an older pod stays stale until the next audit (default 10
  minutes), then shows as a one-time `backstop_repairs_total` increase. An older
  binary does not read the dirty log, but it recomputes every winner on each
  tick, so after a rollback the winners stay current and the log rows wait until
  a newer binary runs again. Do not treat a one-time increase after a rollout as
  a capture defect unless it repeats once every pod runs the new binary.
- A nonzero skip rate means statements run past the lease TTL. Skips are safe,
  but they are the signal that headroom is gone.
- To confirm write volume, compare `pg_stat_user_tables` for
  `supply_chain_impact_canonical_winners` against the counter: `n_tup_upd`
  follows `updated`, `n_tup_ins` follows `inserted`, and `n_tup_del` follows
  `deleted`, none of them the winner count.
