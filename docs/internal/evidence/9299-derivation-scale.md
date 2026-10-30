# Derivation query scale proof (#9299 F1)

## Question

Issue #9299 acceptance requires a performance proof on the derivation
query (`EXPLAIN ANALYZE` on the QA corpus) before the change lands. The
committed proof (`TestScannerDiscoveryPagePlanLive`) covers shape at
5,100 rows on one scope. This note records the same two shipped
statements at QA-scale cardinality: 300,000 observations across 40
scopes, three digest kinds, with manifest verifications for half the
config-digest rows.

## Method

Throwaway live test (run once, then deleted; the committed 5k test
remains the durable shape proof): bootstrap the full lane schema in a
disposable database, seed one scanner instance plus one producer with 40
published generations (one per scope), grants, active scopes, delivery
receipts and 300,000 observations (80% manifest / 10% config / 10%
tag-only digests), `ANALYZE` every touched table, then `EXPLAIN
(ANALYZE, BUFFERS)` the exact shipped statements
(`scannerDiscoveryCandidatesQuery(256)` and both
`scannerDiscoverySkipCountsStatement` arms) with representative cursor
arguments. Seed and statements mirror production call paths, including
the 40-element scope array and the uuid-cast role parameter.

Hardware: disposable PostgreSQL 18 on an aarch64 dev box, default
tuning, cold shared buffers. Results are plan-shape and order-of
magnitude evidence, not a QA-hardware wall-time claim.

## Results

Throwaway-host run, 2026-10-29 (parallel workers). Head-run serial-plan
numbers (1.85 s / 1.09 s / 27.6 ms / 15.3 ms / 1.75 s) are in Performance
Evidence below.

| Statement | Window | Plan shape | Execution |
| --- | --- | --- | --- |
| candidates | page 1 (`after=0`) | Parallel seq scan over 300k observations, hash joins into 40-row dimensions, per-row lateral verification probes (225k loops), top-N sort, LIMIT 256; 707k buffers | 422 ms |
| candidates | mid window (`after=150000`) | Same full-table shape over the remaining half | 645 ms |
| candidates | 300-row tail | Bitmap PK scan (`observation_id < 256`-style bound), nested loops, 15 buffers | 3.2 ms |
| skip-counts | page span | Bitmap PK scan on the page bound, 187 rows, nested loops | 2.5 ms |
| skip-counts | exhausted (window remainder) | Full-table aggregate, same shape as candidates page 1 | 430 ms |

Abridged candidates page-1 plan (full log was captured from the
throwaway run):

```text
Limit (actual time=423.5..428.1 rows=256)
  ->  Sort (top-N heapsort, 202500 rows)
        ->  Nested Loop Left Join (rows=202500, filter on digest class)
              ->  Nested Loop (rows=225000)
                    ->  Seq Scan on scanner_instance (1 row)
                    ->  Gather: Parallel Seq Scan on observations
                        (rows=75000 per worker, scope ANY filter)
                        + hash joins to generations / grants / scopes
              ->  Aggregate over verifications index scan per row
Planning Time: 6.3 ms, Execution Time: 428.1 ms
```

## Reading

Per-page cost is linear in the table size (~1.4 ms per 1,000 rows),
because every page except a tight tail re-scans the whole observation
set: the keyset cursor predicate is an OR over `(id, domain)` that the
planner cannot use as a single index bound except through a BitmapOr,
which only wins when the bound is already tight. A full active-set cycle
costs pages × per-page, i.e. roughly quadratic: ~8 minutes at 300,000
active observations, ~0.5 s at 10,000, milliseconds at QA-realistic
fleet sizes (low thousands of running containers per cycle).

The page-1-spans-everything shape is inherent to a fresh cycle (the
cursor resets on exhaustion, so `upper = max(id)`). Driving pages from
the PK would need a different pagination strategy (e.g. a monotonic
cursor that never resets, or per-scope windows), which is a design
change, not a pre-merge rewrite: observations are immutable intake
facts, but generation switches, scope changes and policy revisions all
interact with cursor resets today.

## Verdict and trigger

The artifact this acceptance box requires exists: both statements are
measured at 300k rows with shapes and wall times above. The lane stays
correct at any scale (bounded pages, per-binding suppression, fail-closed
authority); it is fast at QA-realistic scale and slow past ~50k active
observations per cycle. The lane is dark and triple-gated off by default,
so no production path pays this cost until typed producers
(#8852/#8859) and derived reads (#9468) land. The follow-up criterion
now on #9468 (candidates at 50k+ alongside its skip-counts proof) is the
reopen trigger: if that proof misses its bound, the pagination strategy
gets redesigned before producers or reads go live.

Performance Evidence: committed scale proof
`TestScannerDiscoveryScaleShapeLive` (go/internal/storage/postgres/control_scanner_discovery_scale_live_test.go),
run 2026-10-30 on this lane head, 300k observations / 40 scopes /
PG18 aarch64 dev box, cold buffers, disposable PG; candidates 1.85 s
page-1 / 1.09 s mid-window / 27.6 ms tail, skip-counts 15.3 ms page
span / 1.75 s exhausted remainder. This replaces the stale 2026-10-29
throwaway run (`f1009986d7`, script deleted): the statements are
byte-identical to that run, but this host plans serial nested loops
(1.5M buffers page-1) where the throwaway host planned parallel
workers (422 ms / 645 ms / 3.2 ms / 2.5 ms / 430 ms) — a systematic
environment plan choice, not a lane-code difference. The quadratic
narrative above holds under either plan; the #9468 50k+ proof decides
the pagination strategy on its own hardware.
No-Regression Evidence: the committed 5k shape proof is unchanged and
still pins the no-seqscan invariant at small scale.
No-Observability-Change: no metric, span, or log line was added or
changed for this measurement.
