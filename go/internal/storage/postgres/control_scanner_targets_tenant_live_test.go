// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof for hosted tenant boundaries and grant-revocation target suppression
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerImageTargetHostedBoundaryAndGrantRevocationLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config := `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"shared-domain","registry":"registry.example.invalid"}]}`
	for _, row := range []struct{ id, kind, cfg string }{
		{"scanner-a", "scanner_worker", config},
		{"scanner-b", "scanner_worker", config},
		{"source-instance", "kubernetes_live", `{}`},
	} {
		if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
          (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
          VALUES ($1,$2,'continuous',TRUE,TRUE,$3::jsonb,$4,$4,$4)`, row.id, row.kind, row.cfg, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
      (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,observed_at,ingested_at,status,active_generation_id)
      VALUES ('source-scope','kubernetes','kubernetes_live','source-scope','kubernetes_live','source-scope',$1,$1,'active','hosted')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
      (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
      VALUES ('hosted','source-scope','snapshot',$1,$1,'active')`, now); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"a", "b"} {
		tenantID, workspaceID, policy := "tenant-"+suffix, "workspace-"+suffix, "policy-"+suffix
		if _, err := database.ExecContext(ctx, `INSERT INTO tenants
          (tenant_id,status,policy_revision_hash,created_at,updated_at)
          VALUES ($1,'active',$2,$3,$3)`, tenantID, policy, now); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO workspaces
          (tenant_id,workspace_id,status,policy_revision_hash,created_at,updated_at)
          VALUES ($1,$2,'active',$3,$4,$4)`, tenantID, workspaceID, policy, now); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO tenant_scope_grants
          (tenant_id,workspace_id,scope_id,subject_class,grant_source,policy_revision_hash,
           effective_at,created_at,updated_at)
          VALUES ($1,$2,'source-scope','collector','operator',$3,$4,$5,$5)`,
			tenantID, workspaceID, policy, now.Add(-time.Minute), now); err != nil {
			t.Fatal(err)
		}
	}
	principals := &scannerProofPrincipals{admin: database}
	principals.producer = mustCreateScannerProofRole(t, database, ctx, "producer")
	principals.coordinator = mustCreateScannerProofRole(t, database, ctx, "coordinator")
	principals.worker = mustCreateScannerProofRole(t, database, ctx, "worker")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.producer, "typed-source-producer")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.coordinator, "authority-control")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.worker, "runtime-writer")
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
				SourceGenerationID: "hosted", ObjectID: "pod-a", SlotID: "regular:c-a",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeImageReference: "registry.example.invalid/team/service:release",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
		})
	hostedRequest := func(instance, tenant, workspace, policy string) workflow.RunningImageDiscoveryRequest {
		return workflow.RunningImageDiscoveryRequest{
			ScannerCollectorInstanceID: instance,
			AuthorizationNamespace:     workflow.ScannerAuthorizationHosted,
			TenantID:                   tenant, WorkspaceID: workspace,
			SubjectClass: "collector", PolicyRevisionHash: policy,
			DiscoveryPolicyRevision:    1,
			ExpectedRolePolicyRevision: scannerProofPolicyRevision,
			ExpectedDatabaseIdentity:   scannerProofDatabaseIdentity,
			Limit:                      256,
		}
	}
	plan := func(suffix string) (workflow.Run, workflow.WorkItem, workflow.ScannerImageTarget) {
		tenantID, workspaceID, policy := "tenant-"+suffix, "workspace-"+suffix, "policy-"+suffix
		runID := "scanner_worker:hosted:" + suffix
		workID := runID + ":item"
		target := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationHosted,
			TenantID: tenantID, WorkspaceID: workspaceID, SubjectClass: "collector", PolicyRevisionHash: policy,
			AuthorizationDomain: "shared-domain", ManifestDigest: digest,
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
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "scanner-" + suffix,
			SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
			AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
			TenantID: tenantID, WorkspaceID: workspaceID, SubjectClass: "collector", PolicyRevisionHash: policy,
			Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
		}
		return run, item, target
	}
	runA, itemA, targetA := plan("a")
	runB, itemB, targetB := plan("b")
	if targetA.ScanTargetID == targetB.ScanTargetID {
		t.Fatal("two hosted tenants share one scan scope")
	}
	admit := func(suffix string, run workflow.Run, item workflow.WorkItem, target workflow.ScannerImageTarget) {
		t.Helper()
		tenantID, workspaceID, policy := "tenant-"+suffix, "workspace-"+suffix, "policy-"+suffix
		admission := mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
			"scanner-"+suffix, hostedRequest("scanner-"+suffix, tenantID, workspaceID, policy),
			run, []workflow.WorkItem{item}, []workflow.ScannerImageTarget{target})
		if admission.InsertedWorkItems != 1 {
			t.Fatalf("hosted admission = %+v; want one per tenant", admission)
		}
	}
	admit("a", runA, itemA, targetA)
	admit("b", runB, itemB, targetB)
	for _, planned := range []struct{ instance, workID, claimID string }{
		{"scanner-a", itemA.WorkItemID, "claim-a"}, {"scanner-b", itemB.WorkItemID, "claim-b"},
	} {
		item, claim, found, err := workerStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: planned.instance,
			OwnerID: "worker", ClaimID: planned.claimID,
		}, now, time.Minute)
		if err != nil || !found || item.WorkItemID != planned.workID {
			t.Fatalf("hosted claim = %+v, %t, %v", item, found, err)
		}
		if _, err := workerStore.GetScannerImageTarget(ctx, item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); err != nil {
			t.Fatalf("authorized target lookup: %v", err)
		}
		if err := workerStore.HeartbeatClaim(ctx, workflow.ClaimMutation{
			WorkItemID: item.WorkItemID, ClaimID: claim.ClaimID,
			FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
			ObservedAt: now.Add(time.Second), LeaseDuration: time.Minute,
			DerivedScannerImage: true,
		}); err != nil {
			t.Fatalf("authorized target heartbeat: %v", err)
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=$1
      WHERE tenant_id='tenant-a' AND workspace_id='workspace-a' AND scope_id='source-scope'`, now); err != nil {
		t.Fatal(err)
	}
	var claimID string
	var fence int64
	if err := database.QueryRowContext(ctx, `SELECT current_claim_id,current_fencing_token FROM workflow_work_items
      WHERE work_item_id=$1`, itemA.WorkItemID).Scan(&claimID, &fence); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, itemA.WorkItemID, claimID, fence, "worker"); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("revoked tenant target lookup = %v, want rejection", err)
	}
	if err := workerStore.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker",
		ObservedAt: now.Add(2 * time.Second), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("revoked tenant heartbeat = %v, want claim rejection", err)
	}
	if err := workerStore.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker", ObservedAt: now.Add(2 * time.Second),
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("revoked tenant completion = %v, want claim rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=NULL
      WHERE tenant_id='tenant-a' AND workspace_id='workspace-a' AND scope_id='source-scope'`); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, itemA.WorkItemID, claimID, fence, "worker"); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("regranted tenant old claim lookup = %v, want rejection", err)
	}
	if err := workerStore.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker",
		ObservedAt: now.Add(3 * time.Second), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("regranted tenant old claim heartbeat = %v, want rejection", err)
	}
	if err := workerStore.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker", ObservedAt: now.Add(3 * time.Second),
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("regranted tenant old claim completion = %v, want rejection", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT current_claim_id,current_fencing_token FROM workflow_work_items
      WHERE work_item_id=$1`, itemB.WorkItemID).Scan(&claimID, &fence); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, itemB.WorkItemID, claimID, fence, "worker"); err != nil {
		t.Fatalf("other tenant target lookup after unrelated revocation: %v", err)
	}
}
