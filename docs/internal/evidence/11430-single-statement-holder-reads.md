# #11430 single-statement supports-table holder reads: guard proof and routing

Date: 2026-11-25. Lane: `drive/11430-drain-bottleneck` (leaf child).
Tree: branch `drive/11430-drain-bottleneck` in
`/Users/allen/personal-repos/eshu-wt-11430-bottleneck`.

Context: the [SCI service-time decomposition](11430-sci-service-time-decomposition.md)
convicted holder Postgres reads at p50 (~29.2s of ~50.8s handler): 16 holder
key-range statements at ~2.2–2.6s each on QA. The [holder-read-phase
shim](11430-holder-read-phase-shim.md) proved the theory first: on QA-shaped
data (84k hot supports, 120k background, disposable PG 18.7) the single
shipped statement ran the phase ~4x faster than the 16 ranged statements
(ratio 4.09 / 3.93, bar 2.0), same holder set. This lane implements the
routing: the already-shipped `FindSupplyChainImpactHoldersQuery` is the
hot-singleton path.

## QA guard proof (arbiter binding order (a)): PASS, proceed

Guard: median of >=3 `EXPLAIN ANALYZE BUFFERS` runs of the single statement
<= 5.0s, no run >= 10s, generic AND custom plans, worst holder id, 10s
timeout itself, read-only via `/tmp/qa-read.sh` (port-forward to
`eshu-postgresql-read`, `statement_timeout` 10s set inside each file).

Worst holder id: `CVE-2025-27789` — 83,642 cve-arm rows at the time of
#10643 (largest documented), 101,377 rows measured on QA today (`Bitmap Heap
Scan`, `Heap Blocks: exact=78453`). It carries no advisory or member rows,
and it still dominates every member-heavy id combined (max documented:
22,626 cve-arm + 21,342 member-arm rows; max foreign alias 13k–49k rows).

| plan mode | runs (Execution Time, 10s timeout) | median |
|---|---|---|
| default (first touch, buffer-cold: 44,924 reads + 31,123 temp writes) | 6,344ms | — |
| `force_generic_plan` (PREPARE + EXECUTE) | 1,109 / 848 / 825ms | 848ms |
| `force_custom_plan` (PREPARE + EXECUTE) | 721 / 715 / 728ms | 721ms |

No run >= 10s; both per-plan medians <= 5.0s; no median lands in 5–10s.
The cold first touch (6.3s, buffer-cold with temp writes) stays under the
10s guard and under the retired 16-statement cold cost (3.1–21.7s per hot
id on QA, #10643). Verdict: PASS — implement.

## Routing change (arbiter binding order (b))

`go/internal/reducer/supplychain/core/fanout_split.go` only: every planned
statement — a hot singleton included — runs the shipped single holder
statement once. The `ranged` branch, the `HolderKeyRangeReader` seam lookup,
and the 16-statement fan-out are gone from routing; the key-range store read
(`supply_chain_impact_fanout_key_range.go`) stays implemented with its
>1,024-byte fallback branch, but routing sends it no statements. A lookup id
over 1,024 bytes reads the same shipped statement (the token function never
stores one for it), so the long-id fallback is preserved by resting on the
shipped shape.

No changes to lock/claim/lease/queue SQL: `git diff --stat` shows only
`fanout_split.go`, `HOLDER_FANOUT.md`, core tests, and this note; no file
under `go/internal/storage/postgres/` (SQL lives there) is touched, and no
worker, batch-size, or serialization knob changed. Telemetry read kinds
(`holders`, `holders_key_range`, `holders_key_range_read`) and the
`holder_key_range_statement` signal helper plus the `fanout_holder_*` /
`fanout_key_ranged_ids` sub-signal keys are unchanged; only the routing
counts changed (hot singletons now count as `holders` statements).

TDD: `TestHotSingletonHolderReadRunsAsSingleStatement` was added first and
failed RED against the old routing (hot id ran 16 ranged statements), then
passed GREEN after the routing change. Superseded routing pins were updated
to the new contract, not deleted silently: the old key-range test body is a
supersession pointer, and the 10643l signal contract is now pinned directly
against `noteKeyRangeStatement` (all 8 cases) plus routing-quiet proofs.

## Exactness differential

The shipped statement text is byte-identical (no postgres diff), and its
equivalence to the union-of-16 was re-proven live on disposable PG 18.7
(full 375-migration bootstrap):

- `TestHolderKeyRangeReadMatchesShippedLive` PASS (3.10s): union-of-16 ==
  single, edge holders included (last range, NULL impact_status,
  advisory-only, member-only, below/above bounds), exclusions held
  (asking scope), seeded RED (dropped last range loses holders).
- `TestSupplyChainImpactSupportsHolderFamilyEqualsTheFactOracleLive` PASS
  (2.18s): shipped read == fact oracle (foreign-member, advisory, own-scope
  exclusion, superseded rows).
- `TestSupplyChainImpactSupportsHolderReadsEqualFactReadsLive` PASS (0.50s):
  supports reads == fact reads.

## Focused proof (arbiter binding order (c))

- `go test ./internal/reducer/supplychain/core/ -count=1` PASS (2.08s, full
  package, includes the new RED→GREEN test and the rewritten signal/count
  tests).
- `go vet ./internal/reducer/supplychain/core/` clean; `gofmt -l` clean.
- Plan-shape live proofs on disposable PG 18.7:
  `TestSupplyChainImpactSupportsHolderPlanShapeLive` PASS (18.55s),
  `TestHolderKeyRangePlanViolationShapes` PASS (all sub-cases).
- Before/after phase timing (holder phase only): lab fixture medians from
  the shim lane — 16 ranged statements 96–97ms vs single shipped statement
  ~24ms (~4x); QA guard medians above (generic 848ms, custom 721ms on the
  101k-row worst id, 10s guard). Stop gate: none of the gates failed.

## Phase timing claim

Holder phase only: the retired shape paid 16 sorts/uniques/joins serially
(~2.2–2.6s each on QA at p50); the shipped shape pays one (~0.7–1.1s warm
on the worst id under the 10s guard). No other phase was touched.

## Evidence markers

- Performance Evidence: baseline = 16 key-range holder statements per hot
  singleton (QA p50 ~2.2–2.6s each; lab phase median 96–97ms on 84k-hot /
  120k-background QA-shaped data, disposable PostgreSQL 18.7, full
  375-migration bootstrap); after = one shipped holder statement (lab phase
  median ~24ms, ~4x; QA read-replica EXPLAIN ANALYZE BUFFERS on the worst
  id CVE-2025-27789 with 101,377 cve-arm rows under statement_timeout 10s:
  generic-plan median 848ms, custom-plan median 721ms, no run >= 10s).
  Backend/version: disposable PG 18.7 (aarch64) for differentials, QA
  `eshu-postgresql-read` replica for the guard. Input shape: hot singleton
  CVE id plus cold pack, 10s per-statement guard, force_generic_plan and
  force_custom_plan via PREPARE/EXECUTE. The change is safe because the
  shipped statement is byte-identical, the union-of-16 == single
  differential re-passes live (edge holders, exclusions, seeded RED), and
  every statement keeps the same 10s guard with fail-closed retry.
- Observability Evidence: the read-duration histogram keeps one sample per
  shipped statement under `holders` (hot singleton included) with the same
  `ok`/`read_timeout`/`read_error` outcomes — no `holders_key_range` or
  `holders_key_range_read` samples are recorded anymore; the
  `fanout_holder_statements`, `fanout_holder_key_range_statements` (now 0),
  and `fanout_key_ranged_ids` (now 0) sub-signal keys are unchanged; the
  `holder_key_range_statement` span event and warn log fire only via
  `noteKeyRangeStatement`, whose contract is pinned directly by unit tests
  (routing emits none).
