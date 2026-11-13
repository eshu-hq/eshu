# 10564: holder-read chunk 25 to 5, production measurements

Issue #10564 (H leaf follow-up to #10570). This note commits the production
measurements behind the chunk-size change, answering the evidence-overclaim
finding on #10613: headroom figures now live here, not only in prose.

## Corpus and backend

The QA environment primary Postgres, `fact_records` 404 GB total
(193 GB heap), 140 M live rows, shared_buffers 4 GB. Shipped query: `WITH candidates AS
MATERIALIZED` in `go/internal/storage/postgres/supply_chain_impact_fanout.go`
(three-arm pair union, single active-generation join). Guard:
`supplyChainImpactFanOutReadTimeout = 10 s`. All probes read-only
transactions with 5-15 s `statement_timeout`; no writes.

## Before (chunk 25, production)

One 25-id chunk of the failing scope's own CVEs, exact H3 text:
6,440.6 ms — CVE arm 129,289 rows, members arm 8,698, advisory arm 0,
137,987 candidates, 201,922 buffers (51,364 read from disk, I/O read
4,651 ms = 72% of exec). Raw output: `10564-chunk5-explain-25id.txt`.

Fleet `pg_stat_statements` for the candidates query: 475 calls, mean
2,073.8 ms, max 9,802.7 ms; later 504 calls, mean 2,127.1 ms. Three
post-deploy holder-read 57014 timeouts (21:04Z during rollout, 21:32:57Z
and 21:49:49Z steady state).

## After (chunk 5, production)

Hottest 5 of those 25 ids, same shape: 810.8 ms. Raw output:
`10564-chunk5-explain-5id.txt` (section `F1_chunk5_analyze`; that file
also carries the undisclosed-for-this-note sections `F2_chunk25_bitmap_forced`
— the 2,750.6 ms forced-bitmap 25-id run — `F3` statistics objects, and `F4`
settings, kept as run provenance).

Path B tail series (post-deploy 3274bd4205, hottest 5-id inputs, 20 runs
alternating cold DISCARD PLANS / warm, read-only, 12 s guard), ms:
3527, 453, 372, 392, 402, 352, 384, 384, 366, 472, 483, 467, 554, 410,
362, 410, 398, 433, 470, 466. p95 = 554 ms (nearest-rank, codebase
definition), max = 3527 ms (first, coldest run). Bar p95 < 2 s, max < 5 s:
met.

Fleet delta over 794 chunk-5-era calls: implied new-call mean ~745 ms;
cumulative max flat at 9,843.9 ms (no new call at or above it); zero new
57014 and zero new `supply_chain_impact_fanout_failed` dead letters across
three snapshots (01:39Z, 01:40Z, 02:42Z). Snapshots: 614 calls mean
2,061.3 ms; 616 calls; 1,408 calls mean 1,319.0 ms sd 1,940.7.

## Deployment and wave markers

Chunk-5 rollout complete on all core replicas 01:33:57-58Z (eshu-0,
resolution-engine, workflow-coordinator on image sha-3274bd4205); first
snapshot 01:39:29Z postdates it. Wave inside window: 58 vuln-intel
generations 01:34:33Z-02:39:31.5Z. Replay: 1,866 rows to zero, then 18
re-replayed post-chunk-5 to zero in class.

## Why the change is safe

Every lookup id is still read in sequential statements with identical
dedup and the 256 holder cap, so the admitted id set per pass is
unchanged. Statement count grows about 5x (more round trips); per-statement
heap work dominates (72% I/O read at chunk 25), so total work is id-bound,
not statement-bound. Query text, 10 s guard, timeout classification, and
read-site telemetry are untouched; rollback is a one-constant revert.
H-3 covering index stays deferred per the arbiter ruling (build only if
post-chunk p95 exceeds 5 s or the tail reaches the guard).
