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
	// Migration 321 (#10877) adds the workload and service-catalog reopen
	// fingerprint tables for the per-item read-set waits with late-arrival
	// restart. Renumbered 314→317→319→320 across base moves, then 320 to
	// 321 onto the base carrying 320_aws_drift_audit_clock.
	"corpus_narrow_fingerprints",
	// Migration 322 (#10879 phase 1) adds the reducer lease sidecar:
	// the fencing-triple mirror with fillfactor 70 and the per-stage
	// conflict unique. Main columns stay authoritative; no read switch.
	// 317/318 are taken by #11018, 319 by #11021, 320 by the #10878
	// audit-clock follow-up, 321 by #10877 (all merged), so this lane
	// takes 322/323.
	"fact_work_item_leases",
	// Migration 323 (#10879 phase 1) is the idempotent live-lease
	// backfill, split from 322 per #7961.
	"fact_work_item_leases_backfill",
	// Migration 324 (#10879 follow-up) reconciles backfill-raced mirror
	// rows with a locking anti-join; DML-only per #7961.
	"fact_work_item_leases_reconcile",
	// Migration 325 (#10671) adds the projector admission-wait counter
	// on fact_work_items so admission-timeout failures retry against
	// their own finite budget instead of the shared projection budget.
	"projector_admission_wait_attempts",
	// Migration 326 (#11081) deletes leftover reducer lease rows under the
	// claim-time liveness definition; DML-only per #7961.
	"fact_work_item_leases_11081_cleanup",
	// Migration 327 (#11082) adds the drive-from-fact partial index for
	// the code-import ownership load.
	"fact_records_package_ownership_active_idx",
	// Migration 328 (#11078) adds the GHSA-proxy bucket column to the
	// winners dirty tally so the NVD/GHSA inflow split stops reading
	// NOT_CHECKED.
	"supply_chain_impact_winners_dirty_tally_ghsa",
	// Migration 329 (#11117) adds the nullable winners contract version and
	// hash to the winners watermark row (renumbered from 328: #11078 owns 328).
	"supply_chain_impact_winners_contract",
	// Migration 330 (#11129) adds the captured_at btree on the winners
	// staging queue for the exact residual minimum.
	"supply_chain_impact_winners_dirty_staging_captured_at_idx",
	// Migration 331 (#10880 slice 1) adds the winner payload and
	// source_confidence to the canonical winners read model
	// (renumbered 325 to 331 at rebase: main owns 325-330).
	"supply_chain_impact_winners_payload",
	// Migration 332 (#10880 slice 1 follow-up) backfills the 331
	// columns for pre-existing winner rows, split per #7961
	// (renumbered 326 to 332 at rebase: main owns 325-330).
	"supply_chain_impact_winners_payload_backfill",
	// Migration 333 (#10882) adds the projector source-paced wait counter
	// so a per-source backpressure release refunds both attempt budgets
	// and never spends the admission budget (331 and 332 belong to the
	// winners payload migrations).
	"projector_source_paced_waits",
	// Migration 334 (#11182) adds the projector replay baseline on
	// fact_work_items and its two audit columns on fact_replay_events
	// (numbered 334: 333 is held by the #10882 lane).
	"fact_work_items_replay_attempt_baseline",
	// Migration 335 (#11131) adds the staged winners audit sweep's cursor
	// table (renumbered 331 to 335 at rebase: main owns 331-332 and 334;
	// 333 held by an unpushed lane).
	"supply_chain_impact_winners_audit_sweep",
	// Migrations 336-337 (#10643 PR 2) add the frozen foreign-member token
	// function and its concurrent GIN index for the key-range holder read
	// (numbered 336/337, the next free numbers after main's 333-335).
	"eshu_sci_foreign_member_tokens_v4",
	"fact_records_sci_foreign_member_tokens_v4_idx",
	// Migration 338 (#10880 PR-B1) adds the per-support table behind the
	// grant-scoped impact reads (333 to 337 are main's).
	"supply_chain_impact_finding_supports",
	// Migrations 339-342 (#11197) add the four holder-read indexes on the
	// per-support table, one concurrent build per file.
	"supply_chain_impact_finding_supports_cve_idx",
	"supply_chain_impact_finding_supports_advisory_idx",
	"supply_chain_impact_finding_supports_members_idx",
	"supply_chain_impact_finding_supports_tokens_idx",
	// Migrations 343-344 (#10880 PR-B2a) add the two partial indexes behind the
	// unscoped existence-filter reads of that table.
	"supply_chain_impact_finding_supports_precise_key_ordered_idx",
	"supply_chain_impact_finding_supports_image_ref_idx",
	// Migration 345 (#10880 PR-B2c) adds the partial package_id btree on the
	// supports table (339 to 342 are PR-B2b's, 343 and 344 PR-B2a's).
	"supply_chain_impact_supports_package_idx",
	// Migration 346 (#10880 PR-B2c) adds the partial (repository_id, fact_id) btree
	// on the supports table.
	"supply_chain_impact_supports_repository_fact_idx",
	// Migration 347 (#11376) adds the folded-ecosystem partial index
	// serving the package-keyed holder read's ecosystem arm, built
	// CONCURRENTLY like migrations 156 and 211.
	"fact_records_vuln_pkg_eco_scope_gen_active_idx",
	// Migration 348 (#10880 decision 6) adds the resumable verification cursor
	// to the supports backfill marker.
	"supply_chain_impact_finding_supports_backfill_verify_cursor",
	// Migration 349 (#11376) adds the single-expression extended
	// statistics object on the same folded ecosystem expression holding
	// the eco arm on its partial index (CREATE STATISTICS plus ANALYZE),
	// like migrations 283 and 290.
	"fact_records_vuln_pkg_eco_stats",
	// Migration 350 (#10880 PR-C1 a) adds the nullable stable_fact_key column to
	// the supports table (an ALTER, no new relation: this names its migration file).
	"supply_chain_impact_finding_supports_stable_fact_key",
	// Migration 351 (#10880 PR-C1 c) adds the retracted finding keys table the
	// recovery reads take over from tombstoned fact rows.
	"supply_chain_impact_retracted_finding_keys",
	// Migrations 352 and 353 (#10880 PR-C1 e) replace the winners dirty-capture
	// functions that read finding facts with supports-reading ones.
	"supply_chain_impact_winners_supports_scope_capture",
	"supply_chain_impact_winners_supports_scanner_capture",
	// Migration 354 (#10880 PR-C1 index set g) adds the keyed probe btree on
	// the supports table.
	"supply_chain_impact_supports_keyed_probe_idx",
	// Migration 355 (#10880 PR-C1) redefines the scope-delete capture over
	// finding facts and supports so a pre-backfill retirement still stages.
	"supply_chain_impact_winners_scope_capture_facts_and_supports",
	// Migration 356 (#10972) adds the stored-side identifier index for the
	// staged-audit single-scan range, built CONCURRENTLY.
	"supply_chain_impact_winners_canonical_ident_idx",
	// Migration 357 (#11031) adds the durable set-once first-seen stamp
	// table backing per-source publish-to-finding lag measurement.
	"supply_chain_impact_finding_first_seen",
	// Migration 358 (#10880 PR-C2 k) installs the reject trigger on fact_records for
	// the finding kind (a function and a trigger, no new relation).
	"supply_chain_impact_finding_fact_write_stop",
	// Migration 359 (#11466) adds the durable set-once-per-value
	// enrichment first-carry stamp table backing per-source
	// enrichment-arrival-to-rescore measurement.
	"supply_chain_impact_winners_enrichment_first_carry",
	// Migration 360 (#7584) adds the partial trigram GIN serving the
	// targeted anchor-scoped relationship fact load's lower(payload::text)
	// LIKE ANY predicate, built CONCURRENTLY.
	"fact_records_anchor_payload_trgm_idx",
	// Migration 361 (#11613) adds the holder freshness watermark columns
	// to the scope pass state (token, generation, evidence-complete), all
	// nullable with no default or backfill, so the reopen can compare the
	// origin token against the last proven pass instead of wall times.
	"supply_chain_impact_scope_pass_state_watermark",
	// Migration 362 (#9975 round 6) adds the key_presence bitmask column on
	// the supports table the triple-guarded keyed probe reads, NOT NULL
	// DEFAULT 0 (fail-closed until the key-presence fill completes).
	"supply_chain_impact_supports_key_presence",
	// Migration 363 (#9975 round 6) adds the covering btree for the
	// triple-guarded keyed probe on the plain supports columns, built
	// CONCURRENTLY (migration 354 stays for the pre-marker fallback probe).
	"supply_chain_impact_supports_keyed_triple_guard_idx",
	// Migration 364 (#11615) adds the nullable last_requeue_reason label on
	// the shared fact_work_items row, written inside every existing guarded
	// requeue UPDATE.
	"fact_work_items_last_requeue_reason",
	// Migration 365 (#11615) converges the 093 cross-scope ACK trigger to
	// stamp cross_scope_replay on the conversion it performs.
	"cross_scope_ack_trigger_requeue_reason",
	// Migration 366 (#11120 lane A) adds the covering winner btree for the
	// TOP-1-per-key keyed probe: the identifier prefix plus the complete
	// ranking order as key columns, built CONCURRENTLY (migrations 354 and
	// 363 stay for the pre-marker fallback and the chunked-full path).
	"supply_chain_impact_supports_covering_winner_idx",
}
