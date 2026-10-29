// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

// RetireTableEntry is one Postgres table's retirement disposition: which
// purge step deletes its rows for a retired repository, or why the rows
// stay. The closed table below is the machine-readable form of the #7766
// table census (docs/internal/design/7766-repository-retirement-table-census.md):
// the live census test joins it against information_schema on every
// migrated schema, so a table added later fails the test until someone
// records its disposition here.
type RetireTableEntry struct {
	// Table is the table name (partitions roll up to their parent).
	Table string
	// KeyColumns are the scope or repository identity columns the purge
	// predicates bind. Empty only for reaped tables the runner never
	// predicates on.
	KeyColumns []string
	// RemoveSteps are the ordered purge steps deleting this table's rows
	// for the retired repository (2b, 2d, 2e, 3). Empty for kept tables.
	RemoveSteps []string
	// Disposition is the census code: 2b, 2d, 2e, 3, 3+count, reaped,
	// kept, or not_a_key. Multi-step tables carry their last step here;
	// RemoveSteps holds the full path.
	Disposition string
	// Reason cites the deleter, the cascade, or the keep reason. Required
	// on every row.
	Reason string
	// InvisibleToScan marks rows the information_schema scan cannot
	// produce (no matching scope/repository key column), so the stale
	// check skips them instead of prose-matching Reason. Set it exactly
	// on rows whose Reason says the scan cannot see them.
	InvisibleToScan bool
}

// RetireTableDispositions holds every scope- or repo-keyed table's
// disposition. Coverage is enforced by the live census test in the
// Postgres package, which fails on any information_schema hit missing
// from this table (plus a seeded-violation pair proving the join sees new
// tables).
var RetireTableDispositions = map[string]RetireTableEntry{
	// Removed with a generation at 2d: every one references
	// scope_generations ON DELETE CASCADE (or is deleted by a retention
	// step the runner reuses), so deleting the batch's generations
	// removes its rows.
	"scope_generations":                       {Table: "scope_generations", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "the batch target; deleted oldest-first, at most 100 generations per batch (ADR 2248)"},
	"fact_records":                            {Table: "fact_records", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/005:3-4)"},
	"fact_work_items":                         {Table: "fact_work_items", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/005:3-4)"},
	"fact_replay_events":                      {Table: "fact_replay_events", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "references fact_work_items; cascades with the generation"},
	"shared_projection_acceptance":            {Table: "shared_projection_acceptance", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/011); deleted only after 2b (invariant I3)"},
	"graph_projection_phase_state":            {Table: "graph_projection_phase_state", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/012)"},
	"graph_projection_phase_repair_queue":     {Table: "graph_projection_phase_repair_queue", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/013)"},
	"admission_decisions":                     {Table: "admission_decisions", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/007a)"},
	"admission_decision_evidence":             {Table: "admission_decision_evidence", KeyColumns: []string{"decision_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "cascades with its admission_decisions parent (migrations/007a); no matching column so the schema scan cannot see it", InvisibleToScan: true},
	"security_alert_target_read_states":       {Table: "security_alert_target_read_states", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/230)"},
	"security_alert_unread_repositories":      {Table: "security_alert_unread_repositories", KeyColumns: []string{"generation_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "cascades with its security_alert_target_read_states parent (migrations/230); keyed by generation_id and a provider repository slug, never a scope id"},
	"semantic_extraction_jobs":                {Table: "semantic_extraction_jobs", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/006a)"},
	"iac_reachability_rows":                   {Table: "iac_reachability_rows", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/016)"},
	"code_reachability_rows":                  {Table: "code_reachability_rows", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/027)"},
	"code_reachability_repository_watermarks": {Table: "code_reachability_repository_watermarks", KeyColumns: []string{"scope_id", "repository_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/027)"},
	"deferred_backfill_partition_memo":        {Table: "deferred_backfill_partition_memo", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/042)"},
	"eshu_search_index_documents":             {Table: "eshu_search_index_documents", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/003b)"},
	"eshu_search_index_stats":                 {Table: "eshu_search_index_stats", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/003b)"},
	"eshu_search_index_terms":                 {Table: "eshu_search_index_terms", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "2d search-index pre-delete per generation batch (#7427; generation FK dropped in migration 191), partitions roll up to the parent"},
	"eshu_search_vector_metadata":             {Table: "eshu_search_vector_metadata", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/003c)"},
	"eshu_search_vector_values":               {Table: "eshu_search_vector_values", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/003d)"},
	"eshu_search_document_projection_state":   {Table: "eshu_search_document_projection_state", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/054)"},
	"eshu_search_vector_scope_state":          {Table: "eshu_search_vector_scope_state", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/055)"},
	"reducer_input_invalid_facts":             {Table: "reducer_input_invalid_facts", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/060)"},
	"code_root_verdicts":                      {Table: "code_root_verdicts", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/064)"},
	"container_image_identity_cutovers":       {Table: "container_image_identity_cutovers", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/088)"},
	"deployable_unit_reopen_fingerprints":     {Table: "deployable_unit_reopen_fingerprints", KeyColumns: []string{"scope_id", "repo_ids"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "fact_work_items cascade (migrations/193); 2c also invalidates for the repos after the graph commit"},
	"content_file_secret_lines":               {Table: "content_file_secret_lines", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "cascades with its content_files parent (migrations/138)"},
	"activation_obligations":                  {Table: "activation_obligations", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/160)"},
	"producer_activation_obligations":         {Table: "producer_activation_obligations", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2d"}, Disposition: "2d", Reason: "generation FK cascade (migrations/163)"},
	// Key sidecars: no fact_records FK since migrations 188 to 190, but
	// keyed by scope and generation, so the 2d factkeys.DeleteGenerations
	// step covers them (#7420) with a 2e leftover sweep by scope.
	"relationship_reference_candidate_keys": {Table: "relationship_reference_candidate_keys", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{"2d", "2e"}, Disposition: "2e", Reason: "2d through factkeys.DeleteGenerations, then 2e by scope for leftovers"},
	"package_manifest_consumption_keys":     {Table: "package_manifest_consumption_keys", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{"2d", "2e"}, Disposition: "2e", Reason: "2d through factkeys.DeleteGenerations, then 2e by scope for leftovers"},
	"package_registry_identity_keys":        {Table: "package_registry_identity_keys", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{"2d", "2e"}, Disposition: "2e", Reason: "2d through factkeys.DeleteGenerations, then 2e by scope for leftovers"},
	// Deleted at 2b, before the graph retraction, with the barrier that
	// follows (concurrency contract 2b/2b').
	"shared_projection_intents":            {Table: "shared_projection_intents", KeyColumns: []string{"repository_id", "scope_id"}, RemoveSteps: []string{"2b"}, Disposition: "2b", Reason: "chunked 10,000-row delete by repository_id and scope (2b), barrier stamped after commit"},
	"shared_projection_unroutable_intents": {Table: "shared_projection_unroutable_intents", KeyColumns: []string{"repository_id", "scope_id"}, RemoveSteps: []string{"2b"}, Disposition: "2b", Reason: "chunked 10,000-row delete by repository_id and scope (2b), under the same barrier"},
	"relationship_evidence_facts":          {Table: "relationship_evidence_facts", KeyColumns: []string{"source_repo_id", "generation_id"}, RemoveSteps: []string{"2b", "2d", "2e"}, Disposition: "2e", Reason: "2b by source_repo_id, each 2d batch by its generation ids, 2e for the rest; target-only rows under other scopes are foreign_target"},
	"relationship_candidates":              {Table: "relationship_candidates", KeyColumns: []string{"source_repo_id", "generation_id"}, RemoveSteps: []string{"2b", "2d", "2e"}, Disposition: "2e", Reason: "2b by source_repo_id, each 2d batch by its generation ids, 2e for the rest; target-only rows under other scopes are foreign_target"},
	"resolved_relationships":               {Table: "resolved_relationships", KeyColumns: []string{"source_repo_id", "target_repo_id", "generation_id"}, RemoveSteps: []string{"2b", "2d", "2e"}, Disposition: "2e", Reason: "2b by source_repo_id (indexed), each 2d batch by its generation ids, 2e for the rest; target-only rows under other scopes are foreign_target"},
	"relationship_generations":             {Table: "relationship_generations", KeyColumns: []string{"scope", "generation_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "deleted last by scope, after the three tables above; the only surviving generation-to-scope map"},
	// Repo- and scope-keyed leftovers at 2e, chunked 10,000 by ctid,
	// after 2c (the graph retraction reads the projected ledgers).
	"content_files":                                 {Table: "content_files", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id (migrations/004:57-60)"},
	"content_entities":                              {Table: "content_entities", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id"},
	"content_file_references":                       {Table: "content_file_references", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id"},
	"infra_resource_entities":                       {Table: "infra_resource_entities", KeyColumns: []string{"repo_id", "scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep (retention orphan delete covers the live path, not a whole repository)"},
	"infra_resource_entity_dirty_repos":             {Table: "infra_resource_entity_dirty_repos", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id (infra/inventory/fence.go deletes one repo at a time, never a whole retirement)"},
	"repository_refs":                               {Table: "repository_refs", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id (migrations/004:46-55)"},
	"repository_selection_observations":             {Table: "repository_selection_observations", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope (the observations sweep only expires rows, never a whole scope)"},
	"repository_reindex_requests":                   {Table: "repository_reindex_requests", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope (phase 1 deletes, but a refinalize can re-create between fence and purge)"},
	"code_function_fingerprint":                     {Table: "code_function_fingerprint", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id (content_writer_sql.go deletes by repo and entity on rewrite only)"},
	"code_fingerprint_band":                         {Table: "code_fingerprint_band", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id, with its fingerprint parent"},
	"function_summaries":                            {Table: "function_summaries", KeyColumns: []string{"repo"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo (summary_store.go deletes by repo on rewrite only)"},
	"function_sources":                              {Table: "function_sources", KeyColumns: []string{"repo"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo, with its summary parent"},
	"function_graph_ids":                            {Table: "function_graph_ids", KeyColumns: []string{"repo"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo, with its summary parent"},
	"projected_source_edge":                         {Table: "projected_source_edge", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope after 2c; the graph retraction enumerates source uids from it (storage/postgres/projected_source_edge_store.go ListSourceUIDsForScopes)"},
	"code_interproc_projected_edge":                 {Table: "code_interproc_projected_edge", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope after 2c, same ledger reason (code/taint/interprocedural_edge.go)"},
	"code_taint_evidence_projected_node":            {Table: "code_taint_evidence_projected_node", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope after 2c, same ledger reason (code/taint/projected_node.go)"},
	"reducer_readiness_waits":                       {Table: "reducer_readiness_waits", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope (no deleter exists today)"},
	"collector_generation_dead_letters":             {Table: "collector_generation_dead_letters", KeyColumns: []string{"scope_id", "partition_key"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope (a replay is refused by the commit gate, so the row has no use)"},
	"projection_decisions":                          {Table: "projection_decisions", KeyColumns: []string{"repository_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repository_id (no deleter exists today)"},
	"projection_decision_evidence":                  {Table: "projection_decision_evidence", KeyColumns: []string{"decision_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "deleted by decision_id before its projection_decisions parent; no matching column so the schema scan cannot see it", InvisibleToScan: true},
	"package_manifest_consumption_key_dirty_scopes": {Table: "package_manifest_consumption_key_dirty_scopes", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope (consumption/statements.go runs after a backfill pass, never a whole retirement)"},
	"supply_chain_impact_canonical_winners":         {Table: "supply_chain_impact_canonical_winners", KeyColumns: []string{"winner_scope_id", "repository_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by winner_scope_id (RebuildAllWinners reconciles, never deletes a whole scope)"},
	"supply_chain_impact_write_admission":           {Table: "supply_chain_impact_write_admission", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope"},
	"crossplane_satisfied_by_redrive_state":         {Table: "crossplane_satisfied_by_redrive_state", KeyColumns: []string{"xrd_scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by xrd_scope_id; a leftover queued row completes itself (crossplane/sweep.go)"},
	"crossplane_satisfied_by_redrive_target_ledger": {Table: "crossplane_satisfied_by_redrive_target_ledger", KeyColumns: []string{"target_scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by target_scope_id; leaving it would fence a re-added repository (crossplane/query.go NOT EXISTS fence)"},
	"config_artifact_content_versions":              {Table: "config_artifact_content_versions", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "deleted with its state row in one statement after the content_files sweep (7126 protocol, versions_guard_test.go); the migration 186 trigger stays enabled"},
	"config_artifact_repo_state":                    {Table: "config_artifact_repo_state", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "deleted with its version row in one statement (7126 protocol)"},
	"config_artifact_paths":                         {Table: "config_artifact_paths", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id"},
	"content_file_ref_deltas":                       {Table: "content_file_ref_deltas", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id (not wired yet)"},
	"content_file_ref_delta_bases":                  {Table: "content_file_ref_delta_bases", KeyColumns: []string{"repo_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by repo_id (not wired yet)"},
	"service_materialization_generations":           {Table: "service_materialization_generations", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "leftover sweep by scope; service_evidence_snapshots cascades with the parent"},
	"service_evidence_snapshots":                    {Table: "service_evidence_snapshots", KeyColumns: []string{"generation_id"}, RemoveSteps: []string{"2e"}, Disposition: "2e", Reason: "cascades with its service_materialization_generations parent (migrations/026); keyed by generation_id only, so the schema scan cannot see it", InvisibleToScan: true},
	// Cascaded at phase 3 when the scope rows go. The four grant tables
	// are pre-counted into the tombstone inside the transaction.
	"tenant_scope_grants":                   {Table: "tenant_scope_grants", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3+count", Reason: "scope FK cascade at phase 3, pre-counted into rows_deleted; access fails closed (deliberately dropped)"},
	"tenant_repository_grants":              {Table: "tenant_repository_grants", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3+count", Reason: "scope FK cascade at phase 3, pre-counted; access fails closed (deliberately dropped)"},
	"identity_role_scope_targets":           {Table: "identity_role_scope_targets", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3+count", Reason: "scope FK cascade at phase 3, pre-counted; access fails closed (deliberately dropped)"},
	"identity_role_repository_targets":      {Table: "identity_role_repository_targets", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3+count", Reason: "scope FK cascade at phase 3, pre-counted; access fails closed (deliberately dropped)"},
	"graph_endpoint_presence":               {Table: "graph_endpoint_presence", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3", Reason: "scope FK cascade at phase 3 (derived graph state; 2c retracted the graph first)"},
	"container_image_identity_support_sets": {Table: "container_image_identity_support_sets", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3", Reason: "scope FK cascade at phase 3 (derived)"},
	"container_image_identity_scope_state":  {Table: "container_image_identity_scope_state", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3", Reason: "scope FK cascade at phase 3 (derived)"},
	"container_image_identity_supports":     {Table: "container_image_identity_supports", KeyColumns: []string{"repository_id"}, RemoveSteps: []string{"3"}, Disposition: "3", Reason: "cascades with its support set; repository_id holds oci-registry ids (not_a_key for git scopes) and a retired git id inside another scope's arrays is foreign_target"},
	"projector_scope_claim_fences":          {Table: "projector_scope_claim_fences", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3", Reason: "scope FK cascade at phase 3 (claim fence state)"},
	"ingestion_scopes":                      {Table: "ingestion_scopes", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{"3"}, Disposition: "3", Reason: "the scope rows themselves, deleted last at phase 3 after 2z passes; the transaction holds the NOWAIT scope lock while they go"},
	// Reaped by an existing reaper after the scope row is gone; the
	// runner does not wait for them and rows_deleted does not count them.
	"changed_since_activations":        {Table: "changed_since_activations", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "retention prunes per generation at 2d; the link runner orphan-scope pass removes the rest (freshness/links/journal.go DeleteOrphanScope)"},
	"changed_since_key_state":          {Table: "changed_since_key_state", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "same link-runner orphan-scope reaper as changed_since_activations"},
	"changed_since_link_bucket_counts": {Table: "changed_since_link_bucket_counts", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "same link-runner orphan-scope reaper as changed_since_activations"},
	"changed_since_link_deltas":        {Table: "changed_since_link_deltas", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "same link-runner orphan-scope reaper as changed_since_activations"},
	"changed_since_links":              {Table: "changed_since_links", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "same link-runner orphan-scope reaper as changed_since_activations"},
	"changed_since_scope_cursor":       {Table: "changed_since_scope_cursor", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "same link-runner orphan-scope reaper as changed_since_activations"},
	"collector_evidence_summary":       {Table: "collector_evidence_summary", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "reaped by its own maintainer every 60s (rows whose scope has no active generation go)"},
	"component_quota_windows":          {Table: "component_quota_windows", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "reaped by the quota writer prune_quota step (buckets older than the previous hour go)"},
	"config_artifact_fence_pending":    {Table: "config_artifact_fence_pending", KeyColumns: []string{}, RemoveSteps: []string{}, Disposition: "reaped", Reason: "emptied by the migration 186 content_files trigger at commit"},
	// Kept, with a reason. Audit and operator records, allow-lists, and
	// control-plane history stay; the retirement only needs them to stop
	// granting or reopening work, which the gate and the cascade do.
	"repository_retirements":                             {Table: "repository_retirements", KeyColumns: []string{"repo_id", "scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "the tombstone itself: complete rows are history and readmission stamps readmitted_at; never deleted"},
	"fact_backfill_requests":                             {Table: "fact_backfill_requests", KeyColumns: []string{"scope_id", "generation_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "ON DELETE SET NULL keeps the operator backfill record; nulling the ids loses nothing it needs"},
	"webhook_refresh_triggers":                           {Table: "webhook_refresh_triggers", KeyColumns: []string{"repository_external_id", "repository_full_name"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "delivery history; the webhook gate fails live triggers repository_retiring"},
	"governance_audit_events":                            {Table: "governance_audit_events", KeyColumns: []string{"scope_id_hash", "repository_id_hash"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "audit records hold hashes only"},
	"generation_retention_events":                        {Table: "generation_retention_events", KeyColumns: []string{"scope_id_hash", "repository_id_hash"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "audit records hold hashes only"},
	"identity_role_grants":                               {Table: "identity_role_grants", KeyColumns: []string{"scope_id_hash", "repository_id_hash"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "audit records hold hashes only"},
	"relationship_assertions":                            {Table: "relationship_assertions", KeyColumns: []string{"source_repo_id", "target_repo_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "operator-authored decisions, not derived state"},
	"browser_sessions":                                   {Table: "browser_sessions", KeyColumns: []string{"allowed_scope_ids", "allowed_repository_ids"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "allow-lists; a retired id in a list grants nothing once the scope is gone"},
	"workflow_work_items":                                {Table: "workflow_work_items", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "collector control-plane history; a later claim for a retired repo is refused at commit"},
	"collector_terminal_failure_acknowledgements":        {Table: "collector_terminal_failure_acknowledgements", KeyColumns: []string{"collector_kind", "collector_instance_id", "scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "operator audit record (#8830) with no foreign key by design (migrations/231); it only suppresses a failure of the kept workflow_work_items, so it is inert only until the same (kind, instance, scope_id) fails again; then a same-class failure with no intervening completed item stays acknowledged per the #8830 episode rule (docs/internal/evidence/8830-collector-terminal-failure-acknowledgement.md, Known limits), because retirement does not purge it; bounded by the (instance, scope) pairs ever acknowledged"},
	"workflow_runs":                                      {Table: "workflow_runs", KeyColumns: []string{"requested_scope_set"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "the audit record of what was scheduled, and the parent of the kept workflow_work_items"},
	"package_manifest_consumption_key_backfill_progress": {Table: "package_manifest_consumption_key_backfill_progress", KeyColumns: []string{"cursor_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "a cursor value, not repo data"},
	"aws_cloud_runtime_drift_write_admission":            {Table: "aws_cloud_runtime_drift_write_admission", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by cloud scopes, not repository scopes"},
	"aws_scan_status":                                    {Table: "aws_scan_status", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by cloud scopes, not repository scopes"},
	"vulnerability_source_states":                        {Table: "vulnerability_source_states", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by vulnerability-intelligence scopes, not repository scopes"},
	"incident_freshness_triggers":                        {Table: "incident_freshness_triggers", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by incident scopes, not repository scopes"},
	"gcp_freshness_triggers":                             {Table: "gcp_freshness_triggers", KeyColumns: []string{"scope_id", "parent_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by GCP scopes, not repository scopes"},
	"cicd_run_watermarks":                                {Table: "cicd_run_watermarks", KeyColumns: []string{"scope_id", "repository"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by CI scopes"},
	// Peer tables the census join sees that merged without census entries;
	// the live test fails until someone records them here.
	"vulnerability_source_state_packages": {Table: "vulnerability_source_state_packages", KeyColumns: []string{"scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by vulnerability-intelligence scopes, not repository scopes (migration 243, #9227)"},
	"cicd_actions_deliveries":             {Table: "cicd_actions_deliveries", KeyColumns: []string{"repository", "repository_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by CI scopes (migration 236, #9397)"},
	// Scanner running-image intake (#9299, migrations 241-252). The
	// source_scope_id values are producer workload scopes (Kubernetes
	// namespaces, ECS tasks, Lambda functions), never repository scopes,
	// so repository retirement has no key to purge by.
	"scanner_running_image_producer_grants":        {Table: "scanner_running_image_producer_grants", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by producer workload scopes, not repository scopes"},
	"scanner_running_image_scope_claims":           {Table: "scanner_running_image_scope_claims", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by producer workload scopes, not repository scopes"},
	"scanner_running_image_generations":            {Table: "scanner_running_image_generations", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "source generation registry; superseded generations go inert once active_scopes moves on"},
	"scanner_running_image_page_receipts":          {Table: "scanner_running_image_page_receipts", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "collection receipts keyed by producer workload scopes"},
	"scanner_running_image_delivery_receipts":      {Table: "scanner_running_image_delivery_receipts", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "delivery receipts keyed by producer workload scopes"},
	"scanner_running_image_observations":           {Table: "scanner_running_image_observations", KeyColumns: []string{"source_scope_id", "repository"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "workload intake facts; repository holds registry paths (team/service), not git repos"},
	"scanner_running_image_manifest_verifications": {Table: "scanner_running_image_manifest_verifications", KeyColumns: []string{"repository"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "verification records keyed by observation; repository holds registry paths, not git repos"},
	"scanner_running_image_active_scopes":          {Table: "scanner_running_image_active_scopes", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "current-generation pointers keyed by producer workload scopes"},
	"scanner_running_image_activation_outbox":      {Table: "scanner_running_image_activation_outbox", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "activation outbox rows keyed by producer workload scopes"},
	"scanner_running_image_projection_outbox":      {Table: "scanner_running_image_projection_outbox", KeyColumns: []string{"source_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "projection outbox rows keyed by producer workload scopes"},
	// Scanner derived targets and results (#9299, migrations 240-250). The
	// scan_scope_id values are derived image identities
	// (image://derived/<sha256> over boundary, tenant, domain, and digest),
	// never repository scopes, so this is control-plane history: a later
	// admission for a gone scope is refused by authority.
	"scanner_image_targets":                      {Table: "scanner_image_targets", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "derived target rows keyed by image identity; later admission for a gone scope is refused by authority"},
	"scanner_image_target_sources":               {Table: "scanner_image_target_sources", KeyColumns: []string{"scan_scope_id", "source_scope_id", "repository"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "kept with its scanner_image_targets parent; source and evidence pins are workload and registry identities"},
	"scanner_image_target_attempts":              {Table: "scanner_image_target_attempts", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "attempt history keyed by image identity"},
	"scanner_image_scan_results":                 {Table: "scanner_image_scan_results", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "scan receipts keyed by image identity"},
	"scanner_image_scan_fact_associations":       {Table: "scanner_image_scan_fact_associations", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "result associations keyed by image identity"},
	"scanner_image_result_fact_associations":     {Table: "scanner_image_result_fact_associations", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "result associations keyed by image identity"},
	"scanner_image_result_fact_classifications":  {Table: "scanner_image_result_fact_classifications", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "result classifications keyed by image identity"},
	"scanner_image_result_evidence_associations": {Table: "scanner_image_result_evidence_associations", KeyColumns: []string{"scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "evidence associations keyed by image identity"},
	"scanner_image_result_suppressions":          {Table: "scanner_image_result_suppressions", KeyColumns: []string{"scan_scope_id", "operator_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "operator-authored suppression decisions, not derived state"},
	"scanner_image_result_advisory_dependencies": {Table: "scanner_image_result_advisory_dependencies", KeyColumns: []string{"scan_scope_id", "advisory_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "dependency edges between image results and advisory generations"},
	// Publications and impact authority (#9299, migrations 241-263) carry
	// an impact_scope_id written only by the not-yet-landed publication
	// path, so no repository key exists to purge today. If impact scopes
	// ever name repository scopes, these rows owe a 2e sweep entry.
	"scanner_image_result_publications":                 {Table: "scanner_image_result_publications", KeyColumns: []string{"scan_scope_id", "impact_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "publication history; impact scopes are written only by the unlanded publication path, so no repository key exists to purge today"},
	"scanner_static_impact_finding_authority_revisions": {Table: "scanner_static_impact_finding_authority_revisions", KeyColumns: []string{"impact_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "impact authority revisions; same unlanded-path caveat as publications"},
	// Scanner advisory policy (#9299, migrations 254-257). Advisory scopes
	// are per-instance policy scopes (which scopes an instance may publish
	// advisories for), never repository scopes.
	"scanner_advisory_producer_grants":      {Table: "scanner_advisory_producer_grants", KeyColumns: []string{"advisory_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by advisory policy scopes, not repository scopes"},
	"scanner_advisory_generation_receipts":  {Table: "scanner_advisory_generation_receipts", KeyColumns: []string{"advisory_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by advisory policy scopes, not repository scopes"},
	"scanner_advisory_private_bindings":     {Table: "scanner_advisory_private_bindings", KeyColumns: []string{"advisory_scope_id", "scan_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "private advisory bindings over advisory policy scopes and image identities"},
	"scanner_advisory_public_activations":   {Table: "scanner_advisory_public_activations", KeyColumns: []string{"advisory_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by advisory policy scopes, not repository scopes"},
	"scanner_advisory_public_generations":   {Table: "scanner_advisory_public_generations", KeyColumns: []string{"pinned_advisory_scope_epoch"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "advisory generation epochs, not repository data"},
	"scanner_advisory_public_registrations": {Table: "scanner_advisory_public_registrations", KeyColumns: []string{"advisory_scope_id"}, RemoveSteps: []string{}, Disposition: "kept", Reason: "keyed by advisory policy scopes, not repository scopes"},
}

// RetireTableDispositionFor returns the census entry for one table, or
// false when the table is not classified (a bug the live census test
// turns into a failure, never a silent skip at runtime).
func RetireTableDispositionFor(table string) (RetireTableEntry, bool) {
	entry, ok := RetireTableDispositions[table]
	return entry, ok
}
