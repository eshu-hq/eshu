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
Source check: origin/main f4f14a4096, 2026-10-19, read from
`go/internal/storage/postgres/migrations/*.sql`. The schema up to migration 177
was confirmed against a scratch Postgres 18 schema at ef5703ec26 (first checked
at c88c3806a); migrations 178 to 206 were read from source.

## Dispositions

| Code | Meaning | Who removes it |
| --- | --- | --- |
| `2b` | Shared intents, deleted before the graph retraction | 2b, chunked 10,000 rows |
| `2d` | Removed with a generation: by its FK cascade, or by a retention step | 2d, the ADR 2248 cascade (`generation_retention.go:401-445`) |
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
`eshu_search_index_terms`, so it counts once). The scratch schema agrees: 32
parent tables. Its 64 partitions, `eshu_search_index_terms_p00` to `_p63`, carry
cloned FKs to `ingestion_scopes` and `scope_generations`, so the census test
rolls a partition up to its parent (`pg_class.relispartition`) instead of
listing 64 entries. Migration 191 dropped the terms table's generation FK, so
its rows go in 2d's search-index pre-delete (#7427), and only its
`ingestion_scopes` FK is left for phase 3.

### Purged at 2d by the generation cascade

All of these also reference `scope_generations ON DELETE CASCADE`, so deleting a
generation removes its rows. Disposition `2d`.

`scope_generations` (the batch target), `fact_records`, `fact_work_items`
(`005:3-4`), `fact_replay_events` (also references `fact_work_items`),
`shared_projection_acceptance` (`011`), `graph_projection_phase_state` (`012`),
`graph_projection_phase_repair_queue` (`013`), `admission_decisions` (`007a`),
`semantic_extraction_jobs` (`006a`), `iac_reachability_rows` (`016`),
`code_reachability_rows` and `code_reachability_repository_watermarks` (`027`),
`deferred_backfill_partition_memo` (`042`), `eshu_search_index_documents` and
`eshu_search_index_stats` (`003b`; `eshu_search_index_terms` is deleted by the
2d pre-delete since migration 191),
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
| `relationship_reference_candidate_keys` (`058`), `package_manifest_consumption_keys` (`139`), `package_registry_identity_keys` (`143`) | None since migrations 188 to 190 dropped the `fact_records` FK; keyed by `scope_id` and `generation_id` | `2d` through retention's `factkeys.DeleteGenerations` step (#7420), then `2e` by `scope_id` for leftovers |
| `deployable_unit_reopen_fingerprints` (`193`) | `fact_work_items ON DELETE CASCADE`; also carries `repo_ids` | `2d`. 2c also calls `InvalidateDeployableUnitReopenFingerprintsForRepos` (`ingestion_reopen_correlation.go:374-400`) after the graph commit, as both Repository deleters do. |
| `content_file_secret_lines` (`138`) | `content_files` | `2d` (with `content_files`) |
| `activation_obligations` (`160`), `producer_activation_obligations` (`163`) | `scope_generations ON DELETE CASCADE`; `scope_id` has no FK | `2d` |
| `container_image_identity_supports` (`092_container_image_identity_support_store.sql:263-265`) | `container_image_identity_support_sets ON DELETE CASCADE`; the set's `scope_id` cascades from `ingestion_scopes` (`092:255`) | `3`. Its `repository_id` holds `oci-registry://` ids (`containerimage/container_image_identity_registry.go:128`), so it is `not_a_key`. A retired git repo id inside another scope's repo-id arrays is `foreign_target`. |
| `service_evidence_snapshots` (`026:2`) | `service_materialization_generations ON DELETE CASCADE` | With its parent at `2e` |
| `projection_decision_evidence` (`007:18-25`) | `projection_decisions` by `decision_id`, no FK and no matching column, so the census test cannot see it | `2e`, by `decision_id`, before its parent |

## Scope- And Repo-Keyed Tables With No FK

Each row names its key and the deleter that exists today. "No deleter" means a
search of non-test source found no `DELETE FROM <table>`.

### Removed at 2e

Predicates are `WHERE <key> = ANY($scopes)` or `= $repo`. Ledgers that the graph
retraction reads (`projected_source_edge` and its siblings) must go after 2c,
which is why 2e follows 2c.

| Table | Key | Today's deleter | Disposition |
| --- | --- | --- | --- |
| `content_files`, `content_entities`, `content_file_references` | `repo_id` | Retention prune steps, keyed by the pruned generations' file and entity facts (2d, `generation_retention_sql.go:292-390`). Migration 169's `content_files.generation_id` is advisory and retention does not read it (`169:12-15`). | `2e` leftover sweep (`migrations/004:57-60`) |
| `infra_resource_entities` | `repo_id`, `scope_id` | Retention orphan delete (`inventory.DeleteOrphanedRows`) | `2e` leftover sweep |
| `infra_resource_entity_dirty_repos` | `repo_id` | `infra/inventory/fence.go:91`, one repo | `2e` |
| `repository_refs` | `repo_id` | None found for a whole repo | `2e` (`004:46-55`) |
| `repository_selection_observations` | `scope_id` | Expired rows (`membership/observations.go:119-131`) and abandoned selectors' rows (`membership/observations_sweep.go:46-61`), not a whole scope | `2e` |
| `repository_reindex_requests` | `scope_id` | Phase 1 7e. A refinalize whose read predates phase 1 can re-create it (`rebuild/reset/delta_active.go:101-115`) | `2e` |
| `code_function_fingerprint`, `code_fingerprint_band` | `repo_id` | `content_writer_sql.go:211-225`, by path only | `2e` |
| `function_summaries`, `function_sources`, `function_graph_ids` | `repo` | By repo on rewrite (`summary_store.go:74`) | `2e`; whether `repo` equals the canonical repo id is NOT_CHECKED (`summary_store.go:319`) |
| `projected_source_edge` | `scope_id`, `evidence_source` | `projected_source_edge_store.go:61`, per scope and source | `2e`, after 2c: retract reads it to enumerate source uids |
| `code_interproc_projected_edge`, `code_taint_evidence_projected_node` | `scope_id`, `evidence_source` | `code/taint/interprocedural_edge.go:78`, `code/taint/projected_node.go:71` | `2e`, after 2c, same reason |
| `reducer_readiness_waits` | `scope_id` | None | `2e` |
| `collector_generation_dead_letters` | `scope_id`, `partition_key` | None | `2e`; a replay is refused by the commit gate, so the row has no use |
| `projection_decisions` | `repository_id` | None | `2e` |
| `package_manifest_consumption_key_dirty_scopes` | `scope_id` | `consumption/statements.go:53-60`, run at `consumption/rebuild.go:100`, after a backfill pass | `2e` |
| `supply_chain_impact_canonical_winners` | `winner_scope_id`, `repository_id` | `RebuildAllWinners` reconciles to the active fact set | `2e` by `winner_scope_id`; how often the rebuild runs is NOT_CHECKED |
| `supply_chain_impact_write_admission` | `scope_id` | Not read | `2e`; which scope kinds write it is NOT_CHECKED |
| `crossplane_satisfied_by_redrive_state` | `xrd_scope_id` (`076:39-50`) | None | `2e`. A leftover queued row finds no XRD keys and completes itself (`crossplane/sweep.go:205-216`), so it is residue, not a loop. |
| `crossplane_satisfied_by_redrive_target_ledger` | `target_scope_id` (`076:105-111`) | None | `2e`. Leaving it is wrong: the redrive's `NOT EXISTS` fence (`crossplane/query.go:77-83`) would skip a re-added repository, because git scope ids are derived from the remote URL. |
| `relationship_evidence_facts`, `relationship_candidates`, `resolved_relationships` | `generation_id` (no FK), `source_repo_id`, `target_repo_id` (`010:31-79`) | None. Retention never touches `relationship_*` | `2d` by each batch's generation ids, then `2e` for the rest; see [Relationship tables](#relationship-tables). Rows where the retired repo is only the target, under other scopes' generations, are `foreign_target`. |
| `relationship_generations` | `scope` (the ingestion `scope_id`), `generation_id` (`010:19-29`) | None; `rebuild/reset/reset.go:226` only marks rows superseded | `2e` by `scope = ANY($scopes)`, after the three tables above |
| `config_artifact_content_versions`, `config_artifact_repo_state` | `repo_id` (`170:54-77`) | None. The `content_files` trigger (`186`) inserts or bumps version rows, and migration 187 seeds them | `2e` by `repo_id`, both tables in one statement, after the `content_files` sweep (see [Config artifact tables](#config-artifact-tables)) |
| `config_artifact_paths` | `repo_id` (`170:83-94`) | None | `2e` by `repo_id` |
| `content_file_ref_deltas`, `content_file_ref_delta_bases` | `repo_id` (`197:43-66`) | None; not wired yet | `2e` by `repo_id` |
| `service_materialization_generations` | `scope_id` (`126:22-23`) | None | `2e` by `scope_id`; `service_evidence_snapshots` cascades. Legacy rows with a NULL `scope_id` are NOT_CHECKED. |

### Relationship Tables

The three relationship tables need both predicates:
- **The generation predicate alone misses rows.** Per-commit backfill writes the
  retired repository's outgoing evidence under the onboarding generation of the
  repository it targets (`backfill/per_commit.go:93`, via
  `ingestion_backfill_seam.go:294`), and rows of long-pruned generations survive
  because nothing deletes them.
- **`source_repo_id` alone misses rows.** Backward evidence and the
  candidates, which are not ownership-partitioned
  (`crossrepo/cross_repo_resolution.go:311`), can sit under the retired
  repository's generations with another source.
- **So the delete is split.** Generation ids are opaque hashes, and 2d deletes
  `scope_generations`, so the generation predicate must run while the ids are in
  hand. Each 2d batch deletes the three tables' rows `WHERE generation_id =
  ANY($batch_generation_ids)` in its own transaction. Retention has no such
  step, so this is a runner statement, not part of the ADR 2248 cascade. 2e
  then deletes the rest `WHERE source_repo_id = $repo OR generation_id IN
  (SELECT generation_id FROM relationship_generations WHERE scope = ANY($scopes))`,
  and deletes `relationship_generations` last, because it is the only surviving
  map from generation to scope.
- **Residual.** A row under a generation of the retired repository that was
  pruned before the retirement, never reached resolution, and has another
  source is matched by neither predicate.
- **Index cost.** Only `resolved_relationships` has a `source_repo_id` index
  (`010:84-90`). P4 covers the evidence and candidate deletes, and adds an index
  if a chunk is a sequential scan.

The tables have no deleter today, so rows of every generation accumulate. That
predates #7766.

### Config Artifact Tables

The 7126 protocol requires a version row to be deleted only together with its
repository's state row, and names this design as the retirement path that must
honour it (`docs/internal/design/7126-ingest-time-config-artifacts-protocol.md:36-43`).
`config/artifacts/versions_guard_test.go` enforces it per statement: the
versions delete must be the same statement that deletes the state row. The
`content_files` trigger shipped in migration 186. It bumps the version row on a
`content_files` delete, and inserts one at version 1 when the row is missing
(`186:119`). So the 2e versions statement commits after the last `content_files`
delete, and retirement leaves the trigger enabled. `config_artifact_fence_pending`
(`186`) is emptied by the same trigger at commit: `reaped`.
`config_artifact_backfill_markers` and `config_artifact_reconcile_cursor` have no
matching column and need no entry.

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
`storage/postgres/freshness/links/journal.go:317`, statements at
`journal_sql.go:238-245`, driven from `reducer/freshness/links/runner.go:286`).
Disposition `reaped`. The runner does not wait for it, and the pass cadence is
NOT_CHECKED.

`collector_evidence_summary` (`scope_id`, `036:1-15`, no FK) is reaped by its
own maintainer. The summary statement deletes every row whose scope is not in
the active set (`collector_evidence_summary.go:119-126`), and that set requires
an active generation (`:20-51`). It runs every 60 s
(`reducer/maintenance/evidence/maintainer.go:48`). Disposition `reaped`; its
rows are not counted in `rows_deleted`.

`component_quota_windows` (`206`, `scope_id`) is reaped by the quota writer,
whose `prune_quota` step deletes buckets older than the previous hour
(`workflow_control_quota_sql.go:137-141`). Disposition `reaped`. `repository_selectors`
(`200`) and `workflow_claim_pacing` (`205`) have no matching column and need no
entry.

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
| `workflow_runs` | `requested_scope_set` (`014:5`) | The audit record of what was scheduled, and the parent of `workflow_work_items`, which is kept. |

### NOT_CHECKED, blocks PR 7

Every table the c88c3806a census left NOT_CHECKED is classified above, and the
tables migrations 178 to 206 added are classified at `f4f14a4096`. What remains
unread:
- whether every writer of the relationship tables stores `source_repo_id` in
  the same canonical `repository:` form;
- legacy `service_materialization_generations` rows with a NULL `scope_id`;
- the size of `deployable_unit_reopen_fingerprints`, and which scope kinds
  write `component_quota_windows` (reaped by `prune_quota`,
  `workflow_control_quota_sql.go:137-141`).

## Consequences For The Runner

- 2e runs the `2e` rows. Each statement is chunked and counts into `rows_deleted`.
- Phase 3 pre-counts the `3+count` rows and then deletes the scope rows.
- 2z does not query this whole list at runtime. It counts the four hot tables
  and the graph. The census test, not a runtime query, proves every table has an
  owner.
- A new table with a scope or repo key fails the census test until a disposition
  is added, the same shape as `TestRetirementRetractPlanClassifiesEveryDomain`.
