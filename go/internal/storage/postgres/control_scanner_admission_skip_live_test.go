// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Readiness enrollment suspended under drift-gate option A (PR #9544 fourth
// round): the skip proof below stays in the Go suite and skips clean without
// proof DSNs; re-enroll post-refit. See live-tests-07b.yaml.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func readScannerProofCursor(t *testing.T, ctx context.Context, admin *sql.DB, instanceID string, policyRevision int64) workflow.RunningImageDiscoveryCursor {
	t.Helper()
	var cursor workflow.RunningImageDiscoveryCursor
	err := admin.QueryRowContext(ctx, `SELECT version, cycle_upper_observation_id,
		after_observation_id, after_authorization_domain
		FROM scanner_image_discovery_cursors
		WHERE scanner_collector_instance_id=$1 AND authorization_namespace='local'
		  AND tenant_id='' AND workspace_id='' AND subject_class=''
		  AND policy_revision_hash='' AND discovery_policy_revision=$2`,
		instanceID, policyRevision).Scan(
		&cursor.Version, &cursor.CycleUpperObservationID,
		&cursor.AfterObservationID, &cursor.AfterAuthorizationDomain)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		cursor = workflow.RunningImageDiscoveryCursor{}
	default:
		t.Fatal(err)
	}
	return cursor
}

func assertScannerProofCursor(t *testing.T, ctx context.Context, admin *sql.DB, instanceID string, policyRevision int64, want workflow.RunningImageDiscoveryCursor) {
	t.Helper()
	if got := readScannerProofCursor(t, ctx, admin, instanceID, policyRevision); got != want {
		t.Fatalf("persisted cursor = %+v, want %+v", got, want)
	}
}

// TestScannerAdmissionSkipsBadBindingAndAdvancesCursorLive proves verdict B
// for #9299 P2-3: a forged binding skips in isolation instead of wedging its
// page. Admission order is the ScanTargetID sort, so the digests below are
// chosen with a fixed middle sort for the bad plan: the bad plan sorts
// between the two good plans and neither sibling may stall on it.
func TestScannerAdmissionSkipsBadBindingAndAdvancesCursorLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	digestA := "sha256:" + strings.Repeat("c", 64)
	digestBad := "sha256:" + strings.Repeat("b", 64)
	digestB := "sha256:" + strings.Repeat("a", 64)
	for _, row := range []struct{ id, kind, config string }{
		{"skip-scanner", "scanner_worker", `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"}]}`},
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
	observationIDs := mustStageScannerProofGeneration(t, ctx, database, sourceStore, now, "skipgen",
		[]workflow.RunningImageObservation{
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "skipgen", ObjectID: "pod-a", SlotID: "regular:c-a",
				Registry: "registry.example.invalid", Repository: "team/alpha",
				RuntimeImageReference: "registry.example.invalid/team/alpha:release",
				RuntimeDigest:         digestA, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "skipgen", ObjectID: "pod-b", SlotID: "regular:c-b",
				Registry: "registry.example.invalid", Repository: "team/bravo",
				RuntimeImageReference: "registry.example.invalid/team/bravo:release",
				RuntimeDigest:         digestBad, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "skipgen", ObjectID: "pod-c", SlotID: "regular:c-c",
				Registry: "registry.example.invalid", Repository: "team/charlie",
				RuntimeImageReference: "registry.example.invalid/team/charlie:release",
				RuntimeDigest:         digestB, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
		})
	request := scannerProofDiscoveryRequest("skip-scanner")
	binding := func(observationID int64, digest, repository, reference string) workflow.ScannerImageSourceBinding {
		return workflow.ScannerImageSourceBinding{
			SourceObservationID: observationID, SourceCollectorInstanceID: "source-instance",
			SourceScopeID: "source-scope", SourceGenerationID: "skipgen",
			Registry: "registry.example.invalid", Repository: repository,
			RuntimeDigest: digest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: reference,
		}
	}
	validA := binding(observationIDs[0], digestA, "team/alpha", "registry.example.invalid/team/alpha:release")
	forgedBad := binding(observationIDs[1], digestBad, "unrelated/service", "registry.example.invalid/unrelated/service:release")
	canonicalBad := binding(observationIDs[1], digestBad, "team/bravo", "registry.example.invalid/team/bravo:release")
	validB := binding(observationIDs[2], digestB, "team/charlie", "registry.example.invalid/team/charlie:release")
	makeTarget := func(workID, digest string, sources []workflow.ScannerImageSourceBinding) workflow.ScannerImageTarget {
		target := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
			AuthorizationDomain: "domain-a", ManifestDigest: digest,
			Sources: sources,
		}
		target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
		return target
	}
	admit := func(name string, sourcesByDigest map[string][]workflow.ScannerImageSourceBinding) workflow.RunAdmission {
		t.Helper()
		runID := "scanner:skip:" + name
		var items []workflow.WorkItem
		var targets []workflow.ScannerImageTarget
		for _, digest := range []string{digestA, digestBad, digestB} {
			sources, ok := sourcesByDigest[digest]
			if !ok {
				continue
			}
			workID := runID + ":" + digest[7:11] + ":work"
			target := makeTarget(workID, digest, sources)
			targets = append(targets, target)
			items = append(items, workflow.WorkItem{
				WorkItemID: workID, RunID: runID,
				CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "skip-scanner",
				SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
				AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
				Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
			})
		}
		run := workflow.Run{
			RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
			Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
			RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
		}
		expected := readScannerProofCursor(t, ctx, database, "skip-scanner", request.DiscoveryPolicyRevision)
		next := workflow.RunningImageDiscoveryCursor{
			Version: expected.Version + 1, CycleUpperObservationID: int64(1) << 62,
			AfterObservationID: observationIDs[2], AfterAuthorizationDomain: "domain-a",
		}
		admission, err := coordinatorStore.CreateScannerRunWithTargetsAndCursor(
			ctx, run, items, targets, request, expected, next)
		if err != nil {
			t.Fatal(err)
		}
		assertScannerProofCursor(t, ctx, database, "skip-scanner", request.DiscoveryPolicyRevision, next)
		return admission
	}
	durableRows := func(target workflow.ScannerImageTarget, workID, runID string) int {
		t.Helper()
		var count int
		if err := database.QueryRowContext(ctx, `SELECT
			(SELECT count(*) FROM scanner_image_targets WHERE scan_scope_id=$1)+
			(SELECT count(*) FROM scanner_image_target_sources WHERE scan_scope_id=$1)+
			(SELECT count(*) FROM scanner_image_target_attempts WHERE scan_scope_id=$1)+
			(SELECT count(*) FROM workflow_work_items WHERE work_item_id=$2)`,
			target.ScanTargetID, workID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	badTarget := makeTarget("probe", digestBad, []workflow.ScannerImageSourceBinding{forgedBad})
	aTarget := makeTarget("probe", digestA, []workflow.ScannerImageSourceBinding{validA})
	bTarget := makeTarget("probe", digestB, []workflow.ScannerImageSourceBinding{validB})
	if aTarget.ScanTargetID >= badTarget.ScanTargetID || badTarget.ScanTargetID >= bTarget.ScanTargetID {
		t.Fatalf("bad plan does not sort between good plans: %s %s %s",
			aTarget.ScanTargetID, badTarget.ScanTargetID, bTarget.ScanTargetID)
	}

	admission := admit("cycle1", map[string][]workflow.ScannerImageSourceBinding{
		digestA: {validA}, digestBad: {forgedBad}, digestB: {validB},
	})
	if admission.InsertedWorkItems != 2 {
		t.Fatalf("mixed-page admission inserted %d work items, want 2 (good A and B)", admission.InsertedWorkItems)
	}
	if got := admission.AdmissionSkipped[schedule.DerivedTargetSkipReasonAdmissionUnauthorized]["domain-a"]; got != 1 {
		t.Fatalf("mixed-page admission skips = %+v, want one admission_unauthorized in domain-a", admission.AdmissionSkipped)
	}
	if got := durableRows(badTarget, "scanner:skip:cycle1:"+digestBad[7:11]+":work", "scanner:skip:cycle1"); got != 0 {
		t.Fatalf("bad plan durable rows = %d, want zero", got)
	}
	for _, tc := range []struct {
		name   string
		target workflow.ScannerImageTarget
		digest string
	}{
		{"good-A", aTarget, digestA}, {"good-B", bTarget, digestB},
	} {
		workID := "scanner:skip:cycle1:" + tc.digest[7:11] + ":work"
		if got := durableRows(tc.target, workID, "scanner:skip:cycle1"); got != 4 {
			t.Fatalf("%s durable rows = %d, want 4 (target, binding, attempt, work item)", tc.name, got)
		}
	}

	second := admit("cycle2", map[string][]workflow.ScannerImageSourceBinding{
		digestBad: {forgedBad},
	})
	if second.InsertedWorkItems != 0 {
		t.Fatalf("all-skipped admission inserted %d work items, want 0", second.InsertedWorkItems)
	}
	if got := second.AdmissionSkipped[schedule.DerivedTargetSkipReasonAdmissionUnauthorized]["domain-a"]; got != 1 {
		t.Fatalf("all-skipped admission skips = %+v, want one admission_unauthorized in domain-a", second.AdmissionSkipped)
	}
	if got := durableRows(badTarget, "scanner:skip:cycle2:"+digestBad[7:11]+":work", "scanner:skip:cycle2"); got != 0 {
		t.Fatalf("bad plan durable rows after all-skipped cycle = %d, want zero", got)
	}

	third := admit("cycle3", map[string][]workflow.ScannerImageSourceBinding{
		digestBad: {canonicalBad},
	})
	if third.InsertedWorkItems != 1 {
		t.Fatalf("corrected-binding admission inserted %d work items, want exactly 1", third.InsertedWorkItems)
	}
	if len(third.AdmissionSkipped) != 0 {
		t.Fatalf("corrected-binding admission skips = %+v, want none", third.AdmissionSkipped)
	}

	before := readScannerProofCursor(t, ctx, database, "skip-scanner", request.DiscoveryPolicyRevision)
	if _, err := database.ExecContext(ctx, `REVOKE EXECUTE ON FUNCTION
		eshu_security.validate_scanner_source_admission_v1(BIGINT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT[],BIGINT,UUID)
		FROM "`+principals.coordinator.name+`"`); err != nil {
		t.Fatal(err)
	}
	runID := "scanner:skip:cycle4"
	workID := runID + ":work"
	target := makeTarget(workID, digestA, []workflow.ScannerImageSourceBinding{validA})
	run := workflow.Run{
		RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
		Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
		RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
	}
	item := workflow.WorkItem{
		WorkItemID: workID, RunID: runID,
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "skip-scanner",
		SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
		AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
		Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	next := workflow.RunningImageDiscoveryCursor{
		Version: before.Version + 1, CycleUpperObservationID: int64(1) << 62,
		AfterObservationID: observationIDs[2], AfterAuthorizationDomain: "domain-a",
	}
	_, err := coordinatorStore.CreateScannerRunWithTargetsAndCursor(
		ctx, run, []workflow.WorkItem{item}, []workflow.ScannerImageTarget{target},
		request, before, next)
	if err == nil || errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("infrastructure proof error = %v, want a non-sentinel abort", err)
	}
	assertScannerProofCursor(t, ctx, database, "skip-scanner", request.DiscoveryPolicyRevision, before)
	if _, err := database.ExecContext(ctx, `GRANT EXECUTE ON FUNCTION
		eshu_security.validate_scanner_source_admission_v1(BIGINT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT[],BIGINT,UUID)
		TO "`+principals.coordinator.name+`"`); err != nil {
		t.Fatal(err)
	}
}
