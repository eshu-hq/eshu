// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The role half of the finding-fact write stop (#10880 PR-C2). The superuser live test proves
// what the trigger raises; this one proves it for the login that runs in production. Two
// things differ for a restricted role and neither shows up as the owner:
//
//   - The trigger must be what refuses the write. PostgreSQL checks table privileges before
//     row triggers fire, so a role without INSERT on fact_records would fail with 42501 and
//     the classified 55000 would never be exercised. The role here holds the INSERT grant
//     the other reducer domains need, and the test writes another kind first to prove it.
//   - The armed check reads pg_trigger as that role.
//
// Grant list source: the proof's own pregrants. grantInventoryRequired issues every required
// runtime-writer row of eshu_security.role_acl_inventory (migration 243a) and
// grantImpactWriterPregrants the base-table DML the impact writer issues outside any
// SECURITY DEFINER body. No deployment artifact in this repository lists those pregrants (the
// 9805 evidence note names them a DBA pregrant outside the migration set), so this proves the
// code under the stated grants, not that a deployed login carries them.

const (
	roleWriteStopKind      = "reducer_supply_chain_impact_finding"
	roleWriteStopOtherKind = "reducer_container_image_identity"
	roleWriteStopInsert    = `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
    observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ($1, 'role-scope', 'role-generation', $2, $1, 'synthetic', $1, now(), now(), FALSE, '{}'::jsonb, 1)`
)

// TestSupplyChainImpactSupportsFactWriteStopAsRuntimeWriterLive drives the stop as the contracted
// runtime-writer login on an armed clone.
func TestSupplyChainImpactSupportsFactWriteStopAsRuntimeWriterLive(t *testing.T) {
	proof := openProtectedPublishProof(t)
	ctx, admin, worker := proof.ctx, proof.admin, proof.worker

	t.Run("a finding fact write is refused by the trigger, not by a missing privilege", func(t *testing.T) {
		// The role may write another kind: the refusal below is the trigger's.
		if _, err := worker.ExecContext(ctx, roleWriteStopInsert, "role-other-kind", roleWriteStopOtherKind); err != nil {
			t.Fatalf("runtime-writer insert of another fact kind: %v (the role lacks the fact_records grant, so a refusal below would prove nothing)", err)
		}
		_, err := worker.ExecContext(ctx, roleWriteStopInsert, "role-finding", roleWriteStopKind)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("runtime-writer finding insert error = %v (%T), want a *pgconn.PgError", err, err)
		}
		if pgErr.Code != "55000" || pgErr.ConstraintName != "fact_records_reject_supply_chain_impact_finding" {
			t.Fatalf("PgError code=%q constraint=%q, want 55000 and fact_records_reject_supply_chain_impact_finding (42501 would be a missing grant, not the stop)", pgErr.Code, pgErr.ConstraintName)
		}

		tx, err := (SupplyChainImpactBeginner{Beginner: SQLDB{DB: worker}}).BeginSupplyChainImpactTx(ctx)
		if err != nil {
			t.Fatalf("begin the impact transaction as the runtime-writer: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		_, execErr := tx.ExecContext(ctx, roleWriteStopInsert, "role-finding-tx", roleWriteStopKind)
		var stopped *SupplyChainImpactFactWriteStoppedError
		if !errors.As(execErr, &stopped) {
			t.Fatalf("impact transaction error = %v, want *SupplyChainImpactFactWriteStoppedError", execErr)
		}
	})

	t.Run("the armed check reads pg_trigger as the runtime-writer", func(t *testing.T) {
		watch := &SupplyChainImpactFactWriteStopWatch{DB: SQLDB{DB: worker}}
		armed, err := watch.Refresh(ctx)
		if err != nil || !armed {
			t.Fatalf("Refresh() as the runtime-writer = %v, %v, want true, nil", armed, err)
		}
		// The read must report a disabled trigger too, not a constant: it answers from pg_trigger.
		// A test of C2 code uses the scoped form, not the whole-test disarm. Refresh only
		// reads pg_trigger and issues no fact write, so running it in the window cannot
		// mask a 55000; the deferred re-arm still verifies the stop is back on.
		postgresproof.SeedFindingFacts(t, admin, func() {
			armed, err = watch.Refresh(ctx)
			if err != nil || armed {
				t.Errorf("Refresh() with the trigger disabled = %v, %v, want false, nil", armed, err)
			}
		})
	})
}

// TestSupplyChainImpactWriterPathLeavesFindingsInSupportsAsRuntimeWriterLive runs the
// production writer as the runtime-writer on an armed clone: the finding lands in the supports
// table, no finding fact is written, and a second pass with the finding gone retracts it
// through the role's DELETE ... RETURNING on supports.
func TestSupplyChainImpactWriterPathLeavesFindingsInSupportsAsRuntimeWriterLive(t *testing.T) {
	proof := openProtectedPublishProof(t)
	ctx, admin, worker := proof.ctx, proof.admin, proof.worker

	const scopeID, generationID, factID = "role-supports-scope", "role-supports-gen", "role-supports-fact"
	now := time.Now().UTC().Truncate(time.Second)
	mustExecProofRow(t, ctx, admin, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind,
		 partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'container_image','synthetic',$1,'synthetic',$1,$2,$2,'active','{}'::jsonb)`, scopeID, now)
	mustExecProofRow(t, ctx, admin, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at,
		 status, activated_at, payload)
		VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`, generationID, scopeID, now)
	mustExecProofRow(t, ctx, admin, `UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`, generationID, scopeID)

	write := supplychaincore.SupplyChainImpactWrite{
		FencingToken: 7, IntentID: "intent-role-supports", ScopeID: scopeID, GenerationID: generationID,
		SourceSystem: "vulnerability_intelligence",
		Findings: []supplychaincore.SupplyChainImpactFinding{{
			CVEID: "CVE-2026-10880-1", PackageID: "pkg:npm/role-supports",
			Status: supplychaincore.SupplyChainImpactAffectedExact, RepositoryID: "repo://role-supports/api",
			EvidenceFactIDs: []string{factID},
		}},
		StaticNonAdvisoryEvidenceFactIDs: []string{factID},
		ProtectedCompleteSet:             true,
	}
	writer := supplychaincore.PostgresSupplyChainImpactWriter{DB: SupplyChainImpactBeginner{Beginner: SQLDB{DB: worker}}}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() as the runtime-writer error = %v, want nil", err)
	}
	count := func(query string) int {
		var n int
		if err := admin.QueryRowContext(ctx, query, scopeID, generationID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id=$1 AND generation_id=$2`); n != 1 {
		t.Fatalf("supports after the role's write = %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM fact_records WHERE scope_id=$1 AND generation_id=$2 AND fact_kind='reducer_supply_chain_impact_finding'`); n != 0 {
		t.Fatalf("finding facts after the role's write = %d, want 0 (the writer no longer writes them)", n)
	}

	// A later pass whose finding set is empty retracts the support as the same role.
	retract := write
	retract.FencingToken, retract.IntentID, retract.Findings, retract.StaticNonAdvisoryEvidenceFactIDs = 8, "intent-role-retract", nil, nil
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, retract); err != nil {
		t.Fatalf("retracting pass as the runtime-writer error = %v, want nil", err)
	}
	if n := count(`SELECT count(*) FROM supply_chain_impact_finding_supports WHERE scope_id=$1 AND generation_id=$2`); n != 0 {
		t.Fatalf("supports after the retracting pass = %d, want 0", n)
	}
	if n := count(`SELECT count(*) FROM supply_chain_impact_retracted_finding_keys WHERE scope_id=$1 AND generation_id=$2`); n < 1 {
		t.Fatalf("retracted keys after the retracting pass = %d, want the retracted support's key recorded", n)
	}
}
