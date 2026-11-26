# Fanout holder-read cost: executing shim convicts the below-gate middle band, gate retuned 78435 to 3000

Date: 2026-11-26. Lane: `drive/fanout-holder-read-cost` (leaf child).
Tree: branch `drive/fanout-holder-read-cost` off `origin/main`; production
query texts untouched, one constant retuned.
Context: the holder-read lineage (`drive/10643-*`, `drive/11430-holder-routing`,
`drive/10972-h2-reroute`, `drive/h2-holder-isolation-shim`) routes probe-hot
singletons by routing mass: above `HolderKeyRangeMassGate` reads as 16
fact_id key-range statements, at-or-under reads the shipped single holder
statement. The [H-3 record](../../go/internal/storage/postgres/evidence-holder-covering-index-h3.md)
disproved covering indexes on the old fact-record expression arms; this lane
re-fires the cost question on the supports table with executing proof.

## Summary

We tested which holder-read shape actually burns the 10s statement budget at QA mass. Singletons in the 9k–78k row middle band that the old gate admitted breach the 2s per-statement bar — a 29,417-row id runs the shipped statement in 2.36s — while the same id reads as key-range statements in 0.56s total with a byte-identical holder set. The gate is therefore retuned from 78435 to 3000: ids at or under 3000 rows keep the cheap shipped statement, everything hotter reads as ranges.

## Method (read-only, replica only)

Every number below is `EXPLAIN (ANALYZE, BUFFERS)` (single runs, labelled
warm/cold by buffer state) executed on the QA standby `eshu-postgresql-read-0`
(`in_recovery = true`, role `eshu`), each shape in its own
`REPEATABLE READ, READ ONLY` transaction with an explicit ~115 s
`statement_timeout`. All SQL texts, key ranges, and member-token shares were
rendered programmatically from the shipped Go constants in this tree
(`FindSupplyChainImpactHoldersQuery`, `HolderMassProbeQuery`,
`HolderRoutingMassQuery`, `HolderKeyRanges`, `HolderMemberTokens`) — nothing
hand-copied. Nothing ran on the primary. Planning-only numbers from the prior
lane are superseded by these actuals.

## The eating arm, by actuals

Full 5-hot-id holder statement (962,259 candidate rows, all buffers hit):
**7.75 s**. Per-arm split of the same chunk shape: cve arm 4.62 s
(660,856 rows, Bitmap Heap Scan), members GIN arm 1.08 s (67,415 rows,
422,601 buffers hit — 6.2 buffers/row, the worst per-row arm), advisory arm
0.68 s. The Aggregate over the 962k-row union adds ~4.6 s of wide-`scope_id`
string-hash CPU (no temp spill; in-memory quicksort/hash throughout). The
5-in-1 shape does not run in production (the splitter isolates hot ids), but
its arms do — as singletons.

1-hot-id shipped singletons (the shape the old gate admitted to 78,435 rows):

| id rows (cve/adv/mem) | statement | holders | verdict vs bar |
|---|---|---|---|
| 29,417 / 0 / 0, partly cold (9,505 reads) | 2.36 s | 110 | over the 2 s p95 bar |
| 9,032 / 0 / 0, mixed cache | 1.67 s | 201 | no margin |
| 6,006 triple-counted (2k x 3 arms), warm | 0.13 s | 141 | passes |
| 301,395 mixed, warm | 3.08 s | 245 | over the bar (already ranges by routing mass) |

Same 29,417-row id as 16 key-range statements (production semantics:
one `REPEATABLE READ` txn, `plan_cache_mode = force_custom_plan`):
**0.56 s total, 0.105 s max statement, byte-identical 110 holders** —
4.2x faster total, 22x under the old statement time.

Mass shapes for one hot id: probe ~0.02 s warm (cheap, stays); routing
count 0.02–0.27 s warm with 17k–49k heap fetches on a visibility-map-degraded
Index Only Scan (cold cost unmeasured — see NOT_CHECKED).

## Rejected alternatives (with numbers)

- Covering INCLUDE indexes on the direct arms: removes only the heap-I/O
  share (~3.0 s of the 7.75 s; the ~4.6 s wide-string hash CPU stays), needs
  ~20 GB per index at production `scope_id` widths, and needs primary DDL.
  Insufficient alone — no-build.
- Arm split (per-arm statements, member-arm skip): does not reduce total
  heap volume; skipping the member arm on probe evidence is unsound across
  the probe/read snapshot gap. No-build.
- Bounding the retry-widened recovery set at `fanout.go:260`: disproven as
  a timeout cause at current mass — the widest QA scopes hold 13.7k–21k
  supports with at most 87 distinct CVEs (<=18 five-id chunks, seconds per
  retry, all under per-statement guard). Truncation without a carry would
  under-fan-out and strand stale copies. No change.
- Raising the 10 s guard, serializing workers, skip-and-park: ruled out by
  the lane brief, not re-litigated.

## Fix

`HolderKeyRangeMassGate` 78435 to 3000
(`go/internal/reducer/supplychain/core/fanout_key_range.go`): the largest
comfortably-passing singleton mass halved for margin (6006/2, rounded),
mirroring the old gate's methodology. Small-hot ids (<=3000 rows) keep the
cheap shipped statement (~0.1 s); everything hotter ranges. No query text,
migration, wire, or telemetry change; the routing count derives its new
LIMIT 3001 from the same constant.

## Verification

- TDD red first: new boundary row (routing mass 9000 must range) failed on
  the old constant, green after.
- `go test ./internal/reducer/supplychain/core/ -count=1`: ok.
- `go test ./internal/storage/postgres/ -count=1`: ok (unit; live skips
  without env).
- Live on disposable PG 18.6: `TestHolderMassProbeLive`,
  `TestHolderMassProbeCappedLive`, `TestHolderKeyRangeReadMatchesShippedLive`,
  `TestHolderKeyRangeTokenReadMatchesShippedLive`,
  `TestSupplyChainImpactSupportsHolderPlanShapeLive`: all PASS (key-range
  identical to shipped, plans unchanged).
- `go vet` and `gofmt`: clean on both packages.

## NOT_CHECKED

- Cold-cache key-range and routing-count timings (non-superuser role cannot
  drop shared buffers; all after-numbers above are warm/partly-cold).
- Production traffic shape after rollout (p95/max vs the `fanout.go:36`
  bar, `fanout_failed` trend, dead-letter disposition): coordinator/CI
  territory; this lane pushed nothing.
- The full-scope max distinct-CVE census (the whole-table aggregate times
  out at 100 s on the replica; the worst-case bound rests on the
  widest-by-rows scopes at <=87).
