# #11430 holder-read-phase shim: single holder statement beats 16 key-range statements ~4x — PROCEED, no implementation

Date: 2026-11-25. Lane: `drive/11430-drain-bottleneck` (leaf child).
Tree: branch `drive/11430-drain-bottleneck`; no production code touched.
Context: the [SCI service-time decomposition](11430-sci-service-time-decomposition.md)
convicted holder Postgres reads at p50 (~29.2s of ~50.8s handler): 16 holder
key-range statements at ~2.2–2.6s each on QA (saturation shim). This lane is the
prove-the-theory-first step for the holder-read phase: EXPLAIN ANALYZE the
key-range reads on QA-shaped data, name the expensive operator, shim ONE
alternative head-to-head, STOP unless decisively faster.

## Threshold (set before measuring)

PROCEED only if the shim runs the phase in at most half the production shape:
`ratio = ranged16_total / single_total >= 2.0` (median of 5 timed runs each,
1 warmup discarded, identical data and snapshot semantics). Anything less is
STOP. The ratio bar is the whole gate; absolute milliseconds on a 204k-row
fixture do not transfer to QA's 8.6M supports.

## Shim

ONE alternative, different access path: replace the 16 ranged statements
(`FindSupplyChainImpactHolderKeyRangeQuery`, one per `HolderKeyRanges` range,
each with cve/advisory btree arms bounded to its `fact_id` range plus its
share of foreign-member token buckets over the tokens GIN) with the single
shipped holder statement (`FindSupplyChainImpactHoldersQuery`: unbounded,
cve/advisory/member arms once each). All SQL, ranges, and token shares derive
from the shipped production constants — nothing hand-copied. Both shapes run
with production semantics: the 16 in one `REPEATABLE READ` read-only
transaction under `plan_cache_mode = force_custom_plan` with the 10s
per-statement timeout; the single in autocommit under the default plan mode.

## Verdict

PROCEED by the pre-declared math. No production implementation follows in this
lane regardless of outcome (task rule); the verdict authorizes the coordinator,
not this child, to carry the single-statement shape forward.

| shape | run 1 median (5 runs) | run 2 median (5 runs) | buffers (EXPLAIN) |
|---|---|---|---|
| 16 ranged statements (phase total) | 97.27ms (95.40–110.58) | 96.36ms (95.64–96.81) | ~16 x 2176 = ~34,816 |
| single shipped statement | 23.78ms (23.25–24.14) | 24.53ms (23.48–25.18) | 5,397 |
| ratio | **4.09** | **3.93** | 6.45x buffer amplification |

Both runs return the identical holder set (1680 scopes); the union-equals-single
exactness the #10643 differentials pin holds on this fixture too.

## Expensive operator (observed, not inferred)

Per key-range statement (`ranged-mid`, ~5.2k of the hot id's rows), the arms
are cheap and the fixed per-statement costs dominate:

- `Unique` 6.05ms + `Sort` 5.96ms + `Hash Join` (scope active-generation)
  3.10ms over the `MATERIALIZED` 3-arm candidate set (~6ms of ~6.5ms).
- Arms: cve btree bitmap 0.50ms; advisory index scan and token GIN probe
  ~0.00ms each (the hot CVE has no foreign rows on QA either).
- The range split is serial (one transaction, 16 statements back-to-back), so
  it multiplies the fixed costs x16 while the indexed arms stay sub-ms. The
  token arm has no `fact_id` bound, so each statement additionally prices a
  GIN probe over its bucket share — free here, list-price on QA with a fuller
  pending list.

The single statement pays each arm and one sort/unique/join once. Notably its
local plan seq-scans the cve arm (13.55ms: 84k of 204k rows match, 41%
selectivity — a seq scan is legitimately optimal there) and still wins ~4x,
because it pays that scan once instead of paying 16 sorts/uniques/joins.

## Commands and observed numbers

- `git symbolic-ref -q HEAD` → `refs/heads/drive/11430-drain-bottleneck`
  (verified before editing; all reads/writes inside that worktree).
- Disposable PostgreSQL 18.7 cluster on 127.0.0.1:55439 (`initdb`, removed
  after; port 55434 belonged to another worker's cluster and was left alone).
  Full production bootstrap applied (375 migrations, ~0.7s), then seeded to
  the QA hot-id shape: 84,000 supports on `CVE-2024-0001` over 1,680 scopes
  (50 each, `md5`-uniform fact ids so the 16 ranges split ~5,250 each — the QA
  per-statement row count from #10643), 120,000 background supports over 4,300
  scopes on 200 CVEs (2% with advisory-group members, never the hot CVE),
  200 own-scope hot rows for the `<>` filter; both GIN pending lists flushed,
  `ANALYZE` on both tables.
- `go vet ./internal/storage/postgres/` → clean; `gofumpt` → clean.
- `TestHolderPhaseShimVerdictStopsBelowTwoX` +
...[truncated 1587 chars]
