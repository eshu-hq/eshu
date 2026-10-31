// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof for scanner target admission and completion, concurrent cross-instance admission, temp-shadow rejection, and the 241 public-schema guard refusal
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerImageTargetAdmissionAndCompletionLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
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
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rejectDigest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	observationIDs := mustStageScannerProofGeneration(t, ctx, database, sourceStore, now, "proof-generation",
		[]workflow.RunningImageObservation{
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "proof-generation", ObjectID: "pod-a", SlotID: "regular:c-a",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeImageReference: "registry.example.invalid/team/service:one",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "proof-generation", ObjectID: "pod-b", SlotID: "regular:c-b",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeImageReference: "registry.example.invalid/team/service:two",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "proof-generation", ObjectID: "pod-c", SlotID: "regular:c-c",
				Registry: "registry.example.invalid", Repository: "team/other",
				RuntimeImageReference: "registry.example.invalid/team/other:one",
				RuntimeDigest:         rejectDigest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "proof-generation", ObjectID: "pod-d", SlotID: "regular:c-d",
				Registry: "registry.example.invalid", Repository: "team/other",
				RuntimeImageReference: "registry.example.invalid/team/other:two",
				RuntimeDigest:         rejectDigest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
		})
	request := scannerProofDiscoveryRequest("target-live-instance")
	plan := func(bucket, domain string, observationID int64, reference string) (workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget) {
		runID := "scanner_worker:typed-live:" + bucket
		workID := runID + ":item"
		run := workflow.Run{
			RunID: runID, TriggerKind: workflow.TriggerKindSchedule,
			Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
			RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
		}
		target := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
			AuthorizationDomain: domain, ManifestDigest: digest,
			Sources: []workflow.ScannerImageSourceBinding{{
				SourceObservationID: observationID, SourceCollectorInstanceID: "source-instance",
				SourceScopeID: "source-scope", SourceGenerationID: "proof-generation",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeDigest: digest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
				RuntimeImageReference: reference,
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
	run, items, targets := plan("first", "domain-a", observationIDs[0], "registry.example.invalid/team/service:one")
	secondSource := targets[0].Sources[0]
	secondSource.SourceObservationID = observationIDs[1]
	secondSource.RuntimeImageReference = "registry.example.invalid/team/service:two"
	targets[0].Sources = append(targets[0].Sources, secondSource)
	// The rejection phases below run on an isolated digest so the rows they
	// admit never disturb the completion flow on the main digest.
	rejectRunID := "scanner_worker:typed-live:reject"
	rejectSource := func(observation int, repository, reference string, forge bool) workflow.ScannerImageSourceBinding {
		sourceRepository, sourceReference := repository, reference
		if forge {
			sourceRepository, sourceReference = "unrelated/service", "registry.example.invalid/unrelated/service:one"
		}
		return workflow.ScannerImageSourceBinding{
			SourceObservationID: observationIDs[observation], SourceCollectorInstanceID: "source-instance",
			SourceScopeID: "source-scope", SourceGenerationID: "proof-generation",
			Registry: "registry.example.invalid", Repository: sourceRepository,
			RuntimeDigest: rejectDigest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: sourceReference,
		}
	}
	rejectPlan := func(workID string, sources []workflow.ScannerImageSourceBinding) (workflow.Run, []workflow.WorkItem, []workflow.ScannerImageTarget) {
		rejectRun := workflow.Run{
			RunID: rejectRunID, TriggerKind: workflow.TriggerKindSchedule,
			Status: workflow.RunStatusCollectionPending, RequestedScopeSet: "[]",
			RequestedCollector: string(scope.CollectorScannerWorker), CreatedAt: now, UpdatedAt: now,
		}
		rejectTarget := workflow.ScannerImageTarget{
			WorkItemID: workID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
			AuthorizationDomain: "domain-a", ManifestDigest: rejectDigest,
			Sources: sources,
		}
		rejectTarget.ScanTargetID = workflow.ScannerImageTargetScopeID(rejectTarget)
		rejectItem := workflow.WorkItem{
			WorkItemID: workID, RunID: rejectRunID,
			CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
			SourceSystem: string(scope.CollectorScannerWorker), ScopeID: rejectTarget.ScanTargetID,
			AcceptanceUnitID: rejectTarget.ScanTargetID, SourceRunID: rejectRunID, GenerationID: rejectRunID,
			Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
		}
		return rejectRun, []workflow.WorkItem{rejectItem}, []workflow.ScannerImageTarget{rejectTarget}
	}
	admit := func(run workflow.Run, items []workflow.WorkItem, targets []workflow.ScannerImageTarget) (workflow.RunAdmission, error) {
		return mustAdmitScannerProofTargetsRaw(t, ctx, coordinatorStore, database,
			"target-live-instance", request, run, items, targets)
	}
	// A forged binding skips in isolation: no error, no work, one counted
	// skip, and the cursor still advances for the next cycle.
	rejectRun, rejectItems, rejectTargets := rejectPlan(rejectRunID+":forged",
		[]workflow.ScannerImageSourceBinding{rejectSource(2, "team/other", "registry.example.invalid/team/other:one", true)})
	if admission, err := admit(rejectRun, rejectItems,
		rejectTargets); err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("forged source admission = %+v, %v; want an isolated skip", admission, err)
	} else if got := admission.AdmissionSkipped[schedule.DerivedTargetSkipReasonAdmissionUnauthorized]["domain-a"]; got != 1 {
		t.Fatalf("forged source admission skips = %+v, want one admission_unauthorized", admission.AdmissionSkipped)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=FALSE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	// A disabled producer refuses the binding the same way: no error, no
	// work, one counted skip. The wedge this replaces used to pin the page
	// on exactly this revocation shape.
	rejectRun, rejectItems, rejectTargets = rejectPlan(rejectRunID+":disabled",
		[]workflow.ScannerImageSourceBinding{rejectSource(2, "team/other", "registry.example.invalid/team/other:one", false)})
	if admission, err := admit(rejectRun, rejectItems, rejectTargets); err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("disabled source admission = %+v, %v; want an isolated skip", admission, err)
	} else if got := admission.AdmissionSkipped[schedule.DerivedTargetSkipReasonAdmissionUnauthorized]["domain-a"]; got != 1 {
		t.Fatalf("disabled source admission skips = %+v, want one admission_unauthorized", admission.AdmissionSkipped)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=TRUE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	// Re-proof after re-enable returns a proof row through the same
	// function that denied: the skip was a denial, not a blacklist. A
	// direct call keeps this phase row-free so later phases keep their
	// suppression accounting.
	proofRows, proofErr := principals.coordinator.db.QueryContext(ctx,
		`SELECT * FROM eshu_security.validate_scanner_source_admission_v1(
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::text[],$12,$13::uuid)`,
		observationIDs[2], "target-live-instance", "local", "", "", "", "",
		"domain-a", rejectDigest, "", []string{"source-scope"},
		scannerProofPolicyRevision, scannerProofDatabaseIdentity)
	if proofErr != nil {
		t.Fatalf("re-enabled source proof: %v", proofErr)
	}
	if !proofRows.Next() {
		_ = proofRows.Close()
		t.Fatal("re-enabled source proof returned no rows, want one proof row")
	}
	_ = proofRows.Close()
	if err := proofRows.Err(); err != nil {
		t.Fatalf("re-enabled source proof rows: %v", err)
	}
	admission := mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("first admission = %+v; want one", admission)
	}
	var bindingCount int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM scanner_image_target_sources
		WHERE scan_scope_id=$1`, targets[0].ScanTargetID).Scan(&bindingCount); err != nil || bindingCount != 2 {
		t.Fatalf("source bindings = %d, %v; want two on one target", bindingCount, err)
	}
	_, err := workerStore.GetScannerImageTarget(ctx, items[0].WorkItemID, "claim-1", 1, "worker-1")
	if !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("unclaimed lookup = %v, want authorization rejection", err)
	}
	claimed, claim, found, err := workerStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
		OwnerID: "worker-1", ClaimID: "claim-1",
	}, now, time.Minute)
	if err != nil || !found {
		t.Fatalf("claim = %t, %v", found, err)
	}
	got, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID)
	if err != nil || got.ManifestDigest != targets[0].ManifestDigest {
		t.Fatalf("GetScannerImageTarget = %+v, %v", got, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE workflow_work_items
		SET authorization_scope_id='' WHERE work_item_id=$1`, claimed.WorkItemID); err != nil {
		t.Fatal(err)
	}
	if err := workerStore.HeartbeatClaim(ctx, workflow.ClaimMutation{
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
	// Revoking the producer grant under a live claim must fail the lookup:
	// the attempt authority rechecks the pinned grant epoch every call.
	if _, err := database.ExecContext(ctx, `UPDATE scanner_running_image_producer_grants
		SET active=FALSE WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'`); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("revoked producer grant lookup = %v, want authorization rejection", err)
	} else {
		t.Logf("revoked producer grant lookup rejection: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE scanner_running_image_producer_grants
		SET active=TRUE WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=jsonb_set(
		configuration, '{registry_domains}', configuration->'registry_domains' ||
		jsonb_build_array(jsonb_build_object('authorization_domain','domain-a',
		'registry','registry.example.invalid','base_url','https://other.example.invalid')))
		WHERE instance_id='target-live-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("ambiguous domain lookup = %v, want authorization rejection", err)
	} else {
		t.Logf("ambiguous domain lookup rejection: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=$1::jsonb WHERE instance_id='target-live-instance'`, `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken+1, claim.OwnerID); err == nil {
		t.Fatal("stale fencing token yielded registry target")
	}

	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=FALSE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	// A disabled instance surfaces P0001 through the authority wrapper, not
	// the 28000 authorization mapping (253 raises without ERRCODE, which
	// defaults to raise_exception), so this stays an any-error assertion.
	if _, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); err == nil {
		t.Fatal("disabled source lookup succeeded, want rejection")
	} else {
		t.Logf("disabled source lookup rejection: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=TRUE WHERE instance_id='source-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration='{"derive_from_running_images":false}'::jsonb WHERE instance_id='target-live-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("revoked domain lookup = %v, want authorization rejection", err)
	} else {
		t.Logf("revoked domain lookup rejection: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=$1::jsonb WHERE instance_id='target-live-instance'`, `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=jsonb_set(configuration, '{registry_domains,0,base_url}', '"https://changed.example.invalid"'::jsonb) WHERE instance_id='target-live-instance'`); err != nil {
		t.Fatal(err)
	}
	if _, err := workerStore.GetScannerImageTarget(ctx, claimed.WorkItemID, claim.ClaimID, claim.FencingToken, claim.OwnerID); !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("changed registry config lookup = %v, want authorization rejection", err)
	} else {
		t.Logf("changed registry config lookup rejection: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration=$1::jsonb WHERE instance_id='target-live-instance'`, `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"},{"authorization_domain":"domain-b","registry":"registry.example.invalid"}]}`); err != nil {
		t.Fatal(err)
	}
	if err := workerStore.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID, ClaimID: claim.ClaimID,
		FencingToken: claim.FencingToken, OwnerID: claim.OwnerID,
		ObservedAt: now.Add(time.Second), ResolvedScopeID: "unrelated-scope",
		ResolvedAcceptanceUnitID: targets[0].ScanTargetID,
		ResolvedSourceRunID:      run.RunID,
		ResolvedGenerationID:     run.RunID,
	}); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("derived scope rewrite completion = %v, want rejection", err)
	}
	// Derived completion requires the protected scan receipt the guard
	// rechecks: record the finished scan before completing the claim.
	var receiptBinding string
	if err := database.QueryRowContext(ctx, `SELECT binding_id FROM scanner_image_target_attempts
		WHERE work_item_id=$1`, claimed.WorkItemID).Scan(&receiptBinding); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scanner_image_scan_results
		(result_receipt_id,work_item_id,binding_id,scan_scope_id,scan_generation_id,
		 claim_id,claim_fencing_token,claim_owner_id,completed_authority_vector,completed_at)
		VALUES ('proof-receipt-1',$1,$2,$3,$4,$5,$6,$7,'{}'::jsonb,$8)`,
		claimed.WorkItemID, receiptBinding, targets[0].ScanTargetID, run.RunID,
		claim.ClaimID, claim.FencingToken, claim.OwnerID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := workerStore.CompleteClaim(ctx, workflow.ClaimMutation{
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
	run, items, targets = plan("second", "domain-a", observationIDs[0], "registry.example.invalid/team/service:one")
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 0 {
		t.Fatalf("repeat admission = %+v; want zero", admission)
	}
	// Instance flaps bump the pinned epochs but must not fork binding
	// identity: the same completed target still suppresses to zero.
	for _, enabled := range []bool{false, true} {
		if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET enabled=$1
			WHERE instance_id='source-instance'`, enabled); err != nil {
			t.Fatal(err)
		}
	}
	run, items, targets = plan("flap", "domain-a", observationIDs[0], "registry.example.invalid/team/service:one")
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 0 {
		t.Fatalf("post-flap repeat admission = %+v; want zero", admission)
	}
	run, items, targets = plan("third", "domain-b", observationIDs[0], "registry.example.invalid/team/service:one")
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("other domain admission = %+v; want one", admission)
	}
	failedItem, failedClaim, found, err := workerStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
		OwnerID: "worker-2", ClaimID: "claim-2",
	}, now, time.Minute)
	if err != nil || !found || failedItem.WorkItemID != items[0].WorkItemID {
		t.Fatalf("failure claim = %+v, %t, %v", failedItem, found, err)
	}
	if err := workerStore.FailClaimTerminal(ctx, workflow.ClaimMutation{
		WorkItemID: failedItem.WorkItemID, ClaimID: failedClaim.ClaimID,
		FencingToken: failedClaim.FencingToken, OwnerID: failedClaim.OwnerID,
		ObservedAt: now.Add(time.Second), FailureClass: "unsupported", FailureTerminal: true,
	}); err != nil {
		t.Fatal(err)
	}
	run, items, targets = plan("fourth", "domain-b", observationIDs[0], "registry.example.invalid/team/service:one")
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 0 {
		t.Fatalf("retry before run reconciliation = %+v; want zero", admission)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE workflow_runs SET status='failed' WHERE run_id=$1`, failedItem.RunID); err != nil {
		t.Fatal(err)
	}
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 0 {
		t.Fatalf("same terminal binding retried = %+v; want zero", admission)
	}
	alternateSource := targets[0].Sources[0]
	alternateSource.SourceObservationID = observationIDs[1]
	alternateSource.RuntimeImageReference = "registry.example.invalid/team/service:two"
	targets[0].Sources = append(targets[0].Sources, alternateSource)
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("alternate binding retry = %+v; want one", admission)
	}
	// A multi-source target whose every distinct binding is closed earns no
	// new attempt. Fail the alternate attempt and reconcile its run first:
	// its attempt pins the first binding, so the second binding stays open
	// and must still earn exactly one attempt on its own.
	alternateItem, alternateClaim, found, err := workerStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
		OwnerID: "worker-3", ClaimID: "claim-3",
	}, now, time.Minute)
	if err != nil || !found || alternateItem.WorkItemID != items[0].WorkItemID {
		t.Fatalf("alternate claim = %+v, %t, %v", alternateItem, found, err)
	}
	if err := workerStore.FailClaimTerminal(ctx, workflow.ClaimMutation{
		WorkItemID: alternateItem.WorkItemID, ClaimID: alternateClaim.ClaimID,
		FencingToken: alternateClaim.FencingToken, OwnerID: alternateClaim.OwnerID,
		ObservedAt: now.Add(time.Second), FailureClass: "unsupported", FailureTerminal: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE workflow_runs SET status='failed' WHERE run_id=$1`, alternateItem.RunID); err != nil {
		t.Fatal(err)
	}
	run, items, targets = plan("fifth", "domain-b", observationIDs[1], "registry.example.invalid/team/service:two")
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 1 {
		t.Fatalf("second binding admission = %+v; want one", admission)
	}
	secondItem, secondClaim, found, err := workerStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: "target-live-instance",
		OwnerID: "worker-4", ClaimID: "claim-4",
	}, now, time.Minute)
	if err != nil || !found || secondItem.WorkItemID != items[0].WorkItemID {
		t.Fatalf("second binding claim = %+v, %t, %v", secondItem, found, err)
	}
	if err := workerStore.FailClaimTerminal(ctx, workflow.ClaimMutation{
		WorkItemID: secondItem.WorkItemID, ClaimID: secondClaim.ClaimID,
		FencingToken: secondClaim.FencingToken, OwnerID: secondClaim.OwnerID,
		ObservedAt: now.Add(time.Second), FailureClass: "unsupported", FailureTerminal: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx,
		`UPDATE workflow_runs SET status='failed' WHERE run_id=$1`, secondItem.RunID); err != nil {
		t.Fatal(err)
	}
	// Both distinct bindings of the domain-b target are now terminally
	// failed, so re-admitting the identical two-source set must suppress
	// to zero eligible targets with no new attempt.
	run, items, targets = plan("sixth", "domain-b", observationIDs[0], "registry.example.invalid/team/service:one")
	twinSource := targets[0].Sources[0]
	twinSource.SourceObservationID = observationIDs[1]
	twinSource.RuntimeImageReference = "registry.example.invalid/team/service:two"
	targets[0].Sources = append(targets[0].Sources, twinSource)
	admission = mustAdmitScannerProofTargets(t, ctx, coordinatorStore, database,
		"target-live-instance", request, run, items, targets)
	if admission.InsertedWorkItems != 0 || admission.EligibleTargets != 0 {
		t.Fatalf("all-closed multi-source admission = %+v; want zero", admission)
	}
	// A mixed plan admits its valid binding while the forged binding on a
	// different observation skips: per-binding isolation inside one plan,
	// with one counted skip. (Same observation with differing claims is a
	// conflicting duplicate and drops the whole plan instead; see below.)
	rejectRun, rejectItems, rejectTargets = rejectPlan(rejectRunID+":mixed",
		[]workflow.ScannerImageSourceBinding{
			rejectSource(2, "team/other", "registry.example.invalid/team/other:one", false),
			rejectSource(3, "team/other", "registry.example.invalid/team/other:two", true),
		})
	if admission, err := admit(rejectRun, rejectItems,
		rejectTargets); err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("mixed valid and forged source admission = %+v, %v; want the valid binding admitted", admission, err)
	} else if got := admission.AdmissionSkipped[schedule.DerivedTargetSkipReasonAdmissionUnauthorized]["domain-a"]; got != 1 {
		t.Fatalf("mixed admission skips = %+v, want one admission_unauthorized", admission.AdmissionSkipped)
	}
	// A conflicting duplicate drops its whole plan: no error, no work, and
	// exactly one conflicting_duplicate skip. Conflict is detected before
	// any binding proves, so no binding-level skip leaks in regardless of
	// source order.
	rejectRun, rejectItems, rejectTargets = rejectPlan(rejectRunID+":conflict",
		[]workflow.ScannerImageSourceBinding{
			rejectSource(2, "team/other", "registry.example.invalid/team/other:one", false),
			rejectSource(2, "team/other", "registry.example.invalid/team/other:one", true),
		})
	if admission, err := admit(rejectRun, rejectItems,
		rejectTargets); err != nil || admission.InsertedWorkItems != 0 {
		t.Fatalf("conflicting duplicate source admission = %+v, %v; want a whole-plan skip", admission, err)
	} else if got := admission.AdmissionSkipped[schedule.DerivedTargetSkipReasonConflictingDuplicate]["domain-a"]; got != 1 {
		t.Fatalf("conflicting duplicate admission skips = %+v, want one conflicting_duplicate", admission.AdmissionSkipped)
	} else if len(admission.AdmissionSkipped) != 1 {
		t.Fatalf("conflicting duplicate admission skips = %+v, want only the plan-level reason", admission.AdmissionSkipped)
	}
	var conflictItems int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM workflow_work_items
		WHERE work_item_id=$1`, rejectRunID+":conflict").Scan(&conflictItems); err != nil || conflictItems != 0 {
		t.Fatalf("conflicted plan work items = %d, %v; want zero", conflictItems, err)
	}
}

// TestScannerMigration241RefusesNonPublicSchemaLive proves the 241
// fail-closed guard (#9299, mirroring the #7033 public-only refusal): a full
// bootstrap applied outside public refuses at 241 before any lane DDL, so a
// missed harness caller errors loudly instead of half-installing across two
// schemas. It asserts the refusal names the guard, none of 241's signature
// objects landed in the attempt's schema, and no 241 receipt was recorded.
func TestScannerMigration241RefusesNonPublicSchemaLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the 241 public-schema guard proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "guard241", func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `CREATE TABLE guard_probe (id integer PRIMARY KEY)`)
		return err
	})
	ctx := context.Background()
	// Apply the 241 definition directly: ApplyBootstrap skips public-only
	// definitions for isolated test schemas (see
	// skipPublicOnlyDefinitionsForIsolatedTestBootstrap), so the guard
	// proof targets the migration file itself, not the bootstrap entry
	// point. Production binaries never take the skip path.
	var target *Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "scanner_typed_source_and_results" {
			def := def
			target = &def
			break
		}
	}
	if target == nil {
		t.Fatal("241 scanner_typed_source_and_results definition missing from bootstrap")
	}
	err := ApplyDefinitions(ctx, SQLDB{DB: database}, []Definition{*target})
	if err == nil {
		t.Fatal("241 outside public succeeded, want the public-schema refusal")
	}
	if !strings.Contains(err.Error(), "public schema only") {
		t.Fatalf("241 error = %v, want the public-schema refusal", err)
	}
	if !strings.Contains(err.Error(), "scanner_typed_source_and_results") {
		t.Fatalf("241 error = %v, want the refusal to name the 241 migration", err)
	}
	// The shared proof database may carry lane objects from other runs, and
	// the pre-241 lane migration 240 legitimately installs its tables here,
	// so scope the no-partial-install assertion to 241's signature objects:
	// with the guard firing first, neither its first table, its authority
	// witness, nor its epoch sequence may exist in the attempt's schema.
	var schema string
	if err := database.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	for _, object := range []string{
		`scanner_running_image_producer_grants`,
		`scanner_authority_epochs`,
		`scanner_authority_epoch_seq`,
	} {
		var found int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace = $1::regnamespace AND relname = $2`, schema, object).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found != 0 {
			t.Fatalf("%s exists in %s after the guard fired: the refusal came after 241 DDL", object, schema)
		}
	}
	// Direct application records no receipts; a missing ledger table is the
	// same proof as an empty one. A present table must hold no 241 row.
	var receipts int
	err = database.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s.eshu_schema_migrations WHERE path LIKE '%%241%%'`, schema)).Scan(&receipts)
	if err != nil {
		if !strings.Contains(err.Error(), "does not exist") {
			t.Fatal(err)
		}
	} else if receipts != 0 {
		t.Fatalf("241 receipts = %d, want none: the guard must fire before the migration records", receipts)
	}
}

// TestScannerProtectedApiIgnoresTempShadowLive proves the locked search_path
// on eshu_security.start_running_image_collection_v1 is load-bearing (#9299):
// a low-privilege producer LOGIN with an attacker-first search_path plants a
// pg_temp shadow of scanner_running_image_producer_grants carrying a bogus
// grant for its own OID, and the protected API still refuses with the
// identical denial it returns without the shadow, minting no claim rows.
// An unqualified read in the same session does see the shadow (control), so
// a caller-inherited search_path would have minted authority from it.
func TestScannerProtectedApiIgnoresTempShadowLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	role := fmt.Sprintf("scanner_shadow_proof_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, `CREATE ROLE "`+role+`" LOGIN`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(), `DROP ROLE IF EXISTS "`+role+`"`)
	})
	var dbname string
	if err := database.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbname); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`GRANT TEMPORARY ON DATABASE "` + dbname + `" TO "` + role + `"`,
		`GRANT USAGE ON SCHEMA eshu_security TO "` + role + `"`,
		`GRANT EXECUTE ON FUNCTION eshu_security.start_running_image_collection_v1(TEXT,TEXT,TEXT,TEXT,TEXT,TIMESTAMPTZ,TIMESTAMPTZ,TEXT,INTEGER,BIGINT,UUID) TO "` + role + `"`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	var producerOID uint32
	if err := database.QueryRowContext(ctx, `SELECT oid FROM pg_roles WHERE rolname = $1`, role).Scan(&producerOID); err != nil {
		t.Fatal(err)
	}
	callCollection := func(withShadow bool) error {
		conn, err := database.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Pooled sessions outlive one arm: drop the previous arm's shadow,
		// reset the role and the path, and return the session clean, so the
		// second arm never inherits the first and the role drop in cleanup
		// owns nothing. pg_temp-qualified so the real public table can
		// never resolve here even if the path were wrong.
		defer func() {
			_, _ = conn.ExecContext(ctx, `DROP TABLE IF EXISTS pg_temp.scanner_running_image_producer_grants`)
			_, _ = conn.ExecContext(ctx, `RESET ROLE`)
			_, _ = conn.ExecContext(ctx, `SET search_path = public`)
			_ = conn.Close()
		}()
		if _, err := conn.ExecContext(ctx, `SET ROLE "`+role+`"`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `DROP TABLE IF EXISTS pg_temp.scanner_running_image_producer_grants`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE scanner_running_image_producer_grants (
			principal_oid OID NOT NULL, producer_instance_id TEXT NOT NULL,
			source_scope_id TEXT NOT NULL, producer_class TEXT NOT NULL,
			observation_class TEXT NOT NULL, contract_version INTEGER NOT NULL,
			grant_epoch BIGINT NOT NULL, active BOOLEAN NOT NULL
		)`); err != nil {
			t.Fatal(err)
		}
		if withShadow {
			if _, err := conn.ExecContext(ctx, `INSERT INTO scanner_running_image_producer_grants
				(principal_oid, producer_instance_id, source_scope_id, producer_class,
				 observation_class, contract_version, grant_epoch, active)
				VALUES ($1, 'shadow-instance', 'shadow-scope', 'kubernetes_pod',
					'running_container', 1, 1, TRUE)`, producerOID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := conn.ExecContext(ctx, `SET search_path = pg_temp, public`); err != nil {
			t.Fatal(err)
		}
		// Control: the unqualified read in this attacker-first session sees
		// the shadow, so the session setup would fool a caller-inherited path.
		var shadowRows int
		if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM scanner_running_image_producer_grants`).Scan(&shadowRows); err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{true: 1, false: 0}[withShadow]; shadowRows != want {
			t.Fatalf("control read saw %d shadow rows, want %d", shadowRows, want)
		}
		var fence int64
		err = conn.QueryRowContext(ctx, `SELECT eshu_security.start_running_image_collection_v1(
			'shadow-instance', 'shadow-scope', 'shadow-generation',
			'kubernetes_pod', 'running_container', now(), now(),
			'observed_interval', 60, 1, gen_random_uuid())`).Scan(&fence)
		return err
	}
	withoutShadow := callCollection(false)
	withShadow := callCollection(true)
	if withoutShadow == nil || withShadow == nil {
		t.Fatalf("collection call errors = %v / %v, want denials in both arms", withoutShadow, withShadow)
	}
	if withoutShadow.Error() != withShadow.Error() {
		t.Fatalf("denial changed with the shadow present:\nwithout: %v\nwith:    %v", withoutShadow, withShadow)
	}
	t.Logf("protected API denial (identical with and without the pg_temp shadow): %v", withShadow)
	for _, table := range []string{"scanner_running_image_scope_claims", "scanner_running_image_generations"} {
		var minted int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM public.`+table).Scan(&minted); err != nil {
			t.Fatal(err)
		}
		if minted != 0 {
			t.Fatalf("%s holds %d rows after denied calls, want none minted", table, minted)
		}
	}
}

func TestScannerImageTargetConcurrentCrossInstanceAdmissionLive(t *testing.T) {
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
            (instance_id, collector_kind, mode, enabled, claims_enabled, configuration,
             last_observed_at, created_at, updated_at)
            VALUES ($1,$2,'continuous',TRUE,TRUE,$3::jsonb,$4,$4,$4)`,
			row.id, row.kind, row.cfg, now); err != nil {
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
	observationIDs := mustStageScannerProofGeneration(t, ctx, database, sourceStore, now, "race",
		[]workflow.RunningImageObservation{
			{
				ProducerClass:      workflow.RunningImageProducerKubernetesPod,
				ObservationClass:   workflow.RunningImageClassRunningContainer,
				ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
				SourceGenerationID: "race", ObjectID: "pod-a", SlotID: "regular:c-a",
				Registry: "registry.example.invalid", Repository: "team/service",
				RuntimeImageReference: "registry.example.invalid/team/service:release",
				RuntimeDigest:         digest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
			},
		})
	type outcome struct {
		admission workflow.RunAdmission
		err       error
	}
	type cursorPair struct {
		expected, next workflow.RunningImageDiscoveryCursor
	}
	// Cursors are chained per scanner instance on separate rows, so both
	// racers read version zero up front and the goroutines below touch no
	// testing state at all.
	cursors := make(map[string]cursorPair, 2)
	for _, instanceID := range []string{"scanner-a", "scanner-b"} {
		var expected workflow.RunningImageDiscoveryCursor
		err := database.QueryRowContext(ctx, `SELECT version, cycle_upper_observation_id,
			after_observation_id, after_authorization_domain
			FROM scanner_image_discovery_cursors
			WHERE scanner_collector_instance_id=$1 AND authorization_namespace='local'
			  AND tenant_id='' AND workspace_id='' AND subject_class=''
			  AND policy_revision_hash='' AND discovery_policy_revision=$2`,
			instanceID, 1).Scan(
			&expected.Version, &expected.CycleUpperObservationID,
			&expected.AfterObservationID, &expected.AfterAuthorizationDomain)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			expected = workflow.RunningImageDiscoveryCursor{}
		default:
			t.Fatal(err)
		}
		const cursorUpper = int64(1) << 62
		cursors[instanceID] = cursorPair{expected: expected, next: workflow.RunningImageDiscoveryCursor{
			Version: expected.Version + 1, CycleUpperObservationID: cursorUpper,
		}}
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
				AuthorizationDomain: "shared-domain", ManifestDigest: digest,
				Sources: []workflow.ScannerImageSourceBinding{{
					SourceObservationID: observationIDs[0], SourceCollectorInstanceID: "source-instance",
					SourceScopeID: "source-scope", SourceGenerationID: "race",
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
				CollectorKind: scope.CollectorScannerWorker, CollectorInstanceID: instanceID,
				SourceSystem: string(scope.CollectorScannerWorker), ScopeID: target.ScanTargetID,
				AcceptanceUnitID: target.ScanTargetID, SourceRunID: runID, GenerationID: runID,
				Status: workflow.WorkItemStatusPending, CreatedAt: now, UpdatedAt: now,
			}
			// The loser's suppression is an ordinary result, not a failure.
			pair := cursors[instanceID]
			admission, err := coordinatorStore.CreateScannerRunWithTargetsAndCursor(ctx, run,
				[]workflow.WorkItem{item}, []workflow.ScannerImageTarget{target},
				scannerProofDiscoveryRequest(instanceID), pair.expected, pair.next)
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

// ---- typed-source vertical-slice fixture ----
//
// The scanner admission path below runs under three separated database
// principals that mirror the deployment role wiring: a typed-source
// producer stages and publishes observations, the workflow coordinator
// admits discovery cursors, and the scanner worker claims and completes
// attempts. Every principal holds exactly the inventory-required
// privileges for its profile plus working DML on ordinary lane tables,
// so a failure here names the grant production wiring must provide.

const (
	scannerProofDatabaseIdentity = "11111111-2222-4333-8444-555555555555"
	scannerProofPolicyRevision   = int64(1)
	scannerProofRolePassword     = "local-proof-only"
)

type scannerProofRole struct {
	name string
	oid  int64
	db   *sql.DB
}

type scannerProofPrincipals struct {
	admin       *sql.DB
	producer    *scannerProofRole
	coordinator *scannerProofRole
	worker      *scannerProofRole
}

func mustCreateScannerProofRole(t *testing.T, admin *sql.DB, ctx context.Context, suffix string) *scannerProofRole {
	t.Helper()
	name := fmt.Sprintf("scanner_proof_%s_%d", suffix, time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE ROLE "`+name+`" LOGIN PASSWORD '`+scannerProofRolePassword+`'`); err != nil {
		t.Fatal(err)
	}
	var oid int64
	if err := admin.QueryRowContext(ctx, `SELECT oid::bigint FROM pg_roles WHERE rolname=$1`, name).Scan(&oid); err != nil {
		t.Fatal(err)
	}
	dsn, err := url.Parse(os.Getenv(workflowControlIntegrationDSNEnv))
	if err != nil {
		t.Fatal(err)
	}
	var currentDB string
	if err := admin.QueryRowContext(ctx, `SELECT current_database()`).Scan(&currentDB); err != nil {
		t.Fatal(err)
	}
	dsn.User = url.UserPassword(name, scannerProofRolePassword)
	dsn.Path = "/" + currentDB
	pool, err := sql.Open("pgx", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `GRANT CONNECT ON DATABASE "`+currentDB+`" TO "`+name+`"`); err != nil {
		_ = pool.Close()
		t.Fatal(err)
	}
	role := &scannerProofRole{name: name, oid: oid, db: pool}
	t.Cleanup(func() {
		_ = pool.Close()
		_, _ = admin.ExecContext(context.Background(), `DROP ROLE IF EXISTS "`+name+`"`)
	})
	return role
}

// mustGrantScannerProofPrivileges grants exactly the inventory-required
// privileges for the profile plus working DML on ordinary lane tables.
// Anything more (protected DML, foreign EXECUTE, schema CREATE) would trip
// the fail-closed ACL mismatch the deployment guards rely on.
func mustGrantScannerProofPrivileges(t *testing.T, admin *sql.DB, ctx context.Context, role *scannerProofRole, profile string) {
	t.Helper()
	quoted := `"` + role.name + `"`
	// Inventory-required privileges first: every required row must hold and
	// every non-required row must not, so each grant below names the row's
	// own privilege. Sequences, protected DML, schema CREATE and foreign
	// EXECUTE stay absent exactly as the inventory demands; SECURITY
	// DEFINER bodies run with the owner's rights, so callers need no
	// sequence or table rights beyond these rows.
	rows, err := admin.QueryContext(ctx, `SELECT object_kind, object_identity, privilege_name
		FROM eshu_security.role_acl_inventory
		WHERE inventory_version=1 AND profile=$1 AND required AND object_kind<>'table_owner'`, profile)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind, identity, privilege string
		if err := rows.Scan(&kind, &identity, &privilege); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		statement := ""
		switch kind {
		case "table":
			statement = `GRANT ` + privilege + ` ON TABLE ` + identity + ` TO ` + quoted
		case "function":
			statement = `GRANT ` + privilege + ` ON FUNCTION ` + identity + ` TO ` + quoted
		case "schema":
			statement = `GRANT ` + privilege + ` ON SCHEMA ` + identity + ` TO ` + quoted
		default:
			_ = rows.Close()
			t.Fatalf("unsupported inventory kind %q for %s", kind, identity)
		}
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			_ = rows.Close()
			t.Fatalf("grant %s on %s: %v", privilege, identity, err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	// Working DML on ordinary tables the inventory does not constrain.
	// Anything the inventory forbids for this profile stays ungranted.
	grantWorking := func(privilege, tables string) {
		forbidden, err := admin.QueryContext(ctx, `SELECT object_identity
			FROM eshu_security.role_acl_inventory
			WHERE inventory_version=1 AND profile=$1 AND NOT required
			  AND object_kind='table' AND privilege_name=$2`, profile, privilege)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = forbidden.Close() }()
		denied := map[string]bool{}
		for forbidden.Next() {
			var identity string
			if err := forbidden.Scan(&identity); err != nil {
				t.Fatal(err)
			}
			denied[identity] = true
		}
		if err := forbidden.Err(); err != nil {
			t.Fatal(err)
		}
		allowed := []string{}
		for _, table := range strings.Split(tables, ",") {
			table = strings.TrimSpace(table)
			if !denied["public."+table] {
				allowed = append(allowed, table)
			}
		}
		if len(allowed) == 0 {
			return
		}
		if _, err := admin.ExecContext(ctx, `GRANT `+privilege+` ON `+
			strings.Join(allowed, ",")+" TO "+quoted); err != nil {
			t.Fatal(err)
		}
	}
	grantWorking("SELECT", `workflow_runs,workflow_work_items,workflow_claims,collector_instances,
		tenants,workspaces,tenant_scope_grants,
		scanner_image_targets,scanner_image_target_sources,scanner_image_target_attempts,
		scanner_image_discovery_cursors`)
	grantWorking("INSERT", `workflow_runs,workflow_work_items,workflow_claims,
		scanner_image_targets,scanner_image_target_sources,scanner_image_target_attempts`)
	grantWorking("UPDATE", `workflow_runs,workflow_work_items,workflow_claims,
		tenants,workspaces,tenant_scope_grants,
		scanner_image_targets,scanner_image_target_sources,scanner_image_target_attempts`)
	grantWorking("DELETE", `workflow_runs,workflow_work_items,workflow_claims,
		scanner_image_targets,scanner_image_target_sources,scanner_image_target_attempts`)
}

func mustSeedScannerProofPolicy(t *testing.T, admin *sql.DB, ctx context.Context, principals *scannerProofPrincipals) {
	t.Helper()
	contracts := []struct {
		role          *scannerProofRole
		workload, way string
		profile       string
	}{
		{principals.producer, "scanner-producer", "producer", "typed-source-producer"},
		{principals.coordinator, "workflow-coordinator", "control", "authority-control"},
		{principals.worker, "scanner-worker", "writer", "runtime-writer"},
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO eshu_security.role_policy_state
		(policy_revision, database_oid, database_identity, system_identifier,
		 inventory_version, owner_role_oid, authority_epoch, active)
		SELECT $1, (SELECT oid FROM pg_database WHERE datname=current_database()), $2,
		 (SELECT system_identifier FROM pg_control_system()), 1,
		 (SELECT oid FROM pg_roles WHERE rolname=current_user), 1, TRUE`,
		scannerProofPolicyRevision, scannerProofDatabaseIdentity); err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		if _, err := admin.ExecContext(ctx, `INSERT INTO eshu_security.role_contracts
			(contract_id, policy_revision, workload_class, connection_route, profile,
			 session_role_oid, effective_role_oid, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$6,TRUE)`,
			"proof-"+contract.profile, scannerProofPolicyRevision,
			contract.workload, contract.way, contract.profile, contract.role.oid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO scanner_running_image_producer_grants
		(principal_oid, producer_instance_id, source_scope_id, producer_class,
		 observation_class, contract_version, active)
		VALUES ($1,'source-instance','source-scope','kubernetes_pod','running_container',1,TRUE)`,
		principals.producer.oid); err != nil {
		t.Fatal(err)
	}
}

// mustStageScannerProofGeneration stages and publishes one terminal page of
// manifest observations through the producer principal and returns the
// observation IDs in intake order.
func mustStageScannerProofGeneration(t *testing.T, ctx context.Context, admin *sql.DB, source RunningImageSourceStore,
	now time.Time, generation string, observations []workflow.RunningImageObservation,
) []int64 {
	t.Helper()
	fence, err := source.StartCollection(ctx, RunningImageCollectionStart{
		ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
		FreshGenerationID: generation, ProducerClass: workflow.RunningImageProducerKubernetesPod,
		ObservationClass: workflow.RunningImageClassRunningContainer,
		ObservedFrom:     now.Add(-time.Hour), ObservedThrough: now,
		ConsistencyClass: "observed_interval", LeaseSeconds: 600,
		PolicyRevision: scannerProofPolicyRevision, DatabaseIdentity: scannerProofDatabaseIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := source.StagePage(ctx, RunningImagePage{
		ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
		SourceGenerationID: generation, CollectionFence: fence,
		DeliveryID: "delivery-" + generation, PageIndex: 0,
		StartTokenSHA256: strings.Repeat("0", 64), NextTokenSHA256: strings.Repeat("1", 64),
		Terminal: true, Observations: observations,
		PolicyRevision: scannerProofPolicyRevision, DatabaseIdentity: scannerProofDatabaseIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "accepted" {
		t.Fatalf("stage page status = %q, want accepted", receipt.Status)
	}
	manifest := sha256.Sum256([]byte("0:" + receipt.RequestSHA256))
	if _, err := source.PublishGeneration(ctx, RunningImagePublication{
		ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
		SourceGenerationID: generation, CollectionFence: fence,
		ExpectedPages: 1, ExpectedSlots: len(observations),
		ManifestSHA256: hex.EncodeToString(manifest[:]),
		PolicyRevision: scannerProofPolicyRevision, DatabaseIdentity: scannerProofDatabaseIdentity,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := admin.QueryContext(ctx, `SELECT observation_id
		FROM scanner_running_image_observations
		WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'
		  AND source_generation_id=$1 ORDER BY observation_id`, generation)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != len(observations) {
		t.Fatalf("staged observations = %d, want %d", len(ids), len(observations))
	}
	return ids
}

func scannerProofDiscoveryRequest(instanceID string) workflow.RunningImageDiscoveryRequest {
	return workflow.RunningImageDiscoveryRequest{
		ScannerCollectorInstanceID: instanceID,
		AuthorizationNamespace:     workflow.ScannerAuthorizationLocal,
		LocalSourceScopeIDs:        []string{"source-scope"},
		DiscoveryPolicyRevision:    1,
		ExpectedRolePolicyRevision: scannerProofPolicyRevision,
		ExpectedDatabaseIdentity:   scannerProofDatabaseIdentity,
		Limit:                      256,
	}
}

// mustAdmitScannerProofTargets admits through the coordinator principal,
// advancing the durable discovery cursor. The cursor row is read back on
// every call so sequential admissions chain versions without the caller
// tracking them.
func mustAdmitScannerProofTargets(t *testing.T, ctx context.Context, store *WorkflowControlStore,
	admin *sql.DB, instanceID string, request workflow.RunningImageDiscoveryRequest,
	run workflow.Run, items []workflow.WorkItem, targets []workflow.ScannerImageTarget,
) workflow.RunAdmission {
	t.Helper()
	var expected workflow.RunningImageDiscoveryCursor
	err := admin.QueryRowContext(ctx, `SELECT version, cycle_upper_observation_id,
		after_observation_id, after_authorization_domain
		FROM scanner_image_discovery_cursors
		WHERE scanner_collector_instance_id=$1 AND authorization_namespace='local'
		  AND tenant_id='' AND workspace_id='' AND subject_class=''
		  AND policy_revision_hash='' AND discovery_policy_revision=$2`,
		instanceID, request.DiscoveryPolicyRevision).Scan(
		&expected.Version, &expected.CycleUpperObservationID,
		&expected.AfterObservationID, &expected.AfterAuthorizationDomain)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		expected = workflow.RunningImageDiscoveryCursor{}
	default:
		t.Fatal(err)
	}
	const cursorUpper = int64(1) << 62
	next := workflow.RunningImageDiscoveryCursor{
		Version: expected.Version + 1, CycleUpperObservationID: cursorUpper,
		AfterObservationID: 0, AfterAuthorizationDomain: "",
	}
	admission, err := store.CreateScannerRunWithTargetsAndCursor(
		ctx, run, items, targets, request, expected, next)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

// mustAdmitScannerProofTargetsRaw chains the cursor like
// mustAdmitScannerProofTargets but returns the raw outcome for rejection
// assertions.
func mustAdmitScannerProofTargetsRaw(t *testing.T, ctx context.Context, store *WorkflowControlStore,
	admin *sql.DB, instanceID string, request workflow.RunningImageDiscoveryRequest,
	run workflow.Run, items []workflow.WorkItem, targets []workflow.ScannerImageTarget,
) (workflow.RunAdmission, error) {
	t.Helper()
	var expected workflow.RunningImageDiscoveryCursor
	err := admin.QueryRowContext(ctx, `SELECT version, cycle_upper_observation_id,
		after_observation_id, after_authorization_domain
		FROM scanner_image_discovery_cursors
		WHERE scanner_collector_instance_id=$1 AND authorization_namespace='local'
		  AND tenant_id='' AND workspace_id='' AND subject_class=''
		  AND policy_revision_hash='' AND discovery_policy_revision=$2`,
		instanceID, request.DiscoveryPolicyRevision).Scan(
		&expected.Version, &expected.CycleUpperObservationID,
		&expected.AfterObservationID, &expected.AfterAuthorizationDomain)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		expected = workflow.RunningImageDiscoveryCursor{}
	default:
		t.Fatal(err)
	}
	const cursorUpper = int64(1) << 62
	next := workflow.RunningImageDiscoveryCursor{
		Version: expected.Version + 1, CycleUpperObservationID: cursorUpper,
		AfterObservationID: 0, AfterAuthorizationDomain: "",
	}
	return store.CreateScannerRunWithTargetsAndCursor(
		ctx, run, items, targets, request, expected, next)
}
