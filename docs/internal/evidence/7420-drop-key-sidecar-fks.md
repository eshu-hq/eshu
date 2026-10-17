# Key Sidecar Foreign Key Drop Evidence (#7420 PR2)

PR1 (`7420-key-sidecar-explicit-deletes.md`) made retention delete the three
key sidecars explicitly before their facts: `relationship_reference_candidate_keys`
(rrck), `package_manifest_consumption_keys` (pmck) and
`package_registry_identity_keys` (prik). This PR drops their per-fact
foreign keys `fact_id REFERENCES fact_records(fact_id) ON DELETE CASCADE` in
migrations 188, 189 and 190. Each deleted fact then stops firing three
referential-integrity triggers.

## Root cause

Root-Cause Evidence: the per-fact trigger invocation is the cost. PR1
measured 1,500,000 calls per child on a 1.5M-fact generation (rrck 7.05 s,
pmck 5.71 s, prik 5.06 s of a 28.1 s transaction). It also showed that
pre-deleting the children with the keys in place saves only about 4%,
because each trigger still runs a primary-key probe. On this PR's fixture
the cascade-only statements took 594.8 / 455.6 / 399.5 ms for 150,000 calls
each, with zero rows deleted (PR1 had already removed them).

## Why each migration is a DO block, not one ALTER

The arbiter ruled one `ALTER TABLE <child> DROP CONSTRAINT` per file. That
statement deadlocks ingestion. `DROP CONSTRAINT` takes ACCESS EXCLUSIVE on
the child, then waits for ACCESS EXCLUSIVE on `fact_records` while it holds
the child. Ingestion holds `fact_records` (ROW EXCLUSIVE, the fact insert)
and then asks for the child (the key insert). PostgreSQL 18.6 reported
`deadlock detected (SQLSTATE 40P01)`. The migrator retries only 55P03, so a
deadlock lost by the migration fails `db-migrate`. A deadlock lost by
ingestion fails the write. The repro is
`7420-drop-key-sidecar-fks-deadlock.sql`.
`TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive` was RED with the
plain form for all three tables:

```text
drop migration beside the ingestion-order writer: ERROR: deadlock detected (SQLSTATE 40P01)
```

A plain `DROP CONSTRAINT IF EXISTS` also takes ACCESS EXCLUSIVE on the child
at every local supervisor start, even when the constraint is already gone.

The coordinator approved the shipped form. Each file is one `DO` block:

1. Find the foreign key in `pg_constraint` (`contype = 'f'`, `conrelid` the
   child, `confrelid` `fact_records`). If it finds none, it does nothing and
   takes no table lock.
2. `LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE`. This step waits while
   it holds no lock on either table, bounded by the runner's 5 s
   `lock_timeout`.
3. `LOCK TABLE <child> IN ACCESS EXCLUSIVE MODE NOWAIT`. It never waits, so it
   cannot close a cycle. A busy child fails at once with 55P03.
4. `EXECUTE format('ALTER TABLE <child> DROP CONSTRAINT IF EXISTS %I', conname)`.
   The locks are already held.

Generalized rule: wait only for the busiest table and only while holding
nothing. Take every other table in the same statement sequence with
`NOWAIT`, and let the runner's 55P03 retry absorb contention.
`migration_lock_shape.py check` classes `LOCK` as table-locking DDL. The
files hold no DML, so each exits 0. A seeded `LOCK` plus `DELETE` exits 1.

## Live proof

| Test | What it proves |
| --- | --- |
| `TestBootstrapDropsEveryFactRecordsForeignKeyLive` | After bootstrap no foreign key references `fact_records`. RED before 188-190 listed the three `*_fact_id_fkey`. |
| `TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive` | Fact-then-key writer: the drop waits, the writer commits, the key is gone. Key-first holder: 55P03 (`coordination.IsLockNotAvailable`) in under 2 s, the holder commits, a retry drops the key. A no-op re-run locks no table. Removing `NOWAIT` makes the key-first case wait 5.04 s (RED). |
| `TestFactKeyForeignKeyDropFailsLoudlyWhenKeyTableStaysBusyLive` | With the child held for the whole budget, the tracked migrator logs `bootstrap.postgres.migration.lock_wait` and fails with `apply package_registry_identity_keys_drop_fact_fk: lock retry budget 1.5s for this bootstrap run exhausted after 2 attempts on .../185_...sql (495ms left): ERROR: could not obtain lock on relation "package_registry_identity_keys" (SQLSTATE 55P03)`. It records nothing in the ledger and keeps the key. |
| `TestFactKeyForeignKeyReverseProcedureLive` | Run on one session as psql would: the script's `SET lock_timeout = '5s'` makes step 2 give up with 55P03 after about 5 s beside an open fact writer, instead of queueing writers without bound. The procedure refuses (census row, VALIDATE 23503) with an orphan. After cleanup it restores three validated cascading keys, and a rerun is a no-op. A later bootstrap keeps them (188-190 are in the ledger) until step 4 deletes those ledger rows; the next bootstrap drops them. |
| `TestFactKeyOrphanCensusCountsPlantedOrphansLive` | The census reads no rows on a clean schema and counts each planted orphan by table, scope and generation, including a key whose fact lives in another generation. A fact_id-only mutation missed that row (RED). `OrphanCleanupSQL` clears exactly the reported rows and keeps the live keys. |
| `TestFactRecordTruncateSitesAreKeySidecarSafe` | Every `TRUNCATE` of `fact_records` (or a parent `CASCADE`) in Go, tests included, is classified and names its key tables. Seeded RED: restoring the bare `TRUNCATE fact_records` at the write-tax site fails it; planted unclassified, key-missing, cascade and stale cases fail in `TestFactRecordTruncateGuardCatchesPlantedViolations`. |
| `TestOperatorRunbookQuotesTheShippedSQL` | The 3 AM runbook quotes `OrphanCensusSQL` and `OrphanCleanupSQL` byte for byte (a one-clause edit of the runbook fails it). |
| PR1 retention, accuracy and concurrency live tests | They now run on the bootstrapped schema with the keys really dropped (`requireFactKeyForeignKeysDropped`). The orphan check is the shipped census. |

## Re-measurement on the PR2 head

Fixture: `7420-key-sidecar-fixture.sql`, 150,000-fact `gen-target` plus
`gen-2..20-live` (3,000,000 facts, 3,000,000 rrck, 599,231 pmck, 150,000 prik
rows, 5% share), PostgreSQL 18.6, `jit = off`. Script:
`7420-drop-key-sidecar-fks-sites.sql`, the shipped statements, each arm rolled
back. The host was shared (load average near 30 on 16 cores), so buffers are
the primary figure. Warm second run of each state:

| Site | Foreign keys | Time | Shared hits | Shared reads | WAL bytes |
| --- | --- | --- | --- | --- | --- |
| A, whole generation | present (PR1 head) | 2.635 s | 2,652,368 | 94,665 | 18,215,766 |
| A, whole generation | dropped by 188-190 | 0.358 s | 516,151 | 93,501 | 18,215,766 |
| B, two chunk pages | present (PR1 head) | 3.585 s (2,367 + 1,162 ms) | 2,927,637 | 94,365 | 18,215,712 |
| B, two chunk pages | dropped by 188-190 | 1.329 s (873 + 398 ms) | 791,501 | 93,213 | 18,215,712 |

Site A drops by 86% (2.635 s to 0.358 s) and site B by 63% (3.585 s to
1.329 s). Shared hits fall by 5.1x and 3.7x, and WAL is unchanged. These
match PR1's projections (A2 0.35 s, B2 1.35 s). The first run after the load
took 19.6 s and 80 MB of WAL with the keys present (hint bits and full-page
images). It is not comparable, and it is excluded.

Migration cost on this fixture: 17.6, 4.2 and 3.4 ms. A re-run took 1.5 to
1.8 ms each with no table lock. Reverse procedure on the same fixture:
census 4.78 s, `ADD ... NOT VALID` 33.9 / 3.5 / 1.2 ms, `VALIDATE` 1.14 /
0.98 / 0.67 s (one anti-join scan per table), and the re-drop 7.3 / 4.0 /
8.1 ms.

Census (`factkeys.OrphanCensusSQL`) on the same fixture with zero orphans:
1.48 s. It is one parallel hash anti-join per table over a sequential scan of
`fact_records`. The `fact_id`-only form took 1.24 s, but it misses keys that
point at another generation.

Performance Evidence: the site A and site B table above, the PR1 note's
1.5M-fact coordinator fixture (28.133 s to 2.428 s warm), and the migration,
reverse and census timings. No query plan changes: the explicit deletes keep
the PR1 partition-index plans
(`TestGenerationRetentionFactKeyPlansStayOnPartitionIndexLive` passes on the
dropped schema).

## Accuracy after the drop

The arbiter's numbered accuracy conditions, with the test that holds each on
the real dropped schema:

1. Orphans are read-invisible. `readiness_package_consumption_query.go` and
   `readiness_target_ownership.go` join each key to its live fact by
   `(fact_id, scope_id, generation_id)`; `backfill/deferred_facts.go` reaches
   keys only through a live fact. Tests:
   `TestReadinessIgnoresOrphanPackageKeysLive`,
   `TestOrphanRelationshipKeysAreInvisibleToDeferredLoadLive`.
2. No shadowing on a `fact_id` re-mint, and no key write into a pruned
   generation. Writers delete by `fact_id`, then insert; `fact_records` keeps
   its foreign key to `scope_generations`. Tests:
   `TestKeyWritersReplaceOrphanFactKeysLive`,
   `TestOrphanFactKeysCannotBeWrittenForAPrunedGenerationLive`.
3. Every `DELETE FROM fact_records` site is paired or kind-disjoint. The
   guard classifies nine statements: retention sites A and B paired with the
   explicit key deletes, and seven reducer retires of `reducer_*` kinds
   (three kind literals, four kind parameters). Test:
   `TestFactRecordDeleteSitesAreKeySidecarSafe` with the seeded RED/GREEN
   pair `TestFactRecordDeleteGuardCatchesPlantedViolations`.
4. Census. The three tables' `RowsPruned` flow to
   `eshu_dp_generation_retention_rows_pruned_total`, the chunk path reports
   page counts, the live tests assert zero orphans with the shipped
   `factkeys.OrphanCensusSQL`, and the operator census and cleanup are quoted
   in the 3 AM runbook. Tests: the PR1 retention live tests,
   `TestFactKeyOrphanCensusCountsPlantedOrphansLive`,
   `TestOperatorRunbookQuotesTheShippedSQL`.
5. Same edit. The write-tax helper's `TRUNCATE fact_records` also truncates
   `relationship_reference_candidate_keys` (its fixture schema never had the
   key, so this keeps every round's starting state identical in both arms),
   and `TestBootstrapDropsEveryFactRecordsForeignKeyLive` asserts no foreign
   key has `confrelid = fact_records`. `schema_test.go` still passes (058 is
   unchanged). Added hardening: `TestFactRecordTruncateSitesAreKeySidecarSafe`
   fails on any new `TRUNCATE` of `fact_records` that is not classified with
   its key tables.

## Approved deviations from the arbiter ruling

- Index order: the arbiter planned the partition indexes as a separate PR.
  They shipped with the PR1 code (migrations 182 and 183) because, without
  the scope-leading index, the explicit delete flips to a sequential scan at
  a 5% share (PR1 evidence).
- Drop form: the arbiter ruled one `ALTER TABLE <child> DROP CONSTRAINT IF
  EXISTS <name>` per file. The files are the lock-ordered `DO` blocks above,
  because the single ALTER deadlocks ingestion (40P01) and the migrator
  retries only 55P03. The coordinator approved both.

Parent deletes that cascade to `fact_records` but no longer to the children
(`rg -i 'delete\s+from\s+(only\s+)?(public\.)?(scope_generations|ingestion_scopes)'`
and `TRUNCATE` over non-test Go, scripts and migrations):

- `DELETE FROM scope_generations`: only `deleteScopeGenerationsForRetentionQuery`
  (`generation_retention_sql.go`, site A). PR1 runs `factkeys.DeleteGenerations`
  for the same candidates earlier in the same transaction.
- `DELETE FROM ingestion_scopes`: no production site in Go, `scripts/` or
  migrations.
- `TRUNCATE` of `fact_records`, `scope_generations` or `ingestion_scopes`: no
  production site. In tests, `relationship_family_index_write_tax_helpers_test.go`
  now truncates `relationship_reference_candidate_keys` with `fact_records`.
  `supersede_prior_failure_cost_live_test.go` truncates the parents
  `CASCADE` in a schema where it writes no keys, so the census reads zero
  there. Both are classified in `factTruncateSites`.
- `scripts/lib/golden-corpus-vulnerability-suppression.sh` deletes
  `vulnerability.suppression` facts, a kind that owns no key rows.

## Review carry-overs (P3)

- Key and fact share (scope_id, generation_id). `DeleteGenerations` deletes
  keys by the key row's own `(scope_id, generation_id)`. That matches the
  cascade only while every key carries its fact's scope and generation. Two
  checks report a key that does not. `OrphanCensusSQL` matches keys to
  facts on `(fact_id, scope_id, generation_id)`, so such a key reads as an
  orphan; `TestFactKeyOrphanCensusCountsPlantedOrphansLive` plants one and
  reads zero for keys from the production writers. For the two package
  tables, the retention pre-count joins keys to facts by `fact_id` alone, so
  the same key makes the count and the delete differ (next item). No new
  guard test: the census test already pins both directions.
- Pre-count WARN. Retention now keeps the batch's pre-count of the three key
  tables (`FactKeyRowsCounted`) beside the whole-generation delete's rows
  (`FactKeyRowsPruned`, chunk page keys excluded). When they differ, the
  runner logs WARN `generation retention fact key delete differs from its
  pre-count`, as it does for the changed-since ledger. Tests:
  `TestGenerationRetentionWarnsWhenFactKeyDeleteDiffersFromCount` and
  `TestGenerationRetentionNoFactKeyWarnWhenCountsAgree` (runner, RED first),
  the two fact key unit tests in `generation_retention_fact_keys_test.go`
  (RED first), and the `clean-keys` / `planted-orphan-key` subtests of
  `TestGenerationRetentionNormalPruneLeavesNoOrphanFactKeysLive`: equal on
  clean keys, and counted one fewer than deleted with a planted orphan.
- Batch cost of site A at the default 100-generation batch. Performance
  Evidence: `DeleteGenerations` sends 3 x 100 = 300 statements under the
  scope lock. PostgreSQL 18.6 on loopback, 1,585,000 rrck, 317,000 pmck and
  79,200 prik rows, `ANALYZE`d, each run rolled back, median of 7:

  | Keys per generation (rrck / pmck / prik) | Rows deleted | Shipped, 300 statements | `unnest` form, 3 statements |
  | --- | --- | --- | --- |
  | 0 / 0 / 0 | 0 | 11.57 ms | 2.03 ms |
  | 250 / 50 / 12 | 31,200 | 31.51 ms | 22.80 ms |
  | 600 / 120 / 30 | 75,000 | 60.47 ms | 54.36 ms |

  The fixed cost is about 39 us per statement on loopback; the deleted rows
  dominate otherwise. This is small next to the 0.358 s whole-generation
  delete above, so the shipped form stays. Over a network the fixed cost
  grows by 300 round trips per batch (for example 150 ms more at a 0.5 ms
  round trip); a set form over `unnest($1, $2)` removes it if that matters.

## Rollout and rollback

The deployment note is in `go/internal/storage/postgres/migrations/README.md`
("Migrations 188 to 190 deployment note"). Deploy the PR1 image first. While
the drop waits for `fact_records`, readers queue for up to 5 s per attempt.
After the drop, a code-only rollback is safe only to the PR1 image. Rolling
back further requires `7420-drop-key-sidecar-fks-reverse.sql` first; it sets
`lock_timeout = '5s'` because psql sets none, and a waiting step 2 lock
request stalls every new `fact_records` writer until it is granted. Rolling
forward again needs its step 4, or the tracked migrator skips 188-190 and the
keys stay. If one release carries both PRs, run the census after the rollout:
a retention pod from the older image that runs after `db-migrate` leaves the
key rows of the generations it prunes.

## Signals

Observability Evidence: the migrator's existing `bootstrap.postgres.migration.lock_wait`
WARN and its exhausted-budget error name the migration, the busy table and
SQLSTATE 55P03 (asserted by the live test above). Retention's
`eshu_dp_generation_retention_rows_pruned_total` already reports the three
table values from the explicit deletes' affected rows, and the chunk path
reports its page key counts (PR1). The orphan census is on-demand SQL,
documented in the 3 AM runbook ("key sidecar tables keep growing").
Retention also logs WARN `generation retention fact key delete differs from
its pre-count` when the whole-generation key delete and the batch's
pre-count differ (carry-over above). No new metric. A sampled orphan gauge would scan
`fact_records` once per table per sample (1.5 s on 3M facts, linear in
table size), and an orphan is a storage leak that readers never see, not a
wrong answer.
