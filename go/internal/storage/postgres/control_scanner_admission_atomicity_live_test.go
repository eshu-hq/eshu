// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that conflicting same-observation source bindings fail admission atomically with no partial plan
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerAdmissionRejectsConflictingDuplicateSourceLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	siblingDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	siblingDigestTwo := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	for _, row := range []struct{ id, kind, config string }{
		{"atomicity-scanner", "scanner_worker", `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"}]}`},
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
	principals := &scannerProofPrincipals{admin: database}
	principals.producer = mustCreateScannerProofRole(t, database, ctx, "producer")
	principals.coordinator = mustCreateScannerProofRole(t, database, ctx, "coordinator")
	principals.worker = mustCreateScannerProofRole(t, database, ctx, "worker")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.producer, "typed-source-producer")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.coordinator, "authority-control")
	mustGrantScannerProofPrivileges(t, database, ctx, principals.worker, "runtime-writer")
	mustSeedScannerProofPolicy(t, database, ctx, principals)
	coordinatorStore := NewWorkflowControlStore(SQLDB{DB: principals.coordinator.db})
	sourceStore := RunningImageSourceStore{DB: SQLDB{DB: principals.producer.db}}
	observationIDs := mustStageScannerProofGeneration(t, ctx, database, sourceStore, now, "atomicity",
		[]workflow.RunningImageObservation{
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "atomicity", ObjectID: "pod-a", SlotID: "regular:c-a",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeImageReference: "registry.example.invalid/team/service:release",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "atomicity", ObjectID: "pod-b", SlotID: "regular:c-b",
				Registry: "registry.example.invalid", Repository: "team/alternate",
				RuntimeImageReference: "registry.example.invalid/team/alternate:release",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "atomicity", ObjectID: "pod-c", SlotID: "regular:c-c",
				Registry: "registry.example.invalid", Repository: "team/sibling",
				RuntimeImageReference: "registry.example.invalid/team/sibling:release",
				RuntimeDigest:         siblingDigest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "atomicity", ObjectID: "pod-d", SlotID: "regular:c-d",
				Registry: "registry.example.invalid", Repository: "team/sibling",
				RuntimeImageReference: "registry.example.invalid/team/sibling:release",
				RuntimeDigest:         siblingDigestTwo, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
		})
	request := scannerProofDiscoveryRequest("atomicity-scanner")
	plan := func(name string, sources []workflow.ScannerImageSourceBinding) (workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget) {
		runID := "scanner:atomicity:" + name
		workID := runID + ":work"
		target := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
			AuthorizationDomain: "domain-a", ManifestDigest: digest,
			Sources: sources,
		}
		target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
		run := workflow.Run{
			RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
			Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
			RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
		}
		item := workflow.WorkItem{
			WorkItemID: workID, RunID: runID,
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "atomicity-scanner",
			SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
			AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
			Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
		}
		return run, []workflow.WorkItem{item}, []workflow.ScannerImageTarget{target}
	}
	binding := func(observationID int64, bindingDigest, repository, reference string) workflow.ScannerImageSourceBinding {
		return workflow.ScannerImageSourceBinding{
			SourceObservationID: observationID, SourceCollectorInstanceID: "source-instance",
			SourceScopeID: "source-scope", SourceGenerationID: "atomicity",
			Registry: "registry.example.invalid", Repository: repository,
			RuntimeDigest: bindingDigest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: reference,
		}
	}
	valid := binding(observationIDs[0], digest, "team/service", "registry.example.invalid/team/service:release")
	forged := binding(observationIDs[0], digest, "unrelated/service", "registry.example.invalid/unrelated/service:release")
	alternate := binding(observationIDs[1], digest, "team/alternate", "registry.example.invalid/team/alternate:release")
	// A conflicting duplicate no longer fails the page: the conflicted plan
	// skips whole while a clean sibling in the same admission still admits
	// and the cursor still advances. The zero-durable-rows assertion for the
	// conflicted target stands unchanged.
	for _, tc := range []struct {
		name          string
		sources       []workflow.ScannerImageSourceBinding
		siblingIndex  int
		siblingDigest string
	}{
		{name: "valid-then-forged", sources: []workflow.ScannerImageSourceBinding{valid, forged}, siblingIndex: 2, siblingDigest: siblingDigest},
		{name: "forged-then-valid", sources: []workflow.ScannerImageSourceBinding{forged, valid}, siblingIndex: 3, siblingDigest: siblingDigestTwo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each subtest gets its own sibling observation: a sibling
			// admitted by an earlier subtest stays open under its still
			// non-terminal run, and open-target suppression would hide it
			// from the later subtest.
			siblingBinding := binding(observationIDs[tc.siblingIndex], tc.siblingDigest, "team/sibling", "registry.example.invalid/team/sibling:release")
			runID := "scanner:atomicity:mixed:" + tc.name
			siblingTarget := workflow.ScannerImageTarget{
				WorkItemID:             runID + ":sibling:work",
				AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
				AuthorizationDomain:    "domain-a", ManifestDigest: tc.siblingDigest,
				Sources: []workflow.ScannerImageSourceBinding{siblingBinding},
			}
			siblingTarget.ScanTargetID = workflow.ScannerImageTargetScopeID(siblingTarget)
			conflictTarget := workflow.ScannerImageTarget{
				WorkItemID:             runID + ":conflict:work",
				AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
				AuthorizationDomain:    "domain-a", ManifestDigest: digest,
				Sources: tc.sources,
			}
			conflictTarget.ScanTargetID = workflow.ScannerImageTargetScopeID(conflictTarget)
			run := workflow.Run{
				RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
				Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
				RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
			}
			makeItem := func(target workflow.ScannerImageTarget) workflow.WorkItem {
				return workflow.WorkItem{
					WorkItemID: target.WorkItemID, RunID: runID,
					CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "atomicity-scanner",
					SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
					AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
					Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
				}
			}
			items := []workflow.WorkItem{makeItem(siblingTarget), makeItem(conflictTarget)}
			targets := []workflow.ScannerImageTarget{siblingTarget, conflictTarget}
			before := readScannerProofCursor(t, ctx, database, "atomicity-scanner", request.DiscoveryPolicyRevision)
			admission, err := mustAdmitScannerProofTargetsRaw(t, ctx, coordinatorStore, database,
				"atomicity-scanner", request, run, items, targets)
			if err != nil {
				t.Fatalf("mixed admission = %+v, %v; want the conflicted plan skipped, not a page failure", admission, err)
			}
			if admission.InsertedWorkItems != 1 {
				t.Fatalf("mixed admission inserted %d work items, want 1 (clean sibling only)", admission.InsertedWorkItems)
			}
			if got := admission.AdmissionSkipped[schedule.DerivedTargetSkipReasonConflictingDuplicate]["domain-a"]; got != 1 {
				t.Fatalf("mixed admission skips = %+v, want one conflicting_duplicate in domain-a", admission.AdmissionSkipped)
			}
			after := readScannerProofCursor(t, ctx, database, "atomicity-scanner", request.DiscoveryPolicyRevision)
			if after.Version != before.Version+1 {
				t.Fatalf("cursor version = %d, want %d", after.Version, before.Version+1)
			}
			var count int
			if err := database.QueryRowContext(ctx, `SELECT
				(SELECT count(*) FROM scanner_image_targets WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM scanner_image_target_sources WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM scanner_image_target_attempts WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM workflow_work_items WHERE work_item_id=$2)`,
				conflictTarget.ScanTargetID, conflictTarget.WorkItemID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("conflicted plan durable rows = %d, %v; want zero", count, err)
			}
			if err := database.QueryRowContext(ctx, `SELECT
				(SELECT count(*) FROM scanner_image_targets WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM scanner_image_target_sources WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM scanner_image_target_attempts WHERE scan_scope_id=$1)+
				(SELECT count(*) FROM workflow_work_items WHERE work_item_id=$2)`,
				siblingTarget.ScanTargetID, siblingTarget.WorkItemID).Scan(&count); err != nil || count != 4 {
				t.Fatalf("sibling durable rows = %d, %v; want 4", count, err)
			}
		})
	}
	run, items, targets := plan("accepted", []workflow.ScannerImageSourceBinding{valid, alternate, valid})
	admission := mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"atomicity-scanner", request, run, items, targets)
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("same-observation identical-duplicate admission = %+v; want one work item", admission)
	}
	var bindings int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scanner_image_target_sources
		WHERE scan_scope_id=$1`, targets[0].ScanTargetID).Scan(&bindings); err != nil || bindings != 2 {
		t.Fatalf("same-observation identical-duplicate bindings = %d, %v; want two", bindings, err)
	}
}
