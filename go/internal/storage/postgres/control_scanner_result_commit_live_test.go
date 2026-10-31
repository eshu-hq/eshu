// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9468 live proof for control_scanner_result_commit end to end
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	scannerworkerv1 "github.com/eshu-hq/eshu-sdk/factschema/scannerworker/v1"
	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestScannerResultCommitAdmitsAnalyzesCompletesLive drives the production
// result-commit path end to end: admit one hosted binding, claim it as the
// scanner worker, analyze two facts (one scanner_worker analysis, one
// os_package), commit the derived result, then read back the receipt, the
// fact associations, and the completed attempt with the exact authority
// vector. This is the first #9468 acceptance criterion: no derived read gate
// opens until this proof passes.
func TestScannerResultCommitAdmitsAnalyzesCompletesLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	config := `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"commit-domain","registry":"registry.example.invalid"}]}`
	for _, row := range []struct{ id, kind, cfg string }{
		{"commit-scanner", "scanner_worker", config},
		{"source-instance", "kubernetes_live", `{}`},
	} {
		mustExecBridgeRow(t, ctx, database, `INSERT INTO collector_instances
          (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
          VALUES ($1,$2,'continuous',TRUE,TRUE,$3::jsonb,$4,$4,$4)`, row.id, row.kind, row.cfg, now)
	}
	mustExecBridgeRow(t, ctx, database, `INSERT INTO ingestion_scopes
      (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,observed_at,ingested_at,status,active_generation_id)
      VALUES ('source-scope','kubernetes','kubernetes_live','source-scope','kubernetes_live','source-scope',$1,$1,'active','hosted')`, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO scope_generations
      (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
      VALUES ('hosted','source-scope','snapshot',$1,$1,'active')`, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO tenants
      (tenant_id,status,policy_revision_hash,created_at,updated_at)
      VALUES ('commit-tenant','active','commit-policy',$1,$1)`, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO workspaces
      (tenant_id,workspace_id,status,policy_revision_hash,created_at,updated_at)
      VALUES ('commit-tenant','commit-workspace','active','commit-policy',$1,$1)`, now)
	mustExecBridgeRow(t, ctx, database, `INSERT INTO tenant_scope_grants
      (tenant_id,workspace_id,scope_id,subject_class,grant_source,policy_revision_hash,
       effective_at,created_at,updated_at)
      VALUES ('commit-tenant','commit-workspace','source-scope','collector','operator','commit-policy',$1,$2,$2)`,
		now.Add(-time.Minute), now)
	principals := &scannerProofPrincipals{admin: database}
	principals.producer = mustCreateScannerProofRole(t, database, ctx, "commit-producer")
	principals.coordinator = mustCreateScannerProofRole(t, database, ctx, "commit-coordinator")
	principals.worker = mustCreateScannerProofRole(t, database, ctx, "commit-worker")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.producer, "typed-source-producer")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.coordinator, "authority-control")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.worker, "runtime-writer")
	// Ordinary ingestion DML the deployment grants the runtime writer
	// outside the protected inventory (the assert only checks listed
	// rows). Protected receipt tables stay ungranted by design: if the
	// commit reaches them, the denial below is the evidence.
	workerRole := `"` + principals.worker.name + `"`
	for _, statement := range []string{
		`GRANT SELECT, INSERT, UPDATE ON ingestion_scopes, scope_generations TO ` + workerRole,
		`GRANT SELECT, INSERT, UPDATE ON fact_records, fact_work_items TO ` + workerRole,
		`GRANT SELECT, INSERT ON projector_scope_claim_fences TO ` + workerRole,
		`GRANT SELECT ON repository_retirements TO ` + workerRole,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON relationship_reference_candidate_keys, package_manifest_consumption_keys, package_registry_identity_keys TO ` + workerRole,
		`GRANT USAGE ON SCHEMA public TO ` + workerRole,
	} {
		mustExecBridgeRow(t, ctx, database, statement)
	}
	mustSeedScannerProofPolicy(t, database, ctx, principals)
	coordinatorStore := NewWorkflowControlStore(SQLDB{DB: principals.coordinator.db})
	workerStore := NewWorkflowControlStore(SQLDB{DB: principals.worker.db})
	sourceStore := RunningImageSourceStore{DB: SQLDB{DB: principals.producer.db}}
	observationIDs := mustStageScannerProofGeneration(t, ctx, database, sourceStore, now, "hosted",
		[]workflow.RunningImageObservation{
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "hosted", ObjectID: "commit-pod", SlotID: "regular:c-1",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeImageReference: "registry.example.invalid/team/service:release",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
		})
	runID := "scanner_worker:hosted:commit"
	workID := runID + ":item"
	target := workflow.ScannerImageTarget{
		WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationHosted,
		TenantID: "commit-tenant", WorkspaceID: "commit-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "commit-policy",
		AuthorizationDomain: "commit-domain", ManifestDigest: digest,
		Sources: []workflow.ScannerImageSourceBinding{{
			SourceObservationID: observationIDs[0], SourceCollectorInstanceID: "source-instance",
			SourceScopeID: "source-scope", SourceGenerationID: "hosted",
			Registry: "registry.example.invalid", Repository: "team/service",
			RuntimeDigest: digest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: "registry.example.invalid/team/service:release",
		}},
	}
	target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
	run := workflow.Run{
		RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
		Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
		RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
	}
	item := workflow.WorkItem{
		WorkItemID: workID, RunID: runID,
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "commit-scanner",
		SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
		AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
		TenantID: "commit-tenant", WorkspaceID: "commit-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "commit-policy",
		Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	admission := mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"commit-scanner", workflow.RunningImageDiscoveryRequest{
			ScannerCollectorInstanceID: "commit-scanner",
			AuthorizationNamespace:     workflow.ScannerAuthorizationHosted,
			TenantID:                   "commit-tenant", WorkspaceID: "commit-workspace",
			SubjectClass: "collector", PolicyRevisionHash: "commit-policy",
			DiscoveryPolicyRevision:    1,
			ExpectedRolePolicyRevision: scannerProofPolicyRevision,
			ExpectedDatabaseIdentity:   scannerProofDatabaseIdentity,
			Limit:                      256,
		}, run, []workflow.WorkItem{item}, []workflow.ScannerImageTarget{target})
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("commit admission = %+v; want one work item", admission)
	}
	claimed, claim, found, err := workerStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "commit-scanner",
		OwnerID: "commit-worker", ClaimID: "commit-claim",
	}, now, time.Minute)
	if err != nil || !found || claimed.WorkItemID != workID {
		t.Fatalf("commit claim = %+v, %t, %v; want the admitted item", claimed, found, err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, workID, claim.ClaimID, claim.FencingToken, claim.OwnerID); err != nil {
		t.Fatalf("authorized target lookup before commit: %v", err)
	}
	derivedScope := target.ScanTargetID
	packageFacts, err := ospackagevulnerability.BuildEnvelopes(ospackagevulnerability.EnvelopeContext{
		ScopeID: derivedScope, GenerationID: runID, CollectorInstanceID: "commit-scanner",
		FencingToken: claim.FencingToken, ObservedAt: now, SourceRecordID: "commit-package-record",
	}, []ospackagevulnerability.OSPackage{{
		Distro: ospackagevulnerability.DistroAlpine, DistroVersion: "3.20",
		PackageManager: ospackagevulnerability.PackageManagerAPK,
		Name:           "commit-lib", Version: "1.0.0", Release: "r0", Arch: "x86_64",
	}}, nil)
	if err != nil || len(packageFacts) != 1 {
		t.Fatalf("BuildEnvelopes() = %d facts, %v; want one package", len(packageFacts), err)
	}
	analysisPayload, err := factschema.EncodeScannerWorkerAnalysis(scannerworkerv1.Analysis{
		Analyzer: "commit-analyzer", TargetKind: "running_image", TargetLocatorHash: "commit-locator",
		AnalysisStatus: "completed", CoverageStatus: "scanned",
		ImageReference: "registry.example.invalid/team/service:release",
		ImageDigest:    digest,
		EvidenceSource: "scanner_worker", ExtractionReason: "scheduled_scan",
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceFacts := []facts.Envelope{packageFacts[0], {
		FactID: "commit-analysis-9468", ScopeID: derivedScope, GenerationID: runID,
		FactKind: facts.ScannerWorkerAnalysisFactKind, StableFactKey: "commit-analysis",
		SchemaVersion: facts.ScannerWorkerSchemaVersionV1,
		CollectorKind: string(scope.CollectorScannerWorker), FencingToken: claim.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: now, Payload: analysisPayload,
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      derivedScope, GenerationID: runID, FactKey: "commit-analysis",
			SourceURI: "https://registry.example.invalid/v2/", SourceRecordID: derivedScope,
		},
	}}
	committer := NewIngestionStore(SQLDB{DB: principals.worker.db})
	mutation := workflow.ClaimMutation{
		WorkItemID: workID, ClaimID: claim.ClaimID,
		FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
		ObservedAt: now, LeaseDuration: time.Minute,
		TenantID: "commit-tenant", WorkspaceID: "commit-workspace",
		SubjectClass: "collector", PolicyRevisionHash: "commit-policy",
		DerivedScannerImage: true,
	}
	scopeValue := scope.IngestionScope{
		ScopeID: derivedScope, SourceSystem: string(scope.CollectorScannerWorker),
		ScopeKind: scope.KindScannerWorker, CollectorKind: scope.CollectorScannerWorker,
		PartitionKey: "commit-analyzer:commit-locator",
	}
	generation := scope.ScopeGeneration{
		GenerationID: runID, ScopeID: derivedScope,
		ObservedAt: now, IngestedAt: now,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
	}
	if err := committer.CommitDerivedImageResult(ctx, mutation, scopeValue, generation, sourceFacts); err != nil {
		t.Fatalf("CommitDerivedImageResult() error = %v; want nil", err)
	}
	var receiptID, workItemID, bindingID, receiptScope, receiptGen, claimID, ownerID, vector string
	var receiptFence int64
	if err := database.QueryRowContext(ctx, `SELECT result_receipt_id,work_item_id,binding_id,
		scan_scope_id,scan_generation_id,claim_id,claim_fencing_token,claim_owner_id,
		completed_authority_vector::text
		FROM scanner_image_scan_results WHERE work_item_id=$1`, workID).Scan(
		&receiptID, &workItemID, &bindingID, &receiptScope, &receiptGen,
		&claimID, &receiptFence, &ownerID, &vector); err != nil {
		t.Fatalf("read back scan receipt: %v", err)
	}
	if workItemID != workID || receiptScope != derivedScope || receiptGen != runID ||
		claimID != claim.ClaimID || receiptFence != claim.FencingToken || ownerID != claim.OwnerID ||
		bindingID == "" || vector == "" || vector == "{}" {
		t.Fatalf("scan receipt = %+v; want the committed binding with a non-empty authority vector",
			map[string]any{"receipt": receiptID, "binding": bindingID, "vector": vector})
	}
	rows, err := database.QueryContext(ctx, `SELECT fact_id,fact_kind
		FROM scanner_image_scan_fact_associations WHERE result_receipt_id=$1`, receiptID)
	if err != nil {
		t.Fatal(err)
	}
	associated := map[string]string{}
	for rows.Next() {
		var factID, kind string
		if err := rows.Scan(&factID, &kind); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		associated[factID] = kind
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	_ = rows.Close()
	if associated[packageFacts[0].FactID] == "" || associated["commit-analysis-9468"] == "" {
		t.Fatalf("associated facts = %v; want the package and analysis facts", associated)
	}
	var attemptStatus, attemptVector string
	if err := database.QueryRowContext(ctx, `SELECT status,completed_authority_vector::text
		FROM scanner_image_target_attempts WHERE work_item_id=$1`, workID).Scan(
		&attemptStatus, &attemptVector); err != nil {
		t.Fatalf("read back completed attempt: %v", err)
	}
	if attemptStatus != "completed" {
		t.Fatalf("attempt status = %q; want completed", attemptStatus)
	}
	if attemptVector != vector {
		t.Fatalf("attempt vector = %s; want the exact receipt vector %s", attemptVector, vector)
	}
}
