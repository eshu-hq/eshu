# code_drifted overfull band-bucket skip (#7228)

The pairs nomination self-joins `code_fingerprint_band` on
`(repo_id, band_no, band_hash)`, so its cost is quadratic in the largest
band bucket. One repository's `code_drifted` item ran the pairs query for
60+ minutes on CPU with no wait event and outlived its lease. This note
records the shim proof, the three settled questions, and the fix.

## Shim (local only, no QA environment)

`postgres:18` in a throwaway container, tables per migrations 111 + 117 +
151 (band PK, lookup index, entity index). One repo `r_shim`: 4,181
functions (~31 bands each, 131,146 band rows, matching the issue's
131,040), token floor 50, budget 200. Pathological shapes layered on:

- five 307-member identical buckets on `band_no = 0` (the issue's top-5),
- one bucket grown to 1,000 members,
- a 1,000-entity then 3,000-entity "boilerplate army" sharing all 31 bands,
- 200 filler repos (2.6M band rows total) for the multi-repo table shape.

All measurements are `EXPLAIN (ANALYZE, BUFFERS)` of the verbatim shipped
query text (extracted from the Go const, never hand-copied), same host,
back-to-back arms.

## Scaling curve (before)

| Shape | Pairs | Time | Buffers | Temp |
| --- | --- | --- | --- | --- |
| 5x307 buckets | 53,725 | 0.75 s | 687k | 0 |
| 1,000-bucket | 506,251 | 4.3 s | 4.08M | ~75 MB |
| 1,000-army x31 bands | 1,005,748 | 15.7 s | 457k | ~1.5 GB |
| 3,000-army x31 bands | 5,001,733 | 144.6 s | 3.9M | ~21 GB |

Pairs 93x → time 193x: super-linear past the spill cliff. The planner
underestimates the pairs CTE 40x to 7,000x (e.g. 784 rows estimated vs
5.5M actual per loop), so sorts and aggregates are never provisioned and
always spill on big buckets. The count query shares the same pairs CTE
and doubles the cost (2.2 s on the 1,000-bucket shape).

## Q1: skip or cap overfull buckets? Verdict: skip

A bucket with more members than the per-entity budget (200) hands every
member more same-bucket partners than the budget verifies: it is
non-discriminating by the query's own semantics (an LSH stop band), and
the budget already drops genuine drift below the cut on such repos.
Per-bucket pair caps were rejected: they keep a biased arbitrary subset
at the cost of an extra window for the same bound class. The loader
passes `MaxBandBucketSize = MaxCandidatesPerEntity` (200) as the pairs
queries' bucket-cap bind; buckets at or under the cap nominate exactly
as before.

## Q2: why was one run slow and others normal?

Per-repo bucket distribution plus the spill cliff: repos without a
boilerplate army run sub-second; a 3,000-army runs 144.6 s on this host
with 21 GB of temp I/O, and a bigger army on a loaded host with slower
temp disk credibly reaches the observed 60 minutes (the pairs AND count
queries each pay the pairs CTE). The issue's top-5 = 307 counts were
captured mid-load, so the reducer-time shape may have held a bigger army
than the snapshot shows. Plan shape is NOT the differentiator on the
shim: the plan is a stable merge join across PostgreSQL 16 and 18,
vacuumed and unvacuumed tables, custom and generic plans, and 20
ANALYZE samples. NOT_CHECKED: the exact production plan (the second
sighting's nested loop did not reproduce here).

## Q3: should lease expiry cancel the statement?

It already does when the loss is observed: the reducer runs the handler
on the heartbeat context (`executeWithTelemetry`), and the first failed
heartbeat tick returns `ErrReducerClaimRejected` and cancels that
context, which cancels the in-flight query. No lease-path change in this
fix. Why the 60-minute statement survived its expired lease is not
established from code alone (unobserved loss, e.g. ticks blocked behind
the runaway statement itself, is the leading hypothesis); it needs that
run's worker logs, not a theory. Recorded here, not widened into this
PR.

## After

Same shim, cap 200, pairs query only:

| Shape | Pairs kept | Time | Buffers | Temp |
| --- | --- | --- | --- | --- |
| 5x307 buckets | 6,754 | 0.50 s | 42k | ~36 MB |
| 3,000-army x31 bands | 6,754 | 0.56 s | 57k | ~46 MB |

The army case drops from 144.6 s to 0.56 s (259x) with the 21 GB spill
gone. The remaining temp is the window sorts over the 6,754 kept pairs.
The new bucket-stats query is one linear grouped scan of the repo's band
rows (same scan the nomination already pays); it was not separately
timed because it is decision-immaterial at 259x headroom.

## Differential

On the 307-bucket shape, old query vs new query:

- New query with an above-max cap is byte-identical to the old query
  (53,725 rows, `cmp` clean): the prefilter mechanics change nothing
  when no bucket is skipped.
- Pairs with neither endpoint in a skipped bucket are byte-identical in
  both directions (5,640 rows, 0 differences): normal-bucket findings
  are unchanged.
- 48,085 old rows touched the pathological entity set; the new query
  keeps 1,114 of their normal-band pairs with corrected shared counts
  and drops the rest. Only pairs touching a skipped bucket change.

## No-Regression Evidence

No-Regression Evidence (#7228): buckets at or under the cap nominate
byte-identical pairs (differential above, plus the live test's C(3,2)
anchor). Buckets over the cap nominate nothing by design; those pairs
were budget-drowned noise (each member's 200-budget filled with
boilerplate partners). The equality-duplicate counter takes the same
skip so its denominator matches the nomination it mirrors. Golden
corpus: no cassette or snapshot change; the B-7 gate decides in CI.

## Observability Evidence

Observability Evidence (#7228): skipped buckets ride the existing
`eshu_dp_correlation_rule_matches_total` counter as
`rule=skipped_band_bucket`, and the per-intent `code drifted generation
evaluated` log carries `pairs_considered`, `skipped_buckets`, and
`max_bucket_size`. Coverage row updated in
`docs/public/observability/telemetry-coverage.md`; verifier green.
