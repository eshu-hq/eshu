// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9855 disposable PostgreSQL proof that the engaged authorized loader lists the folded parser-file IDs, not the fold's synthetic digest ID, so the protected writer publishes a fold-sourced reachability finding and still refuses unknown, digest and tombstoned IDs
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

type foldStatsLoader interface {
	ListActiveSupplyChainImpactFactsWithStats(context.Context, supplychaincore.SupplyChainImpactFactFilter) ([]facts.Envelope, supplychaincore.SupplyChainImpactActiveEvidenceLoadStats, error)
}

// TestAuthorizedLoaderEngagedFoldPublishesFoldedEvidenceLive pins the engaged
// half of the #9855 fix. It drives the real engaged branch of the authorized
// loader (role policy installed, repeatable-read snapshot, advisory and target
// authority reads), folds JS/TS files through the real snapshot FactStore SQL,
// then publishes through the real protected writer as a contracted
// runtime-writer. The disengaged loop's list is discarded by the handler, so
// only this test fails when the engaged loop lists the digest's synthetic ID.
func TestAuthorizedLoaderEngagedFoldPublishesFoldedEvidenceLive(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if adminDSN == "" {
		t.Skip("needs ESHU_POSTGRES_DSN")
	}
	ctx, admin := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})

	const roleName = "fold_engaged_worker"
	const rolePassword = "local-proof-only"
	mustExecProofRow(t, ctx, admin, `DROP ROLE IF EXISTS "`+roleName+`"`)
	mustExecProofRow(t, ctx, admin, `CREATE ROLE "`+roleName+`" LOGIN PASSWORD '`+rolePassword+`'`)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP OWNED BY "`+roleName+`"; DROP ROLE IF EXISTS "`+roleName+`"`)
	})
	var roleOID int64
	if err := admin.QueryRowContext(ctx, `SELECT oid::bigint FROM pg_roles WHERE rolname=$1`, roleName).Scan(&roleOID); err != nil {
		t.Fatal(err)
	}
	const policyRevision = 985501
	mustExecProofRow(t, ctx, admin, `INSERT INTO eshu_security.role_policy_state
		(policy_revision, database_oid, database_identity, system_identifier,
		 inventory_version, owner_role_oid, authority_epoch, active)
		SELECT $1, (SELECT oid FROM pg_database WHERE datname=current_database()), '11111111-2222-4333-8444-555555555555',
		 (SELECT system_identifier FROM pg_control_system()), 1,
		 (SELECT oid FROM pg_roles WHERE rolname=current_user), 1, TRUE`, policyRevision)
	mustExecProofRow(t, ctx, admin, `INSERT INTO eshu_security.role_contracts
		(contract_id, policy_revision, workload_class, connection_route, profile,
		 session_role_oid, effective_role_oid, enabled)
		VALUES ('fold-engaged-writer',$1,'scanner-worker','writer','runtime-writer',$2,$2,TRUE)`, policyRevision, roleOID)
	mustExecProofRow(t, ctx, admin, `INSERT INTO scanner_advisory_desired_policy (desired_revision, desired_sha256) VALUES (1,$1)`, strings.Repeat("0", 64))
	var dbName string
	if err := admin.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatal(err)
	}
	grantInventoryRequired(t, ctx, admin, roleName, "runtime-writer")
	for _, s := range []string{
		`GRANT CONNECT ON DATABASE "` + dbName + `" TO "` + roleName + `"`,
		`GRANT SELECT, INSERT, UPDATE ON supply_chain_impact_write_admission TO "` + roleName + `"`,
		`GRANT SELECT, INSERT, UPDATE ON fact_records TO "` + roleName + `"`,
		`GRANT SELECT ON scope_generations TO "` + roleName + `"`,
		// The impact writer names the winners it changed in the same transaction
		// (#9805), so the worker login needs the dirty log: a DBA pregrant outside
		// the migration set, as the protected-publish proof grants it.
		`GRANT INSERT ON supply_chain_impact_winners_dirty_log TO "` + roleName + `"`,
		`GRANT USAGE ON SEQUENCE supply_chain_impact_winners_dirty_log_seq_seq TO "` + roleName + `"`,
	} {
		mustExecProofRow(t, ctx, admin, s)
	}

	const scopeID, generationID, repoID = "fold-engaged-scope", "fold-engaged-gen", "repo://fold-engaged/web"
	now := time.Now().UTC().Truncate(time.Second)
	mustExecProofRow(t, ctx, admin, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
		partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'vulnerability_intelligence','synthetic',$1,'synthetic',$1,$2,$2,'active','{}'::jsonb)`, scopeID, now)
	mustExecProofRow(t, ctx, admin, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
		status, activated_at, payload) VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`, generationID, scopeID, now)
	mustExecProofRow(t, ctx, admin, `UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`, generationID, scopeID)
	seed := func(id, kind, payload string, tomb bool) {
		mustExecProofRow(t, ctx, admin, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
			source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload, fencing_token)
			VALUES ($1,$2,$3,$4,$1,'synthetic',$1,$5,$5,$6,$7::jsonb,0)`, id, scopeID, generationID, kind, now, tomb, payload)
	}
	jsFile := `{"repo_id":"` + repoID + `","language":"typescript","parsed_file_data":{"language":"typescript","lang":"typescript","imports":[{"name":"left-pad","source":"left-pad"}]}}`
	// Byte order deliberately differs from insert order; 'Z' < 'a'.
	seed("file:b-ts", "file", jsFile, false)
	seed("file:Z-ts", "file", jsFile, false)
	seed("file:a-ts", "file", jsFile, false)
	seed("file:tomb-ts", "file", jsFile, true)
	seed("file:py", "file", `{"repo_id":"`+repoID+`","language":"python","parsed_file_data":{"language":"python"}}`, false)

	var foldedFiles int
	var leakedFiles []string
	loader := NewAuthorizedSupplyChainImpactEvidenceLoader(SQLDB{DB: admin})
	loaded, err := loader.ListAuthorizedSupplyChainImpactEvidence(ctx,
		reducercontract.Intent{ScopeID: scopeID, GenerationID: generationID, Domain: reducercontract.DomainSupplyChainImpact},
		func(ctx context.Context, fl factload.FactLoader, public []facts.Envelope) (supplychaincore.AuthorizedStaticSupplyChainImpactLoad, error) {
			if public == nil {
				t.Fatal("static expansion got nil public advisories: the loader took the DISENGAGED branch")
			}
			sl, ok := fl.(foldStatsLoader)
			if !ok {
				t.Fatalf("snapshot loader %T has no stats method", fl)
			}
			fold := supplychaincore.NewFileReachabilityFold(nil)
			envs, stats, err := sl.ListActiveSupplyChainImpactFactsWithStats(ctx, supplychaincore.SupplyChainImpactFactFilter{
				FileRepositoryIDs: []string{repoID}, FileFold: fold,
			})
			if err != nil {
				return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{}, err
			}
			foldedFiles = stats.FoldedFiles
			for _, e := range envs {
				if e.FactKind == "file" {
					leakedFiles = append(leakedFiles, e.FactID)
				}
			}
			digest, ok := fold.DigestEnvelope()
			if !ok {
				t.Fatal("no digest")
			}
			out := append(envs, facts.Envelope{FactID: "fold-engaged-scope-fact", FactKind: "package_registry.package", ScopeID: scopeID}, digest)
			return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{Envelopes: out, SuppressionEnvelopes: out, Complete: true}, nil
		})
	if err != nil {
		t.Fatalf("engaged load: %v", err)
	}
	t.Logf("engaged=%v complete=%v reason=%q folded=%d leaked=%v ids=%v", loaded.ProtectionEngaged, loaded.Complete, loaded.CoverageReason, foldedFiles, leakedFiles, loaded.StaticNonAdvisoryEvidenceFactIDs)
	if !loaded.ProtectionEngaged || !loaded.Complete {
		t.Fatalf("want engaged+complete, got engaged=%v complete=%v reason=%q", loaded.ProtectionEngaged, loaded.Complete, loaded.CoverageReason)
	}
	if foldedFiles != 3 || len(leakedFiles) != 0 {
		t.Fatalf("fold: folded=%d leaked=%v, want 3 folded (tombstone filtered by SQL, python by language), none returned", foldedFiles, leakedFiles)
	}
	want := []string{"fold-engaged-scope-fact", "file:Z-ts", "file:a-ts", "file:b-ts"}
	if !slices.Equal(loaded.StaticNonAdvisoryEvidenceFactIDs, want) {
		t.Fatalf("StaticNonAdvisoryEvidenceFactIDs = %v, want %v (byte-sorted folded IDs, digest ID excluded)", loaded.StaticNonAdvisoryEvidenceFactIDs, want)
	}

	workerDSN, err := proofWorkerDSN(adminDSN, dbName, roleName, rolePassword)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := sql.Open("pgx", workerDSN)
	if err != nil {
		t.Fatal(err)
	}
	worker.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = worker.Close() })
	writer := supplychaincore.PostgresSupplyChainImpactWriter{DB: SupplyChainImpactBeginner{Beginner: SQLDB{DB: worker}}}
	mk := func(token int64, ids []string, evidence ...string) supplychaincore.SupplyChainImpactWrite {
		return supplychaincore.SupplyChainImpactWrite{
			FencingToken: token, IntentID: "fold-engaged-intent", ScopeID: scopeID, GenerationID: generationID,
			SourceSystem: "vulnerability_intelligence",
			Findings: []supplychaincore.SupplyChainImpactFinding{{
				CVEID: "CVE-2026-98550", PackageID: "pkg:npm/left-pad", Status: supplychaincore.SupplyChainImpactAffectedExact,
				RepositoryID: repoID, EvidenceFactIDs: evidence,
			}},
			StaticNonAdvisoryEvidenceFactIDs: ids,
			ProtectedCompleteSet:             true,
		}
	}
	// 1. Old-style list (envelope FactIDs incl. synthetic digest ID) rejects the folded reachability evidence.
	oldStyle := []string{"fold-engaged-scope-fact", supplychaincore.FileReachabilityDigestFactKind}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, mk(5, oldStyle, "file:a-ts", "fold-engaged-scope-fact")); err == nil || !strings.Contains(err.Error(), "unclassified evidence") {
		t.Fatalf("old-style list write err = %v, want unclassified evidence (control: the test discriminates F1)", err)
	} else {
		t.Logf("control (pre-fix list) refused: %v", err)
	}
	// 2. An unknown evidence ID is still refused with the expanded list.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, mk(6, loaded.StaticNonAdvisoryEvidenceFactIDs, "file:unknown", "file:a-ts")); err == nil || !strings.Contains(err.Error(), "unclassified evidence") {
		t.Fatalf("unknown-ID write err = %v, want unclassified evidence", err)
	} else {
		t.Logf("unknown ID refused: %v", err)
	}
	// 2b. The synthetic digest ID cited as evidence is refused (it is not listed).
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, mk(7, loaded.StaticNonAdvisoryEvidenceFactIDs, supplychaincore.FileReachabilityDigestFactKind)); err == nil || !strings.Contains(err.Error(), "unclassified evidence") {
		t.Fatalf("digest-ID write err = %v, want unclassified evidence", err)
	}
	// 2c. A tombstoned file ID that the fold never saw (SQL filters tombstones) is refused.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, mk(8, loaded.StaticNonAdvisoryEvidenceFactIDs, "file:tomb-ts")); err == nil || !strings.Contains(err.Error(), "unclassified evidence") {
		t.Fatalf("tombstone-ID write err = %v, want unclassified evidence", err)
	}
	// 3. The fixed list publishes the folded reachability evidence.
	result, err := writer.WriteSupplyChainImpactFindings(ctx, mk(9, loaded.StaticNonAdvisoryEvidenceFactIDs, "file:Z-ts", "file:a-ts", "fold-engaged-scope-fact"))
	if err != nil {
		t.Fatalf("fixed-list protected write: %v", err)
	}
	var revisions int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM scanner_static_impact_finding_authority_revisions
		WHERE impact_scope_id=$1 AND impact_generation_id=$2 AND impact_fencing_token=9`, scopeID, generationID).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixed list: FactsWritten=%d revisions=%d", result.FactsWritten, revisions)
	if result.FactsWritten != 1 || revisions != 1 {
		t.Fatalf("FactsWritten=%d revisions=%d, want 1/1", result.FactsWritten, revisions)
	}
}
