# #9805 keyed resweep of the supply-chain impact winners

Refs #9805. Design ruling: `RULING: BUILD NOW` (arbiter, 2026-11-02) as amended
by ruling B (same day): capture in the writer transaction for `fact_records`
(with a filter on the content digest the writer already reads), triggers for the
three low-rate tables, the existing package-lookup index, an operator-scope
union, and a full-recompute audit as the backstop.

## What changed

The winners maintainer ran a full resweep every tick. The write half was fixed
in #9774. This change fixes the read half: after one full resweep at startup,
every tick consumes a dirty log and recomputes only the keys it names.

- Migration `272_supply_chain_impact_winners_dirty_log.sql`: the log table
  (`kind` fact or scope, `source`, five identifier columns or a scope and
  generation) and the triggers below.
- Three capture owners:
  - `fact_records` (no trigger, ruling B decision 3). `fact_upsert`: the impact
    writer appends the identifiers of new or changed rows in its transaction. A
    rewrite whose stored content digest equals the new one captures nothing.
    `fact_retract`: the retract statement is a CTE (`UPDATE ... RETURNING`
    feeding an `INSERT`), so its affected-row count is the capture count.
  - `ingestion_scopes.active_generation_id`: row trigger
    `ingestion_scopes_winners_dirty_capture`, `AFTER INSERT OR UPDATE OF
    active_generation_id`, the shape migration 092 already uses on this column.
    It skips an UPDATE that leaves the pointer unchanged and writes a `scope`
    row for the generation the pointer leaves and the one it enters (a pointer
    set to NULL captures only the old one). Source `operator` for
    `operator:vulnerability_suppressions`, else `activation`. The function is
    `SECURITY DEFINER` with its `search_path` pinned by `ALTER FUNCTION ... SET
    search_path FROM CURRENT` once the session path is `pg_catalog`, then
    `current_schema()`, then `pg_temp`, so the pin names the install-time schema (public
    in every production bootstrap, the test schema in an isolated one), so no
    pointer writer needs a grant on the log. Because the trigger sees
    every writer, no writer carries capture code. The pointer writers it covers
    (rg, non-test): `projector_queue_sql.go:129` (Ack) and `:240` (Fail
    reconcile), `repository_retirements_fence_sql.go:67`,
    `vulnerability_suppression_store.go:27` (scope INSERT) and `:203`
    (activation lands at `projector_queue_sql.go:129`), `ingestion_queries.go:82`
    (upsert conflict arm), and migration 115 line 21 (seed INSERT).
  - `ingestion_scopes` delete: row trigger
    `ingestion_scopes_winners_dirty_capture_delete`, `BEFORE DELETE`, also
    `SECURITY DEFINER`. The only non-test deleter is the retirement purge
    (`repository_retirements_purge_sql.go:91`), which runs right after the fence
    in one request; its foreign keys cascade the scope's facts away before any
    tick could expand the fence's `scope` row (review finding F1, reproduced
    with the real foreign keys: one stale winner until the audit, and a false
    backstop alarm). The trigger runs before the cascade (a referential action is
    an AFTER trigger), reads the scope's non-tombstoned findings through the
    active-scan index, and appends their distinct five-column identifiers as
    `fact` rows (source `activation`, or `operator` for the suppression scope).
    It takes every generation of the scope, a superset of what the last active
    generation fed: recomputing an unaffected key changes nothing. Any deleter
    is covered, not only the purge.
  - `scanner_image_result_fact_classifications` and
    `scanner_image_result_fact_associations`: statement triggers over transition
    tables (INSERT and UPDATE for classifications, INSERT for associations, and
    DELETE for both). They live in migration 272a, which is public-only (the
    tables come from the scanner authority migrations that an isolated-schema
    bootstrap skips) and follows the scanner lane's public-qualified convention.
    The functions are `SECURITY DEFINER` with a pinned `search_path`: the
    protected publish function that fires them is itself
    `SECURITY DEFINER`, and the runtime-writer login has no privilege on the log
    through that path. A conflicting `ON CONFLICT DO NOTHING` row is absent from
    the transition table and captures nothing.
- Keyed rebuild (`ResweepWinnersKeyed`): under the existing try-lock,
  `DELETE ... RETURNING` into a temp table, scope rows expanded through the
  active-scan index, `ON COMMIT DROP` temp tables with `ANALYZE`, a `LATERAL`
  probe of `fact_records_supply_chain_impact_package_lookup_idx`, every active
  operator fact added, then the shipped select body over the keyed
  `active_facts`. Past 50,000 identifiers or half of the stored winners the same
  transaction runs the full rebuild.
- Audit (`AuditWinners`): a full recompute read with no resweep lock, then a
  keyed verification of each differing key under the lock. Verified stale keys
  that are not already pending are queued with source `audit` and counted.
- Env `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL` (default 10m, `0` off).

## Counter `capture_rows_total{source}`

A trigger cannot emit a Go metric, so each source is counted where Go holds the
row count:

| Source | Counted by | When |
| --- | --- | --- |
| `fact_upsert` | the impact writer (`CaptureWinnersDirtyFacts`) | when the writer transaction commits (#9957; counted at capture before that) |
| `fact_retract` | the impact writer (`writer_retract.go`, statement affected rows) | when the writer transaction commits (#9957; counted at capture before that) |
| `activation`, `operator`, `scanner` | the maintainer (`recordKeyedSignals`) | when the keyed tick consumes the rows, on the keyed and the full-fallback path |
| `audit` | the audit | when it queues a repair |

The log row keeps its `source` column, so the maintainer tallies the consumed
rows by source without a second query. A consumed row counts once.

## Why the digest filter is sound

The capture skips a rewrite whose content digest is unchanged. That holds only
if no payload field the winners select reads is outside the digest. Two tests
guard each side: `TestContentDigestCoversEveryPayloadFieldExceptTheExcludedOnes`
mutates every typed payload field and requires the digest to move for all but
the four excluded ones, and `TestWinnerSelectReadsNoDigestExcludedPayloadField`
reads the select SQL and fails if it reads one of the four. A same-fact-id
rewrite keeps scope, generation and tombstone state, so the winner inputs are
unchanged. A new generation mints new fact ids and is always captured.

## Concurrency

Conflict domain: the winners table and the dirty log.

- Keyed ticks, the full resweep and the audit's verification step all take
  `pg_try_advisory_xact_lock(hashtext('supply_chain_impact_winners'),
  hashtext('resweep'))`. At most one of them writes the winners table.
- Writers INSERT into the log, which conflicts with no row lock, so a writer
  never waits on a tick. An AFTER ROW trigger runs before commit, so the log row
  and the pointer change commit or roll back together; two row triggers on
  `ingestion_scopes` (092's and this one) fire in name order, share no state and
  a failure in either aborts the statement.
- The consume is `DELETE ... RETURNING` after the lock: a row committed after
  that statement's snapshot stays for the next tick, and a rollback returns every
  consumed row. `TestSupplyChainImpactWinnersResweepKeyedConsumeKeepsLateArrivalsLive`
  holds a tick blocked after its consume, commits a late change, and requires the
  change to survive. With the consume mutated to a copy plus a delete after the
  recompute (the round 2 mutant) that test fails.
- The audit never holds a lock during its full read. The audit lease domain
  `supply_chain_impact_winners_audit` is separate from the resweep lease.
- A `scope` row is expanded through `fact_records` at consume time, so a
  generation's keys are found only while its facts exist. Retention deletes
  superseded generations after `ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE`
  (default 168h), never within a 30 s tick. The retirement purge is different:
  it deletes the scope row and cascades the facts in the same request as the
  fence, so the delete trigger above makes those identifiers durable at delete
  time instead of at consume time.

## Trigger holes and their cover

| Hole | Cover |
| --- | --- |
| `DELETE` of a scope | closed: the `BEFORE DELETE` trigger expands the scope's findings before the cascade; the real-foreign-key fence-then-purge test is RED without it |
| `DELETE` of a scanner row | closed: `AFTER DELETE` triggers on both scanner tables, each with a seeded pair |
| `supersedeOrphanedActiveGenerationsQuery` (`generation_liveness_sql.go`) sets `status = 'superseded'` on an older active generation and never moves the pointer | open by design (review F4): it only acts in the anomalous state of two active generations, where the winners select reads the pointer's generation and requires `status = 'active'`; the audit repairs it. Not captured, named here |
| `session_replication_role = replica` (a `pg_restore --disable-triggers` load, a logical-replication subscriber) | not a production write path; the startup full resweep and the audit repair it. `ENABLE ALWAYS TRIGGER` would close it and is not required |
| `TRUNCATE` | no code path truncates these tables; a truncate of `ingestion_scopes` is a rebuild covered by the startup full resweep |
| `ALTER TABLE ... DISABLE TRIGGER` | only the seeded-violation tests do this |

## Proof run

Private native PostgreSQL 18.6, fsync on, `go test -race -count=1`.

- `TestSupplyChainImpactWinnersResweepTriggerCaptureViolationsAreCaughtLive`:
  twelve seeded-violation pairs, one per production statement and DELETE path.
  The RED arm runs `ALTER TABLE ... DISABLE TRIGGER` for the named trigger, makes
  the change, and requires no dirty-log row of the wanted source and, where the
  change moves a winner, a table that differs from a fresh recompute. The GREEN
  arm leaves the trigger on and requires at least one row of the source, a keyed
  tick whose result counts that source, and parity. The cases:
  Ack (`projector_queue_sql.go:129`), reconcile (`:240`, with a work item owned
  by the failing lease), retirement fence (`:67`), the collector upsert conflict
  arm with a non-NULL incoming generation, the operator suppression scope
  (source `operator`), `ingestion_scopes` DELETE, classification INSERT derived,
  classification INSERT static (captured, no winner moves), classification
  UPDATE static to retracted, classification DELETE, association INSERT and
  association DELETE.
- `TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive`: the real
  protected publish function, run by the least-privilege worker login, appends
  `scanner` rows for the finding it publishes; with the insert triggers
  disabled, the same publish appends none.
- All `^TestSupplyChainImpactWinnersResweep` live tests pass: keyed equals a
  fresh recompute across the change classes, the consume protocol, rollback, both
  caps, the probe plan (index used, no seq scan of `fact_records`), the audit
  repairing a missed capture and a missed delete and not counting a pending key,
  the dirty-log stats, and the scale harness at its smoke size.
- Unit: `TestWinnersMaintainerRecordsKeyedAndAuditSignals` (counts by source) and
  `TestWinnersMaintainerCountsTriggerSourcesOnAFullFallback`. Deleting the
  `operator` record line from `recordKeyedSignals` fails the first.
- Hermetic suite: `./internal/storage/postgres/` (DDL parity, lock order, the
  migration checksum and embed digest pins, the enrollment guard),
  `./internal/reducer/supplychain/...`, `./internal/telemetry/`, `./cmd/reducer/`,
  `./cmd/api/`, `./internal/envregistry/`: pass. The one failure,
  `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`, needs an
  eshu-deploy checkout and fails the same way at the base commit.
- Re-run safety of migration 272. The base-versus-tree `-race` triage found
  `TestContentWriterUnchangedReplayStaysHOTLive` and
  `TestCorpusFenceSnapshotRetireAndReactivateBetweenStatements` failing on the
  tree and passing at the base: both apply the bootstrap a second time, and the
  migration's plain `CREATE TRIGGER` failed with `trigger ... already exists`
  (SQLSTATE 42710). The triggers now use `CREATE OR REPLACE TRIGGER`, which
  migrations 254 and 260 already use; the checksum and embed digest pins moved
  with the file. Both tests pass after the change (see the triage table in the
  executor report).

## Isolated-schema bootstrap (review N1)

The first version of 272 hung the scanner triggers on tables that only the
public-only scanner migrations create, and its definer bodies named
`public.` tables. An isolated-schema bootstrap (the proof surface of the
`OpenIsolatedSchema` live tests) skips the public-only migrations and creates
every table in its own schema, so 13 live tests that pass at the base failed with
`relation "scanner_image_result_fact_classifications" does not exist`, and
behind that a plain scope insert failed on `public.supply_chain_impact_winners_dirty_log`.
The fix has two parts. The scanner triggers moved to migration 272a, registered in
`publicOnlyMigrationPaths`. The pointer and delete functions pin their path with
`ALTER FUNCTION ... SET search_path FROM CURRENT` after a transaction-local
`set_config` makes the session path `pg_catalog, <current_schema()>, pg_temp` (and
a second one restores the caller's path), so they bind the schema the log lives
in. pg_catalog stays first so nothing in the schema can shadow a built-in, and
pg_temp is last on purpose: their bodies name tables unqualified, and Postgres
searches the session temp schema first for relations unless pg_temp is listed.
Review R1 reproduced the hole on the pin without it: a login with the default
TEMP privilege created a temporary table named like the dirty log and the pointer
capture landed there (the real log got nothing), and a temporary `fact_records`
holding a forged finding made a scope delete write the forged identifiers into the
real log. The first version of the functions qualified every table with `public.`
and was immune; the unqualified bodies are what opened it. The scanner functions
in 272a keep qualified tables and carry the same pin. An `EXECUTE format()`
install was tried first and refused by the migration lock-shape gate (#7961),
which treats an EXECUTE of DML text beside a `CREATE TRIGGER` as bulk DML under a
table lock. In a production bootstrap the schema is `public` and
`pg_proc.proconfig` reads `search_path=pg_catalog, public, pg_temp`.
`TestSupplyChainImpactWinnersResweepCaptureIgnoresCallerTempTablesLive` runs both
probes as a role with no log privilege; it fails on the pin without pg_temp and
passes with it.
`TestSupplyChainImpactWinnersResweepCaptureTriggersBindTheirOwnSchemaLive`
bootstraps an isolated schema on a database with no public bootstrap, moves a
pointer, deletes a scope that holds a finding, and requires the log rows in that
schema and the definer posture (SECURITY DEFINER, the pinned path, no PUBLIC
execute). The role-bound test asserts the same posture for all four functions in
`public`.

## Roles and grants

The pointer and scope-delete trigger functions are `SECURITY DEFINER` with a
pinned `search_path` (`pg_catalog`, the schema the migration ran in, then `pg_temp`), and
the scanner functions in 272a are the same with public-qualified tables, so
the logins that move or delete a scope row (projector Ack and Fail, collector
upsert, retirement fence, suppression store, migration seed, purge) need no
privilege on the log or on `fact_records` for the capture. This replaces the
first design, where the pointer function ran as the caller and a missing grant
halted projection for every scope. The dirty log is an ordinary base table, so
under a role-bound deployment its privileges are a DBA pregrant outside the
migration set (the same model `9695b-holder-fanout.md` describes for
`fact_records`).
`TestSupplyChainImpactWinnersResweepScopeTriggersRunWithoutLogPrivilegesLive`
binds a role with no log privilege, shows a direct insert fails with `permission
denied`, and shows the pointer UPDATE and the scope DELETE succeed and write log
rows. Without `SECURITY DEFINER` it fails with `permission denied for table
supply_chain_impact_winners_dirty_log`.

| Role (writes) | `supply_chain_impact_winners_dirty_log` | `..._seq_seq` |
| --- | --- | --- |
| impact writer login (`fact_records` capture, retract CTE) | `INSERT` | `USAGE` |
| projector, collector, retirement fence, suppression store, migration seed, purge, scanner publish | none: the trigger functions run as their owner | none |
| winners maintainer | `SELECT`, `INSERT` (audit repairs), `DELETE` (the consume) | `USAGE` |

The keyed tick and the audit create `ON COMMIT DROP` temporary tables; the base
full resweep created none. The maintainer login therefore also needs `TEMPORARY`
on the database. `PUBLIC` holds it by default; a database that revoked it fails
the tick with `permission denied` and the watermark-age alarm fires.

Without the impact-writer grant the write fails with `permission denied` and the
reducer retries it. Migration 092's trigger already needs its own grant shape on
`container_image_identity_scope_state` for the pointer writers; this change adds
nothing to it. The grant owner is a deploy-side change that needs its own ruling;
the lead carries it. The protected-publish proof grants `INSERT` and the sequence
to the worker login and failed without them.

## Trigger cost

Arbiter measurement on a mimic schema with the 092 trigger as the baseline, PG
18.6, six interleaved rounds alternating the first mover, per-statement cost
inside one transaction (the common fsync commit excluded), host load 12 to 15,
so the figures are relative: a pointer flip costs about +36 microseconds (median
22 to 60), and a 269-shaped publish costs about +50 microseconds per finding
(median 17.5 to 21.8 against 68.2 to 93.4, with one classification and one
association insert per finding). A flip happens once per generation activation,
under 1% of a transaction that commits with fsync. `NOT_CHECKED`: the real batch
size of a publish, and any figure from this tree's own harness.

The delete trigger's cost is in the scale note (16.8 ms, 74.0 ms and 581.4 ms
for scopes of 1,000, 5,000 and 10,000 findings, load-affected).

## Operator kind: a trace, not a defect

The winners select reads `fact_kind = 'reducer_supply_chain_impact_finding'`
(`supply_chain_impact_canonical_winners_store.go:55`, operator branch `:124`;
`supply_chain_impact_winners_keyed_sql.go:131`), while the suppression store
writes `vulnerability.suppression` facts (`vulnerability_suppression_store.go:400`).
No step in the select or the store converts one to the other. The conversion is
upstream: the store enqueues the operator scope's generation for projection, the
projector builds a `supply_chain_impact` reducer intent for any generation that
holds a `vulnerability.suppression` fact (`projector/supply/chain/impact/intents.go:19,
:62, :86`), and the reducer handler writes the findings it produces with
`ScopeID: intent.ScopeID` (`reducer/supplychain/core/impact.go:303-305`), so the
reducer-kind facts, with `suppression_state` and `suppression.expires_at`
applied, land in the operator scope. That is "the operator copy" the select
reads. I did not assert this chain with a test of its own; the
`TestAdvisoryOnlySuppressionHTTPStoreReducerQueryLive` e2e (store, reducer, query)
passes and the seeded operator cases above write the operator-scope reducer-kind
fact directly. No defect is claimed.

## Scale and writer cost

The 1x / 5x / 10x before and after tables, the plan-stability proof, the
remaining floor and the writer-cost rounds are in
[9805-winners-keyed-resweep-scale.md](9805-winners-keyed-resweep-scale.md).
In short: the keyed tick fell from 1.07 s (good plan) or 2 m 38.6 s (flipped
plan) to 358.9 ms at 1x, from over 600 s (capped, a lower bound) to 6.15 s at
5x (measured at load1 69 to 92, so not a clean read), and from 231 s to 2.94 s
at 10x; shared buffers fell from 28,365,188 or 284,409 to 16,435 at 1x. No run
met the load1 < 9 rule, so the buffer and index-search counts carry the claim.

The writer pays one more statement per pass that changed a finding: the set-based
dirty-log INSERT. The committed cost budget
`testdata/cassettes/replayoffline/supply-chain-impact.cost-budget.json` moves
from 4 to 5 write observations (`TestCostBudget_SupplyChainImpact` measured 5),
and the N+1 negative control now records 10 against it. The statement count does
not grow with the finding count; an unchanged rerun issues none.

## Built-binary proof

Run on 2026-11-02, 07:37 to 07:47 EST, against the built `reducer` binary
(sha256 prefix `5728c8d9d521e909`, built from HEAD `7f6501b7e3` plus the dirty
tree) on a private Postgres 18 and a Neo4j container. Two reducer processes
(ports 59464, 59465) shared the database. A churn writer, built from the
production impact writer, rewrote one 200-finding scope every 4 seconds for 9
minutes (134 passes, 1.0% of 20,000 findings each). The audit interval was 60 s.
Load1 was 6.8 at the start and 5.3 at the end. This run proves correctness and
behavior; it carries no timing claim.

| Check | Result |
| --- | --- |
| Paths committed | `keyed` 15 (A) and 14 (B); `full` 1 per process, the startup resweep only |
| Dirty log after the drain | 0 in both processes |
| Planted missed capture (tombstone of a unique-key fact in a scope the churn never rewrites) | audit found exactly 1 stale key (`backstop_repairs_total` = 1, `capture_rows_total{source="audit"}` = 1, one WARN line), queued it, a keyed tick repaired it |
| Final table vs a fresh full recompute | 16,665 winners vs 16,665, 0 differing (the recompute dropped the tombstoned key's winner, so the repair deleted it) |
| Deadlocks in the Postgres log for the run window | 0 |
| ERROR lines in either reducer log | 0 (one WARN: the audit finding above) |

A first run (07:24 to 07:34) had the same path counts, 0 deadlocks and 0
differing, but its planted change (a priority edit) never made a winner stale, so
the audit correctly reported 0 repairs and that run did not prove the repair
path. It is not counted as the planted-miss proof. The rerun fixed the plant, and
on the first run's database the new plant was found once by `AuditWinners`, then
0 on the next audit.

Harness: `h3driver` (seed, churn, plant, audit, verify) and `run-h3.sh` live in
the operator-local scratch directory, not in the repository; the production
behavior they exercise is covered by the committed live tests above.

## NOT_CHECKED

- A quiet-host (load1 < 9) rerun of any scale; the 5x ratio especially.
- The good-plan old form at 5x and 10x, and an EXPLAIN of the old form at 10x.
- QA numbers (no AWS access by instruction), the audit read routed to a read
  replica, cap thresholds beyond the ruling's defaults, the real batch size of a
  scanner publish.
- Committed-state gates: `verify-performance-evidence.sh`, `verify-package-docs.sh`,
  `query-methodology-static`; the dirgate `Dirgate-Growth` trailer is the
  coordinator's (the postgres, core and telemetry directories were already over
  the 40-file cap).
- `make pre-push` and `make pre-pr`, by instruction.

Performance Evidence: the keyed read half at 1x, 5x and 10x (table in Scale).
Load-independent: shared buffers fall from 28,365,188 (flipped plan) or 284,409
(good plan) to 16,435 at 1x, index searches from 101,000 (or 1,000) to 101, and
the keyed tick falls from 1.07 s (good plan) or 2 m 38.6 s (flipped plan) to
0.359 s at 1x; at 10x it falls from 231 s to 2.94 s. Millisecond figures are
load-affected (load1 10 to 124, no run under the 9 bar). The 5x after-run ratio
(9.9x) ran at load1 69 to 92 and is not claimed as meeting the 10x bar; the 5x
before-run is a lower bound (canceled at the 10-minute cap). The writer adds
0.18 ms per pass at 1% changed rows (median whole-pass delta +0.1%). The
remaining floor is one fetch of the hot triple's candidates (5,000 to 10,000
rows on the QA hottest package, about 40 ms measured at 6,900 rows), which is
small against the 30 s tick interval; that is a judgment, not a measured budget
check.

No-Regression Evidence: the full resweep path is unchanged byte for byte
(`supplyChainImpactWinnerSelectSQL` is the old constant split into a prefix and
a body; the full rebuild statement is untouched).

Observability Evidence: `eshu_dp_supply_chain_impact_winners_resweep_duration_seconds{path}`,
`..._keys_per_sweep`, `..._identifiers_consumed_total`,
`..._capture_rows_total{source}` (counted as the table above says),
`..._backstop_repairs_total`, `..._dirty_log_depth`,
`..._dirty_log_oldest_age_seconds`, the span attributes `path` and `keys`, the
warn log `supply chain impact winners audit found stale keys`, and the `path`,
`identifiers` and `keys` fields on the committed log. See
`docs/public/reference/telemetry/supply-chain-impact-winners.md`. At 3 AM an
operator reads `dirty_log_oldest_age_seconds` for lag, `capture_rows_total` by
source for a capture path that went flat, and `backstop_repairs_total` for a
capture hole.
