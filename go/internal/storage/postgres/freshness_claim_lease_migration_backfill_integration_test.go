// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
)

// preClaimLeaseAWSFreshnessTriggersSQL is migration 020's table shape,
// predating #4576's claim_expires_at/claim_fencing_token columns. It seeds a
// pre-existing deployment's table exactly as migration 041 will find it.
const preClaimLeaseAWSFreshnessTriggersSQL = `
CREATE TABLE aws_freshness_triggers (
    trigger_id TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    freshness_key TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    event_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    region TEXT NOT NULL,
    service_kind TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    claimed_by TEXT NULL,
    claimed_at TIMESTAMPTZ NULL,
    handed_off_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    PRIMARY KEY (trigger_id)
);
`

// preClaimLeaseGCPFreshnessTriggersSQL is migration 039's table shape,
// predating #4576's claim_expires_at/claim_fencing_token columns.
const preClaimLeaseGCPFreshnessTriggersSQL = `
CREATE TABLE gcp_freshness_triggers (
    trigger_id TEXT NOT NULL,
    delivery_key TEXT NOT NULL,
    freshness_key TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    event_id TEXT NOT NULL,
    parent_scope_kind TEXT NOT NULL,
    parent_scope_id TEXT NOT NULL,
    asset_type TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    observed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    claimed_by TEXT NULL,
    claimed_at TIMESTAMPTZ NULL,
    handed_off_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    PRIMARY KEY (trigger_id)
);
`

// TestAWSGCPFreshnessClaimLeaseMigrationBackfillsStuckClaimedRowsIntegration
// proves the #4576 migration 041 heals rows that were ALREADY stuck at
// 'claimed' with no lease before the migration ran (raised in PR #4682
// review): without the backfill, such a row gets claim_expires_at = NULL, and
// ReapExpiredTriggerClaims's "claim_expires_at IS NOT NULL" predicate would
// skip it forever — leaving exactly the stuck triggers #4576 exists to
// rescue permanently stranded. This runs against a real Postgres instance
// because it exercises the actual migration SQL, not the store's own
// EnsureSchema DDL (which only ever sees a fresh #4576-shaped table in the
// store-level integration tests).
func TestAWSGCPFreshnessClaimLeaseMigrationBackfillsStuckClaimedRowsIntegration(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	ctx := context.Background()

	// Seed both tables in their pre-#4576 shape, exactly as a deployment that
	// predates this migration would have them.
	if _, err := db.ExecContext(ctx, preClaimLeaseAWSFreshnessTriggersSQL); err != nil {
		t.Fatalf("create pre-#4576 aws_freshness_triggers: %v", err)
	}
	if _, err := db.ExecContext(ctx, preClaimLeaseGCPFreshnessTriggersSQL); err != nil {
		t.Fatalf("create pre-#4576 gcp_freshness_triggers: %v", err)
	}

	// claimedAt is far enough in the past that claimedAt + the coordinator's
	// default 5-minute lease is already before `now`, so the backfilled
	// claim_expires_at makes this row immediately reap-eligible.
	claimedAt := time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC)
	now := claimedAt.Add(time.Hour)

	stuckAWSTrigger, err := awsfreshness.NewStoredTrigger(awsfreshness.Trigger{
		EventID:      "evt-pre-existing-stuck",
		Kind:         awsfreshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  "lambda",
		ResourceType: "AWS::Lambda::Function",
		ResourceID:   "function-pre-existing-stuck",
		ObservedAt:   claimedAt,
	}, claimedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger(aws) error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO aws_freshness_triggers (
			trigger_id, delivery_key, freshness_key, event_kind, event_id,
			account_id, region, service_kind, resource_type, resource_id,
			status, duplicate_count, observed_at, received_at, updated_at,
			claimed_by, claimed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'claimed', 0, $11, $11, $11, 'claimant-pre-existing', $11)
	`,
		stuckAWSTrigger.TriggerID, stuckAWSTrigger.DeliveryKey, stuckAWSTrigger.FreshnessKey,
		string(stuckAWSTrigger.Kind), stuckAWSTrigger.EventID, stuckAWSTrigger.AccountID,
		stuckAWSTrigger.Region, stuckAWSTrigger.ServiceKind, stuckAWSTrigger.ResourceType,
		stuckAWSTrigger.ResourceID, claimedAt,
	); err != nil {
		t.Fatalf("insert pre-existing stuck AWS trigger: %v", err)
	}

	stuckGCPTrigger, err := gcpfreshness.NewStoredTrigger(gcpfreshness.Trigger{
		EventID:         "evt-pre-existing-stuck",
		Kind:            gcpfreshness.EventKindAssetChange,
		ParentScopeKind: "project",
		ParentScopeID:   "demo-project-pre-existing-stuck",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      claimedAt,
	}, claimedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger(gcp) error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO gcp_freshness_triggers (
			trigger_id, delivery_key, freshness_key, event_kind, event_id,
			parent_scope_kind, parent_scope_id, asset_type, location,
			status, duplicate_count, observed_at, received_at, updated_at,
			claimed_by, claimed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'claimed', 0, $10, $10, $10, 'claimant-pre-existing', $10)
	`,
		stuckGCPTrigger.TriggerID, stuckGCPTrigger.DeliveryKey, stuckGCPTrigger.FreshnessKey,
		string(stuckGCPTrigger.Kind), stuckGCPTrigger.EventID, stuckGCPTrigger.ParentScopeKind,
		stuckGCPTrigger.ParentScopeID, stuckGCPTrigger.AssetType, stuckGCPTrigger.Location, claimedAt,
	); err != nil {
		t.Fatalf("insert pre-existing stuck GCP trigger: %v", err)
	}

	// Apply migration 041 exactly as it ships (adds the lease/fencing columns
	// and backfills claim_expires_at for already-claimed rows).
	if _, err := db.ExecContext(ctx, MigrationSQL("aws_gcp_freshness_claim_lease")); err != nil {
		t.Fatalf("apply migration 041: %v", err)
	}

	awsStore := NewAWSFreshnessStore(SQLDB{DB: db})
	reclaimedAWS, err := awsStore.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims(aws) error = %v", err)
	}
	if got, want := len(reclaimedAWS), 1; got != want {
		t.Fatalf("len(reclaimedAWS) = %d, want %d (the pre-existing stuck row must be reclaimed)", got, want)
	}
	if reclaimedAWS[0].TriggerID != stuckAWSTrigger.TriggerID {
		t.Fatalf("reclaimedAWS[0].TriggerID = %q, want %q", reclaimedAWS[0].TriggerID, stuckAWSTrigger.TriggerID)
	}
	if reclaimedAWS[0].Status != awsfreshness.TriggerStatusQueued {
		t.Fatalf("reclaimedAWS[0].Status = %q, want %q", reclaimedAWS[0].Status, awsfreshness.TriggerStatusQueued)
	}

	gcpStore := NewGCPFreshnessStore(SQLDB{DB: db})
	reclaimedGCP, err := gcpStore.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims(gcp) error = %v", err)
	}
	if got, want := len(reclaimedGCP), 1; got != want {
		t.Fatalf("len(reclaimedGCP) = %d, want %d (the pre-existing stuck row must be reclaimed)", got, want)
	}
	if reclaimedGCP[0].TriggerID != stuckGCPTrigger.TriggerID {
		t.Fatalf("reclaimedGCP[0].TriggerID = %q, want %q", reclaimedGCP[0].TriggerID, stuckGCPTrigger.TriggerID)
	}
	if reclaimedGCP[0].Status != gcpfreshness.TriggerStatusQueued {
		t.Fatalf("reclaimedGCP[0].Status = %q, want %q", reclaimedGCP[0].Status, gcpfreshness.TriggerStatusQueued)
	}
}
