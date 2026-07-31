// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// seedAWSResourceFactMinimal inserts a bare aws_resource fact (arn +
// resource_type only) -- enough for cloudruntime.Classify to see a non-nil
// cloud side and no comparable attributes, so the finding kind is driven
// purely by state presence/absence.
func seedAWSResourceFactMinimal(
	t *testing.T, ctx context.Context, db *sql.DB,
	scopeID, generationID, arn, resourceType string, now time.Time,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"arn": arn, "resource_id": arn, "resource_type": resourceType,
	})
	if err != nil {
		t.Fatalf("marshal aws_resource payload: %v", err)
	}
	factID := "aws_resource:" + arn
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO fact_records (
		    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		    collector_kind, source_confidence, source_system, source_fact_key,
		    observed_at, ingested_at, is_tombstone, payload
		) VALUES ($1, $2, $3, 'aws_resource', $1, 'aws', 'inferred', 'aws', $1, $4, $4, false, $5::jsonb)
		ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload`,
		factID, scopeID, generationID, now, string(payload),
	); err != nil {
		t.Fatalf("seed aws_resource fact for %s: %v", arn, err)
	}
}

// seedTerraformStateResourceFactMinimal inserts a bare terraform_state_resource
// fact (attributes.arn + type + address only), enough for the ARN join to
// resolve once the owning state_snapshot scope is active.
func seedTerraformStateResourceFactMinimal(
	t *testing.T, ctx context.Context, db *sql.DB,
	scopeID, generationID, address, resourceType, arn string, now time.Time,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"address": address, "type": resourceType,
		"attributes": map[string]any{"arn": arn},
	})
	if err != nil {
		t.Fatalf("marshal terraform_state_resource payload: %v", err)
	}
	factID := "terraform_state_resource:" + scopeID + ":" + address
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO fact_records (
		    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
		    collector_kind, source_confidence, source_system, source_fact_key,
		    observed_at, ingested_at, is_tombstone, payload
		) VALUES ($1, $2, $3, 'terraform_state_resource', $1, 'terraform', 'inferred', 'terraform', $1, $4, $4, false, $5::jsonb)
		ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload`,
		factID, scopeID, generationID, now, string(payload),
	); err != nil {
		t.Fatalf("seed terraform_state_resource fact for %s: %v", arn, err)
	}
}

// TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive is the
// deterministic (not statistical) reproduction #5837's issue asked for,
// mirroring the owner's own technique of forcing the collector order rather
// than racing for the 1-in-3 shape: instead of forcing the AWS and
// terraform-state COLLECTORS to drain in a specific order, this forces the
// EVIDENCE STATE they leave behind directly -- a state_snapshot scope
// registered with a 'pending' generation is the exact race window, held open
// by construction rather than by timing, so it reproduces 100% of runs, not
// 1-in-3.
//
// It drives the REAL production Handler, EvidenceLoader, ReadinessChecker,
// and Writer together (not stubs), so this is the end-to-end path, not just
// the readiness-checker's own SQL.
//
// Phase 1 (BEFORE, pre-#5848 shape): AWSCloudRuntimeDriftHandler with NO
// ReadinessChecker -- byte-identical to the code that shipped before this
// branch -- durably writes orphaned_cloud_resource while the state scope is
// pending. This is the bug: a wrong verdict committed as a success.
//
// Phase 2 (AFTER, #5848 shape): the SAME evidence shape, but the Handler has
// ReadinessChecker wired. It defers instead of writing -- no row, no error
// swallowed, a retryable "state pending" error instead.
//
// Phase 3 (state catches up): the state_snapshot scope's generation
// activates with matching Terraform state for the same ARN. A second Handle
// call (simulating the reopen-triggered retry) now writes -- and the
// verdict is NO LONGER orphaned_cloud_resource, because state is visible.
func TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)
	now := time.Now().UTC()
	suffix := fmt.Sprintf("5837-deterministic-%d", time.Now().UnixNano())

	// One state_snapshot scope, registered but PENDING -- the forced race
	// window. Shared by both phases; readiness is a global "any pending
	// state_snapshot scope" signal (see aws_cloud_runtime_drift_readiness.go).
	stateScopeID := "state_snapshot:s3:" + suffix
	statePendingGenerationID := "gen-state-pending-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, stateScopeID, "terraform_state", "", now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, statePendingGenerationID, stateScopeID, "pending", now)

	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{DB: SQLDB{DB: sqlDB}}
	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: sqlDB}},
	}
	readinessChecker := PostgresAWSCloudRuntimeDriftReadinessChecker{DB: SQLDB{DB: sqlDB}}
	fencingTokenIssuer := PostgresAWSCloudRuntimeDriftFencingTokenIssuer{DB: SQLDB{DB: sqlDB}}

	// --- Phase 1: BEFORE (no ReadinessChecker) --------------------------
	beforeScopeID := "aws:" + suffix + ":before"
	beforeGenerationID := "gen-before-" + suffix
	beforeARN := "arn:aws:lambda:us-east-1:123456789012:function:before-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, beforeScopeID, "aws", beforeGenerationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, beforeGenerationID, beforeScopeID, "active", now)
	seedAWSResourceFactMinimal(t, ctx, sqlDB, beforeScopeID, beforeGenerationID, beforeARN, "aws_lambda_function", now)

	beforeHandler := reducer.AWSCloudRuntimeDriftHandler{
		EvidenceLoader: loader,
		Writer:         writer,
		// ReadinessChecker deliberately nil: byte-identical to pre-#5848.
		FencingTokenIssuer: fencingTokenIssuer,
	}
	beforeResult, err := beforeHandler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-before-" + suffix,
		ScopeID:      beforeScopeID,
		GenerationID: beforeGenerationID,
		SourceSystem: "aws",
		Domain:       reducer.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
	})
	if err != nil {
		t.Fatalf("BEFORE: Handle() error = %v, want nil (pre-#5848 always writes)", err)
	}
	if beforeResult.CanonicalWrites != 1 {
		t.Fatalf("BEFORE: CanonicalWrites = %d, want 1", beforeResult.CanonicalWrites)
	}
	beforeKinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, beforeScopeID, beforeGenerationID)
	if len(beforeKinds) != 1 || beforeKinds[0] != string(cloudruntime.FindingKindOrphanedCloudResource) {
		t.Fatalf(
			"BEFORE: finding rows = %v, want exactly [orphaned_cloud_resource]: "+
				"this is the #5837 bug, reproduced deterministically by forcing the state_snapshot "+
				"scope to be pending rather than racing for it",
			beforeKinds,
		)
	}
	t.Logf("BEFORE (no ReadinessChecker): durably wrote %v while state scope was pending -- the #5837 bug", beforeKinds)

	// --- Phase 2: AFTER, deferred (ReadinessChecker wired) ---------------
	afterScopeID := "aws:" + suffix + ":after"
	afterGenerationID := "gen-after-" + suffix
	afterARN := "arn:aws:lambda:us-east-1:123456789012:function:after-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, afterScopeID, "aws", afterGenerationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, afterGenerationID, afterScopeID, "active", now)
	seedAWSResourceFactMinimal(t, ctx, sqlDB, afterScopeID, afterGenerationID, afterARN, "aws_lambda_function", now)

	afterHandler := reducer.AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		ReadinessChecker:   readinessChecker,
		FencingTokenIssuer: fencingTokenIssuer,
	}
	_, err = afterHandler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-after-defer-" + suffix,
		ScopeID:      afterScopeID,
		GenerationID: afterGenerationID,
		SourceSystem: "aws",
		Domain:       reducer.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		AttemptCount: 0,
	})
	if err == nil {
		t.Fatal("AFTER (attempt 0): Handle() error = nil, want a deferred error")
	}
	afterDeferredKinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, afterScopeID, afterGenerationID)
	if len(afterDeferredKinds) != 0 {
		t.Fatalf(
			"AFTER (attempt 0): finding rows = %v, want NONE: a deferred pass must not durably write "+
				"a degraded verdict",
			afterDeferredKinds,
		)
	}
	t.Logf("AFTER (ReadinessChecker wired, attempt 0): deferred, zero rows written -- the #5837 fix")

	// --- Phase 3: state catches up, reopen-triggered retry converges -----
	if _, err := sqlDB.ExecContext(
		ctx,
		`UPDATE scope_generations SET status = 'active', activated_at = $1 WHERE generation_id = $2`,
		now, statePendingGenerationID,
	); err != nil {
		t.Fatalf("activate state_snapshot generation: %v", err)
	}
	if _, err := sqlDB.ExecContext(
		ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		statePendingGenerationID, stateScopeID,
	); err != nil {
		t.Fatalf("set state_snapshot active_generation_id: %v", err)
	}
	seedTerraformStateResourceFactMinimal(
		t, ctx, sqlDB, stateScopeID, statePendingGenerationID,
		"aws_lambda_function.after", "aws_lambda_function", afterARN, now,
	)

	afterResult, err := afterHandler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-after-retry-" + suffix,
		ScopeID:      afterScopeID,
		GenerationID: afterGenerationID,
		SourceSystem: "aws",
		Domain:       reducer.DomainAWSCloudRuntimeDrift,
		Cause:        "aws runtime resource facts observed",
		AttemptCount: 1,
	})
	if err != nil {
		t.Fatalf("AFTER (attempt 1, state active): Handle() error = %v, want nil", err)
	}
	if afterResult.CanonicalWrites != 1 {
		t.Fatalf("AFTER (attempt 1): CanonicalWrites = %d, want 1", afterResult.CanonicalWrites)
	}
	convergedKinds := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, afterScopeID, afterGenerationID)
	if len(convergedKinds) != 1 {
		t.Fatalf("AFTER (attempt 1): finding rows = %v, want exactly 1", convergedKinds)
	}
	if convergedKinds[0] == string(cloudruntime.FindingKindOrphanedCloudResource) {
		t.Fatalf(
			"AFTER (attempt 1): finding_kind = %q, want anything OTHER than orphaned_cloud_resource: "+
				"state is now visible, so the verdict must no longer be the degraded one",
			convergedKinds[0],
		)
	}
	t.Logf("AFTER (attempt 1, state active): reclassified to %v once state activated", convergedKinds)
}
