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
	// Clones the process template (#10148 PR-A) instead of bootstrapping again.
	template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		postgresproof.FlavourPublicBootstrap,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	ctx, admin := postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)

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
	grantImpactWriterPregrants(t, ctx, admin, dbName, roleName)

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
	requireNoSupportsFenceMarks(t, ctx, admin)
}

// engagedLoaderProof opens a disposable PostgreSQL clone with the role policy
// installed (protection engaged) and returns its admin handle.
func engagedLoaderProof(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()

	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if adminDSN == "" {
		t.Skip("needs ESHU_POSTGRES_DSN")
	}
	template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		postgresproof.FlavourPublicBootstrap,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	ctx, admin := postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)

	const roleName = "go_warning_engaged_worker"
	mustExecProofRow(t, ctx, admin, `DROP ROLE IF EXISTS "`+roleName+`"`)
	mustExecProofRow(t, ctx, admin, `CREATE ROLE "`+roleName+`" LOGIN PASSWORD 'local-proof-only'`)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP OWNED BY "`+roleName+`"; DROP ROLE IF EXISTS "`+roleName+`"`)
	})
	var roleOID int64
	if err := admin.QueryRowContext(ctx, `SELECT oid::bigint FROM pg_roles WHERE rolname=$1`, roleName).Scan(&roleOID); err != nil {
		t.Fatal(err)
	}
	const policyRevision = 1003001
	mustExecProofRow(t, ctx, admin, `INSERT INTO eshu_security.role_policy_state
		(policy_revision, database_oid, database_identity, system_identifier,
		 inventory_version, owner_role_oid, authority_epoch, active)
		SELECT $1, (SELECT oid FROM pg_database WHERE datname=current_database()), '11111111-2222-4333-8444-555555555555',
		 (SELECT system_identifier FROM pg_control_system()), 1,
		 (SELECT oid FROM pg_roles WHERE rolname=current_user), 1, TRUE`, policyRevision)
	mustExecProofRow(t, ctx, admin, `INSERT INTO eshu_security.role_contracts
		(contract_id, policy_revision, workload_class, connection_route, profile,
		 session_role_oid, effective_role_oid, enabled)
		VALUES ('go-warning-engaged-writer',$1,'scanner-worker','writer','runtime-writer',$2,$2,TRUE)`, policyRevision, roleOID)
	mustExecProofRow(t, ctx, admin, `INSERT INTO scanner_advisory_desired_policy (desired_revision, desired_sha256) VALUES (1,$1)`, strings.Repeat("0", 64))
	return ctx, admin
}

// loadEngagedWithStaticFacts runs the engaged authorized loader for one impact
// scope with a static expansion that returns exactly envelopes.
func loadEngagedWithStaticFacts(t *testing.T, ctx context.Context, admin *sql.DB, envelopes []facts.Envelope) supplychaincore.AuthorizedSupplyChainImpactLoad {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	const scopeID, generationID = "vulnerability_intelligence:orders-api:go-alias", "go-warning-engaged-gen"
	mustExecProofRow(t, ctx, admin, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
		partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'vulnerability_intelligence','synthetic',$1,'synthetic',$1,$2,$2,'active','{}'::jsonb)`, scopeID, now)
	mustExecProofRow(t, ctx, admin, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
		status, activated_at, payload) VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`, generationID, scopeID, now)
	mustExecProofRow(t, ctx, admin, `UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`, generationID, scopeID)

	loader := NewAuthorizedSupplyChainImpactEvidenceLoader(SQLDB{DB: admin})
	loaded, err := loader.ListAuthorizedSupplyChainImpactEvidence(ctx,
		reducercontract.Intent{ScopeID: scopeID, GenerationID: generationID, Domain: reducercontract.DomainSupplyChainImpact},
		func(_ context.Context, _ factload.FactLoader, public []facts.Envelope) (supplychaincore.AuthorizedStaticSupplyChainImpactLoad, error) {
			if public == nil {
				t.Fatal("static expansion got nil public advisories: the loader took the DISENGAGED branch")
			}
			return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{Envelopes: envelopes, SuppressionEnvelopes: envelopes, Complete: true}, nil
		})
	if err != nil {
		t.Fatalf("engaged load: %v", err)
	}
	return loaded
}

// TestAuthorizedLoaderEngagedKeepsAGoReachabilityWarningStaticLive proves, on an
// engaged store, that the unfinished-scan warning the cross-scope Go stage loads
// from a go-reachability scope does not fail the pass closed: it passes as static
// non-advisory evidence beside the module evidence of the same scan, so the
// finding can read analysis_failed. Expected on main before the screen change
// (#10030): CoverageReason "policy_unavailable", because the warning kind is
// target-only by kind collision with the image analyzers.
func TestAuthorizedLoaderEngagedKeepsAGoReachabilityWarningStaticLive(t *testing.T) {
	ctx, admin := engagedLoaderProof(t)
	const goScope = "go-reachability://repository:r_ea78e8bb"
	envelopes := []facts.Envelope{
		{
			FactID: "go-module-evidence", ScopeID: goScope, FactKind: "vulnerability.go_module_evidence",
			Payload: map[string]any{"repository_id": "repository:r_ea78e8bb", "relative_path": "legacy/go.mod", "module_path": "github.com/acme/legacy-lib"},
		},
		{
			FactID: "go-toolchain-warning", ScopeID: goScope, FactKind: facts.ScannerWorkerWarningFactKind,
			Payload: map[string]any{
				"analyzer": "go_reachability", "analysis_status": "not_scanned", "reason": "toolchain_unsupported",
				"repository_id": "repository:r_ea78e8bb", "relative_path": "legacy/go.mod",
			},
		},
	}
	loaded := loadEngagedWithStaticFacts(t, ctx, admin, envelopes)
	t.Logf("engaged=%v complete=%v reason=%q ids=%v", loaded.ProtectionEngaged, loaded.Complete, loaded.CoverageReason, loaded.StaticNonAdvisoryEvidenceFactIDs)
	if !loaded.ProtectionEngaged {
		t.Fatalf("want an engaged load, got engaged=%v", loaded.ProtectionEngaged)
	}
	if loaded.CoverageReason != "" || !loaded.Complete {
		t.Fatalf("complete=%v reason=%q, want a complete load: a go_reachability warning in a go-reachability scope is static Go evidence", loaded.Complete, loaded.CoverageReason)
	}
	for _, id := range []string{"go-module-evidence", "go-toolchain-warning"} {
		if !slices.Contains(loaded.StaticNonAdvisoryEvidenceFactIDs, id) {
			t.Fatalf("StaticNonAdvisoryEvidenceFactIDs = %v, want %s listed", loaded.StaticNonAdvisoryEvidenceFactIDs, id)
		}
	}
}

// TestAuthorizedLoaderEngagedRefusesAnImageAnalyzerWarningWithoutReceiptLive is
// the twin: an image-analyzer warning on the static path with no scan receipt
// still fails the pass closed with policy_unavailable. Expected on main and after
// the screen change.
func TestAuthorizedLoaderEngagedRefusesAnImageAnalyzerWarningWithoutReceiptLive(t *testing.T) {
	ctx, admin := engagedLoaderProof(t)
	envelopes := []facts.Envelope{
		{
			FactID: "image-warning", ScopeID: "scanner-worker://repository/repo-123", FactKind: facts.ScannerWorkerWarningFactKind,
			Payload: map[string]any{"analyzer": "trivy", "analysis_status": "not_scanned", "reason": "scan_failed"},
		},
	}
	loaded := loadEngagedWithStaticFacts(t, ctx, admin, envelopes)
	t.Logf("engaged=%v complete=%v reason=%q", loaded.ProtectionEngaged, loaded.Complete, loaded.CoverageReason)
	if loaded.Complete || loaded.CoverageReason != "policy_unavailable" {
		t.Fatalf("complete=%v reason=%q, want an incomplete load with policy_unavailable for a receipt-less image-analyzer warning", loaded.Complete, loaded.CoverageReason)
	}
}
