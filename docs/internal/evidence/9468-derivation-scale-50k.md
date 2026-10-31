# Derivation query scale proof at 50k (#9468 criteria 4-5)

## Question

Issue #9468 requires `EXPLAIN ANALYZE` over the skip-counts query and
the candidates query at 50k+ observations with a stated per-page bound,
following up the #9299 F1 proof
([9299-derivation-scale.md](9299-derivation-scale.md)). If the proof
misses the bound, the pagination strategy gets redesigned before
producers or reads go live.

## Bound (stated before measuring)

At 50,000 observations on dev hardware with default tuning and cold
shared buffers: candidates page-1 <= 1 s, mid-window <= 1 s,
tail <= 100 ms; skip-counts page-span <= 100 ms, exhausted
remainder <= 1 s. Basis: the F1 linear model (~1.4 ms per 1,000 rows
on the parallel-plan host, ~70 ms projected at 50k) and this lane's
serial-plan measurement (1.85 s page-1 at 300k, ~308 ms projected at
50k). The bound keeps a >= 3x margin over the serial projection and a
full 50k cycle (~195 pages) inside ~3 minutes.

## Method

Throwaway live test (run once, then deleted; the committed 300k
`TestScannerDiscoveryScaleShapeLive` remains the durable proof):
bootstrap the full schema in a disposable database, seed one scanner
instance plus one producer with 40 published generations (one per
scope), grants, active scopes, delivery receipts and 50,000
observations (80% manifest / 10% config / 10% tag-only digests, manifest
verifications for half the config rows), `ANALYZE` every touched
table, then `EXPLAIN (ANALYZE, BUFFERS)` the exact shipped statements
(`scannerDiscoveryCandidatesQuery(257)` and both
`scannerDiscoverySkipCountsStatement` arms) with representative cursor
arguments. Seed and statements mirror the committed 300k test at
1/6 the rows (40 scopes x 1250). Page contracts held: page-1 full
(256/false), mid-window full (256/false past observation 25000), tail
short (0/true).

Hardware: disposable PostgreSQL 18 on the lane dev box (aarch64),
default tuning, cold shared buffers. Plan-shape and order-of-magnitude
evidence, not a QA-hardware wall-time claim.

## Results

Throwaway run, 2026-10-31, this lane head `4aa1d6f979`:

| Statement | Window | Plan shape | Execution |
| --- | --- | --- | --- |
| candidates | page 1 (`after=0`) | Indexed nested loops: 40 probes into `observations_generation_idx` (1250 rows each, scope ANY + `observation_id <= 50000`), hash joins into 40-row dimensions, per-row lateral verification probes, top-N heapsort, LIMIT 257 | 132.7 ms |
| candidates | mid window (`after=25000`) | Same indexed shape over the remaining half | 74.5 ms |
| candidates | tail (`after=maxID-100`) | Tight index bound, nested loops | 10.2 ms |
| skip-counts | page span | Bitmap PK scan on the page bound, nested loops | 8.8 ms |
| skip-counts | exhausted (window remainder) | Full-table aggregate over the window | 115.3 ms |

Every window lands inside the bound with wide margin. At 50k the
planner picks per-scope index probes; the F1 full-table shape only
appears past ~50k active observations per cycle, as recorded there.

## Verdict and trigger

No pagination redesign: the 50k+ proof meets its bound on this
hardware, so the reopen trigger from the F1 note does not trip.
Pagination strategy stays as-is. Reopen if a later measurement at
QA-realistic fleet sizes misses the bound restated above, or if the
full-table shape regresses below 50k observations.

Performance Evidence: throwaway `TestZZDiscoveryScale50kLive`
(go/internal/storage/postgres/zz_discovery_scale_50k_live_test.go,
run 2026-10-31 on lane head `4aa1d6f979`, deleted after the run),
50,000 observations / 40 scopes / PG18 aarch64 dev box, cold buffers,
disposable PG; candidates 132.7 ms page-1 / 74.5 ms mid-window /
10.2 ms tail, skip-counts 8.8 ms page span / 115.3 ms exhausted
remainder. Full EXPLAIN logs in `/tmp/scale50k.log` (throwaway-host
artifact, not committed).
No-Regression Evidence: the committed 300k
`TestScannerDiscoveryScaleShapeLive` still gates bounded pages at
QA-scale cardinality; this 50k run adds no shipped code and changes no
statement text.
No-Observability-Change: no metric, span, or log line was added or
changed for this measurement.

## Observed QA per #9296 (criterion 6 state, 2026-10-31)

Read-only census per #9296 (read replica,
`default_transaction_read_only=on`; counts and shapes only):

- Three `scanner_worker` collector instances registered and enabled
  (image, SBOM, OS runtime roles).
- Two active scanner scopes: one pinned registry-image digest and one
  SBOM repository scope. No running-image scope exists.
- Exactly one `scanner_worker.analysis` fact (completed registry-image
  scan, `target_kind=image`), plus its OS-package findings. Zero
  analysis facts with `target_kind=running_image`, zero with
  `workload_ids`.

Criterion 6 (analysis facts for running digests surfacing with
workload context after R0) is NOT yet observable: the R0 data does
not exist in QA. No lane code change can produce it; the read path
for it is live-proven behind completed results in
`TestDerivedFindingAuthorityOpensBehindCurrentPublicationLive`.
Reopen trigger: a `scanner_worker.analysis` fact with
`target_kind=running_image` appears in QA — then re-run this census
and check workload linkage before closing the criterion.
