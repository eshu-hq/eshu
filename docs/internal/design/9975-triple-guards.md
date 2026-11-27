# Triple-Guard Probe Rewrite: Arbiter Round-6 Build Record (#9975)

Status: built on a passing falsifier, 2026-11-27. Worktree
`drive/9975-triple-guards`, unpushed; handoff SHA to the coordinator.
Prior lanes consumed: covering index KILLED, plain-column probe REJECTED,
materialize-apply KILLED, key-presence Exit A KILLED (round-5 record:
`docs/internal/design/9975-key-presence-falsifier.md`).

Refs #9975 (this issue), #9805 (peak bar), #11414 (sibling re-emission lane;
owns `go/internal/reducer/supplychain/core/*`, not touched here).

## 1. The approved shape (arbiter round-6 APPROVE-WITH-SHAPE)

Triple legs with per-key presence guards (bits 1/2/4 only), c0/advisory legs
as plain equalities with NO guards:

- `(s.package_id = ident.package_id AND (ident.package_id <> '' OR (s.key_presence & 1) <> 0))`,
  same with bits 2 (repository_id), 4 (subject_digest)
- `COALESCE(NULLIF(s.cve_id,''), NULLIF(s.advisory_id,''), '') = ident.c0`
- `s.advisory_id = ident.advisory_id`

`s.*` are plain `supply_chain_impact_finding_supports` columns (all
`COALESCE(payload->>'k','')`, `NOT NULL DEFAULT ''`); `key_presence SMALLINT
NOT NULL DEFAULT 0` (migration 362). The covering index (migration 363)
`(package_id, repository_id, subject_digest, (c0-expr), advisory_id)`
`INCLUDE (fact_id, scope_id, generation_id, key_presence, cve_id)`, expressions
byte-identical to the probe per the 354 header rule.

## 2. Falsifier: PASS on every bar (throwaway, deleted after the run)

Disposable scratch PG18, round-5 geometry replicated: 12,000 supports /
125 staged idents (124 formula + the all-`''` operator ident) / ~6KB TOASTed
payloads, co-locating noadv / nulladv / nocveadv (advisory key absent, JSON
null, both-keys absent) plus all triple edge shapes (missing key, JSON null,
explicit `''`, scalar field, operator-like triple-missing, all-empty).
`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` both arms, `EXCEPT` both directions.

| Bar / kill line | Measured | Outcome |
|---|---|---|
| `EXCEPT` 0 both ways (kill: any nonzero) | shipped-only 0, candidate-only 0 | PASS |
| candidate plan Index Only Scan (kill: other plan) | `Index Only Scan using triple_guard_probe_idx`, 125 index searches | PASS |
| Heap Fetches 0 (kill: > 0) | 0 on every IOS node | PASS |
| buffers whole-chunk ≤ 625 (kill: > 625) | candidate 456 vs shipped 3577 | PASS |
| single-ident buffers (diagnostic, ≈3-5) | 5, Heap Fetches 0 | recorded |

Row volume matches the live shape: 4199 pre-filter copies over 125 idents
(~33.6/ident). The guards filter 5 rows post-index (missing-key exclusions the
shipped probe also rejects — `EXCEPT` 0 proves it). Perf-evidence markers for
the diff: plan node Index Only Scan, Heap Fetches 0, buffers 456 whole-chunk /
5 single-ident vs 3577 shipped, winners contract bump 2→3.

## 3. What was built

- Migration 362: `ALTER TABLE ... ADD COLUMN key_presence SMALLINT NOT NULL
  DEFAULT 0` (metadata-only, one statement). Default 0 is fail-closed: the
  guarded probe excludes `''`-identifier rows with unset bits instead of
  widening to them (proven live: unfilled table shipped=2, guarded=1).
- Writer fill: `key_presence` is a fill-only column
  (`supplyChainImpactSupportFillOnlyColumns`), derived from
  `supplyChainImpactKeyPresenceFillExpr("fact")` — the single source the
  fill's chunk/update/verify share, so writer and backfill cannot disagree on
  a bit. Fill-only (never compared) is correct: any payload change that moves
  the bits rewrites the row anyway; a replay with unchanged triple presence
  rewrites nothing. No supports-contract bump (re-pin with two differentials,
  same precedent as the stable-key fill-only column).
- Key-presence fill (`SupplyChainImpactKeyPresenceFill`, same marker table and
  conflict-domain locking as the stable-key fill): batched chunks ≤ 500,
  resumable cursor, disagreement-0 verification before `completed_at`, restart
  on any disagreement. Marker `supply_chain_impact_key_presence_fill_v1`.
- Probe flip in `buildWinnersKeyedChunk`: one indexed marker read per tick;
  marker complete → triple-guarded batch probe, else the shipped
  payload-expression probe. Fail-closed default (missing row, NULL stamp, or
  unreadable gate all read as not ready). Outcome bit `keyPresenceProbe`
  records which text the tick ran for 3 AM plan diagnosis.
- Audit keeps the legacy statements as its oracle (no audit file changed).
- Winners contract 2→3 (new probe text pinned; byte-identical winners proven
  by `TestSupplyChainImpactWinnersNarrowTripleGuardProbeMatchesShippedLive`).
- Migration 363: `CREATE INDEX CONCURRENTLY` covering index (migration 354
  stays for the pre-marker fallback probe).
- Tests: new `EXCEPT`-0 agreement test (noadv/nulladv/nocveadv + all triple
  edge classes, plus per-row writer-fill bit pins), key-presence fill live
  test (fill → restart-on-disagreement → complete → post-fill `EXCEPT`-0 +
  fail-closed direction), concurrent-build live test, hermetic variant/unity/
  fragment/bound/migration-shape tests, 362 coverage/shape/seeded-violation
  tests, plan-parser test on the measured plan. Both round-3/round-5 pinning
  regressions adopted and renamed to the gate-selected names.

## 4. Rollout order (blocking)

1. Land 362 + 363 (additive; old binaries ignore both; 363 builds
   concurrently, invalid-cleanup covered by test).
2. Roll the new binary everywhere (old binaries write `DEFAULT 0` bits and
   run the shipped probe — always correct).
3. Wire and enable the key-presence fill runner (NOT in this diff — named
   follow-up for the coordinator; spec in section 7), drain old binaries
   first: the marker completes only on a clean verification, so a lingering
   old writer holds the flip closed rather than corrupting it, but completion
   needs writers that fill bits.
4. Fill reaches `completed_at` → ticks flip to the guarded probe, first one
   full per the contract bump.

## 5. Proof table (final head, this lane)

| Check | Result |
|---|---|
| Round-6 falsifier (throwaway, deleted) | PASS every bar: 0/0, IOS, fetches 0, 456 ≤ 625, single 5 — section 2 |
| `TestSupplyChainImpactWinnersNarrowTripleGuardProbeMatchesShippedLive` | PASS (red before the build: undefined export) |
| `TestSupplyChainImpactWinnersNarrowProbeExcludesMissingKeyLive`, `...MatchesCoalesceLegsLive` | PASS |
| `TestSupplyChainImpactSupportsKeyPresenceFillLive` (fill + restart sweeps=1 + fail-closed 2v1 + post-fill 0/0) | PASS |
| `TestSupplyChainImpactWinnersNarrowTripleGuardIndexBuildsConcurrentlyLive` (30/30 supports, valid+ready) | PASS |
| Hermetic: variant/unity/fragments/bounds/migration-shape/coverage/shape/seeded-violations/plan-parser/pins/differentials | PASS |
| Keyed parity suites: change classes, interleaving, chunked, scanned budget, settle-skip + deadlock, horizon (7), audit sweep + audit, trigger, rekey, probe plan (354), stream body + scalar + buffer budget, scale, scope plan, admission order, chunk-oldest, convert slow, scope remainder, volatile-digest, evidence-only, retention | PASS (63 tests) |
| Supports differential, fence-scanner, differential, family kill/lease/dup/seeded-reds, guard change/transition/mixed-version, detoast equivalence, write-on-change rows/payload | PASS |
| `go vet ./...` (package), `gofmt`, `git diff --check` | clean |
| QA-replica keyed-tick p95 vs 15 s | NOT_RUN — no QA credentials or route from this lane; coordinator owns QA re-reads |
| `TestSupplyChainImpactSupportsFamilyDigestEqualAcrossWorkersLive` | PASS after re-pin (worker-equality held throughout; pin moved by the new column, worker-invariant digests agree 1/2/4) |
| Pre-existing failures, identical on clean `origin/main` (not this lane) | 6 supports-fence/verify + 2 narrow write-bar; recorded, untouched |

## 6. Joint contract pin (flagged FOR SIBLING-LANE REVIEW, not edited here)

`go/internal/reducer/supplychain/core/*` untouched. The construction is safe on
live data under this invariant, which needs a contract test both lanes agree
on: every staged dirty identifier with a `''` component comes from a finding
whose payload carries that key explicitly (Go zero value serialized, key
present), and every finding-kind fact payload carries all five of
`package_id`, `repository_id`, `subject_digest`, `cve_id`, `advisory_id` as
JSON strings. Coverage must include the non-SDK finding writers both lanes
know about (migration-249 classification, scanner-associated flows, pre-SDK
old-writer rows) and the capture path deriving identifiers from staged state.

## 7. Named follow-up: key-presence fill runner

Not built in this lane (default-off background wiring would double the review
surface without moving any bar). Spec: mirror
`cmd/reducer/supply_chain_impact_stable_key_fill.go` (config, pace, pause,
election, run loop) with `postgres.SupplyChainImpactKeyPresenceFill`,
`start/runSupplyChainImpactKeyPresenceFill` in `run.go` next to the stable-key
wiring, config test mirroring `supply_chain_impact_stable_key_fill_test.go`,
env names `ESHU_SUPPLY_CHAIN_IMPACT_KEY_PRESENCE_FILL_*`. Telemetry: mirror
`instruments_supply_chain_impact_stable_key_fill.go` (steps, rows, pause,
complete gauge) so the fill is observable at 3 AM.

## 8. Observability

No new metric names in this diff (deliberate: the runner follow-up owns the
fill instruments). The flip decision is recorded on
`winnersChunkOutcome.keyPresenceProbe` for tests and future wiring; operators
tell which probe text a tick ran from the marker row and `pg_stat_statements`
(the two probe texts are distinct statements), while the fill is visible via
its marker row (`completed_at`, `sweeps`) and step reports and the index via
`pg_index.indisvalid`. No-observability-change beyond that: no new workers,
knobs, or routes.
