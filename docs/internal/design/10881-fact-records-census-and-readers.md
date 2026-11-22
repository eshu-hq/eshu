# fact_records HASH(scope_id): fact_id census and cross-scope readers (#10881)

Refs #10881. Part of #10860. Companion to
[the design record](10881-fact-records-hash-partition-proof.md) and
[the measurement record](10881-fact-records-hash-partition-measurements.md).
This file answers two acceptance criteria that need no table copy: how the id
rule and the save change (A1), and which work reads across scopes and how it
runs per part (A4). Every count below comes from a command in
`scripts/dev/10881-partition/`; rerun it at the commit you cite.

## What the id rule changes

Today `fact_id` is the primary key, and every producer decides whether a scope
appears in the hash that makes the id. `facts.StableID(kind, identity)`
(`go/internal/facts/stableid.go`) hashes only the identity map it is given, so
nothing in the writer proves an id is scope-qualified. Ingestion checks
`envelope.ScopeID == scope` (`facts_streaming.go`) and never looks at the id.

Because the id is the primary key, the planned "dedup census"
(`GROUP BY fact_id HAVING count(DISTINCT scope_id) > 1`) returns zero by
construction and proves nothing. What can be shown is what the production
upsert does when two scopes present one id, and what changes under
`PRIMARY KEY (scope_id, fact_id)`.
`scripts/dev/10881-partition/fact_id_collision_proof.sql` is self-checking and
passes on PostgreSQL 18.6:

| Step | Result |
| --- | --- |
| Undivided; scope A writes `F1`, then scope B writes `F1` with the shipped upsert (SET list omits `scope_id` and `generation_id`) | One row: scope A, generation A, scope B's payload. Scope B has no row. No error, no log. |
| Divided; the old target `ON CONFLICT (fact_id)` | `42P10`: no unique or exclusion constraint matching the ON CONFLICT specification |
| Divided; the new target `ON CONFLICT (scope_id, fact_id)` | Two rows, nothing lost |
| Undivided; the new target, no `(scope_id, fact_id)` unique index | `42P10` |
| Divided; `DELETE ... WHERE fact_id = ANY(...)` | Removes the row in both scopes |

So a cross-scope id collision is silent loss today, and after the change it
becomes two rows. It changes outcomes only if a collision already exists in
the data, which the old primary key would have merged and so cannot be counted
from the table. The bound on that risk is the producer census below, plus a
real-stream replay into the divided shape (`count(*) - count(DISTINCT
fact_id)` against the undivided row count for the same stream) before any
migration PR. The replay is NOT_CHECKED here: the samples are whole scopes, and
cross-scope collisions need two scopes presenting one id.

## The five writer sites

Derived by `bash scripts/dev/10881-partition/census_code.sh` (first section):
five production statements `INSERT INTO fact_records`, each with its own
`ON CONFLICT (fact_id)` that must become `ON CONFLICT (scope_id, fact_id)`:

| Site | Fence guard |
| --- | --- |
| `go/internal/storage/postgres/facts_streaming.go` (collector commit) | `fencing_token <=` plus `IS DISTINCT FROM` |
| `go/internal/reducer/factwrite/single_insert.go` (intent-keyed row) | none |
| `go/internal/reducer/factwrite/batch_insert.go` | `fencing_token <=` |
| `go/internal/reducer/factwrite/batch_insert_versioned.go` | `fencing_token <=` |
| `go/internal/reducer/eshusearch/eshu_search_document_writer_queries.go` | none |

The earlier record said "seven". The other two `ON CONFLICT (fact_id)` lines
(`relationship_reference_keys.go`, migration 058) upsert
`relationship_reference_candidate_keys` against its own key and do not move.
Fixture and bench writers (`testfixtures/*`, `cmd/read-api-latency-gate`
seeders) move with the same PR but are not production.

## Per-producer id derivation (who feeds the five sites)

Safe means scope and generation are both inside the hashed identity.

| Producer family | Derivation | Verdict |
| --- | --- | --- |
| Git collector, tfstate, AWS, GCP, Kubernetes, CI/CD, access posture, service catalog, codeowners, submodule, runtime declaration, document export/media/OCR, SDK adapter | `(fact kind, stable key, scope id, generation id)` hashed; reproduce with `rg 'StableID|FactID' go/internal/collector go/cmd/fact-envelope-adapter` | Safe |
| Vulnerability suppression store | `sha256(generation, stable key)`; scope is the singleton `operator:vulnerability_suppressions` | Safe only because the scope is a singleton |
| Reducer batch and versioned writers | hash `scope_id` and `generation_id` (supply chain adds the fencing token for protected rows) | Safe |
| Reducer intent-keyed rows (workload identity, cloud asset, platform, kubernetes correlation, obscoverage, secrets) | fact id is `reducerWorkItemID(intent)`: `"reducer_" + join(scope, generation, domain, entity)` with `:` and `/` rewritten to `_` | Safe in practice; not injective (`grp/sub_x` and `grp_sub/x` collide), so a cross-scope collision also needs an equal generation id (a content hash) |
| Eshu search document writer | `StableID(kind, {scope, generation, document_id})` | Safe |
| Semantic documentation and code emitters (`semanticdocs/emitter.go:118`, `semanticcode/emitter.go:148`) | `FactID: stableKey` from `facts/semantic/semantic.go`; the hash has no scope and no generation | **Risk, dormant**: no importer outside the packages writes a row today |

The semantic risk has two faces. Across scopes, two sources that mirror one
document hash to one id. Inside one scope, the same observation in a new
generation hashes to the same id, so the upsert rewrites the payload but keeps
the old `generation_id` (the SET list omits it) and the new generation never
sees the row. Fix both emitters (add scope and generation to the identity) at
wire-up, before the primary key changes. A static guard that every
`facts.Envelope{FactID: ...}` producer hashes both keys, seeded red by those two
emitters, is part of the migration PR, not this record.

## Cross-scope readers (A4)

Method: `census_code.sh` second to fifth sections, plus the replica's
`pg_stat_statements` for the measured side. The Go regexes are a worklist, not
a proof: SQL built by concatenation or scope bound inside a CTE can move a row
between groups. Three kinds of work matter, and a fourth is not a reader at all.

**1. Scope-bounded reads** (`scope_id = $N` in the predicate). They prune to one
part. This is the bulk of fact traffic: the replica's top fact statement by
total time is a `(scope_id, generation_id, fact_kind, is_tombstone)` scan.
Measured in the measurement record (plans L1, L2).

**2. fact_id-only lookups, joins and deletes.** With `PRIMARY KEY (scope_id,
fact_id)` there is no index that serves `fact_id` alone, so each of these either
scans every part or must carry the scope. The sites that hit `fact_records`
(the key-sidecar deletes in `alert_reconciliation_keys.go`,
`package_manifest_consumption_keys.go` and `relationship_reference_keys.go`
match the regex but hit their own tables and are excluded):

| Site | Shape | Scope at the call? |
| --- | --- | --- |
| `storage/postgres/unchanged_guard.go` (`selectUnchangedFactIDs`) | `JOIN fact_records f ON f.fact_id = v.fact_id` on every ingestion batch | Yes: the batch is one scope; add `f.scope_id = $x` |
| `storage/postgres/generation_retention_chunk_predrain_batch.go` | `DELETE ... WHERE fact_id = ANY($1)`; the comment says it deletes only the chunk's own rows but the SQL has no scope | Yes: the page query already binds scope and generation |
| `reducer/containerimage/container_image_identity_writer_atomic.go` (three statements) | `fact_id = ANY($n)` prelock and deletes | Same statement family binds scope and generation elsewhere; confirm per statement |
| `storage/postgres/facts/impact/branches.go`, `query/supply/chain/impact/{explain_postgres,finding_advisory_vector_sql,readiness_*}.go` | payload page by `fact_id = ANY` or join from winner rows | Winner rows carry `scope_id`; add it to the join |
| `query/cloud_resource_list_store.go`, `query/kubernetes/runtime_workload_store.go`, `storage/postgres/supply_chain_impact_advisory_group_winners.go` | `fact.fact_id = winning_row->>'source_fact_id'` | Read-model rows hold the scope; verify per site |
| `storage/postgres/backfill/deferred_facts*.go`, `relationship_reference_keys_backfill.go` | `ref.fact_id = fact.fact_id` against the keys table | Backfill, not hot; the keys table carries scope |

Disposition: add `scope_id` to each. If a site cannot, it needs a per-part
secondary index on `fact_id` (32 more indexes) or it is a full scan of every
part. The unchanged guard is the one on the hot write path; its plan is L3 in
the measurement record.

**3. Cross-scope by design** (predicate on kind, payload or generation only).
These fan out over every part: advisory and supply-chain fan-out
(`supply_chain_impact_fanout*.go`, `scoped_packages.go`, `out_of_stamp.go`),
the vulnerability-intel scan keyed by `fact_id > $2 ORDER BY fact_id`
(`installed_advisory_targets.go`), the repository catalog
(`ingestion_catalog_cache.go`, `fact_kind='repository'`), per-kind active
readers (`facts_active_*.go`, `cloud_resource_liveness.go`,
`tfstate/delta_overlay.go`, `code/reachability/loader.go`) and the documentation
and semantic list reads in `query/`. Two things change: each runs once per
part (the plans show the Append), and a keyset page `ORDER BY fact_id` cannot
use a merge across parts because no per-part index is ordered by `fact_id`
alone. Plan L5 measures that.

**4. Retention keyed by generation only.** `generation_retention_sql.go` and
`infra/inventory/retention.go` delete or count `WHERE generation_id = ANY($1)`,
and facts also leave through the FK `ON DELETE CASCADE` on `generation_id`. A
generation-only predicate does not prune. This is the path the issue exists to
make cheaper, so it is measured as removal arms in the measurement record.

**Not a reader: reducer ownership of parts.** The issue's "a reducer deployment
owns a set of parts" does not follow from dividing the fact table. Claims come
from `fact_work_items`, which stays whole (Decision 6), so any reducer can claim
any scope. A scope-bounded reducer load prunes to one part whichever reducer
holds it. A cross-scope reducer (supply-chain fan-out, catalog) is a single
global job that walks every part. Giving a deployment a fixed set of parts would
need the queue to carry a part key, which is a separate issue with its own
fairness proof.
