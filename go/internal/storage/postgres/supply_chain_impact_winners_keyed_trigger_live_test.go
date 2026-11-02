// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// winnersTrigger names one migration-272 trigger by table and name.
type winnersTrigger struct{ table, name string }

const (
	scopesTable = "ingestion_scopes"
	classTable  = "scanner_image_result_fact_classifications"
	assocTable  = "scanner_image_result_fact_associations"
)

var (
	scopePointerTrigger = winnersTrigger{scopesTable, "ingestion_scopes_winners_dirty_capture"}
	scopeDeleteTrigger  = winnersTrigger{scopesTable, "ingestion_scopes_winners_dirty_capture_delete"}
	classInsertTrigger  = winnersTrigger{classTable, "scanner_classifications_winners_dirty_insert"}
	classUpdateTrigger  = winnersTrigger{classTable, "scanner_classifications_winners_dirty_update"}
	classDeleteTrigger  = winnersTrigger{classTable, "scanner_classifications_winners_dirty_delete"}
	assocInsertTrigger  = winnersTrigger{assocTable, "scanner_associations_winners_dirty_insert"}
	assocDeleteTrigger  = winnersTrigger{assocTable, "scanner_associations_winners_dirty_delete"}
)

// TestSupplyChainImpactWinnersResweepTriggerCaptureViolationsAreCaughtLive is the
// seeded-violation pair for every production statement and DELETE path the
// migration-272 triggers cover (#9805, ruling B). Each case runs the same
// change twice on a settled table. In the RED arm the named trigger is
// disabled, which is the violation: the change must leave no dirty-log row of
// the wanted source and, where the change moves a winner, the table must
// diverge from a fresh recompute, so the parity proof would fail. In the GREEN
// arm the trigger is enabled: the change names its source in the dirty log and
// the keyed tick leaves the table equal to a recompute.
func TestSupplyChainImpactWinnersResweepTriggerCaptureViolationsAreCaughtLive(t *testing.T) {
	override := map[string]map[string]any{"op-real": {
		"cve_id": "CVE-2026-2002", "package_id": "pkg:npm/y", "impact_status": "affected_exact",
		"observed_version": "1.0.0", "repository_id": "repo-9805", "suppression_state": "accepted_risk",
		"suppression": map[string]any{"expires_at": "2099-01-01T00:00:00Z"},
	}}
	insertAssociation := func(h *keyedHarness, fact string) {
		h.exec(`INSERT INTO scanner_image_result_fact_associations
(finding_fact_id, publication_id, scan_scope_id, scan_generation_id, tenant_id, workspace_id)
VALUES ($1, 'pub-9805', 'scan-9805', 'scan-gen', 'tenant', 'workspace')`, fact)
	}
	insertClassification := func(h *keyedHarness, fact, class, lifecycle string) {
		h.exec(`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle)
VALUES ($1, $2, $3)`, fact, class, lifecycle)
	}
	newGeneration := func(h *keyedHarness, scopeB string) {
		h.addGeneration(scopeB, scopeB+"/g2", "pending")
		h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2003", "pkg:npm/z", 1))
	}

	for _, tc := range []struct {
		name       string
		triggers   []winnersTrigger
		wantSource string
		diverges   bool
		prepare    func(h *keyedHarness, scopeA, scopeB string)
		change     func(h *keyedHarness, scopeA, scopeB string)
	}{
		{
			name: "activation (projector Ack, projector_queue_sql.go activation)", triggers: []winnersTrigger{scopePointerTrigger},
			wantSource: "activation", diverges: true,
			prepare: func(h *keyedHarness, _, scopeB string) { newGeneration(h, scopeB) },
			change: func(h *keyedHarness, _, scopeB string) {
				h.activate(scopeB, scopeB+"/g1", scopeB+"/g2")
			},
		},
		{
			name: "reconcile (projector Fail, projector_queue_sql.go reconcile)", triggers: []winnersTrigger{scopePointerTrigger},
			wantSource: "activation", diverges: true,
			prepare: func(h *keyedHarness, _, scopeB string) {
				h.exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
  attempt_count, lease_owner, claim_until, created_at, updated_at)
VALUES ($1, $2, $3, 'projector', 'source_local', 'claimed', 1, 'owner-9805', now() + interval '1 hour', now(), now())`,
					"projector_"+scopeB+"_"+scopeB+"/g1", scopeB, scopeB+"/g1")
			},
			change: func(h *keyedHarness, _, scopeB string) {
				h.exec(postgres.FailProjectorWorkSQLForTest, h.clock, "synthetic_failure", "9805 proof", "{}",
					scopeB, scopeB+"/g1", "owner-9805", 1)
			},
		},
		{
			name: "retirement fence (repository_retirements_fence_sql.go)", triggers: []winnersTrigger{scopePointerTrigger},
			wantSource: "activation", diverges: true,
			change: func(h *keyedHarness, _, scopeB string) {
				h.exec(postgres.FenceNullActivePointerSQLForTest, []string{scopeB})
			},
		},
		{
			name: "collector upsert conflict arm with a non-NULL generation (ingestion_queries.go)", triggers: []winnersTrigger{scopePointerTrigger},
			wantSource: "activation", diverges: true,
			prepare: func(h *keyedHarness, _, scopeB string) {
				h.exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = $1`, scopeB+"/g1")
				h.addGeneration(scopeB, scopeB+"/g2", "active")
				h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2003", "pkg:npm/z", 1))
			},
			change: func(h *keyedHarness, _, scopeB string) {
				h.exec(postgres.UpsertIngestionScopeSQLForTest, scopeB, "vulnerability_intelligence", "synthetic", scopeB,
					nil, "synthetic", scopeB, keyedNow, keyedNow, "active", scopeB+"/g2", "{}")
			},
		},
		{
			name: "operator suppression scope activation", triggers: []winnersTrigger{scopePointerTrigger},
			wantSource: "operator", diverges: true,
			prepare: func(h *keyedHarness, _, _ string) {
				h.seedOperator("op-g1", "", map[string]map[string]any{
					"op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
				})
				h.keyed()
			},
			change: func(h *keyedHarness, _, _ string) {
				h.seedOperator("op-g2", "op-g1", map[string]map[string]any{"op-real": override["op-real"]})
			},
		},
		{
			name: "ingestion_scopes DELETE", triggers: []winnersTrigger{scopeDeleteTrigger},
			wantSource: "activation", diverges: true,
			prepare: func(h *keyedHarness, _, _ string) {
				// This case isolates the trigger: the foreign keys are dropped so the
				// facts survive and a missing capture shows as a stale winner. The
				// real-foreign-key sequence is the retirement test below.
				h.dropForeignKeysTo(scopesTable)
			},
			change: func(h *keyedHarness, _, scopeB string) {
				h.exec(`DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeB)
			},
		},
		{
			name: "classification INSERT, derived", triggers: []winnersTrigger{classInsertTrigger},
			wantSource: "scanner", diverges: true,
			prepare: func(h *keyedHarness, _, _ string) { h.dropForeignKeysOf(classTable) },
			change: func(h *keyedHarness, scopeA, _ string) {
				insertClassification(h, h.findingFactID(scopeA, "CVE-2026-2002"), "derived", "active")
			},
		},
		{
			name: "classification INSERT, static (captured, no winner moves)", triggers: []winnersTrigger{classInsertTrigger},
			wantSource: "scanner", diverges: false,
			prepare: func(h *keyedHarness, _, _ string) { h.dropForeignKeysOf(classTable) },
			change: func(h *keyedHarness, scopeA, _ string) {
				insertClassification(h, h.findingFactID(scopeA, "CVE-2026-2002"), "static", "active")
			},
		},
		{
			name: "classification UPDATE static to retracted", triggers: []winnersTrigger{classUpdateTrigger},
			wantSource: "scanner", diverges: true,
			prepare: func(h *keyedHarness, scopeA, _ string) {
				h.dropForeignKeysOf(classTable)
				insertClassification(h, h.findingFactID(scopeA, "CVE-2026-2002"), "static", "active")
				h.keyed()
			},
			change: func(h *keyedHarness, scopeA, _ string) {
				h.exec(`UPDATE scanner_image_result_fact_classifications SET lifecycle = 'retracted' WHERE finding_fact_id = $1`,
					h.findingFactID(scopeA, "CVE-2026-2002"))
			},
		},
		{
			name: "classification DELETE", triggers: []winnersTrigger{classDeleteTrigger},
			wantSource: "scanner", diverges: true,
			prepare: func(h *keyedHarness, scopeA, _ string) {
				h.dropForeignKeysOf(classTable)
				insertClassification(h, h.findingFactID(scopeA, "CVE-2026-2002"), "derived", "active")
				h.keyed()
			},
			change: func(h *keyedHarness, scopeA, _ string) {
				h.exec(`DELETE FROM scanner_image_result_fact_classifications WHERE finding_fact_id = $1`,
					h.findingFactID(scopeA, "CVE-2026-2002"))
			},
		},
		{
			name: "association INSERT", triggers: []winnersTrigger{assocInsertTrigger},
			wantSource: "scanner", diverges: true,
			prepare: func(h *keyedHarness, _, _ string) { h.dropForeignKeysOf(assocTable) },
			change: func(h *keyedHarness, scopeA, _ string) {
				insertAssociation(h, h.findingFactID(scopeA, "CVE-2026-2002"))
			},
		},
		{
			name: "association DELETE", triggers: []winnersTrigger{assocDeleteTrigger},
			wantSource: "scanner", diverges: true,
			prepare: func(h *keyedHarness, scopeA, _ string) {
				h.dropForeignKeysOf(assocTable)
				insertAssociation(h, h.findingFactID(scopeA, "CVE-2026-2002"))
				h.keyed()
			},
			change: func(h *keyedHarness, scopeA, _ string) {
				h.exec(`DELETE FROM scanner_image_result_fact_associations WHERE finding_fact_id = $1`,
					h.findingFactID(scopeA, "CVE-2026-2002"))
			},
		},
	} {
		setup := func(t *testing.T, proof string) (*keyedHarness, string, string) {
			h := newKeyedHarness(t, proof)
			scopeA, scopeB := keyedSeedSettled(h)
			if tc.prepare != nil {
				tc.prepare(h, scopeA, scopeB)
			}
			// Rows the prepare step queued (the writer's own fact capture) are not
			// the capture under test.
			h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
			return h, scopeA, scopeB
		}
		sourceRows := func(h *keyedHarness) int {
			return h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE source = $1`, tc.wantSource)
		}
		t.Run(tc.name+" without the trigger", func(t *testing.T) {
			h, scopeA, scopeB := setup(t, "#9805 trigger violation proof")
			for _, trigger := range tc.triggers {
				h.exec(`ALTER TABLE ` + trigger.table + ` DISABLE TRIGGER ` + trigger.name)
			}
			tc.change(h, scopeA, scopeB)
			if got := sourceRows(h); got != 0 {
				t.Fatalf("with the trigger disabled the change still appended %d %q rows: the mutant did not disable the capture", got, tc.wantSource)
			}
			if !tc.diverges {
				return
			}
			if _, err := h.store.ResweepWinnersKeyed(h.ctx, h.tick()); err != nil {
				t.Fatalf("keyed tick: %v", err)
			}
			if keyedMatchesRecompute(h) {
				t.Fatalf("without the trigger the table still equals a recompute: the test cannot detect a missing capture")
			}
		})
		t.Run(tc.name+" with the trigger", func(t *testing.T) {
			h, scopeA, scopeB := setup(t, "#9805 trigger capture proof")
			tc.change(h, scopeA, scopeB)
			if got := sourceRows(h); got == 0 {
				t.Fatalf("the change appended no dirty-log row with source %q", tc.wantSource)
			}
			result := h.keyed()
			// The maintainer counts trigger sources when it consumes them, so the
			// tick's result must carry the source the trigger wrote.
			counted := map[string]int64{
				"activation": result.TriggerCaptured.Activation,
				"operator":   result.TriggerCaptured.Operator,
				"scanner":    result.TriggerCaptured.Scanner,
			}[tc.wantSource]
			if counted < 1 {
				t.Fatalf("keyed tick reported %+v, want at least one %q row consumed", result.TriggerCaptured, tc.wantSource)
			}
			h.assertParity(tc.name)
		})
	}
}

// keyedMatchesRecompute compares the table to a fresh recompute without
// failing the test, so a seeded violation can assert the divergence.
func keyedMatchesRecompute(h *keyedHarness) bool {
	h.t.Helper()
	before := winnersLiveRead(h.t, h.ctx, h.db)
	winnersLiveExec(h.t, h.ctx, h.db, `DELETE FROM supply_chain_impact_canonical_winners`)
	if _, err := h.store.ResweepWinners(h.ctx, h.tick()); err != nil {
		h.t.Fatalf("recompute: %v", err)
	}
	after := winnersLiveRead(h.t, h.ctx, h.db)
	if len(before) != len(after) {
		return false
	}
	for key, row := range before {
		if other, ok := after[key]; !ok || other.content != row.content {
			return false
		}
	}
	return true
}

// TestSupplyChainImpactWinnersResweepRetirementFenceThenPurgeLive runs the
// production retirement order with the real foreign keys: the fence supersedes
// the generations and nulls the active pointer, then the purge deletes the
// scope row, whose cascade removes the facts, all before any keyed tick runs.
// The keyed tick must still repair every winner the retired scope fed, so the
// table equals a fresh recompute and the audit backstop finds nothing. The
// drop-the-foreign-keys DELETE case above covers the trigger alone; this one
// covers the sequence an operator actually triggers (#9805 review F1).
func TestSupplyChainImpactWinnersResweepRetirementFenceThenPurgeLive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(h *keyedHarness, scopeA, scopeB string)
		retire  func(scopeA, scopeB string) string
	}{
		{
			name:   "scope holding the winner of a contested key",
			retire: func(_, scopeB string) string { return scopeB },
		},
		{
			name:   "scope holding a losing candidate and a unique key",
			retire: func(scopeA, _ string) string { return scopeA },
		},
		{
			name: "scope with several generations",
			prepare: func(h *keyedHarness, _, scopeB string) {
				h.addGeneration(scopeB, scopeB+"/g2", "pending")
				h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-2001", "pkg:npm/x", 9), keyedFinding("CVE-2026-2003", "pkg:npm/z", 2))
				h.activate(scopeB, scopeB+"/g1", scopeB+"/g2")
				h.keyed()
				h.assertParity("after the second generation")
			},
			retire: func(_, scopeB string) string { return scopeB },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newKeyedHarness(t, "#9805 retirement fence then purge proof")
			scopeA, scopeB := keyedSeedSettled(h)
			if tc.prepare != nil {
				tc.prepare(h, scopeA, scopeB)
			}
			retired := tc.retire(scopeA, scopeB)

			// The production retirement sequence, back to back and before a tick.
			retireScope(h, retired, tc.name)
			if left := h.count(`SELECT count(*) FROM fact_records WHERE scope_id = $1`, retired); left != 0 {
				t.Fatalf("the purge left %d facts: the foreign key cascade did not run, so this is not the production sequence", left)
			}

			h.keyed()
			h.assertParity("after the retirement fence and purge")
			if named := h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE winner_scope_id = $1`, retired); named != 0 {
				t.Fatalf("%d stored winners still name the purged scope", named)
			}
			audit, err := h.store.AuditWinners(h.ctx)
			if err != nil || audit.Skipped || audit.Repairs != 0 {
				t.Fatalf("audit after the retirement = %+v, %v; want zero repairs (the capture left no hole)", audit, err)
			}
		})
	}
}

// TestSupplyChainImpactWinnersResweepScopeTriggersRunWithoutLogPrivilegesLive
// binds a login-like role that may move and delete scope rows and nothing else:
// no privilege on the dirty log, its sequence or fact_records. The pointer and
// delete triggers are SECURITY DEFINER, so the projector, collector, retirement
// fence and suppression store need no grant on the log (#9805 review F2). The
// control statement shows the role really has no INSERT on the log, so a trigger
// that ran as the caller would fail here with permission denied.
func TestSupplyChainImpactWinnersResweepScopeTriggersRunWithoutLogPrivilegesLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 definer trigger role proof")
	scopeA, scopeB := keyedSeedSettled(h)
	// A production (public) bootstrap holds all four definer functions with the
	// pinned path pg_catalog, public, pg_temp: the scanner pair from 272a, the pointer and
	// delete pair from 272.
	assertWinnersCaptureFunctions(h.ctx, t, h.db, "public", 4)
	conn := logBlindConn(h)
	if _, err := conn.ExecContext(h.ctx, `INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id)
VALUES ('scope', 'activation', 'x', 'y')`); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("control insert as the role = %v, want permission denied: the role must have no log privilege", err)
	}
	if _, err := conn.ExecContext(h.ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, scopeA); err != nil {
		t.Fatalf("pointer move as a role without log privileges: %v", err)
	}
	if got := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope' AND scope_id = $1`, scopeA); got != 1 {
		t.Fatalf("pointer trigger wrote %d scope rows for the role's UPDATE, want 1", got)
	}
	if _, err := conn.ExecContext(h.ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeB); err != nil {
		t.Fatalf("scope delete as a role without log or fact privileges: %v", err)
	}
	if got := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact' AND source = 'activation'`); got < 1 {
		t.Fatalf("delete trigger wrote %d fact rows for the role's DELETE, want at least 1", got)
	}
}
