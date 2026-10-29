// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that the authorized impact loader admits a completed scanner_worker analysis alongside its os_package fact instead of fail-closing the scan as a partial snapshot
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	scannerworkerv1 "github.com/eshu-hq/eshu-sdk/factschema/scannerworker/v1"
	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestAuthorizedImpactLoaderAdmitsCompletedScanLive drives the production
// envelope path end to end: real BuildEnvelopes package facts plus a
// completed scanner_worker analysis fact, persisted through the production
// fact upsert, then read back through loadAuthorizedImpactTarget with an
// empty public advisory set. The loader must return status "" with
// Complete=true and exactly one package plus one analysis. The warning fact
// is persisted without a scan association, exactly as the result-commit path
// leaves it, and must not leak into the published evidence.
func TestAuthorizedImpactLoaderAdmitsCompletedScanLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	store := SQLDB{DB: database}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	const scopeID = "bridge-scope-9299"
	const generationID = "bridge-gen-9299"
	const receiptID = "bridge-receipt-9299"
	const bindingID = "bridge-binding-9299"
	const workItemID = "bridge-work-9299"

	packageFacts, err := ospackagevulnerability.BuildEnvelopes(ospackagevulnerability.EnvelopeContext{
		ScopeID: scopeID, GenerationID: generationID, CollectorInstanceID: "scanner",
		FencingToken: 7, ObservedAt: now, SourceRecordID: "package-record",
	}, []ospackagevulnerability.OSPackage{{
		Distro: ospackagevulnerability.DistroAlpine, DistroVersion: "3.20",
		PackageManager: ospackagevulnerability.PackageManagerAPK,
		Name:           "bridge-lib", Version: "1.2.3", Release: "r0", Arch: "x86_64",
	}}, []ospackagevulnerability.OSPackageWarning{{
		Reason: ospackagevulnerability.WarningReasonMalformedPackageRecord, RecordIndex: 1,
	}})
	if err != nil || len(packageFacts) != 2 {
		t.Fatalf("BuildEnvelopes() = %d facts, %v; want one package plus one warning", len(packageFacts), err)
	}
	analysisPayload, err := factschema.EncodeScannerWorkerAnalysis(scannerworkerv1.Analysis{
		Analyzer: "bridge-analyzer", TargetKind: "running_image", TargetLocatorHash: "bridge-locator",
		AnalysisStatus: "completed", CoverageStatus: "scanned",
		ImageReference: "registry.example.invalid/team/service:bridge",
		ImageDigest:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		EvidenceSource: "scanner_worker", ExtractionReason: "scheduled_scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	analysisFact := facts.Envelope{
		FactID: "bridge-analysis-9299", ScopeID: scopeID, GenerationID: generationID,
		FactKind: facts.ScannerWorkerAnalysisFactKind, StableFactKey: "bridge-analysis",
		SchemaVersion: facts.ScannerWorkerSchemaVersionV1,
		CollectorKind: ospackagevulnerability.CollectorKind, FencingToken: 7,
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: now, Payload: analysisPayload,
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      scopeID, GenerationID: generationID, FactKey: "bridge-analysis",
			SourceURI: "https://registry.example.invalid/v2/", SourceRecordID: "bridge-record",
		},
	}
	var packageFact, warningFact facts.Envelope
	for _, envelope := range packageFacts {
		switch envelope.FactKind {
		case "vulnerability.os_package":
			packageFact = envelope
		case "vulnerability.warning":
			warningFact = envelope
		}
	}
	if packageFact.FactID == "" || warningFact.FactID == "" {
		t.Fatalf("BuildEnvelopes kinds = %q, %q; want one os_package plus one warning",
			packageFacts[0].FactKind, packageFacts[1].FactKind)
	}
	mustExecBridgeRow(t, ctx, database, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind,
		 partition_key, observed_at, ingested_at, status)
		VALUES ($1,'scanner_image','scanner_worker','bridge-source','os_package_vulnerability',
			'bridge-partition',$2,$2,'active')`, scopeID, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, freshness_hint, status, observed_at, ingested_at)
		VALUES ($1,$2,'snapshot','bridge-hint','active',$3,$3)`, generationID, scopeID, now)
	if err := upsertFactBatch(ctx, store, []facts.Envelope{packageFact, analysisFact, warningFact}); err != nil {
		t.Fatal(err)
	}
	mustExecBridgeRow(t, ctx, database, `INSERT INTO scanner_image_targets
		(scan_scope_id, authorization_namespace, authorization_domain, manifest_digest)
		VALUES ($1,'local','bridge-domain','sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd')`,
		scopeID)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO scanner_image_target_sources
		(binding_id, scan_scope_id, source_fact_id, source_collector_instance_id,
		 source_scope_id, source_generation_id, source_payload_sha256,
		 scanner_collector_instance_id, registry, repository, runtime_digest,
		 runtime_digest_kind, runtime_image_reference, registry_domain_config_sha256, validated_at)
		VALUES ($1,$2,'bridge-source-fact','source-instance','source-scope','source-gen',
			'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
			'scanner','registry.example.invalid','team/service','sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
			'manifest','registry.example.invalid/team/service:bridge',
			'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',$3)`,
		bindingID, scopeID, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO scanner_image_target_attempts
		(work_item_id, scan_scope_id, binding_id, status, created_at)
		VALUES ($1,$2,$3,'completed',$4)`, workItemID, scopeID, bindingID, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO scanner_image_scan_results
		(result_receipt_id, work_item_id, binding_id, scan_scope_id, scan_generation_id,
		 claim_id, claim_fencing_token, claim_owner_id, completed_authority_vector, completed_at)
		VALUES ($1,$2,$3,$4,$5,'bridge-claim',7,'bridge-owner','{}',$6)`,
		receiptID, workItemID, bindingID, scopeID, generationID, now)
	rows, err := database.QueryContext(ctx, `SELECT fact_id, fencing_token,
		encode(sha256(convert_to(payload::text,'UTF8')),'hex') FROM fact_records
		WHERE fact_id IN ($1,$2)`, packageFact.FactID, analysisFact.FactID)
	if err != nil {
		t.Fatal(err)
	}
	associated := map[string]bool{}
	for rows.Next() {
		var factID string
		var fencing int64
		var sha string
		if err := rows.Scan(&factID, &fencing, &sha); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		kind := packageFact.FactKind
		if factID == analysisFact.FactID {
			kind = analysisFact.FactKind
		}
		mustExecBridgeRow(t, ctx, database, `INSERT INTO scanner_image_scan_fact_associations
			(fact_id, result_receipt_id, scan_scope_id, scan_generation_id, fact_kind,
			 payload_sha256, fencing_token)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			factID, receiptID, scopeID, generationID, kind, sha, fencing)
		associated[factID] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	_ = rows.Close()
	if !associated[packageFact.FactID] || !associated[analysisFact.FactID] {
		t.Fatalf("associated facts = %v; want the package and analysis facts pinned", associated)
	}

	receipt := authorizedImpactTargetReceipt{
		scanTargetID: scopeID, scanGenerationID: generationID, resultReceiptID: receiptID,
		bindingID: bindingID, workItemID: workItemID, claimID: "bridge-claim",
		claimFence: 7, claimOwnerID: "bridge-owner",
	}
	target, witness, status, err := loadAuthorizedImpactTarget(
		ctx, store, contract.Intent{}, receipt, authorizedImpactPublicAdvisorySet{})
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("loader status = %q; want empty (admitted, not fail-closed)", status)
	}
	if !target.Complete || !witness.Complete {
		t.Fatalf("loader complete = target %v witness %v; want both true", target.Complete, witness.Complete)
	}
	if len(target.OSPackages) != 1 || len(target.Analyses) != 1 {
		t.Fatalf("loader evidence = %d packages %d analyses; want one and one",
			len(target.OSPackages), len(target.Analyses))
	}
	for _, factID := range witness.IntrinsicEvidenceFactIDs {
		if factID == warningFact.FactID {
			t.Fatalf("unassociated warning %q leaked into published evidence", factID)
		}
	}
}

func mustExecBridgeRow(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

// TestAuthorizedImpactAdvisoriesEmptyRegistryStaysCompleteLive pins the
// dark-by-default contract: with no public advisory authority registered
// anywhere, advisory evidence is vacuously absent (empty set, empty reason),
// so an ordinary scope completes on static evidence alone instead of
// fail-closing the whole pass as policy_unavailable. A registered set with a
// broken witness still fail-closes on mismatch.
func TestAuthorizedImpactAdvisoriesEmptyRegistryStaysCompleteLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	set, reason, err := loadCurrentAuthorizedPublicAdvisories(context.Background(), SQLDB{DB: database})
	if err != nil {
		t.Fatalf("loadCurrentAuthorizedPublicAdvisories() error = %v, want nil", err)
	}
	if reason != "" {
		t.Fatalf("loadCurrentAuthorizedPublicAdvisories() reason = %q, want empty on an unregistered registry", reason)
	}
	if len(set.facts) != 0 || len(set.dependencies) != 0 {
		t.Fatalf("loadCurrentAuthorizedPublicAdvisories() returned %d facts and %d dependencies, want none",
			len(set.facts), len(set.dependencies))
	}
}
