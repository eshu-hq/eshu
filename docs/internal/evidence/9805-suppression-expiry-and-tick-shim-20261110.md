# #9805 suppression-expiry proof and slow-tick theory shim (2026-11-10)

Refs #9805. Two arbiter-ordered items: the unconditional time-driven
suppression-expiry test (criterion 4), and the prove-theory-first shim for
the slow `build_active_facts` ticks on QA (criterion 5). The first two items
changed no production code: the expiry theory was disproven by test, and the
shim verdict gates the tick fix design. The census addendum below found one
reader that did need a production fix, landed in this same stack.

## Suppression expiry: no resweep fix needed (theory disproven)

Theory: a suppression whose `expires_at` passes with no fact write creates
no dirty-log row, so the keyed table goes stale until the audit repairs it.

Disproof, from source and by test:

- The winner select stores `suppression_state` and `suppression_expires_at`
  verbatim from payloads and has no time predicate:
  `supplyChainImpactWinnerBodySQL` in
  `go/internal/storage/postgres/supply_chain_impact_canonical_winners_store.go`
  (the `suppression_state` / `suppression_expires_at` projections and
  `supplyChainImpactOperatorSuppressionPredicate` in the same file), and
  no `NOW()` anywhere in the select, rebuild, or keyed apply
  SQL. A fresh recompute after an expiry therefore produces byte-identical
  rows, so there is nothing for a tick to repair.
- Readers derive the effective (expired) state at read time from the stored
  pair: `supplyChainImpactEffectiveDecisionStateSQL` over the winners table
  in `go/internal/query/supply/chain/impact/findings_queries.go`, and
  `advisoryGroupWinnerEffectiveStateSQL` in
  `go/internal/storage/postgres/supply_chain_impact_advisory_group_winners.go`.

Proof: subtest `time-driven suppression expiry passes with no fact write`
in `supply_chain_impact_winners_keyed_parity_live_test.go`. It seeds an
operator override expiring 3s out (database clock), ticks, lets the expiry
pass with no write, and asserts dirty depth 0, a zero-write tick, parity
with a fresh recompute, and a stored row whose `suppression_expires_at <=
now()`. GREEN on current code (8.90s full parity suite, disposable
Postgres 18). Sensitivity: with a deliberate mutation making the operator
predicate time-dependent (`expires_at > NOW()`), the same subtest goes RED
at parity (incremental `accepted_risk` + expiry vs fresh `active` + null).
The rejected fix (union time-expired keys into every tick) would recompute
identical rows and add `build_active_facts` volume — the phase already
over budget below.

## Slow-tick shim: candidate volume times cold I/O (QA primary, read-only)

Method: one `BEGIN READ ONLY` session on the QA environment's primary
Postgres pod, `statement_timeout=30s`, count-only mirror of the lateral probe
in `buildWinnersKeyedActiveFactsSQL`
(`supply_chain_impact_winners_keyed_sql.go`), triples taken from
the winners table so the probe is the session's first `fact_records`
touch. Script: `/tmp/9805-shim.sql` on the agent host; exits 0.

Measured (2026-11-10 ~11:00Z):

| Probe | Candidate rows | Probe buffers (hit+read) | Buffers/row | I/O read share |
| --- | ---: | ---: | ---: | ---: |
| Top-25 triples | 43,081 | 15,072+34,245 | 1.144 | 267ms of 512ms |
| Mid triples (offset 1000) | 7,902 | 2,286+11,832 | 1.79 | 2,622ms of 2,804ms |

- `fact_records`: 129.0M live, 1.94M dead (1.50%), last autovacuum
  2026-11-10T02:49Z. Dead-entry waste (12.2-46 buffers/row pre-#10119)
  stays disproven; buffers/row is at post-vacuum health.
- The read fraction (69% hot, 84% mid) did **not** drop on an immediate
  rerun (34,174 reads again): probe pages churn out of shared buffers
  between ticks, so slow ticks pay cold random I/O per fetched row.
- Fetched vs kept: the top-25 triples fetch 43,081 candidate rows for
  1,275 distinct keys (33.8 fetched per key; the probe keeps a whole
  triple per dirty key by design). Winners per triple across 15,338
  triples: p50=2, p95=18, p99=51, max=145 (71,023 winners).
- Dirty-log depth at shim time was 10,482 rows, so real ticks regularly
  face the volume the synthetic 1%-churn scale run (358.9ms at 1x) did
  not: ~865 keys p95 per tick on QA can mean hundreds of thousands of
  candidate heap fetches, mostly cold.

Verdict per the arbiter's pre-named decision rule: volume branch. Bound
the candidate rows (distinct triples) one tick fetches — chunked consume
in `supply_chain_impact_winners_keyed.go`/`_keyed_sql.go` driven from
`winners_keyed_maintainer.go` — while never stamping a fresh watermark
with consumed-but-unapplied keys outstanding. That fix is NOT in this
change; it needs its own failing scale/plan test first (bounded
searches/buffers and tick bound at QA-like hot-triple volume). Neither
branch supports an index/probe rewrite, lease renewal, or serialization.

NOT_CHECKED: per-tick Prometheus correlation of phase seconds vs keys
(existing samples only); apply-phase split (second phase, p95 4.04s);
host CPU throttling; QA re-measurement over a >=3h window (needs the
chunking fix on a rolled build first).

Performance Evidence: the first two items add one live subtest and this
note; no runtime SQL, index, batch, or concurrency change. The QA numbers
above are the before-measurement for the chunking fix. The census
addendum's reader-#10 fix changes one read predicate to the effective-state
form its siblings already run; same index, same row set for non-expired
rows, no new scan shape.

No-Observability-Change: no new metric, span, log, or knob. The tick
phase histogram (`build_active_facts`) remains the operator signal.

## EPSS-reader suppression-expiry census (2026-11-10, follow-up)

Refs #9805. Criterion-sweep item: census every production reader that
consumes suppression expiry state in a way involving EPSS, and every reader
of the suppression `expires_at`-style state on the winners/reader path.
Method: `rg` over `go/` for `suppression_expires_at`,
`suppression_state = 'active'`, `expires_at`, and
`FROM supply_chain_impact_canonical_winners`, reading the actual code of
every hit — counts alone are not evidence. `git grep` corroboration:
`suppression_expires_at` in production Go appears only in the readers and
writer/rebuild/compact sites listed below; raw
`suppression_state = 'active'` in production Go has exactly one hit (the
EPSS CVE-ids query). Test files are excluded from the census.

### Verdict: claim NOT final

The winners-select time-invariance proof stands for the findings surface,
but one EPSS-involving reader breaks it: `listActiveFindingCVEIDsQuery`
filters the stored `suppression_state` with no clock, so a suppression
whose `expires_at` passes with no fact write keeps its CVE out of EPSS
derivation while the findings reads report the same row as
`expired`/visible. A recompute cannot heal it (byte-identical rows, proven
above), so no tick ever rewrites the row. No fix is implemented here; the
failing-test sketch below is the blocker evidence.

### Per-reader table

| # | Reader (file:symbol) | What it reads | Clock | Class |
| --- | --- | --- | --- | --- |
| 1 | `go/internal/query/supply/chain/impact/findings_queries.go:supplyChainImpactEffectiveDecisionStateSQL` (3 use sites in `ListFindingsFromWinnersQuery`) | `w.suppression_state` + `w.suppression_expires_at` (text) | `$24::timestamptz` read clock + `pg_input_is_valid` guard | SAFE (read-time) |
| 2 | `go/internal/query/supply/chain/impact/grouped_inventory_sql.go:groupedInventoryEffectiveStateSQL` | `w.suppression_state` + `w.suppression_expires_at` (text) | `$20::timestamptz` read clock | SAFE (read-time) |
| 3 | `go/internal/storage/postgres/supply_chain_impact_advisory_group_winners.go:advisoryGroupWinnerEffectiveStateSQL` | `w.suppression_state` + typed `w.suppression_expires_at` | `NOW()` | SAFE (read-time) |
| 4 | `go/internal/query/supply/chain/impact/suppression_split_sql.go:supplyChainImpactSplitCanonicalWinnerCTEs`, `findings_split_queries.go`, `aggregates_queries.go` (fact-path read-time dedup/counts) | operator `payload #>> '{suppression,expires_at}'` | `$NN::timestamptz` read clock | SAFE (read-time) |
| 5 | `go/internal/query/supply/chain/impact/explain_postgres.go` (via split CTEs) | operator `payload #>> '{suppression,expires_at}'` | `$13::timestamptz` read clock | SAFE (read-time) |
| 6 | `go/internal/query/supply/chain/impact/findings_decode.go:decodeSupplyChainSuppressionDecision` | already-overlaid effective state in the page payload | none (downstream of #1/#4) | SAFE (downstream) |
| 7 | Winner select `supplyChainImpactWinnerBodySQL` (`go/internal/storage/postgres/supply_chain_impact_canonical_winners_store.go:142-226`) | stores `suppression_state`/`suppression_expires_at` verbatim | none (`NOW()` absent) | NEUTRAL (writer; proof premise) |
| 8 | Staged scan/compact (`supply_chain_impact_winners_staged_scan.go`, `supply_chain_impact_winners_staged_compact.go:stagedStoredWinnersSQL`), rebuild (`supply_chain_impact_canonical_winners_rebuild.go`), audit diff (`supply_chain_impact_winners_audit.go`), keyed apply (`supply_chain_impact_winners_keyed_sql.go`) | full stored row / narrow candidate columns | none (content equality only) | NEUTRAL (no expiry consumption) |
| 9 | `grouped_inventory_sql.go:groupedInventoryHiddenRepositoriesSQL` | grant + static-winner predicate only | none | NEUTRAL (no suppression read) |
| 10 | `go/internal/storage/postgres/supply_chain_impact_canonical_winners_store.go:listActiveFindingCVEIDsQuery` (`WHERE suppression_state = 'active'`) | stored `suppression_state` only | NONE | BREAKS (stored-state reliance) |

Reader #10's chain, all read: `FactStore.ListActiveFindingCVEIDs`
(store file `:435`) implements `FindingCVEIDReader`
(`go/internal/coordinator/installed_advisory_target_readers.go:33-35`),
called at `go/internal/coordinator/vulnerability_intelligence_service.go:350`,
feeding `deriveEPSSFindingTargets`
(`go/internal/coordinator/vulnerability/epss_finding_derivation.go:73-77`),
which normalizes, dedupes, and chunks CVE ids with no suppression re-check.
Staleness direction: after expiry with no fact write the stored row keeps a
hidden state (e.g. `accepted_risk`) with past `suppression_expires_at`;
readers #1-#3 report `expired`/visible, but reader #10 keeps excluding the
CVE from EPSS derivation indefinitely. The advisory-group reader carries an
explicit guard test against the raw-equality pattern
(`supply_chain_impact_advisory_group_winners_test.go:64-65`); no such guard
covers the CVE-ids query — only a shape test pinning the raw equality
(`supply_chain_impact_finding_cves_test.go:20-25`).

Out of scope (different `expires_at`, verified by reading):
`finding_static_advisory_sql.go:approval.expires_at` is static-authority
approval freshness, not suppression expiry. The reducer evaluator
(`go/internal/reducer/supplychain/core/evaluation.go`) decides expiry at
fact-evaluation (writer side), not a reader.

### Failing-test sketch (not implemented; blocker evidence)

Live test sketch against `ESHU_POSTGRES_TEST_DSN`, extending
`TestListActiveFindingCVEIDsLive`
(`supply_chain_impact_finding_cves_live_test.go`): seed a winners row with
`suppression_state='accepted_risk'` and
`suppression_expires_at = NOW() - interval '1 hour'`, then assert the CVE
IS returned (effective state `expired` is visible, matching readers #1-#3).
Current code omits it, so the sketch goes RED. The fix (reader #10 derives
expiry at read time, e.g. an effective-state predicate like #1-#3, or the
derivation input moves to an expiry-aware read) is coordinator scope and is
NOT in this change.

Counts: 6 SAFE (5 read-time, 1 downstream of them), 3 NEUTRAL (no expiry consumption), 1 BREAKS.

## Census addendum: reader #10 fixed, claim final (2026-11-10)

Refs #9805. `listActiveFindingCVEIDsQuery` now derives the effective
suppression state at read time, reusing reader #3's typed-column predicate
(`advisoryGroupWinnerEffectiveStateSQL` plus the hidden-state list derived
from `supplychaincore.SupplyChainSuppressionHiddenStates`): the winners
column is `TIMESTAMPTZ NULL`, so the `pg_input_is_valid` text guard readers
#1/#2 need has no equivalent here — a typed column cannot hold invalid text.
A hidden state whose `suppression_expires_at <= NOW()` reads `expired` and
its CVE reaches EPSS derivation; a non-expired hidden state stays excluded;
visibility is effective-state `NOT IN` hidden, exactly as readers #1-#3
decide it. Row #10 is therefore SAFE (read-time) and the census closes at
7 SAFE, 3 NEUTRAL, 0 BREAKS: the time-invariance claim is final for every
EPSS-involving reader. The shape test that pinned the raw equality now pins
the effective-state predicate and carries the same bare-equality guard as
the advisory-group test; the live seed's non-vocabulary `suppressed` state
became a real non-expired `accepted_risk`.

RED (before the fix, disposable Postgres on `eshu_test`):
`TestListActiveFindingCVEIDsLive` seeding `accepted_risk` with
`suppression_expires_at` one hour past failed with
`missing "CVE-2024-8888" in ["CVE-2024-0001" "CVE-2024-0002" "GHSA-..."]`.
GREEN (after): `TestListActiveFindingCVEIDsLive` and
`TestListActiveFindingCVEIDsQueryShape` pass; the winners parity proof
`TestSupplyChainImpactWinnersResweepKeyedMatchesRecomputeAcrossChangeClassesLive`
(8.94s, includes the time-driven expiry subtest over this store file's
select) and `TestAdvisoryGroupWinnersQualifyingLive` pass; `go vet` on the
package exits 0.

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS)` at QA winners volume
(71,023 synthetic rows, warm cache, disposable database, deleted after):
both predicates plan Seq Scan -> HashAggregate -> Sort with identical
buffers (1244 shared hit); old 24.6ms, new 30.8ms once per schedule tick,
the delta being the per-row CASE plus the correctly-included expired rows
(63,920 kept vs 56,818). No index, DDL, batch, or concurrency change.

No-Observability-Change: no new metric, span, log, or knob. The read stays
on the existing instrumented Postgres query path and the per-plan EPSS
counters in the coordinator; expired CVEs now simply appear in the
`epss_distinct_cves` count they were always meant to feed.
