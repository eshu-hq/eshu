# PR-C2 write stop: evidence and inventory (#10880)

Status: hermetic work for C2 h, i, j, k, l, n, o, built on the C1k head `a8ba1c0687`. Nothing here
was run against Postgres: every live proof named below is owed to a coordinator slot. This note
records what changed, what each proof shows, what is owed, and what C2 m will delete.

## What changed

| Item | What | Hermetic proof |
|---|---|---|
| h | The impact writer upserts supports from its in-memory rows through an `unnest` source with the same projection, SET list and write-on-change guard as the fact-derived upsert. The fact-derived upsert stays for the backfill. | The two upserts differ only in their FROM source; the six statements the supports contract covered still hash to the previous pin (contract stays version 1; the new statement is appended). |
| h2 | The in-memory upsert drops tombstoned rows and rows of another kind, as the fact-derived WHERE did. | Planted tombstone and foreign-kind rows are not bound. |
| i | Retraction is `DELETE FROM supports ... RETURNING` feeding the winners dirty staging and the retracted keys. The per-row fencing predicate is gone; the admission row orders passes. The scoped variant filters on the `cve_id` column. | Staging and counts tail hashes to the base tail; no `fencing_token`, `fact_records` or `is_tombstone` in the statement. |
| j | `WriteSupplyChainImpactFindingRows` no longer inserts fact rows and refuses a transaction without the supports maintainer. | A full and a scoped pass issue no INSERT into or UPDATE of `fact_records`. |
| k | Migration 355: the reject trigger. `k2`: the reducer reads `pg_trigger.tgenabled`, exports the armed gauge, and the impact entry gate defers while it reads 0. | Pinned-pair text test with 11 seeded RED plants; census guard; gate RED/GREEN pair. |
| l | Every audit statement that read facts reads supports by declared counted swaps (panic at init on drift): audit winner select, keyed active and operator reads, scope expansion, pending select, keyed range with its oversize and operator arms, NULL-key probe, staged batch, staged ident fetch. | Each derives from its legacy statement by exactly the declared swaps; none names `fact_records`. |
| n | Route coverage: every `ReadRoute` label has a declared sweep request; the live half scrapes `eshu_dp_supply_chain_impact_read_route_total` around each. | Table routes to its labels by the stores' pure functions; planted scrapes and a fake core. |
| o | The latency gate seeds the winners corpus through the supports step. | Seed rows shape; seeded-table counts include supports. |

After l the audit compares the winners with the supports only. The D3a sweep that runs before the
stop is the last independent oracle.

## The trigger

Migration 355 (the next free slot above C1's 350 to 354; 349 was reserved before C1 and renumbered
while nothing had applied it). `BEFORE INSERT OR UPDATE ON fact_records FOR EACH ROW WHEN (NEW.fact_kind
= 'reducer_supply_chain_impact_finding')`, raising `ERRCODE '55000'`, `CONSTRAINT
'fact_records_reject_supply_chain_impact_finding'`, `TABLE 'fact_records'`: the C0 classifier fires only on
that pair, and the repo's older triggers raise with ERRCODE alone. Unconditional, no DELETE, `pg_trigger`
guard so a re-apply takes no lock and keeps a disabled trigger disabled.

Runner (`schema_bootstrap_lock.go:294-341`): the ledger is keyed by `(path, variant)`; a definition is
applied iff its key is absent. There is no monotonic or maximum check, so a lower number applies at the
next bootstrap on an existing database and in sorted position on a fresh one.

`CREATE TRIGGER` takes `SHARE ROW EXCLUSIVE` on `fact_records` for the catalog write only. Migration 338
took the same lock for its fence triggers at D1; its duration at the QA scale is not measured here (arbiter
unverified item).

## Write-path census

Non-test Go that writes `fact_records`: `factwrite` batch and single inserts (every caller hardcodes its own
kind), `facts_streaming.go` (the generic ingestion upsert), the eshu search writer, the testfixtures
(other kinds), and three COPY seeds in the latency gate (IaC, cloud state, winners). The only writers of
the finding kind were the impact writer (now supports only), `cmd/ifa` (the same step) and the gate's
winners seed (moved by o). `TestNoGoFileWritesTheFindingKindToFactRecords` holds this: no non-test Go file
may name the kind and write the table, with an empty shrink-only allowlist and planted look-alikes.

## Proof clones start armed

Proof databases carry the trigger armed, as production does: a forgotten disarm fails loudly with 55000,
a forgotten arm would pass silently. The first cut of this branch disarmed every clone; that was reverted.

- `postgresproof.SeedFindingFacts` disables the trigger around a raw plant of a frozen finding fact, then
  re-enables it and fails the test if it does not read enabled. The code under test runs armed.
- `postgresproof.DisarmFindingWriteStopForTest` disables it for a whole test that models a schema from before
  C2 and re-arms and checks at cleanup.
- A hermetic shrink-only allowlist (`finding_write_stop_allowlist_test.go`) names every file that calls any
  disarm form with a reason, with planted callers and look-alikes.
- Applied: `winnersLiveExec` (the winners and audit family seed raw writes of finding facts scoped, so the
  writer and maintainer run armed); whole-test through `openPreC2ReplaceSetLiveDB` and
  `openPreC2IsolatedLiveDB` (old-writer fence, backfill, drain, verify, key fill, differentials against the
  frozen fact reads); the fan-out family (`fanOutLiveDB`, about 16 files that plant facts with raw SQL, each
  to become a scoped plant); the capped-scope opener, the isolated-schema capture proof and the
  advisory-group winners proof.
- Not converted (they will fail loudly with 55000 until they are): bootstrap-based proofs in
  `internal/query/supply/chain/impact`, `internal/reducer/advisory/group`, `internal/query` and the
  gomodules package that plant finding facts with raw SQL.
- Live test `TestSupplyChainImpactSupportsFactWriteStopTriggerLive` proves a fresh bootstrap reads
  `tgenabled = 'O'` and a write of the kind raises the real `*pgconn.PgError` (code 55000, constraint, table)
  that the impact transaction classifies as `supply_chain_impact_fact_write_stopped`.


## Claims table (arbiter-test-reality section (d)2)

One row per contract claim of C2. Role is the database role the proof runs as; N is the size it runs at.
`not run` means the test exists and is enrolled but no live run has happened (every live proof is owed to a
coordinator slot); `NOT_CHECKED` means no test of that kind exists yet.

| # | Claim | Test | Seeded-violation test | Role | N | Status |
|---|---|---|---|---|---|---|
| 1 | The writer's pass issues no INSERT into or UPDATE of `fact_records` (full and scoped pass) | `TestWriterWritesNoFindingFactRow` (core) | `TestChangedImpactPassFootprintByRecording` plants a fact write | recording transaction | 1 pass, 2 findings | hermetic: pass |
| 2 | A pass writes exactly the pinned set of tables and takes only the conflict-domain advisory lock | `TestChangedImpactPassFootprintByRecording` (real writer and wrapper over a recording transaction) | the same test plants a fact write and a table lock | recording transaction | 1 changed pass, 2 findings | hermetic: pass |
| 3 | A transaction that cannot maintain the supports table is refused, not silently dropped | `TestWriteSupplyChainImpactFindingRowsRefusesATransactionWithoutASupportsMaintainer` | the test itself (refusal asserted, empty input still a no-op) | none | 1 | hermetic: pass |
| 4 | The in-memory supports upsert differs from the fact-derived upsert only in its source | `TestSupportRowsUpsertDiffersFromFactDerivedOnlyInTheSource` | planted column check (every shared expression still present) | none | statement text | hermetic: pass |
| 5 | The supports contract stays version 1 (append only) | `TestRowsUpsertOnlyAppendsToTheVersion1Contract` | `TestSupplyChainImpactSupportsContractHashPinnedToVersion` | none | statement text | hermetic: pass |
| 6 | Tombstoned and foreign-kind rows get no support | `TestUpsertImpactSupportRowsSkipsTombstonedAndForeignKindRows` | the test plants both | none | 3 rows | hermetic: pass |
| 7 | Supports built from the rows equal supports built from facts, column for column | `TestSupplyChainImpactSupportsRowsUpsertEqualsFactDerivedLive` | the same test plants a changed payload and requires a difference | admin | 60 findings | not run |
| 8 | Retraction deletes supports and stages the same keys as the tombstone statements did | `TestRetractionStatementsKeepTheStagingTailAndGainTheKeyFill`, `TestRetractionCapturesRetractedFindingIdentifiers` | tail digest pin; banned `fact_records` | none | statement text | hermetic: pass |
| 9 | The scoped retraction filters on the `cve_id` column and takes no CVE set for a full pass | `TestScopedRetractionFiltersOnTheCVEIDColumn` | the test itself | none | statement text | hermetic: pass |
| 10 | No per-row fencing token on rows or in the retraction; the admission alone orders passes | `TestSupplyChainImpactWriterRetractionHasNoPerRowFencingToken`, `TestSupplyChainImpactWriterTokenReachesOnlyTheAdmission` | the tests assert the absence | none | 1 pass | hermetic: pass |
| 11 | A stale pass is rejected whole and writes and retracts nothing | `TestSupplyChainImpactWriterRejectsSupersededPassBeforeUpsert` | the test itself (zero supports upsert, zero retraction, rollback) | none | 1 pass | hermetic: pass; live stale-pass proof (`supply_chain_impact_fencing_live_test.go`) not rewritten for supports |
| 12 | The reject trigger raises SQLSTATE 55000 with constraint `fact_records_reject_supply_chain_impact_finding` and table `fact_records` | `TestFactWriteStopTriggerRaisesTheClassifiedError` (text), `TestSupplyChainImpactSupportsFactWriteStopTriggerLive` (real PgError) | `TestFactWriteStopTriggerContractCatchesPlantedViolations` (11 plants) | text: none; live: the clone's owner role | live: 1 insert, upsert, update, delete | text: pass; live: not run; the trigger fires for every role, so a least-privilege role run is NOT_CHECKED (no shared role fixture in postgresproof) |
| 13 | The impact transaction classifies the real error as `supply_chain_impact_fact_write_stopped` | the same live test (through `SupplyChainImpactBeginner`) | `TestSupplyChainImpactFactWriteStoppedClassification` (C0) | owner | 1 | not run |
| 14 | A fresh bootstrap is armed (`tgenabled = 'O'`) | the same live test | scoped and whole-test helpers re-arm and fail if not enabled | owner | 1 | not run |
| 15 | No unlisted proof disarms the trigger | `TestFindingWriteStopCallersAreAllowlisted` | `TestFindingWriteStopCallerScanCatchesPlantedCallers` | none | the Go tree | hermetic: pass |
| 16 | No non-test Go file names the finding kind and writes `fact_records` | `TestNoGoFileWritesTheFindingKindToFactRecords` | `TestFindingWriteCensusCatchesPlantedViolations` | none | the Go tree | hermetic: pass |
| 17 | The writer defers, non-counting, while the trigger reads disabled; an unreadable trigger is an error | `TestFanOutGateDefersWhileTheTriggerIsNotArmed`, `TestFactWriteStopWatch*` | the gate's RED/GREEN cases | the reducer's read of `pg_trigger` under the least-privilege reducer role: NOT_CHECKED | 1 | hermetic: pass |
| 18 | The armed gauge is 1/0 with no labels | `TestRecordSupplyChainImpactFactWriteStopArmed` | the 0 reading | none | 1 | hermetic: pass |
| 19 | Every audit statement reads supports by declared swaps and none names `fact_records` | `TestWinnersAuditReadsSupports`, `TestWinnersAuditSupportsInputsDeriveFromTheLegacyStatements` | `TestWinnersAuditSwapsPanicOnADriftedStatement` | none | statement text | hermetic: pass |
| 20 | The audit range reads the supports keyed probe index and never scans supports sequentially | `TestSupplyChainImpactWinnersResweepAuditRangeCostAtScaleLive` plan guard | plan guard itself | admin | smoke (set by `ESHU_WINNERS_AUDIT_RANGE_SCALE`); QA-ratio fixture owed | not run |
| 21 | Every `ReadRoute` label has a declared sweep request that routes to it | `TestRouteCoverageTableRoutesToItsDeclaredLabels`, `TestAllReadRoutesListsEveryConstant` | `TestRouteCoverageCatchesPlantedViolations` | none | 7 labels | hermetic: pass |
| 22 | The live sweep reaches every label and `legacy` never rises | `TestSweepRouteCoverageAgainstAFakeCore`, `TestRouteRiseComparator`; the gate run itself | planted scrape and fake-core cases | the gate's API key and a granted key | the gate corpus | comparator: pass; live gate run not run |
| 23 | The impact work budget never loosens | `TestImpactWorkBudgetsNeverLoosen` | `TestImpactWorkBudgetRatchetCatchesALoosenedRow` | none | the two shipped tables | hermetic: pass; lowering to 2,403 needs a live run |
| 24 | The gate seeds the winners corpus through the supports step | `TestWinnersFindingVersionedRowsShape` | rows carry kind and anchor | none | 10 seeded findings | hermetic: pass |
| 25 | The trigger migration is numbered 355, applies in one order and is pinned | `TestMigrationChecksumManifestPinsEveryShippedMigration`, `TestBootstrapDefinitionsMatchesPreRefactorGolden` | the manifest's tamper and delete tests | none | the migration set | hermetic: pass |

## Owed live proof (run in a coordinator slot; none has run)

Run first: `TestSupplyChainImpactSupportsFactWriteStopTriggerLive`; the two protected-publish tests (grant
fix of C1); `TestSupplyChainImpactWinnersResweep*` (keyed harness and the audit family, including the range
scale plan guard on the supports keyed probe index); then the supports differentials (stored read, drain,
reader). Debt to expect: about 30 live proofs written for the fact-writing writer assert fact rows after a
writer pass (for example the replace-set, fencing, scoped-write, scope-pass-state and fan-out writer
proofs) and must read the supports table instead; the first run names them. Then T1 (audit and winners
over supports against the fact baseline), T3 (fact insert throughput with and without the trigger), T4
(retraction DELETE and recovery at 20,000 scope rows), T5 (real-writer WAL, B2 against the C2 head), the
latency gate run at the C2 head with the route coverage (`-metrics-url`, a granted key) to confirm the
impact work rows (expect 2,403 blocks), and W2.

## C2 m: what it will delete

Not done here; it waits for the default flip and the D2 readings.

- The legacy read constants in `internal/query/supply/chain/impact`: `aggregates_queries.go:29,104`,
  `findings_split_queries.go:21,84,167-169` and `ListFindingsProtectedQuery` (`:180-182`),
  `explain_postgres.go:334,410,444`, the legacy statements of `findings_queries.go`, and
  `advisory/evidence_sql.go:53,80`.
- The `legacy` route: `read_route.go:15-17,53,92`, its cases at `aggregates.go:266,368`, `findings.go:264`,
  `grouped_inventory_postgres.go:150`, `explain_supports.go:238-240` (and the gate flag's false branch in
  `routeFor`); the `ReadFromWinners` fields at `findings.go:104,128,134`, `aggregates.go:142,169,176`,
  `grouped_inventory_postgres.go:71,90`, `advisory/evidence_store.go:53,72` and
  `evidence_supports.go:77`.
- The gate flag: `WinnersReadEnv` (`findings.go:24`, `compat_supply_chain.go:461`), the registry entry
  (`envregistry/entries.go:123`), its readers (`cmd/core-api/main.go:322`, `query/serve_router.go:391`), the
  startup failure for a set-but-false value, and the docs (HTTP reference, env registry).
- The MCP markers `mcp/route_serves_data_registry.go:96-97` and `route_serves_data_registry_routes.go:176-208`.
- The latency gate's legacy leg: the first leg of `scripts/verify-read-api-latency-gate.sh`, `-winners-read`
  and `WinnersRead` (`latency_report.go`), `doc.go:47`, the fact-leg work table.
- Kept: the backfill, drain and verify (the upgrade path from frozen facts), the stored-read twin join and the
  recovery transition leg, the carrier envelope and decode filter, and the fact indexes until PR-D.

## Performance and observability

Performance Evidence: not measured in this change. The statements change (`DELETE ... RETURNING` for the
tombstone `UPDATE`, an `unnest` upsert for the fact-derived upsert, the audit over supports); the theory
proofs T1, T3, T4 and T5 above are owed before C2 opens, and the work budget stays at its committed ceilings
(never loosened; the impact rows take the winners value once a live run confirms it).

Observability Evidence: new gauge `eshu_dp_supply_chain_impact_fact_write_stop_armed` (the trigger reads
enabled), the reader label `fact_write_stop` on `eshu_dp_supply_chain_impact_supports_reads_total`
(`not_ready` while the trigger is off), the existing `eshu_dp_supply_chain_impact_fact_write_rejected_total`
(a pod on an older image), `eshu_dp_supply_chain_impact_supports_rows_total{action="deleted"}` now counts the
supports the retraction statement deleted, and `eshu_dp_supply_chain_impact_read_route_total{route}` is the
label the route coverage reads. A 0 on the armed gauge after the roll means the trigger was disabled or never
applied; a rising rejected counter means an old reducer image is still running.
