# #10095 Postgres reader + live verdict flip (`drive/10095-go-loader`)

Part of #10002. Branch `drive/10095-go-loader`, off freshly pulled
`origin/main` (`78677748`). Owner order
[#10095#issuecomment-6685512918](https://github.com/eshu-hq/eshu/issues/10095#issuecomment-6685512918)
supersedes the arbiter Q1 wait-for-lane-1 disposition; this branch does the
lane-1 remainder under the full merge bar.

## Pre-edit gate (answered from source before any edit)

- **Entry point:** `SupplyChainImpactHandler.Handle`
  (`go/internal/reducer/supplychain/core/impact.go` symbol `SupplyChainImpactHandler.Handle`) →
  `loadSupplyChainImpactEvidence` (`evidence_load.go:113`) →
  `appendGoReachabilityEvidenceFacts` (`go_reachability_load.go:69`) →
  `loadGoReachabilityEvidenceFacts` (`go_reachability_load.go:43`) →
  `FactLoader.(activeGoReachabilityFactLoader).ListActiveGoReachabilityFacts`.
  The production port is `postgres.FactStore` (`go/.../storage/postgres/facts.go:69`),
  which the convergence live harness already wires as the handler's
  `FactLoader` — so landing the method flips the live chain test by itself.
- **Phase ordering:** sync → discover → parse → emit facts → enqueue work →
  reducer → graph/content projection → query surface. Before: holder scope
  holds advisory facts, `go-reachability://<repo>` holds module + reach
  facts at its active generation. After: holder passes load Go evidence,
  classify `not_called`, write copies, fan out holder reopens; winners
  rebuild picks one row per canonical key; the API serves the winner.
- **Data dependencies:** `ingestion_scopes.active_generation_id`,
  `scope_generations.status = 'active'`,
  `fact_records(scope_id, generation_id, fact_kind, payload, is_tombstone)`.
  Written by ingester/collector upserts and scope activation before any pass;
  read under one read-only repeatable-read snapshot so resolved generations
  and fact rows see one state.
- **Re-trigger:** `SupplyChainImpactFanOut.fanOutHolders` plus
  `gomodules.Store.FenceStaleGoEvidence` with the holder reopener reopens
  holder work items with the fence origin stamp; the reducer queue
  (claim → Handle → Ack, as the convergence `drain` plays it) re-runs them.

## What landed

- `go/internal/storage/postgres/active_go_reachability.go` (new):
  `FactStore.ListActiveGoReachabilityFacts` implementing the
  `activeGoReachabilityFactLoader` seam. Per repository id it maps to the
  `go-reachability://` scope through the canonical-id gate (same
  `repository:r_<8hex>` language `scannercomponent.GoReachabilityScopeID`
  owns; the fence/purge reuse the existing `goReachabilityScopePrefix`),
  resolves active generations, then probes
  `(scope_id, generation_id, fact_kind)` for the two govulncheck kinds with
  the active-generation join re-checking the pair. **No new index, no
  migration, no LIMIT** (core evidence is unbounded). Empty filters,
  non-canonical ids, scopes without an active generation, tombstoned rows,
  non-Go kinds, and superseded generations all load nothing.
- `supply_chain_impact_go_fence_chain_live_test.go`: the chain test now
  asserts the verdict flip (holder `not_called`/`govulncheck` before the
  fence, `package_manifest` detail with `unknown` envelope state after the
  fence reopens the holder — the pre-#10095 truth), the alias case
  (`TestGoFenceAliasCopyReadsNotCalledLive`: GO-keyed advisory, CVE-keyed
  finding), and the probe-level matrix
  (`TestGoFenceReaderLoadsActiveGoEvidenceLive`). Winners assertions read
  through `RebuildAllWinners` + the read-model API and require API rows to
  equal the `supply_chain_impact_canonical_winners` rows for the CVE (one
  winners row per canonical key; the key is the table PK) with
  `reachability.state = not_called, source = govulncheck`.
- `scripts/lib/reducer_contention_inventory.json`: the file's enrolled test
  list updated (sorted) for the two new `TestGoFence*` tests; the workflow
  `-run` filter already selects the `TestGoFence` prefix and the enrollment
  guard (`TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate`)
  requires exactly that naming.

## RED/GREEN (all postdating the final edit unless noted)

- Unit shape tests written first: `go vet` failed with `undefined:
  goReachabilityScopeIDs / listActiveGoReachabilityFactsQuery` (RED), then
  green after the reader landed.
- Chain flip: with the reader in place the pre-fence assertion
  (`not_called`) passed first run; the post-fence expectation was corrected
  from `unknown` to the observed live truth `package_manifest` detail /
  `unknown` envelope state after reading `version_decision.go:335` and the
  issue text ("its finding reads `package_manifest`").
- Stage-removal mutation (temporary, restored with zero diff): replacing the
  `appendGoReachabilityEvidenceFacts` call with a no-op makes
  `TestGoFenceReopensAHolderAndItsReducerPassRunsLive` fail at the pre-fence
  verdict (`package_manifest`, want `not_called`). Restored; file diff
  clean.
- `TestGoFenceAliasCopyReadsNotCalledLive` and
  `TestGoFenceReaderLoadsActiveGoEvidenceLive`: green first run.

## EXPLAIN (representative data, recorded — not a committed gate)

Throwaway internal probe (deleted after the run) on a disposable PG 18.6:
one go-reachability scope with 2 live Go facts + 40 unrelated scopes x 5000
mixed-kind facts (~200k `fact_records` rows), `ANALYZE`, then
`EXPLAIN (ANALYZE, BUFFERS)` of the shipped statement with representative
binds, custom and generic plans:

- Custom: `Index Scan using fact_records_scope_generation_keyset_idx`,
  `Index Cond: scope_id = ANY(...) AND generation_id = ANY(...)`,
  kind as filter, 2 rows, **8 buffers, 0.052 ms**, no seq scan, no scope
  walk (`Index Searches: 1` on facts, 2 on the generation re-check).
- Generic (prepared): same keyset probe.
- Small-fixture custom plan (~2k rows): `Index Scan using
  fact_records_collector_status_active_idx` with all three columns as
  `Index Cond`, 5 buffers, 0.075 ms.

Per the `facts/impact` EquivalentAnchors doctrine (branches.go:37-44), a
resolved-scope probe is bounded by every index leading on
`(scope_id, generation_id)` and the planner picks the cheapest: the
repository-scope probe itself anchors on `fact_records_scope_generation_idx`
with the keyset index as the named equivalent. All three observed anchors
(scope_generation, scope_generation_keyset, collector_status_active) lead
on that pair, so the probe is tight at every measured scale with **no new
index**. These are observed probe records on one fixture, not a
before/after speedup claim.

## Golden-corpus parity (every non-Go finding)

- No classifier, writer, digest, or suppression code changed: the diff
  touches the reader, its tests, the fence-chain test, and the inventory.
  Passes whose filters resolve no go-reachability scope with active Go
  evidence load byte-identical evidence sets (the reader returns nil before
  any statement).
- `go test ./internal/reducer/supplychain/core/ -count=1` → ok (includes
  the golden cassette synthesis tests and the digest-coverage tests).
- Digest `Digest`-named core tests → all pass.
- Full golden-corpus gate runs in CI on the PR (babysit below).

## Proof commands (exit codes captured, post-final-edit)

- `go vet ./internal/storage/postgres/` → clean.
- Unit: `go test ./internal/storage/postgres/ -run
  'TestGoReachabilityScopeIDsGate|TestListActiveGoReachabilityFactsQueryProbesScopeGenerationKind|TestGoReachabilityEvidenceKindsAreTheTwoLoaderKinds'`
  → ok.
- Live (disposable PG 18.6 via socket peer auth):
  `go test ./internal/storage/postgres/ -run 'TestGoFence' -count=1 -v` →
  3/3 PASS.
- `bash scripts/verify-license-header.sh` → all files carry the SPDX header.
- `git diff --check` → clean.
- `bash scripts/verify-live-tests-ledger.sh` → 1109 rows ok (no new live
  file; new tests ride the file's existing `dedicated_ci` row).
- Contention inventory `verify_inventory` → OK.

## Observability

No-Observability-Change: the store layer adds no metric. The pass-level
`go_reachability_facts` count and `load_go_reachability` sub-duration the
stage already emits cover this read. A non-Go pass binds an empty scope
list and issues no statement (same no-loader cost as the benchmarked path:
one type assertion + filter).

## Performance and regression markers

No-Regression Evidence (#10095): baseline is the no-loader stage (zero
statements per pass; holder reads `package_manifest`). After the change, a
Go pass issues one scope-resolution plus one probe statement; a non-Go pass
issues none (empty scope list returns before any statement). Backend:
disposable PostgreSQL 18.6 over a Unix socket. Input shape: 1–2 repository
ids, 2 fact kinds, `~200k` `fact_records` rows after `ANALYZE`. Terminal
counts: exactly the live Go envelopes (2 rows in the fixture). Telemetry:
`go_reachability_facts` and `load_go_reachability` per pass. Why safe: the
read is read-only on one repeatable-read snapshot, binds bounded arrays on
the `(scope_id, generation_id)`-leading index family with no LIMIT, and the
measured probe touches single-digit buffers.

## NOT_CHECKED

- CI gates on the PR head (golden-corpus, go-core-complete,
  go-race-complete) — babysit after push.
- Remote-validation / deploy behavior — out of scope for this bound.
