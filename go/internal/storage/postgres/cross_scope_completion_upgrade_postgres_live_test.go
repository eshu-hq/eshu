// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCrossScopeCompletionQuietUpgradeSeedsCanonicalReplayOnceLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID    = "repository:5740-quiet-upgrade"
		generation = "generation:5740-quiet-upgrade"
		identityID = "reducer_5740_upgrade_identity"
		cicdID     = "reducer_5740_upgrade_cicd"
		owner      = "upgrade-old-reducer"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate quiet-upgrade scope: %v", err)
	}
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, identityID, scopeID, generation,
		reducer.DomainContainerImageIdentity, now,
	)
	insertCrossScopeCompletionRunningProducer(
		t, ctx, db, cicdID, scopeID, generation,
		reducer.DomainCICDRunCorrelation, owner, now,
	)
	if _, err := db.ExecContext(ctx, `
DELETE FROM cross_scope_completion_upgrade_markers
WHERE marker_name = 'cross_scope_completion_upgrade_095'
`); err != nil {
		t.Fatalf("reset isolated upgrade marker: %v", err)
	}
	if _, err := db.ExecContext(ctx, MigrationSQL("cross_scope_completion_upgrade_seed")); err != nil {
		t.Fatalf("apply quiet-upgrade seed: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, identityID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, cicdID, "running", true)

	if _, err := db.ExecContext(ctx, MigrationSQL("cross_scope_completion_upgrade_seed")); err != nil {
		t.Fatalf("reapply quiet-upgrade seed: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, identityID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, cicdID, "running", true)

	legacyAckCICD(t, ctx, db, cicdID, owner)
	assertCrossScopeConsumerState(t, ctx, db, cicdID, "pending", false)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainCICDRunCorrelation, 0)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2, claim_until = clock_timestamp() + INTERVAL '1 minute'
WHERE work_item_id = $1 AND status = 'pending'
`, cicdID, owner); err != nil {
		t.Fatalf("claim clean CI/CD upgrade replay: %v", err)
	}
	legacyAckCICD(t, ctx, db, cicdID, owner)
	assertCrossScopeConsumerState(t, ctx, db, cicdID, "succeeded", false)
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainCICDRunCorrelation, 1)
}

func TestCrossScopeCompletionLegacyRetryAndFailClearDirtyReplayLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID    = "repository:5740-legacy-terminal"
		generation = "generation:5740-legacy-terminal"
		owner      = "legacy-terminal-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	for _, item := range []struct {
		id     string
		status string
	}{
		{id: "reducer_5740_legacy_retry", status: "retrying"},
		{id: "reducer_5740_legacy_deadletter", status: "dead_letter"},
	} {
		insertCrossScopeCompletionRunningProducer(
			t, ctx, db, item.id, scopeID, generation,
			reducer.DomainSupplyChainImpact, owner, now,
		)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items SET cross_scope_replay_required = TRUE WHERE work_item_id = $1
`, item.id); err != nil {
			t.Fatalf("dirty legacy %s: %v", item.status, err)
		}
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $2, lease_owner = NULL, claim_until = NULL, updated_at = clock_timestamp()
WHERE work_item_id = $1 AND status = 'running'
`, item.id, item.status); err != nil {
			t.Fatalf("legacy %s mutation: %v", item.status, err)
		}
		assertCrossScopeConsumerState(t, ctx, db, item.id, item.status, false)
	}
}

func legacyAckCICD(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	owner string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
    visible_at = NULL, updated_at = clock_timestamp(),
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = $1
  AND stage = 'reducer'
  AND domain = 'ci_cd_run_correlation'
  AND lease_owner = $2
  AND status IN ('claimed', 'running')
`, workItemID, owner); err != nil {
		t.Fatalf("legacy CI/CD ACK: %v", err)
	}
}
