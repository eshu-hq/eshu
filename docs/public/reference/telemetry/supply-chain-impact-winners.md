# Supply-Chain Impact Winners Resweep

The reducer keeps a read model of one winning finding per canonical key, so the
impact-findings list does not sort every finding at read time. A maintainer
rebuilds that read model in a loop under a single-owner lease, waiting 30 seconds
between resweeps. The 30 seconds is the pause, not the period: the tick is the
resweep time plus the pause. These signals say what each rebuild wrote and how
far behind it runs.

## How a resweep writes

Each resweep recomputes every winner from the active facts, then writes only
the rows that are new or changed, deletes rows that are no longer winners, and
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

## Signals

| Signal | Type | Labels | What it tells you |
| --- | --- | --- | --- |
| `eshu_dp_supply_chain_impact_winners_resweep_duration_seconds` | histogram | none | Wall time of one committed resweep statement. The recompute is O(findings). A failed or skipped resweep records no sample. |
| `eshu_dp_supply_chain_impact_winners_rows_total` | counter | `action` | Rows a committed resweep handled. `action` is `inserted`, `updated`, `deleted` or `unchanged`. Only the first three write a row version. |
| `eshu_dp_supply_chain_impact_winners_resweep_skipped_total` | counter | none | Resweeps that found another in flight, so wrote and stamped nothing. A resweep can only overlap another after outlasting the lease TTL, so any rate means statements run past 60 seconds. |
| `eshu_dp_supply_chain_impact_winners_watermark_age_seconds` | gauge | none | Seconds since the read watermark was stamped, computed at scrape time from the last watermark the instance saw. Every instance reports it, whether or not it holds the lease. It rises between resweeps, drops on each commit, and keeps climbing through a stall. It has no value until the instance first sees a watermark. |
| `supply_chain_impact.winners_resweep` | span | none | One resweep. Attributes `inserted`, `updated`, `deleted`, `unchanged`, or `skipped`. A failed resweep records the error on the span. |
| `supply chain impact winners resweep committed` | info log | none | The same four counts plus `duration_seconds` and `previous_watermark_age_seconds` (`-1` for the first resweep). |
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
- Then read the resweep duration. The measured 1x baseline is about 40 seconds
  per statement and 72 seconds per tick, on the old write-every-row statement.
  Writing only changed rows removes the write cost, not the read cost, and the
  read is nearly all of that time. A duration near 40 seconds at this scale is
  the known baseline, not an incident. It also already exceeds the original
  trigger for per-key incremental recompute (p95 above 15 seconds for an hour),
  so that design work is due rather than pending. Raising the cadence is not the
  fix.
- A nonzero skip rate means statements run past the lease TTL. Skips are safe,
  but they are the signal that headroom is gone.
- To confirm write volume, compare `pg_stat_user_tables` for
  `supply_chain_impact_canonical_winners` against the counter: `n_tup_upd`
  follows `updated`, `n_tup_ins` follows `inserted`, and `n_tup_del` follows
  `deleted`, none of them the winner count.
