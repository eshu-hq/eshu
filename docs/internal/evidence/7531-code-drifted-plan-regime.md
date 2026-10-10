# code_drifted plan regime after the bucket skip (#7531)

Verdict: the plan regime is stable after #7228. Auto mode still promotes the
generic plan on the sixth execution (the issue's mechanism, confirmed 5/5),
but the generic plan is no longer a different, slower plan: custom and generic
choose the same hash-join shape in every arm and visibility-map state (one
access-method divergence, WITHOUT + empty VM, cost-neutral per the medians
below), and 160/160 ANALYZE-sample plans are identical within their cell. No
index, statistics, or `plan_cache_mode` change is warranted at 5x scale. This
note covers acceptance 1 (the sixth-execution probe, both arms); acceptance 2
and 3 need reference-corpus runs and stay open on the issue.

## Shim (local only, no QA environment)

`postgres:18` in a throwaway container (18.6), tables per migrations 111 + 117
(band PK, lookup index; fingerprint PK, repo index), entity index per migration
151 in the WITH arm only. Sizes match #7254 to within 128 filler rows: 200
filler repos (10,502,400 band rows, against #7254's 10,502,528) plus 1x
(159,840) and 5x (799,200) repos, 11,461,440 band rows in all.
The 5x repo holds 10 clone families of 25 members sharing all 32 bands over a
seeded Poisson background (seed 0.7531, reproducible), yielding 77,721 result
rows (ledger:7531-rows-5x) against #7254's 78,094. One database was loaded,
cloned per arm, and ANALYZEd with autovacuum held off (`relallvisible = 0`);
the vacuumed half ran after `VACUUM (ANALYZE)` on both clones
(`relallvisible` = all pages).

`scripts/probe-code-drifted-plan-regime.sh` rebuilds the shim and runs the
probe: the query text is extracted from the `listCodeDriftedPairsQuery` Go
const by awk at probe time, never hand-copied. Binds are the production floor
(50), budget (200), and bucket cap (200). The arm split and the median are
manual reconstruction from the script's outputs:

```sh
# Per-arm clones from the loaded base (autovacuum already held off by shim).
CREATE DATABASE shim_with TEMPLATE shim_base;
CREATE DATABASE shim_noidx TEMPLATE shim_base;
# WITH arm only, verbatim migration file:
\ir go/internal/storage/postgres/migrations/151_code_fingerprint_band_entity_idx.sql
# Empty-VM half first (relallvisible = 0), then on both clones:
VACUUM (ANALYZE) code_fingerprint_band, code_function_fingerprint;
# Median of one wall cell from the script's round lines (the WITHOUT arm is
# labeled NONE in wall output):
rg 'arm=WITH mode=driftc' wall.txt | sed 's/.*secs=//' | sort -n | awk 'NR==3'
```

## Acceptance 1: sixth-execution probe, both arms

One session per cell: `PREPARE` the verbatim query, five quiet `EXECUTE`s,
then `EXPLAIN (ANALYZE, BUFFERS)` of the sixth, plus the
`pg_prepared_statements` counters. Times are single runs; the medians below
are the timing comparison.

| VM state | Arm | auto 6th (plan) | custom 6th | generic 6th |
| --- | --- | --- | --- | --- |
| empty | WITH | 3167 ms (generic) (ledger:7531-sixth-emptyvm-with-auto) | 3963 ms (ledger:7531-sixth-emptyvm-with-custom) | 3515 ms (ledger:7531-sixth-emptyvm-with-generic) |
| empty | WITHOUT | 3379 ms (generic) (ledger:7531-sixth-emptyvm-noidx-auto) | 3951 ms (ledger:7531-sixth-emptyvm-noidx-custom) | 3595 ms (ledger:7531-sixth-emptyvm-noidx-generic) |
| vacuumed | WITH | 3305 ms (generic) (ledger:7531-sixth-vac-with-auto) | 3705 ms (ledger:7531-sixth-vac-with-custom) | 3736 ms (ledger:7531-sixth-vac-with-generic) |
| vacuumed | WITHOUT | 3523 ms (generic) (ledger:7531-sixth-vac-noidx-auto) | 3487 ms (ledger:7531-sixth-vac-noidx-custom) | 3799 ms (ledger:7531-sixth-vac-noidx-generic) |

Every auto cell shows `generic_plans = 1, custom_plans = 5`: unlike migration
103's query, this one does promote the generic plan, so the issue's theory
about the mechanism is right. But the promoted plan costs the same as the
custom one: shapes are identical (hash joins over the `kept_buckets`
aggregate, nested-loop PK fingerprint probes), shared buffer hits agree
exactly within each state (476,158 empty-VM WITH, ledger:7531-buf-emptyvm-with;
477,126 vacuumed both arms, ledger:7531-buf-vac), temp I/O is within 6%, and
result rows agree exactly (77,721, ledger:7531-rows-5x).

The one surviving access-method divergence is WITHOUT + empty VM: custom takes
a seq scan of the band table while generic takes a lookup-index bitmap (the Go
comment's "lookup index only under a generic plan" still holds there). It is
cost-neutral here: 3.95 s against 3.60 s single-run, and the medians below
agree. WITH + empty VM uses the entity-index bitmap in both modes; vacuumed
uses the pkey index-only scan in both modes and both arms. The single-run
auto sixth lands 6-12% under the forced-generic sixth in every cell, inside
the ~15% round-to-round noise the wall runs show on this shared host, so the
interleaved medians below are the timing comparison, not the single runs.

## Wall medians: custom vs generic

Plain `EXECUTE`, 5 interleaved rounds alternating arm and mode so the shared
host's noise lands on all cells equally:

| VM state | Arm | custom median | generic median |
| --- | --- | --- | --- |
| empty | WITH | 3.28 s (ledger:7531-wall-emptyvm-with-custom) | 3.21 s (ledger:7531-wall-emptyvm-with-generic) |
| empty | WITHOUT | 3.27 s (ledger:7531-wall-emptyvm-noidx-custom) | 3.32 s (ledger:7531-wall-emptyvm-noidx-generic) |
| vacuumed | WITH | 3.11 s (ledger:7531-wall-vac-with-custom) | 3.08 s (ledger:7531-wall-vac-with-generic) |
| vacuumed | WITHOUT | 3.13 s (ledger:7531-wall-vac-noidx-custom) | 3.12 s (ledger:7531-wall-vac-noidx-generic) |

Generic lands -3% to +2% of custom in every cell. The pre-#7228 2x gap (#7254:
about 5 s custom against about 11 s generic on 18.6 at the same shape) is
gone. Absolute seconds are host-bound (shared box); the ratio within each
interleaved round is the finding. Each timed `EXECUTE` is preceded by its own
`SET plan_cache_mode`: the mode is consulted at EXECUTE time, verified on this
shim (custom `EXPLAIN` shows the seq scan, generic the `$1` bitmap), so a
single shared PREPARE block would time one plan in both columns.

## ANALYZE-sample flips: 0/160

Planning-only custom `EXPLAIN` after each of 40 fresh `ANALYZE` samples per
cell, recording the join/access signature:

- empty-VM WITH: 0/40 trials flipped (ledger:7531-flip-emptyvm-with), all hash + nested loop + entity-index bitmap.
- empty-VM WITHOUT: 0/40 trials flipped (ledger:7531-flip-emptyvm-noidx), all hash + nested loop + seq scan.
- vacuumed WITH: 0/40 trials flipped (ledger:7531-flip-vac-with), all hash + nested loop + pkey index-only.
- vacuumed WITHOUT: 0/40 trials flipped (ledger:7531-flip-vac-noidx), all hash + nested loop + pkey index-only.

Pre-#7228 the same survey flipped 0 to 3 of 40 across cells (#7254).
Post-#7228 the custom shape does not move with the statistics sample on this
shim.

## With an active skip

A 1,000-member band-0 bucket layered onto the WITH + empty-VM clone (the skip
path #7228 exists for): auto promotes generic again, all three modes take the
same bitmap shape, times stay in the same band (auto 3.23 s, custom 3.98 s,
generic 3.61 s), and rows drop by only 185 to 77,536
(ledger:7531-rows-5x-army): pairs exclusive to the skipped bucket disappear and
no C(1000,2) explosion appears. The regime holds with a live skip.

## Why it converged, and what did not

The #7228 `kept_buckets` prefilter reshaped the self-join into a hash join
over the bucket aggregate in both plan modes; the pre-#7228 merge-join shape
whose generic costing diverged is gone. The row misestimates persist (generic
top node estimates 1 row, custom 13-14, actual 77,721), so the convergence
holds despite the estimates, not because of them: a future planner or data
change could re-diverge the shapes, and the committed probe is the cheap way
to re-check. The entity index is not a cost differentiator in any cell
(WITH and WITHOUT agree within noise); it only changes the empty-VM access
method at equal cost.

## No-Regression Evidence

No-Regression Evidence (#7531): no query text, index, or knob changes; the
pairs query returns byte-identical row sets (77,721 rows) under custom,
generic, and auto-promoted plans in every arm and VM state. The committed
probe script runs the acceptance-1 probe and the flip survey against the
shim it builds, so a future query change can re-prove the regime.

## Observability Evidence

No-Observability-Change: no metric, span, log, or status signal is added. The
regime stays observable through `pg_prepared_statements`
(`generic_plans`/`custom_plans`) and the existing per-intent drift telemetry
(`pairs_considered`, `skipped_buckets`, `max_bucket_size`).

## NOT_CHECKED and remaining work

- Acceptance 2 and 3 (corpus-run `auto_explain`, entity-index `idx_scan`,
  band-table `relallvisible`, `upsert_fingerprints` sums, both arms from one
  manifest) need reference-corpus bootstrap runs this machine cannot do.
- The 3,444 s corpus tail item is not explained by the 5x-scale regime: every
  plan here runs about 3.5 s, so its cause (host load, a heavier repository,
  or an unobserved corpus-only plan) stays open.
- PostgreSQL 16 (only 18.6 measured), cold cache (all runs warm), concurrent
  reducer connections sharing one pool, and real MinHash sketches (synthetic
  sharing here).
