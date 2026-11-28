# storage/postgres/facts/keys

Explicit deletes for the four per-fact key sidecars that the ingestion key
writers fill next to `fact_records`:

| Table | Owning fact kinds | Writer |
| --- | --- | --- |
| `relationship_reference_candidate_keys` | `content`, `file`, `gcp_cloud_relationship` | `refreshRelationshipReferenceCandidateKeys` |
| `package_manifest_consumption_keys` | git `content_entity` dependency Variables | `refreshPackageManifestConsumptionKeys` |
| `package_registry_identity_keys` | `package_registry.package` | `refreshPackageRegistryIdentityKeys` |
| `security_alert_reconciliation_keys` | `security_alert.repository_alert`, `reducer_package_consumption_correlation`, `reducer_supply_chain_impact_finding` | `refreshSecurityAlertReconciliationKeys` |

## Why this package exists

Each table used to reference `fact_records(fact_id) ON DELETE CASCADE`
(migrations 188 to 190 dropped those keys). A retention
prune of a 1.5M-fact generation fired 1.5M trigger calls per table: 7.1 s,
5.7 s and 5.1 s of a 28.1 s transaction. With the cascade replaced by these
explicit deletes (set-based per generation) the same transaction took 2.4 s
(#7420). This package holds
the deletes. Migrations 188 to 190 dropped the foreign keys, so these deletes
are now the only thing that removes key rows with their facts.

## Contract

- `Deletes()` returns the tables in lock order: relationship, manifest,
  registry, security alert. The package key scope rebuild clears manifest then
  registry keys; the reversed order deadlocks against it.
- `DeleteGenerations` deletes whole generations by `(scope_id,
  generation_id)`, one statement per generation and table, and writes each
  table's deleted rows into the caller's `RowsPruned` map.
- `DeletePage` deletes one chunk page's rows by `(scope_id, generation_id)`
  plus `fact_id = ANY(page)`, and returns the deleted rows by table.
- Every statement starts with `DeleteMarker`.
- `UnavailablePartitionIndexesSQL` names each partition index that is
  missing, invalid or not `(scope_id, generation_id, fact_id)`; retention
  appends it to its key index check and refuses the cycle on any name.

- `OrphanCensusSQL` is the operator census: one row per (table, scope,
  generation) whose key rows have no fact with the same `(fact_id,
  scope_id, generation_id)`. It reads zero on a healthy database. It is
  read-only, but it scans `fact_records` once per table (1.5 s on a 3M-fact
  fixture), so run it off-peak or on a replica at production scale. Readers
  never see an orphan. `OrphanCleanupSQL` removes the orphans of one
  reported `(scope_id, generation_id)` with the same triple anti-join; never
  use `Deletes`, which would also remove the generation's live keys, or a
  `fact_id`-only anti-join, which misses keys that point at a fact of
  another generation. The 3 AM runbook quotes both statements verbatim
  (`TestOperatorRunbookQuotesTheShippedSQL`).

Callers run the deletes in their own transaction, before the fact delete.

## Performance and evidence

Plans stay on each table's `(scope_id, generation_id, fact_id)` partition
index (migrations 058, 182, 183) at 5% and 20% shares, under stale
statistics, and under forced generic plans. See
(#7420),
(#7420) and the live
proofs in the parent package (`generation_retention_fact_keys_*_test.go`).
