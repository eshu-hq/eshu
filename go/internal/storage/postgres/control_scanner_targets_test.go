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

func TestScannerImageTargetAdmissionAndCompletionLive(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, row := range []struct{ id, kind, config string }{
		{"target-live-instance", "scanner_worker", `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`},
		{"source-instance", "kubernetes_live", `{}`},
	} {
		_, err := database.ExecContext(ctx, `INSERT INTO collector_instances
            (instance_id, collector_kind, mode, enabled, claims_enabled, configuration,
             last_observed_at, created_at, updated_at)
            VALUES ($1,$2,'continuous',TRUE,TRUE,$3::jsonb,$4,$4,$4)`, row.id, row.kind, row.config, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
         observed_at, ingested_at, status, active_generation_id)
        VALUES ('source-scope','kubernetes','kubernetes_live','source-key','kubernetes_live',
                'source-partition',$1,$1,'active','source-generation')`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO scope_generations
        (generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
        VALUES ('source-generation','source-scope','snapshot',$1,$1,'active')`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,
         source_fact_key,observed_at,ingested_at,payload)
        VALUES ('fact-1','source-scope','source-generation','kubernetes_live.pod_template',
                'source-key','kubernetes_live','source-key',$1,$1,
                '{"collector_instance_id":"source-instance","group_version_resource":"core/v1/pods","pod_phase":"Running","containers":[{"image":"registry.example.invalid/team/service:release","resolved_image_digest":"registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}'::jsonb)`, now)
	if err != nil {
		t.Fatal(err)
	}

	plan := func(bucket, domain string) (workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget) {
		runID := "scanner_worker:target-live:" + bucket
		workID := runID + ":item"
		run := workflow.Run{
			RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
			Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
			RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
		}
		target := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
			AuthorizationDomain: domain,
			ManifestDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Sources: []workflow.ScannerImageSourceBinding{{
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeDigest:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
				SourceFactID:      "fact-1", SourceCollectorInstanceID: "source-instance",
				SourceScopeID: "source-scope", SourceGenerationID: "source-generation",
				RuntimeImageReference: "registry.example.invalid/team/service:release",
			}},
		}
		target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
		item := workflow.WorkItem{
			WorkItemID: workID, RunID: runID,
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
			SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
			AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
			Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
		}
		return run, []workflow.WorkItem{item}, []workflow.ScannerImageTarget{target}
	}
	run, items, targets := plan("first", "domain-a")
	forged := targets[0]
	forged.Sources = append([]workflow.ScannerImageSourceBinding(nil), forged.Sources...)
	forged.Sources[0].Repository = "unrelated/service"
	forged.Sources[0].RuntimeImageReference = "registry.example.invalid/unrelated/service:release"
	if admission, err := store.CreateScannerRunWithTargets(ctx, run, items,
		[]workflow.ScannerImageTarget{forged}); !errors.Is(err, ErrScannerTargetNotAuthorized) || admission.InsertedWorkItems != 0 {
		t.Fatalf("forged source admission = %+v, %v; want atomic rejection", admission, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=FALSE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	if admission, err := store.CreateScannerRunWithTargets(ctx, run, items, targets); err == nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("disabled source admission = %+v, %v; want rejection", admission, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=TRUE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_records
		(fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,
		source_fact_key,observed_at,ingested_at,payload)
		SELECT 'fact-2',scope_id,generation_id,fact_kind,'source-key-2',source_system,
		'source-key-2',observed_at,ingested_at,payload FROM fact_records WHERE fact_id='fact-1'`); err != nil {
		t.Fatal(err)
	}
	secondSource := targets[0].Sources[0]
	secondSource.SourceFactID = "fact-2"
	targets[0].Sources = append(targets[0].Sources, secondSource)
	mixed := targets[0]
	mixed.Sources = append([]workflow.ScannerImageSourceBinding(nil), mixed.Sources...)
	mixed.Sources[1].Repository = "unrelated/service"
	mixed.Sources[1].RuntimeImageReference = "registry.example.invalid/unrelated/service:release"
	if admission, err := store.CreateScannerRunWithTargets(ctx, run, items,
		[]workflow.ScannerImageTarget{mixed}); !errors.Is(err, ErrScannerTargetNotAuthorized) || admission.InsertedWorkItems != 0 {
		t.Fatalf("mixed valid and forged source admission = %+v, %v; want atomic rejection", admission, err)
	}
	duplicateSource := targets[0]
	duplicateSource.Sources = append([]workflow.ScannerImageSourceBinding(nil), duplicateSource.Sources...)
	duplicateForged := duplicateSource.Sources[0]
	duplicateForged.Repository = "unrelated/service"
	duplicateForged.RuntimeImageReference = "registry.example.invalid/unrelated/service:release"
	duplicateSource.Sources = append(duplicateSource.Sources, duplicateForged)
	if admission, err := store.CreateScannerRunWithTargets(ctx, run, items,
		[]workflow.ScannerImageTarget{duplicateSource}); !errors.Is(err, ErrScannerTargetNotAuthorized) || admission.InsertedWorkItems != 0 {
		t.Fatalf("conflicting duplicate source admission = %+v, %v; want atomic rejection", admission, err)
	}
	admission, err := store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("first admission = %+v, %v; want one", admission, err)
	}
	var bindingCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scanner_image_target_sources
		WHERE scan_scope_id=$1`, targets[0].ScanTargetID).Scan(&bindingCount); err != nil || bindingCount != 2 {
		t.Fatalf("source bindings = %d, %v; want two on one target", bindingCount, err)
	}
	_, err = store.GetScannerImageTarget(ctx, items[0].WorkItemID, "claim-1", 1, "worker-1")
	if !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("unclaimed lookup = %v, want authorization rejection", err)
	}
	claimed, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
		OwnerID: "worker-1", ClaimID: "claim-1",
	}, now, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim = %t, %v", found, err)
	}
	got, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)
	if err != nil || got.ManifestDigest != targets[0].ManifestDigest {
		t.Fatalf("GetScannerImageTarget = %+v, %v", got, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE workflow_work_items
		SET authorization_scope_id='' WHERE work_item_id=$1`, claimed.WorkItemID); err != nil {
		t.Fatal(err)
	}
	if err := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID, ClaimID: claim.ClaimID,
		FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
		ObservedAt: now.Add(time.Second), LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("cleared derived authorization scope heartbeat = %v, want rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE workflow_work_items
		SET authorization_scope_id='source-scope' WHERE work_item_id=$1`, claimed.WorkItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload='{"collector_instance_id":"different-source","group_version_resource":"core/v1/pods","pod_phase":"Running","containers":[{"image":"registry.example.invalid/team/service:release","resolved_image_digest":"registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}'::jsonb WHERE fact_id='fact-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("source provenance mismatch lookup = %v, want authorization rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload='{"collector_instance_id":"source-instance","group_version_resource":"core/v1/pods","pod_phase":"Running","containers":[{"image":"registry.example.invalid/team/service:release","resolved_image_digest":"registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}'::jsonb WHERE fact_id='fact-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=jsonb_set(
		configuration, '{registry_domains}', configuration->'registry_domains' ||
		jsonb_build_array(jsonb_build_object('authorization_domain','domain-a',
		'registry','registry.example.invalid','base_url','https://other.example.invalid')))
		WHERE instance_id='target-live-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("ambiguous domain lookup = %v, want authorization rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=$1::jsonb WHERE instance_id='target-live-instance'`, `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken+1, claim.OwnerID); err == nil {
		t.Fatal("stale fencing token yielded registry target")
	}

	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=FALSE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("disabled source lookup = %v, want authorization rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=TRUE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration='{"derive_from_running_images":false}'::jsonb WHERE instance_id='target-live-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("revoked domain lookup = %v, want authorization rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=$1::jsonb WHERE instance_id='target-live-instance'`, `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=jsonb_set(configuration, '{registry_domains,0,base_url}', '"https://changed.example.invalid"'::jsonb) WHERE instance_id='target-live-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("changed registry config lookup = %v, want authorization rejection", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=$1::jsonb WHERE instance_id='target-live-instance'`, `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID, ClaimID: claim.ClaimID,
		FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
		ObservedAt: now.Add(time.Second), ResolvedScopeID: "unrelated-scope",
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("derived scope rewrite completion = %v, want rejection", err)
	}
	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID,
		ClaimID:    claim.ClaimID, FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
		ObservedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	var completedAt *time.Time
	if err := database.QueryRowContext(ctx, `SELECT completed_at FROM scanner_image_targets
		WHERE authorization_domain='domain-a' AND manifest_digest=$1`, targets[0].ManifestDigest).Scan(&completedAt); err != nil {
		t.Fatal(err)
	}
	if completedAt == nil {
		t.Fatal("completed scan missing durable completion timestamp")
	}
	// Work-item retention must leave the completed digest in the ledger.
	if _, err := database.ExecContext(ctx, `DELETE FROM workflow_runs WHERE run_id=$1`, claimed.RunID); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scanner_image_targets
		WHERE authorization_domain='domain-a' AND completed_at IS NOT NULL`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatalf("retained digest rows = %d, want 1", retained)
	}
	run, items, targets = plan("second", "domain-a")
	admission, err = store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("repeat admission = %+v, %v; want zero", admission, err)
	}
	// Instance flaps bump the pinned epochs but must not fork binding
	// identity: the same completed target still suppresses to zero.
	for _, enabled := range []bool{false, true} {
		if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=$1
			WHERE instance_id='source-instance'`, enabled); err != nil {
			t.Fatal(err)
		}
	}
	run, items, targets = plan("flap", "domain-a")
	admission, err = store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("post-flap repeat admission = %+v, %v; want zero", admission, err)
	}
	run, items, targets = plan("third", "domain-b")
	admission, err = store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("other domain admission = %+v, %v; want one", admission, err)
	}
	failedItem, failedClaim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
		OwnerID: "worker-2", ClaimID: "claim-2",
	}, now, time.Minute)
	if err != nil || !found || failedItem.WorkItemID != items[0].WorkItemID {
		t.Fatalf("failure claim = %+v, %t, %v", failedItem, found, err)
	}
	if err := store.FailClaimTerminal(ctx, workflow.ClaimMutation{
		WorkItemID: failedItem.WorkItemID, ClaimID: failedClaim.ClaimID,
		FencingToken: failedClaim.FencingToken, OwnerID: failedClaim.OwnerID,
		ObservedAt: now.Add(time.Second), FailureClass: "unsupported", FailureTerminal: true,
	}); err != nil {
		t.Fatal(err)
	}
	run, items, targets = plan("fourth", "domain-b")
	admission, err = store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("retry before run reconciliation = %+v, %v; want zero", admission, err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE workflow_runs SET status='failed' WHERE run_id=$1`, failedItem.RunID); err != nil {
		t.Fatal(err)
	}
	admission, err = store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("same terminal binding retried = %+v, %v; want zero", admission, err)
	}
	targets[0].Sources = append(targets[0].Sources, secondSource)
	admission, err = store.CreateScannerRunWithTargets(ctx, run, items, targets)
	if err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("alternate binding retry = %+v, %v; want one", admission, err)
	}
}

func TestScannerImageTargetConcurrentCrossInstanceAdmissionLive(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	config := `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"shared-domain","registry":"registry.example.invalid"}]}`
	for _, instanceID := range []string{"scanner-a", "scanner-b"} {
		if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
            (instance_id, collector_kind, mode, enabled, claims_enabled, configuration,
             last_observed_at, created_at, updated_at)
            VALUES ($1,'scanner_worker','continuous',TRUE,TRUE,$2::jsonb,$3,$3,$3)`,
			instanceID, config, now); err != nil {
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
	type outcome struct {
		admission workflow.RunAdmission
		err       error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for _, instanceID := range []string{"scanner-a", "scanner-b"} {
		go func(instanceID string) {
			<-start
			runID := "scanner_worker:race:" + instanceID
			workID := runID + ":item"
			target := workflow.ScannerImageTarget{
				WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
				AuthorizationDomain: "shared-domain",
				ManifestDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Sources: []workflow.ScannerImageSourceBinding{{
					Registry: "registry.example.invalid", Repository: "team/service",
					RuntimeDigest:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					RuntimeDigestKind:     workflow.ScannerRuntimeDigestManifest,
					RuntimeImageReference: "registry.example.invalid/team/service:release",
					SourceFactID:          "source-fact", SourceCollectorInstanceID: "source-instance",
					SourceScopeID: "source-scope", SourceGenerationID: "source-generation",
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
				CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: instanceID,
				SourceSystem: "scanner_worker", ScopeID: target.ScanTargetID, AcceptanceUnitID: target.ScanTargetID,
				SourceRunID: runID, GenerationID: runID, Status: workflow.WorkItemStatusPending,
				CreatedAt: now, UpdatedAt: now,
			}
			admission, err := store.CreateScannerRunWithTargets(ctx, run, []workflow.WorkItem{item}, []workflow.ScannerImageTarget{target})
			results <- outcome{admission: admission, err: err}
		}(instanceID)
	}
	close(start)
	inserted := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent admission: %v", result.err)
		}
		inserted += result.admission.InsertedWorkItems
	}
	if inserted != 1 {
		t.Fatalf("concurrent admitted items = %d, want 1", inserted)
	}
	var persisted int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scanner_image_targets WHERE authorization_domain='shared-domain'`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != 1 {
		t.Fatalf("persisted targets = %d, want 1", persisted)
	}
}
