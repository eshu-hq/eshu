// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactSupportsAdvisoryGroupWinnersReadLive is the #10880 PR-C2 proof for the one reader of
// finding payloads that is neither a route nor an audit (the advisory group winners read): with the reject trigger
// ARMED, the production writer leaves a finding only in the supports table, the production resweep derives the winner
// from supports, and the read still returns the purl and the provenance members. Before the supports-first join the
// read took both from fact_records, so every new winner came back with an empty purl and no members and no error.
// It complements TestAdvisoryGroupWinnersQualifyingLive, which seeds finding facts with the stop disarmed.
func TestSupplyChainImpactSupportsAdvisoryGroupWinnersReadLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the supports-only advisory group winners proof")
	}
	// The family DSN names the proof's working database (for example "eshu" in
	// the reducer contention gate); the disposable helpers require the
	// administrative "postgres" database, so derive it the way
	// openIsolatedLiveDB does instead of passing the family DSN through.
	adminDSN := disposableAdminDSN(t, dsn)
	template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		postgresproof.FlavourPublicBootstrap,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	ctx, db := postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)
	if armed, err := postgresproof.FindingWriteStopArmed(ctx, db); err != nil || !armed {
		t.Fatalf("proof clone armed = %v, err = %v, want armed: the proof would not show that the read works without finding facts", armed, err)
	}

	const scopeID, generationID, factID, group = "agw-supports-scope", "agw-supports-gen", "agw-supports-evidence", "CVE-10880-AGW"
	now := time.Now().UTC().Truncate(time.Second)
	mustExecProofRow(t, ctx, db, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind,
		 partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'vulnerability_intelligence','osv',$1,'osv',$1,$2,$2,'active','{}'::jsonb)`, scopeID, now)
	mustExecProofRow(t, ctx, db, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
		VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`, generationID, scopeID, now)
	mustExecProofRow(t, ctx, db, `UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`, generationID, scopeID)

	writer := supplychaincore.PostgresSupplyChainImpactWriter{DB: SupplyChainImpactBeginner{Beginner: SQLDB{DB: db}}}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
		FencingToken: 1, IntentID: "intent-agw-supports", ScopeID: scopeID, GenerationID: generationID,
		SourceSystem: "osv",
		Findings: []supplychaincore.SupplyChainImpactFinding{{
			CVEID: group, PackageID: "pkg-agw-supports", Ecosystem: "npm",
			PURL:   "pkg:npm/pkg-agw-supports@1.2.3",
			Status: supplychaincore.SupplyChainImpactAffectedExact, RepositoryID: "repo-agw-supports",
			EvidenceFactIDs:      []string{factID},
			AdvisoryGroupMembers: []string{"GHSA-10880-m1", "GHSA-10880-m2"},
		}},
	}); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want nil", err)
	}
	var facts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = 'reducer_supply_chain_impact_finding'`).Scan(&facts); err != nil || facts != 0 {
		t.Fatalf("finding facts = %d (err %v), want 0: the proof must read supports only", facts, err)
	}
	backfill := SupplyChainImpactSupportsBackfill{DB: SQLDB{DB: db}, ChunkSize: 200}
	for step := 0; step < 100; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step: %v", err)
		}
		if report.Complete && report.DirtyScopes == 0 {
			break
		}
	}
	if result, err := NewSupplyChainImpactWinnersStore(SQLDB{DB: db}).ResweepWinners(ctx); err != nil || result.Skipped {
		t.Fatalf("ResweepWinners() = %+v, %v, want a completed resweep", result, err)
	}

	got, err := NewFactStore(SQLDB{DB: db}).QualifyingAdvisoryGroupWinners(ctx, group)
	if err != nil {
		t.Fatalf("QualifyingAdvisoryGroupWinners() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("QualifyingAdvisoryGroupWinners() = %#v, want the one supports-only winner", got)
	}
	row := got[0]
	if row.PURL != "pkg:npm/pkg-agw-supports@1.2.3" {
		t.Fatalf("purl = %q, want the supports payload purl (an empty purl is the fact_records-only join)", row.PURL)
	}
	if len(row.Members) != 2 || row.Members[0] != "GHSA-10880-m1" || row.Members[1] != "GHSA-10880-m2" {
		t.Fatalf("members = %q, want the supports payload provenance members", row.Members)
	}
}
