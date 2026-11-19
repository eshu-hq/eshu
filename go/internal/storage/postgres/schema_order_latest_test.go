// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

var orderedBootstrapDefinitionNamesLatest = []string{
	// Migration 291 (#9975) adds the five-expression partial index the
	// keyed winners resweep probe matches per dirty identifier.
	"fact_records_supply_chain_impact_winners_keyed_idx",
	// Migration 292 (#10512) adds the generation index backing the
	// retention row-count arm and intents DELETE.
	"shared_projection_intents_generation_idx",
	// Migration 293 (#10544) drops the triple-only package-lookup index
	// whose rows=1 cost tie kept the planner off the 291 keyed index.
	"drop_fact_records_supply_chain_impact_package_lookup_idx",
	// Migration 294 (#10525) adds the generation index backing the
	// retention work-items count arm and pre-delete lock. 293 went to
	// #10544, which rolled first, so this lane takes 294.
	"fact_work_items_generation_idx",
	// Migration 295 (#10566) seeds the synthetic advisory-groups
	// scope/generation anchor the coalesced projection rows re-home to.
	"advisory_groups_synthetic_scope",
	// Migration 296 (#10601 reopened) adds the resweep-maintained
	// collector-instance identity column on collector_evidence_summary.
	"collector_evidence_summary_instance_id",
	// Migration 297 (#10728) adds the per-run, per-year NVD baseline
	// resume checkpoints, separate from the window cursors.
	"vulnerability_nvd_baseline_checkpoints",
	// Migration 298 (#10768) adds the durable per-run NVD baseline trip
	// latch, so an open trip survives a coordinator restart.
	"vulnerability_nvd_baseline_trip_latches",
	// Migration 299 (#10767) adds the drain tally the NVD baseline trip
	// sampler reads inflow from, since the dirty log is drained on read.
	"supply_chain_impact_winners_dirty_tally",
	// Migration 300 (#10791) adds the open-items updated index the run-age
	// bound expiry candidate walk reads newest first; 299 went to #10767,
	// which rolled first.
	"workflow_items_open_updated_idx",
	// Migration 301 (#10809, #10810) adds the baseline resume CVE ID, the
	// seed generation stamp, and the chunk identity columns to the baseline
	// checkpoints. 300 is #10791's open work-items index.
	"vulnerability_nvd_baseline_resume_position",
	// Migration 302 (#10814) adds the pace streak to the NVD baseline trip
	// latch. 301 is #10809/#10810's resume position.
	"vulnerability_nvd_baseline_trip_latch_streak",
	// Migration 303 (#10827 R2) adds the dirty_version column the writer
	// upserts bump.
	"supply_chain_impact_winners_dirty_version",
	// Migration 304 (#10827 R2) collapses the append-only backlog to the
	// earliest mark per key.
	"supply_chain_impact_winners_dirty_dedup",
	// Migrations 305-306 (#10827 R2) add the partial unique indexes the
	// writer upserts target: the fact five-tuple and the scope pair.
	"supply_chain_impact_winners_dirty_log_fact_key_uniq",
	"supply_chain_impact_winners_dirty_log_scope_key_uniq",
	// Migrations 307-308 (#10827 R2) replace the 272 and 272a capture
	// trigger functions with key-ordered upserts. They land after the
	// indexes their ON CONFLICT clauses need.
	"supply_chain_impact_winners_dirty_capture_upsert",
	"supply_chain_impact_winners_scanner_capture_upsert",
	// Migration 309 (#10837 direction C) adds the shared staging queue the
	// capture owners plain-INSERT into: the table, its ordering sequence,
	// and its non-unique seq index. No unique index rides the hot path.
	"supply_chain_impact_winners_dirty_staging",
	// Migrations 310-311 (#10837 direction C) retarget the 272 and 272a
	// capture trigger functions to plain staging INSERTs, so no writer
	// touches the versioned log's conflict arbiter anymore.
	"supply_chain_impact_winners_staging_capture",
	"supply_chain_impact_winners_scanner_staging_capture",
	// Migration 312 (#10828) adds the per-year NVD baseline plan row the
	// admission transaction writes for each inserted baseline chunk. 311 is
	// #10837 direction C's scanner staging capture.
	"vulnerability_nvd_baseline_plans",
	// Migration 313 (#10874) sets per-leaf autovacuum reloptions on the
	// search-index terms leaves and documents table: catalog-only SET, no
	// rewrite, replay is a no-op.
	"search_index_terms_autovacuum_reloptions",
	// Migration 314 (#10947) adds the durable COALESCE-once first-shortfall
	// anchor for the code-call CALLS shortfall park bound.
	"shared_projection_intents_first_shortfall_at",
	// Migration 315 (#10875) adds the version-1 family/marker columns on
	// relationship_reference_candidate_keys plus the
	// security_alert_reconciliation_keys sidecar and its backfill marker.
	// 314 is #10947's shared-projection first-shortfall anchor.
	"relationship_lookup_keys",
	// Migration 316 (#10876) adds the reducer waiting set-aside: one row
	// per parked item keyed by waits-for kind+key, plain table plus two
	// indexes, no queue-table change. 315 is #10875's relationship
	// lookup keys.
	"reducer_waiting_items",
	// Migration 317 (#10878) adds the changed-evidence gate table for the
	// aws_cloud_runtime_drift reopen: one row per succeeded work item
	// carrying the ARN snapshot and fingerprint, mirroring #8283's 193.
	"aws_drift_reopen_fingerprints",
	// Migration 318 (#10878) adds the same gate for
	// container_image_identity: one row per succeeded work item carrying
	// the fleet identity epoch.
	"container_identity_reopen_epochs",
	// Migration 319 (#11021) adds the scope probe the 2e purge sweep needs
	// for parked waits of retired scopes.
	"reducer_waiting_items_scope_idx",
	// Migration 320 (#10878 follow-up) adds the audit clock to the drift
	// reopen fingerprints: last_audit_at per succeeded work item, NULL
	// meaning never audited and therefore due.
	"aws_drift_reopen_fingerprints_audit_clock",
	// Migration 317 (#10877) adds the workload and service-catalog reopen
	// fingerprint tables for the per-item read-set waits with late-arrival
	// restart. Renumbered 314 to 317 onto the #10874/#10947/#10875/#10876
	// base.
	"corpus_narrow_fingerprints",
}
