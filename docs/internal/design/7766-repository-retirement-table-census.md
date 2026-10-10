# Repository Retirement: Table Census (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md),
[Runner](7766-repository-retirement-runner.md) (2d, 2e, phase 3 act on this
list), and [Proof And Rollout](7766-repository-retirement-proof-and-rollout.md).

Binding inputs: the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)), F6,
and the arbiter ruling, round 4 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6085855231)), section 4. The
design's earlier phase 3 text said the scope delete "cascades
`projector_scope_claim_fences`". That was incomplete: about 30 tables reference
`ingestion_scopes`, and more are keyed by scope or repo with no FK. This file
classifies each of them.
Source check: origin/main c88c3806a, 2026-10-09, read from
`go/internal/storage/postgres/migrations/*.sql`.

## Dispositions

| Code | Meaning | Who removes it |
| --- | --- | --- |
| `2b` | Shared intents, deleted before the graph retraction | 2b, chunked 10,000 rows |
| `2d` | Cascades from a generation or fact delete | 2d, the ADR 2248 cascade (`generation_retention.go:437-471`) |
| `2e` | Scope- or repo-keyed, no FK, retention does not reach it | 2e, chunked 10,000 by ctid, after 2c |
| `3` | Cascades from the scope row | Phase 3 `DELETE FROM ingestion_scopes` |
| `3+count` | Cascades at phase 3 and the tombstone reports the count | Phase 3, pre-counted inside the transaction |
| `reaped` | An existing reaper removes it once the scope row is gone | Named reaper |
| `kept` | Stays, with a reason | Nobody |
| `not_a_key` | The column name matches the pattern but holds no scope or repo identity (`scope_kind`, `scope_class`, and the like) | Nobody |
| `foreign_target` | The retired id is only the target of the row: `target_repo_id` without `source_repo_id`, mirroring incoming graph edges | Nobody; kept until the owner re-resolves |
| `NOT_CHECKED` | Key semantics not read for this design | The census test blocks PR 7 until classified |

The census is not a hand list. `TestRetirementTableCensusClassifiesEveryScopeKeyedTable`
runs against a migrated schema and queries `information_schema` (and
`pg_constraint` for the delete rule): every foreign key whose referenced table is
`ingestion_scopes`, `scope_generations`, `fact_records`, `fact_work_items`, or
`content_files`, plus every table with a matching column and no FK. The match is broad on
purpose: any column whose name contains `repo`, `repository`, or `scope`, plus
`partition_key`, across every table. A narrow name list missed
`source_repo_id`, `allowed_scope_ids`, `allowed_repository_ids`, `repository`,
`repository_full_name`, `repository_external_id`, and the `*_hash` columns. Each
hit must appear in the closed classification table in
`reducer/retirement/census.go` with a disposition and, for `kept`, `not_a_key`,
and `foreign_target`, a reason. The test needs a seeded-violation pair: a
scratch table with a single `source_repo_id` column and no entry in the closed
table fails it, and the clean tree passes. The counts below are a snapshot, not a
contract.

## Tables That Reference `ingestion_scopes`

The 33 `REFERENCES ingestion_scopes` lines in the migrations name 32 tables
(migration 039a builds `eshu_search_index_terms_shadow` and renames it over
`eshu_search_index_terms`, so it counts once).

### Purged at 2d by the generation cascade

All of these also reference `scope_generations ON DELETE CASCADE`, so deleting a
generation removes its rows. Disposition `2d`.

`scope_generations` (the batch target), `fact_records`, `fact_work_items`
(`005:3-4`), `fact_replay_events` (also references `fact_work_items`),
`shared_projection_acceptance` (`011`), `graph_projection_phase_state` (`012`),
`graph_projection_phase_repair_queue` (`013`), `admission_decisions` (`007a`),
`semantic_extraction_jobs` (`006a`), `iac_reachability_rows` (`016`),
`code_reachability_rows` and `code_reachability_repository_watermarks` (`027`),
`deferred_backfill_partition_memo` (`042`), `eshu_search_index_documents`,
`eshu_search_index_terms`, and `eshu_search_index_stats` (`003b`),
`eshu_search_vector_metadata` (`003c`), `eshu_search_vector_values` (`003d`),
`eshu_search_document_projection_state` (`054`), `eshu_search_vector_scope_state`
(`055`), `reducer_input_invalid_facts` (`060`), `code_root_verdicts` (`064`), and
`container_image_identity_cutovers` (`088`).

### Cascaded at phase 3 (scope FK only)

| Table | Migration | Disposition |
| --- | --- | --- |
| `tenant_scope_grants` | `006c_tenant_workspace_grants.sql:34` | `3+count` |
| `tenant_repository_grants` | `006c:62` | `3+count` |
| `identity_role_scope_targets` | `006g_identity_oidc_login.sql:58` | `3+count` |
| `identity_role_repository_targets` | `006g:83`, also cascades from the scope targets (`:93-94`) | `3+count` |
| `graph_endpoint_presence` | `024:4` | `3` (derived graph state; 2c retracted the graph first) |
| `container_image_identity_support_sets` | `092:255` | `3` (derived) |
| `container_image_identity_scope_state` | `092:299` | `3` (derived) |
| `projector_scope_claim_fences` | `130:22` | `3` (claim fence state) |

The four grant tables are the grants the design drops on purpose. The
tombstone reports their counts. The other four are derived or fence state and
the tombstone does not count them.

### Kept: `ON DELETE SET NULL`

`fact_backfill_requests` (`006_fact_work_item_audit.sql:16-17`) nulls `scope_id`
and `generation_id` and keeps the row. It is kept: it is an operator record of a
backfill request, and nulling the ids loses nothing the record needs. Round 3
named this table `fact_work_item_audit`; that is the migration file name, and the
table is `fact_backfill_requests`.

## Tables Reached Through Another Parent

| Table | Parent and rule | Disposition |
| --- | --- | --- |
| `relationship_reference_candidate_keys` (`058`) | `fact_records ON DELETE CASCADE` | `2d` |
| `package_manifest_consumption_keys` (`139`) | `fact_records ON DELETE CASCADE` | `2d` |
| `package_registry_identity_keys` (`143`) | `fact_records ON DELETE CASCADE` | `2d` |
| `content_file_secret_lines` (`138`) | `content_files` | `2d` (with `content_files`) |
| `activation_obligations` (`160`), `producer_activation_obligations` (`163`) | `scope_generations ON DELETE CASCADE`; `scope_id` has no FK | `2d` |

## Scope- And Repo-Keyed Tables With No FK

Each row names its key and the deleter that exists today. "No deleter" means a
search of non-test source found no `DELETE FROM <table>`.

### Removed at 2e

Predicates are `WHERE <key> = ANY($scopes)` or `= $repo`. Ledgers that the graph
retraction reads (`projected_source_edge` and its siblings) must go after 2c,
which is why 2e follows 2c.

| Table | Key | Today's deleter | Disposition |
| --- | --- | --- | --- |
| `content_files`, `content_entities`, `content_file_references` | `repo_id` | Retention prune steps by generation (2d) | `2e` leftover sweep (`migrations/004:57-60`) |
| `infra_resource_entities` | `repo_id`, `scope_id` | Retention orphan delete (`inventory.DeleteOrphanedRows`) | `2e` leftover sweep |
| `infra_resource_entity_dirty_repos` | `repo_id` | `infra/inventory/fence.go:91`, one repo | `2e` |
| `repository_refs` | `repo_id` | None found for a whole repo | `2e` (`004:46-55`) |
| `repository_selection_observations` | `scope_id` | None (#7774) | `2e` |
| `repository_reindex_requests` | `scope_id` | Phase 1 7e, again for safety | `2e` |
| `code_function_fingerprint`, `code_fingerprint_band` | `repo_id` | `content_writer_sql.go:193-207`, by path only | `2e` |
| `function_summaries`, `function_sources`, `function_graph_ids` | `repo` | By repo on rewrite (`summary_store.go:74`) | `2e`; whether `repo` equals the canonical repo id is NOT_CHECKED (`summary_store.go:319`) |
| `projected_source_edge` | `scope_id`, `evidence_source` | `projected_source_edge_store.go:61`, per scope and source | `2e`, after 2c: retract reads it to enumerate source uids |
| `code_interproc_projected_edge`, `code_taint_evidence_projected_node` | `scope_id`, `evidence_source` | `code/taint/interprocedural_edge.go:78`, `code/taint/projected_node.go:71` | `2e`, after 2c, same reason |
| `reducer_readiness_waits` | `scope_id` | None | `2e` |
| `collector_generation_dead_letters` | `scope_id`, `partition_key` | None | `2e`; a replay is refused by the commit gate, so the row has no use |
| `projection_decisions` | `repository_id` | None | `2e` |
| `package_manifest_consumption_key_dirty_scopes` | `scope_id` | `package_manifest_consumption_backfill.go:99`, after a backfill pass | `2e` |
| `supply_chain_impact_canonical_winners` | `winner_scope_id`, `repository_id` | `RebuildAllWinners` reconciles to the active fact set | `2e` by `winner_scope_id`; how often the rebuild runs is NOT_CHECKED |
| `supply_chain_impact_write_admission` | `scope_id` | Not read | `2e`; which scope kinds write it is NOT_CHECKED |

### Deleted at 2b

`shared_projection_intents` and `shared_projection_unroutable_intents` (keys
`repository_id`, `scope_id`, no FK) are deleted at 2b, before 2c, with the
barrier that follows (see the concurrency contract). Disposition `2b`.

### Reaped by an existing reaper

`changed_since_activations`, `changed_since_key_state`,
`changed_since_link_bucket_counts`, `changed_since_link_deltas`,
`changed_since_links`, and `changed_since_scope_cursor` (`136`) have no FK, by
design (migration 136 explains the multixact deadlock). Retention prunes them per
generation at 2d. After phase 3 deletes the scope row, the link runner's
orphan-scope pass removes the rest (`DeleteOrphanScope`,
`storage/postgres/freshness/links/journal.go:310`, statements at
`journal_sql.go:218-225`, driven from `reducer/freshness/links/runner.go:279`).
Disposition `reaped`. The runner does not wait for it, and the pass cadence is
NOT_CHECKED.

### Kept, with a reason

| Table | Key | Reason |
| --- | --- | --- |
| `webhook_refresh_triggers` | `repository_external_id`, `repository_full_name` | Delivery history. The webhook gate fails live triggers `repository_retiring`. |
| `governance_audit_events`, `generation_retention_events`, `identity_role_grants` | hashed ids (`scope_id_hash`, `repository_id_hash`) | Audit records hold hashes only. |
| `relationship_assertions` | `source_repo_id`, `target_repo_id` | Operator-authored decisions, not derived state. |
| `browser_sessions` | `allowed_scope_ids`, `allowed_repository_ids` | Allow-lists. A retired id in a list grants nothing once the scope is gone. |
| `workflow_work_items` | `scope_id` | Collector control-plane history. A later claim for a retired repo is refused at commit with a nil return. NOT_CHECKED: whether a stale item loops. |
| `package_manifest_consumption_key_backfill_progress` | `cursor_scope_id` | A cursor value, not repo data. |
| `aws_cloud_runtime_drift_write_admission`, `aws_scan_status`, `vulnerability_source_states`, `incident_freshness_triggers`, `gcp_freshness_triggers` | `scope_id`, `parent_scope_id` | Keyed by cloud, vulnerability-intelligence, incident, and GCP scopes, not `repository` scopes. |
| `cicd_run_watermarks` | `scope_id`, `repository` | Keyed by a CI scope. NOT_CHECKED: whether `repository` ever names a git scope's repo. |

### NOT_CHECKED, blocks PR 7

These carry a scope or repo key and their semantics were not read for this
design. The census test fails until each has a disposition.
- `collector_evidence_summary` (`scope_id`; rebuilt by the evidence maintainer,
  whether it prunes missing scopes is unread);
- `container_image_identity_supports` (`repository_id` and repo id arrays);
- `crossplane_satisfied_by_redrive_state` (`xrd_scope_id`) and
  `crossplane_satisfied_by_redrive_target_ledger` (`target_scope_id`);
- `relationship_candidates`, `relationship_evidence_facts`, and
  `resolved_relationships` (`source_repo_id`, `target_repo_id`;
  `migrations/010_relationship_tables.sql:31-70`). Their `generation_id` has no
  `REFERENCES`, so the 2d cascade does not reach them and `2d` is not an available
  disposition. The final disposition must be `2e` by `source_repo_id` or by the
  retired scopes' generation ids. Rows where the retired repo is only the target
  belong to other repositories' evidence, the same shape as incoming graph
  edges: `foreign_target`. Which of the two applies is unread, so the three
  tables stay NOT_CHECKED.
- `relationship_generations` (`scope`, no FK; `010:28-29`) is a column the broad
  match now flags. Its semantics are unread.

## Consequences For The Runner

- 2e runs the `2e` rows. Each statement is chunked and counts into `rows_deleted`.
- Phase 3 pre-counts the `3+count` rows and then deletes the scope rows.
- 2z does not query this whole list at runtime. It counts the four hot tables
  and the graph. The census test, not a runtime query, proves every table has an
  owner.
- A new table with a scope or repo key fails the census test until a disposition
  is added, the same shape as `TestRetirementRetractPlanClassifiesEveryDomain`.
