# 9926 staleness-gate evidence

Why a bulk onto an analyzed table can still misplan, what the gate checks
now, and the measured before/after on the same corpus.

## Setup

Local PostgreSQL 18 over socket on the dev box. Corpus: 100,000 pending
items across 200 fairness keys, the #9871 shape, bulk-loaded with no stats
refresh while the table carries an ANALYZE from its empty state (what
autovacuum leaves on a fresh table within a minute of creation).
Autovacuum stays off during measurement so the only analyzer is the gate.
Statement under test is the production claim query constant (plan guard
derives EXPLAIN from the shipped text with a prefix check, not a copy).

## Measurements

Analyzed-empty then +100k, gate unfixed (never-analyzed check passes, no
ANALYZE): candidate plans all five scans on
`family_queue_depth_idx` — the full #9871 cold shape on an analyzed table.

Analyzed base of 1 row then +100k: degrades to `claimable_idx` scans with
sorts. Analyzed bases of 10, 100, 1000, and 5000 rows hold the fair-claim
plan through +100k growth. Insert growth onto a 100k base holds through
+185% (n_mod_since_analyze 185k), and 60% update churn (60k dead tuples)
holds with 5.2 ms candidate execution. Estimates track growth through the
physical index size, so the flip is not a growth ratio: it is statistics
that describe a trivially small base. A fully production-seeded bulk does
not reproduce the cold shape (wide rows tip the zero-statistics cost tie
toward the fair index); the committed live test therefore bulks raw and
triggers through the production admission.

After the fix (staleness rule: analyzed base under 10 rows with growth
since ANALYZE past the base, counting flushed n_mod_since_analyze plus the
admitted rows): the same admission ANALYZEs once post-commit, first claim
plans on `fair_claim_idx` with no family or sequential scan; post-admission
claims n=20 p50 4.1 ms, max 7.5 ms. Warm-plan p50 is not regressed (4.1 ms
vs the #9871 4.7 ms warm baseline). Six concurrent claimers during ANALYZE
overlap with zero errors (56 ms wall on this box; the owner measured
738 ms). Firing refreshes statistics, which resets the growth counter and
reprices the base, so the gate fires once per staleness accumulation and
never on every admission.

Performance Evidence (#9926): analyzed-empty +100k plans 5/5 scans on the
family index while the never-analyzed gate passes; base 1 degrades to
claimable-index sorts; bases ≥10 hold fair through +185% inserts and 60%
churn; post-fix first claim via production admission p50 4.1 ms max
7.5 ms (n=20), warm p50 unchanged. The claim path is untouched, so
per-claim cost is zero; the one bounded ANALYZE runs once per stale
accumulation, post-commit, SKIP_LOCKED, non-fatal and counted. Rejected by
measurement: a growth-ratio predicate (no flip through 32x growth — the
axis is analyzed-base size, not ratio) and per-table autovacuum settings
(same global naptime floor as #9871).

Observability Evidence (#9926): no new counter or span — the stale-triggered
ANALYZE reports the existing `analyzed` outcome on
`eshu_dp_workflow_work_items_stats_analyze_total` (site x outcome
cardinality unchanged) with the admitted-rows log key; the
`verify-telemetry-coverage.sh` gate agrees and the telemetry package tests
pass.
