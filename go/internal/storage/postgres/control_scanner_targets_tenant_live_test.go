// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerImageTargetHostedBoundaryAndGrantRevocationLive(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config := `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"shared-domain","registry":"registry.example.invalid"}]}`
	for _, instanceID := range []string{"scanner-a", "scanner-b"} {
		if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
          (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
          VALUES ($1,'scanner_worker','continuous',TRUE,TRUE,$2::jsonb,$3,$3,$3)`, instanceID, config, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
      (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
      VALUES ('source-instance','kubernetes_live','continuous',TRUE,TRUE,'{}'::jsonb,$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
      (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,observed_at,ingested_at,status,active_generation_id)
      VALUES ('source-scope','kubernetes','kubernetes_live','source-scope','kubernetes_live','source-scope',$1,$1,'active','source-generation')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
      (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
      VALUES ('source-generation','source-scope','snapshot',$1,$1,'active')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
      (fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,source_fact_key,observed_at,ingested_at,payload)
      VALUES ('source-fact','source-scope','source-generation','kubernetes_live.pod_template','source-fact',
      'kubernetes_live','source-fact',$1,$1,'{"collector_instance_id":"source-instance","group_version_resource":"core/v1/pods","pod_phase":"Running","containers":[{"image":"registry.example.invalid/team/service:release","resolved_image_digest":"registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}'::jsonb)`, now); err != nil {
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
	plan := func(suffix string) (workflow.Run, workflow.WorkItem, workflow.ScannerImageTarget) {
		tenantID, workspaceID, policy := "tenant-"+suffix, "workspace-"+suffix, "policy-"+suffix
		runID := "scanner_worker:hosted:" + suffix
		workID := runID + ":item"
		target := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationHosted,
			TenantID: tenantID, WorkspaceID: workspaceID, SubjectClass: "collector", PolicyRevisionHash: policy,
			AuthorizationDomain: "shared-domain", ManifestDigest: digest,
			Sources: []workflow.ScannerImageSourceBinding{{
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeDigest: digest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
				SourceFactID: "source-fact", SourceCollectorInstanceID: "source-instance",
				SourceScopeID: "source-scope", SourceGenerationID: "source-generation",
				RuntimeImageReference: "registry.example.invalid/team/service:release",
			}},
		}
		target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
		run := workflow.Run{
			RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
			Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
			RequestedCollector: "scanner_worker", CreatedAt: now, UpdatedAt: now,
		}
		item := workflow.WorkItem{
			WorkItemID: workID, RunID: runID,
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "scanner-" + suffix,
			SourceSystem: "scanner_worker", ScopeID: target.ScanTargetID, AcceptanceUnitID: target.ScanTargetID,
			TenantID: tenantID, WorkspaceID: workspaceID, SubjectClass: "collector", PolicyRevisionHash: policy,
			SourceRunID: runID, GenerationID: runID, Status: workflow.WorkItemStatusPending,
			CreatedAt: now, UpdatedAt: now,
		}
		return run, item, target
	}
	runA, itemA, targetA := plan("a")
	runB, itemB, targetB := plan("b")
	if targetA.ScanTargetID == targetB.ScanTargetID {
		t.Fatal("two hosted tenants share one scan scope")
	}
	for _, planned := range []struct {
		run    workflow.Run
		item   workflow.WorkItem
		target workflow.ScannerImageTarget
	}{
		{runA, itemA, targetA}, {runB, itemB, targetB},
	} {
		admission, err := store.CreateScannerRunWithTargets(ctx, planned.run,
			[]workflow.WorkItem{planned.item}, []workflow.ScannerImageTarget{planned.target})
		if err != nil || admission.InsertedWorkItems != 1 {
			t.Fatalf("hosted admission = %+v, %v; want one per tenant", admission, err)
		}
	}
	for _, planned := range []struct{ instance, workID, claimID string }{
		{"scanner-a", itemA.WorkItemID, "claim-a"}, {"scanner-b", itemB.WorkItemID, "claim-b"},
	} {
		item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: planned.instance,
			OwnerID: "worker", ClaimID: planned.claimID,
		}, now, time.Minute)
		if err != nil || !found || item.WorkItemID != planned.workID {
			t.Fatalf("hosted claim = %+v, %t, %v", item, found, err)
		}
		if _, err := store.GetScannerImageTarget(ctx, item.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); err != nil {
			t.Fatalf("authorized target lookup: %v", err)
		}
		if err := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
			WorkItemID: item.WorkItemID, ClaimID: claim.ClaimID,
			FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
			ObservedAt: now.Add(time.Second), LeaseDuration: time.Minute,
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
	if _, err := store.GetScannerImageTarget(ctx, itemA.WorkItemID, claimID, fence, "worker"); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("revoked tenant target lookup = %v, want rejection", err)
	}
	if err := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker",
		ObservedAt: now.Add(2 * time.Second), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("revoked tenant heartbeat = %v, want claim rejection", err)
	}
	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker", ObservedAt: now.Add(2 * time.Second),
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("revoked tenant completion = %v, want claim rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE tenant_scope_grants SET tombstoned_at=NULL
      WHERE tenant_id='tenant-a' AND workspace_id='workspace-a' AND scope_id='source-scope'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, itemA.WorkItemID, claimID, fence, "worker"); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("regranted tenant old claim lookup = %v, want pinned epoch rejection", err)
	}
	if err := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker",
		ObservedAt: now.Add(3 * time.Second), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("regranted tenant old claim heartbeat = %v, want pinned epoch rejection", err)
	}
	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: itemA.WorkItemID, ClaimID: claimID,
		FencingToken: fence, OwnerID: "worker", ObservedAt: now.Add(3 * time.Second),
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("regranted tenant old claim completion = %v, want pinned epoch rejection", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT current_claim_id,current_fencing_token FROM workflow_work_items
      WHERE work_item_id=$1`, itemB.WorkItemID).Scan(&claimID, &fence); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, itemB.WorkItemID, claimID, fence, "worker"); err != nil {
		t.Fatalf("other tenant target lookup after unrelated revocation: %v", err)
	}
}
