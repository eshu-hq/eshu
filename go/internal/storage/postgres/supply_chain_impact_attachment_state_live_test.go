// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// TestReducerContentionGateSBOMAttachmentProducerStateScopesToTheGeneration
// pins the attachment probe's lifecycle semantics against the real schema:
// only the requested scope generation's sbom_attestation_attachment rows
// count; the impact row of the same generation, the same domain at another
// generation, and another scope's row never do.
func TestReducerContentionGateSBOMAttachmentProducerStateScopesToTheGeneration(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	store := producerreadiness.ImpactStore{DB: SQLDB{DB: sqlDB}}
	attempt := time.Now().UTC().Add(-10 * time.Minute)
	const attachment = "sbom_attestation_attachment"

	seedImpactProducerScope(t, ctx, sqlDB, "scope-attach-a", "gen-a1", "gen-a2")
	seedImpactProducerScope(t, ctx, sqlDB, "scope-attach-b", "gen-b1")
	seedImpactProducerRow(t, ctx, sqlDB, "a1-attach-retrying", "scope-attach-a", "gen-a1", attachment, "retrying", &attempt)
	seedImpactProducerRow(t, ctx, sqlDB, "a1-attach-dead", "scope-attach-a", "gen-a1", attachment, "dead_letter", nil)
	seedImpactProducerRow(t, ctx, sqlDB, "a1-impact", "scope-attach-a", "gen-a1", "supply_chain_impact", "pending", nil)
	seedImpactProducerRow(t, ctx, sqlDB, "a2-attach", "scope-attach-a", "gen-a2", attachment, "pending", nil)
	seedImpactProducerRow(t, ctx, sqlDB, "b1-attach", "scope-attach-b", "gen-b1", attachment, "claimed", nil)

	got, err := store.SBOMAttestationAttachmentState(ctx, "scope-attach-a", "gen-a1")
	if err != nil {
		t.Fatalf("SBOMAttestationAttachmentState() error = %v", err)
	}
	if got.Retrying != 1 || got.Failed != 1 || got.Total != 2 || got.InFlight() != 1 || got.Pending != 0 {
		t.Fatalf("scope-attach-a/gen-a1 = %+v, want 1 retrying + 1 dead_letter of 2 (no impact, other-generation, or other-scope rows)", got)
	}
	if got.NewestInFlightWorkableAt.Before(attempt.Add(-time.Second)) {
		t.Fatalf("anchor = %s, want at least the retrying row's last attempt %s", got.NewestInFlightWorkableAt, attempt)
	}
	impact, err := store.ImpactProducerState(ctx, "scope-attach-a", "gen-a1")
	if err != nil || impact.Total != 1 || impact.Pending != 1 {
		t.Fatalf("ImpactProducerState() = %+v, %v; want the one impact row only (domain parameter honored)", impact, err)
	}
	absent, err := store.SBOMAttestationAttachmentState(ctx, "scope-attach-b", "gen-b9")
	if err != nil || absent.Total != 0 || !absent.NewestInFlightWorkableAt.IsZero() {
		t.Fatalf("absent generation = %+v, %v; want the zero state", absent, err)
	}
}

// TestReducerContentionGateSBOMAttachmentProducerStatePlanUsesScopeGenerationIndex
// is the #11105 theory proof and plan guard: 2,000 scopes x 5 generations x 35
// domains = 350,000 reducer rows, one of the 35 domains
// sbom_attestation_attachment. The probe must read only the one scope
// generation's rows through fact_work_items_scope_generation_idx, never a
// sequential scan; the ANALYZE plan is logged for the PR evidence.
func TestReducerContentionGateSBOMAttachmentProducerStatePlanUsesScopeGenerationIndex(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	for _, statement := range []string{
		`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
                              observed_at, ingested_at, status, payload)
SELECT 'plan11105-' || s, 'container_image', 'oci', 'plan11105-' || s, 'oci_registry', 'p', now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, 2000) AS s`, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'plan11105-' || s || '-g' || g, 'plan11105-' || s, 'snapshot', now(), now(),
       CASE WHEN g = 5 THEN 'active' ELSE 'superseded' END
FROM generate_series(1, 2000) AS s CROSS JOIN generate_series(1, 5) AS g`, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
                             status, payload, created_at, updated_at)
SELECT 'plan11105-' || s || '-g' || g || '-' || d, 'plan11105-' || s, 'plan11105-' || s || '-g' || g, 'reducer',
       CASE d WHEN 0 THEN 'supply_chain_impact' WHEN 1 THEN 'sbom_attestation_attachment' ELSE 'domain_' || d END,
       'scope', 'plan11105-' || s || '-g' || g || '-' || d,
       CASE WHEN d = 1 AND g = 5 AND s % 10 = 0 THEN 'retrying' ELSE 'succeeded' END, '{}'::jsonb, now(), now()
FROM generate_series(1, 2000) AS s CROSS JOIN generate_series(1, 5) AS g CROSS JOIN generate_series(0, 34) AS d`,
	} {
		if _, err := sqlDB.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed plan rows: %v", err)
		}
	}
	for _, table := range []string{"ingestion_scopes", "scope_generations", "fact_work_items"} {
		if _, err := sqlDB.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	var rows int
	if err := sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id LIKE 'plan11105-%'`).Scan(&rows); err != nil || rows != 350000 {
		t.Fatalf("seeded rows = %d, err = %v, want 350000", rows, err)
	}

	var planJSON string
	if err := sqlDB.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+producerreadiness.ImpactProducerStateQuery,
		"plan11105-10", "plan11105-10-g5", "sbom_attestation_attachment").Scan(&planJSON); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if seqScanOn(plan[0]["Plan"], "fact_work_items") {
		t.Fatalf("attachment producer probe sequentially scans fact_work_items:\n%s", planJSON)
	}
	if !strings.Contains(planJSON, "fact_work_items_scope_generation_idx") {
		t.Fatalf("attachment producer probe does not use fact_work_items_scope_generation_idx:\n%s", planJSON)
	}
	top, _ := plan[0]["Plan"].(map[string]any)
	t.Logf("plan (350,000 rows): planning=%v ms execution=%v ms shared_hit_blocks=%v shared_read_blocks=%v",
		plan[0]["Planning Time"], plan[0]["Execution Time"], top["Shared Hit Blocks"], top["Shared Read Blocks"])
	t.Logf("plan json:\n%s", planJSON)

	store := producerreadiness.ImpactStore{DB: SQLDB{DB: sqlDB}}
	waiting, err := store.SBOMAttestationAttachmentState(ctx, "plan11105-10", "plan11105-10-g5")
	if err != nil || waiting.Retrying != 1 || waiting.Total != 1 {
		t.Fatalf("SBOMAttestationAttachmentState() = %+v, %v; want 1 retrying of 1", waiting, err)
	}
	settled, err := store.SBOMAttestationAttachmentState(ctx, "plan11105-11", "plan11105-11-g5")
	if err != nil || settled.Succeeded != 1 || settled.Total != 1 || settled.InFlight() != 0 {
		t.Fatalf("SBOMAttestationAttachmentState() = %+v, %v; want 1 succeeded of 1", settled, err)
	}
}
