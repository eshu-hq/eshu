// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Readiness enrollment suspended under drift-gate option A (PR #9544 fourth
// round): the matrix proof below stays in the Go suite and skips clean
// without proof DSNs; re-enroll post-refit. See live-tests-07b.yaml.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestScannerAdmissionAuthorityDenialMatrixLive proves the denial classes of
// validate_scanner_source_admission_v1 through the real proveScannerSource
// path: authority-state denials surface as ErrScannerTargetNotAuthorized
// (the admission transaction skips the binding and stays healthy) while
// caller-shape denials abort with a PostgreSQL error. Every denial runs
// inside its own transaction followed by a trivial statement and a commit,
// so a denial that poisoned its transaction would fail here instead of
// passing as a false skip.
func TestScannerAdmissionAuthorityDenialMatrixLive(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	digest := "sha256:" + strings.Repeat("c", 64)
	unpubDigest := "sha256:" + strings.Repeat("e", 64)
	for _, row := range []struct{ id, kind, config string }{
		{"matrix-scanner", "scanner_worker", `{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"}]}`},
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
	sourceStore := RunningImageSourceStore{DB: SQLDB{DB: principals.producer.db}}
	mkObservation := func(object, slot, repo, reference, observationDigest, generation string) workflow.RunningImageObservation {
		return workflow.RunningImageObservation{
			ProducerClass:      workflow.RunningImageProducerKubernetesPod,
			ObservationClass:   workflow.RunningImageClassRunningContainer,
			ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
			SourceGenerationID: generation, ObjectID: object, SlotID: slot,
			Registry: "registry.example.invalid", Repository: repo,
			RuntimeImageReference: reference,
			RuntimeDigest:         observationDigest, RuntimeDigestKind: workflow.RunningImageDigestManifest,
		}
	}
	mainIDs := mustStageScannerProofGeneration(t, ctx, database, sourceStore, now, "matrixgen",
		[]workflow.RunningImageObservation{
			mkObservation("pod-a", "regular:c-a", "team/alpha", "registry.example.invalid/team/alpha:release", digest, "matrixgen"),
		})
	unpubFence, err := sourceStore.StartCollection(ctx, RunningImageCollectionStart{
		ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
		FreshGenerationID: "matrixunpub", ProducerClass: workflow.RunningImageProducerKubernetesPod,
		ObservationClass: workflow.RunningImageClassRunningContainer,
		ObservedFrom:     now.Add(-time.Hour), ObservedThrough: now,
		ConsistencyClass: "observed_interval", LeaseSeconds: 600,
		PolicyRevision: scannerProofPolicyRevision, DatabaseIdentity: scannerProofDatabaseIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	unpubReceipt, err := sourceStore.StagePage(ctx, RunningImagePage{
		ProducerInstanceID: "source-instance", SourceScopeID: "source-scope",
		SourceGenerationID: "matrixunpub", CollectionFence: unpubFence,
		DeliveryID: "delivery-matrixunpub", PageIndex: 0,
		StartTokenSHA256: strings.Repeat("0", 64), NextTokenSHA256: strings.Repeat("1", 64),
		Terminal: true,
		Observations: []workflow.RunningImageObservation{
			mkObservation("pod-u", "regular:c-u", "team/upu", "registry.example.invalid/team/upu:release", unpubDigest, "matrixunpub"),
		},
		PolicyRevision: scannerProofPolicyRevision, DatabaseIdentity: scannerProofDatabaseIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unpubReceipt.Status != "accepted" {
		t.Fatalf("unpublished stage status = %q, want accepted", unpubReceipt.Status)
	}
	var unpubID int64
	if err := database.QueryRowContext(ctx, `SELECT observation_id FROM scanner_running_image_observations
		WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'
		  AND source_generation_id='matrixunpub'`).Scan(&unpubID); err != nil {
		t.Fatal(err)
	}

	item := workflow.WorkItem{CollectorInstanceID: "matrix-scanner"}
	baseTarget := workflow.ScannerImageTarget{
		AuthorizationNamespace: workflow.ScannerAuthorizationLocal,
		AuthorizationDomain:    "domain-a", ManifestDigest: digest,
	}
	baseSource := workflow.ScannerImageSourceBinding{
		SourceObservationID: mainIDs[0], SourceCollectorInstanceID: "source-instance",
		SourceScopeID: "source-scope", SourceGenerationID: "matrixgen",
		Registry: "registry.example.invalid", Repository: "team/alpha",
		RuntimeDigest: digest, RuntimeDigestKind: workflow.ScannerRuntimeDigestManifest,
		RuntimeImageReference: "registry.example.invalid/team/alpha:release",
	}
	baseRequest := scannerProofDiscoveryRequest("matrix-scanner")

	// proveInTx runs one proof inside its own transaction, then proves the
	// transaction is still healthy with a follow-up statement and a commit.
	// It returns the proof error, if any.
	proveInTx := func(target workflow.ScannerImageTarget, source workflow.ScannerImageSourceBinding, request workflow.RunningImageDiscoveryRequest) error {
		t.Helper()
		tx, err := SQLDB{DB: principals.coordinator.db}.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()
		_, proveErr := proveScannerSource(ctx, tx, item, target, source, request)
		if proveErr != nil {
			return proveErr
		}
		healthRows, err := tx.QueryContext(ctx, `SELECT 1`)
		if err != nil {
			t.Fatalf("transaction unhealthy after proof denial: %v", err)
		}
		var one int
		if !healthRows.Next() {
			_ = healthRows.Close()
			t.Fatal("transaction unhealthy after proof denial: no health row")
		}
		if err := healthRows.Scan(&one); err != nil || one != 1 {
			_ = healthRows.Close()
			t.Fatalf("transaction unhealthy after proof denial: %v", err)
		}
		_ = healthRows.Close()
		if err := healthRows.Err(); err != nil {
			t.Fatalf("transaction unhealthy after proof denial: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit after proof denial: %v", err)
		}
		committed = true
		return nil
	}
	mutate := func(stmt string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatal(err)
		}
	}
	// denySkip asserts one argument-only authority-state denial: the proof
	// surfaces the sentinel with a healthy transaction.
	denySkip := func(name string, target workflow.ScannerImageTarget, source workflow.ScannerImageSourceBinding, request workflow.RunningImageDiscoveryRequest) {
		t.Helper()
		if err := proveInTx(target, source, request); !errors.Is(err, ErrScannerTargetNotAuthorized) {
			t.Fatalf("%s: prove error = %v, want unauthorized skip", name, err)
		}
	}
	// denyMutate asserts one mutation-driven denial, runs the restore, then
	// re-proves the SAME triple: a broken restore fails on its own case,
	// never on a later one.
	denyMutate := func(name string, target workflow.ScannerImageTarget, source workflow.ScannerImageSourceBinding, request workflow.RunningImageDiscoveryRequest, restore func()) {
		t.Helper()
		if err := proveInTx(target, source, request); !errors.Is(err, ErrScannerTargetNotAuthorized) {
			t.Fatalf("%s: prove error = %v, want unauthorized skip", name, err)
		}
		restore()
		if err := proveInTx(target, source, request); err != nil {
			t.Fatalf("%s: re-proof after restore: %v", name, err)
		}
	}

	if err := proveInTx(baseTarget, baseSource, baseRequest); err != nil {
		t.Fatalf("canonical local proof: %v", err)
	}
	unknownSource := baseSource
	unknownSource.SourceObservationID = 999999999
	denySkip("observation absent", baseTarget, unknownSource, baseRequest)

	staleGenerationSource := baseSource
	staleGenerationSource.SourceGenerationID = "matrixgen-retired"
	denySkip("generation absent", baseTarget, staleGenerationSource, baseRequest)

	changedSource := baseSource
	changedSource.SourceObservationID = unpubID
	changedSource.SourceGenerationID = "matrixgen"
	changedSource.RuntimeDigest = digest
	denySkip("observation changed generation", baseTarget, changedSource, baseRequest)

	unpubSource := baseSource
	unpubSource.SourceObservationID = unpubID
	unpubSource.SourceGenerationID = "matrixunpub"
	unpubSource.Repository = "team/upu"
	unpubSource.RuntimeImageReference = "registry.example.invalid/team/upu:release"
	unpubSource.RuntimeDigest = unpubDigest
	denySkip("generation unpublished", baseTarget, unpubSource, baseRequest)

	mismatchedDigestTarget := baseTarget
	mismatchedDigestTarget.ManifestDigest = "sha256:" + strings.Repeat("0", 64)
	denySkip("runtime manifest mismatch", mismatchedDigestTarget, baseSource, baseRequest)

	narrowRequest := baseRequest
	narrowRequest.LocalSourceScopeIDs = []string{"other-scope"}
	denySkip("local allowlist drop", baseTarget, baseSource, narrowRequest)

	mutate(`UPDATE scanner_running_image_producer_grants SET active=FALSE
		WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'`)
	denyMutate("producer grant revoked", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE scanner_running_image_producer_grants SET active=TRUE
			WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'`)
	})

	mutate(`UPDATE scanner_running_image_active_scopes SET active=FALSE
		WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'`)
	denyMutate("source activation absent", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE scanner_running_image_active_scopes SET active=TRUE
			WHERE producer_instance_id='source-instance' AND source_scope_id='source-scope'`)
	})

	mutate(`UPDATE collector_instances SET enabled=FALSE WHERE instance_id='source-instance'`)
	denyMutate("source instance disabled", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE collector_instances SET enabled=TRUE WHERE instance_id='source-instance'`)
	})

	mutate(`UPDATE collector_instances SET enabled=FALSE WHERE instance_id='matrix-scanner'`)
	denyMutate("scanner instance disabled", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE collector_instances SET enabled=TRUE WHERE instance_id='matrix-scanner'`)
	})

	mutate(`UPDATE scanner_authority_epochs SET active=FALSE
		WHERE authority_kind='collector_instance' AND authority_key='["matrix-scanner"]'`)
	denyMutate("scanner instance epoch absent", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE scanner_authority_epochs SET active=TRUE
			WHERE authority_kind='collector_instance' AND authority_key='["matrix-scanner"]'`)
	})

	mutate(`UPDATE collector_instances SET configuration='{"derive_from_running_images":true,"registry_domains":[]}'::jsonb
		WHERE instance_id='matrix-scanner'`)
	denyMutate("registry authority ambiguous", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE collector_instances SET configuration='{"derive_from_running_images":true,"registry_domains":[{"authorization_domain":"domain-a","registry":"registry.example.invalid","base_url":"https://registry.example.invalid"}]}'::jsonb
			WHERE instance_id='matrix-scanner'`)
	})

	// The manifest-mapping check only runs for config-kind observations, so
	// this case flips the staged observation to config-kind with no
	// verification row present and restores manifest-kind afterwards.
	mutate(`UPDATE scanner_running_image_observations SET runtime_digest_kind='config'
		WHERE observation_id=$1`, mainIDs[0])
	denyMutate("manifest mapping absent", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE scanner_running_image_observations SET runtime_digest_kind='manifest'
			WHERE observation_id=$1`, mainIDs[0])
	})

	mutate(`UPDATE scanner_running_image_observations SET runtime_digest_kind='unknown'
		WHERE observation_id=$1`, mainIDs[0])
	denyMutate("unknown digest kind", baseTarget, baseSource, baseRequest, func() {
		mutate(`UPDATE scanner_running_image_observations SET runtime_digest_kind='manifest'
			WHERE observation_id=$1`, mainIDs[0])
	})

	mutate(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
		collector_kind, partition_key, observed_at, ingested_at, status)
		VALUES ('source-scope','running_images','kubernetes','matrix-seed',
		'kubernetes_live','matrix-seed',$1,$1,'active')
		ON CONFLICT (scope_id) DO NOTHING`, now)
	mutate(`INSERT INTO tenants (tenant_id, status, policy_revision_hash, created_at, updated_at)
		VALUES ('matrix-tenant','active','test-policy',$1,$1)`, now)
	mutate(`INSERT INTO workspaces (tenant_id, workspace_id, status, policy_revision_hash, created_at, updated_at)
		VALUES ('matrix-tenant','matrix-workspace','active','test-policy',$1,$1)`, now)
	mutate(`INSERT INTO tenant_scope_grants (tenant_id, workspace_id, scope_id, subject_class, grant_source,
		policy_revision_hash, effective_at, created_at, updated_at)
		VALUES ('matrix-tenant','matrix-workspace','source-scope','matrix-subject','matrix-seed',
		'test-policy',$1,$1,$1)`, now)
	// Ancestor epoch rows are minted by the staging path; the seed only
	// ensures they exist and are active for the canonical hosted proof.
	mutate(`INSERT INTO scanner_authority_epochs (authority_kind, authority_key, active)
		VALUES ('tenant','["matrix-tenant"]',TRUE),
			('workspace',(SELECT jsonb_build_array('matrix-tenant','matrix-workspace')::text),TRUE),
			('scope_grant',(SELECT jsonb_build_array('matrix-tenant','matrix-workspace','source-scope','matrix-subject')::text),TRUE)
		ON CONFLICT (authority_kind, authority_key) DO UPDATE SET active=TRUE`)
	hostedTarget := workflow.ScannerImageTarget{
		AuthorizationNamespace: workflow.ScannerAuthorizationHosted,
		TenantID:               "matrix-tenant", WorkspaceID: "matrix-workspace",
		SubjectClass: "matrix-subject", PolicyRevisionHash: "test-policy",
		AuthorizationDomain: "domain-a", ManifestDigest: digest,
	}
	hostedRequest := baseRequest
	hostedRequest.AuthorizationNamespace = workflow.ScannerAuthorizationHosted
	hostedRequest.TenantID = "matrix-tenant"
	hostedRequest.WorkspaceID = "matrix-workspace"
	hostedRequest.SubjectClass = "matrix-subject"
	hostedRequest.PolicyRevisionHash = "test-policy"
	if err := proveInTx(hostedTarget, baseSource, hostedRequest); err != nil {
		t.Fatalf("canonical hosted proof: %v", err)
	}

	mutate(`UPDATE tenants SET status='revoked' WHERE tenant_id='matrix-tenant'`)
	denyMutate("tenant revoked", hostedTarget, baseSource, hostedRequest, func() {
		mutate(`UPDATE tenants SET status='active' WHERE tenant_id='matrix-tenant'`)
	})

	mutate(`UPDATE scanner_authority_epochs SET active=FALSE
		WHERE authority_kind='tenant' AND authority_key='["matrix-tenant"]'`)
	denyMutate("tenant epoch absent", hostedTarget, baseSource, hostedRequest, func() {
		mutate(`UPDATE scanner_authority_epochs SET active=TRUE
			WHERE authority_kind='tenant' AND authority_key='["matrix-tenant"]'`)
	})

	mutate(`UPDATE workspaces SET status='revoked' WHERE tenant_id='matrix-tenant' AND workspace_id='matrix-workspace'`)
	denyMutate("workspace revoked", hostedTarget, baseSource, hostedRequest, func() {
		mutate(`UPDATE workspaces SET status='active' WHERE tenant_id='matrix-tenant' AND workspace_id='matrix-workspace'`)
	})

	mutate(`UPDATE scanner_authority_epochs SET active=FALSE
		WHERE authority_kind='workspace' AND authority_key=(SELECT jsonb_build_array('matrix-tenant','matrix-workspace')::text)`)
	denyMutate("workspace epoch absent", hostedTarget, baseSource, hostedRequest, func() {
		mutate(`UPDATE scanner_authority_epochs SET active=TRUE
			WHERE authority_kind='workspace' AND authority_key=(SELECT jsonb_build_array('matrix-tenant','matrix-workspace')::text)`)
	})

	mutate(`UPDATE tenant_scope_grants SET tombstoned_at=$1
		WHERE tenant_id='matrix-tenant' AND workspace_id='matrix-workspace'
		  AND scope_id='source-scope' AND subject_class='matrix-subject'`, now)
	denyMutate("scope grant revoked", hostedTarget, baseSource, hostedRequest, func() {
		mutate(`UPDATE tenant_scope_grants SET tombstoned_at=NULL
			WHERE tenant_id='matrix-tenant' AND workspace_id='matrix-workspace'
			  AND scope_id='source-scope' AND subject_class='matrix-subject'`)
	})

	mutate(`UPDATE scanner_authority_epochs SET active=FALSE
		WHERE authority_kind='scope_grant'
		  AND authority_key=(SELECT jsonb_build_array('matrix-tenant','matrix-workspace','source-scope','matrix-subject')::text)`)
	denyMutate("scope grant epoch absent", hostedTarget, baseSource, hostedRequest, func() {
		mutate(`UPDATE scanner_authority_epochs SET active=TRUE
			WHERE authority_kind='scope_grant'
			  AND authority_key=(SELECT jsonb_build_array('matrix-tenant','matrix-workspace','source-scope','matrix-subject')::text)`)
	})

	// fatalCase asserts a caller-shape denial: a PostgreSQL error naming
	// the invariant, never the sentinel. The item and request travel
	// together so the Go-side cross-checks pass and the SQL invariant is
	// the layer that refuses.
	fatalCase := func(name, want string, proveItem workflow.WorkItem, target workflow.ScannerImageTarget, source workflow.ScannerImageSourceBinding, request workflow.RunningImageDiscoveryRequest) {
		t.Helper()
		tx, err := SQLDB{DB: principals.coordinator.db}.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, proveErr := proveScannerSource(ctx, tx, proveItem, target, source, request)
		_ = tx.Rollback()
		if proveErr == nil || errors.Is(proveErr, ErrScannerTargetNotAuthorized) || !strings.Contains(proveErr.Error(), want) {
			t.Fatalf("%s: prove error = %v, want fatal PostgreSQL %q", name, proveErr, want)
		}
	}

	emptyItem := workflow.WorkItem{CollectorInstanceID: ""}
	emptyInstanceRequest := baseRequest
	emptyInstanceRequest.ScannerCollectorInstanceID = ""
	fatalCase("empty scanner instance input", "scanner canonical admission input invalid",
		emptyItem, baseTarget, baseSource, emptyInstanceRequest)

	coincideItem := workflow.WorkItem{CollectorInstanceID: "source-instance"}
	coincideRequest := baseRequest
	coincideRequest.ScannerCollectorInstanceID = "source-instance"
	fatalCase("coincident instances", "scanner and source instances cannot coincide",
		coincideItem, baseTarget, baseSource, coincideRequest)

	hostedBoundaryTarget := hostedTarget
	hostedBoundaryTarget.TenantID = ""
	hostedBoundaryRequest := hostedRequest
	hostedBoundaryRequest.TenantID = ""
	fatalCase("hosted boundary missing tenant", "scanner hosted boundary invalid",
		item, hostedBoundaryTarget, baseSource, hostedBoundaryRequest)

	localBoundaryTarget := baseTarget
	localBoundaryTarget.TenantID = "matrix-tenant"
	localBoundaryRequest := baseRequest
	localBoundaryRequest.TenantID = "matrix-tenant"
	fatalCase("local boundary carrying tenant", "scanner local boundary invalid",
		item, localBoundaryTarget, baseSource, localBoundaryRequest)
}
