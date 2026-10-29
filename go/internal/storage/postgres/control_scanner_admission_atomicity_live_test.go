// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that conflicting same-observation source bindings fail admission atomically with no partial plan
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerAdmissionRejectsConflictingDuplicateSourceLive(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	_, err := database.ExecContext(ctx, `INSERT INTO collector_instances
		(instance_id,collector_kind,mode,enabled,claims_enabled,configuration,
		 last_observed_at,created_at,updated_at)
		VALUES
		('atomicity-scanner','scanner_worker','continuous',TRUE,TRUE,
		 '{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid"}]}'::jsonb,$1,$1,$1),
		('atomicity-source','kubernetes_live','continuous',TRUE,TRUE,'{}'::jsonb,$1,$1,$1)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,
		 observed_at,ingested_at,status,active_generation_id)
		VALUES ('atomicity-scope','kubernetes','kubernetes_live','source-key',
		 'kubernetes_live','source-partition',$1,$1,'active','atomicity-generation')`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id,scope_id,trigger_kind,observed_at,ingested_at,status)
		VALUES ('atomicity-generation','atomicity-scope','snapshot',$1,$1,'active')`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO fact_records
		(fact_id,scope_id,generation_id,fact_kind,stable_fact_key,source_system,
		 source_fact_key,observed_at,ingested_at,payload)
		VALUES ('atomicity-fact','atomicity-scope','atomicity-generation',
		 'kubernetes_live.pod_template','source-key','kubernetes_live','source-key',$1,$1,
		 '{"collector_instance_id":"atomicity-source","group_version_resource":"core/v1/pods","pod_phase":"Running","containers":[{"image":"registry.example.invalid/team/service:release","resolved_image_digest":"registry.example.invalid/team/service@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"image":"registry.example.invalid/team/alternate:release","resolved_image_digest":"registry.example.invalid/team/alternate@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}'::jsonb)`, now)
	if err != nil {
		t.Fatal(err)
	}

	valid := workflow.ScannerImageSourceBinding{
		SourceFactID: "atomicity-fact", SourceCollectorInstanceID: "atomicity-source",
		SourceScopeID: "atomicity-scope", SourceGenerationID: "atomicity-generation",
		Registry: "registry.example.invalid", Repository: "team/service",
		RuntimeDigest:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RuntimeDigestKind:     workflow.ScannerRuntimeDigestManifest,
		RuntimeImageReference: "registry.example.invalid/team/service:release",
	}
	forged := valid
	forged.Repository = "unrelated/service"
	forged.RuntimeImageReference = "registry.example.invalid/unrelated/service:release"
	alternate := valid
	alternate.Repository = "team/alternate"
	alternate.RuntimeImageReference = "registry.example.invalid/team/alternate:release"
	for _, tc := range []struct {
		name    string
		sources []workflow.ScannerImageSourceBinding
	}{
		{name: "valid then forged", sources: []workflow.ScannerImageSourceBinding{valid, forged}},
		{name: "forged then valid", sources: []workflow.ScannerImageSourceBinding{forged, valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runID := "scanner:atomicity:" + tc.name
			workID := runID + ":work"
			run := workflow.Run{
				RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
				Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
				RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
			}
			target := workflow.ScannerImageTarget{
				WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
				AuthorizationDomain: "domain-a", ManifestDigest: valid.RuntimeDigest,
				Sources: tc.sources,
			}
			target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
			item := workflow.WorkItem{
				WorkItemID: workID, RunID: runID,
				CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "atomicity-scanner",
				SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
				AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
				Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
			}
			admission, err := store.CreateScannerRunWithTargets(ctx, run,
				[]workflow.WorkItem{item}, []workflow.ScannerImageTarget{target})
			if !errors.Is(err, ErrScannerTargetNotAuthorized) || admission.InsertedWorkItems != 0 {
				t.Fatalf("admission = %+v, %v; want atomic authorization rejection", admission, err)
			}
			var count int
			if err := database.QueryRowContext(ctx, `SELECT
				(SELECT count(*) FROM scanner_image_targets WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM scanner_image_target_sources WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM scanner_image_target_attempts WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM workflow_work_items WHERE work_item_id=$2)+
				(SELECT count(*) FROM workflow_runs WHERE run_id=$3)`,
				target.ScanTargetID, workID, runID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("durable rows after rejected admission = %d, %v; want zero", count, err)
			}
		})
	}
	runID := "scanner:atomicity:accepted"
	workID := runID + ":work"
	run := workflow.Run{
		RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
		Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
		RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
	}
	target := workflow.ScannerImageTarget{
		WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
		AuthorizationDomain: "domain-a", ManifestDigest: valid.RuntimeDigest,
		Sources: []workflow.ScannerImageSourceBinding{valid, alternate, valid},
	}
	target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
	item := workflow.WorkItem{
		WorkItemID: workID, RunID: runID,
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "atomicity-scanner",
		SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
		AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
		Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	admission, err := store.CreateScannerRunWithTargets(ctx, run,
		[]workflow.WorkItem{item}, []workflow.ScannerImageTarget{target})
	if err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("same-fact alternate admission = %+v, %v; want one work item", admission, err)
	}
	var bindings int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scanner_image_target_sources
		WHERE scan_scope_id=$1`, target.ScanTargetID).Scan(&bindings); err != nil || bindings != 2 {
		t.Fatalf("same-fact alternate bindings = %d, %v; want two", bindings, err)
	}
}
