// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestProvenanceEdgeIdentityUpgradeSeedsCurrentReplayOnceLive proves migration
// 096 reopens only current succeeded provenance producers and dirties an
// in-flight old producer so its ACK must replay once through the new writer.
func TestProvenanceEdgeIdentityUpgradeSeedsCurrentReplayOnceLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID       = "repository:5827-provenance-upgrade"
		generation    = "generation:5827-provenance-upgrade"
		staleGen      = "generation:5827-provenance-upgrade-stale"
		identityID    = "reducer_5827_upgrade_identity"
		packageID     = "reducer_5827_upgrade_package"
		pendingID     = "reducer_5827_upgrade_pending"
		retryingID    = "reducer_5827_upgrade_retrying"
		futureID      = "reducer_5827_upgrade_future"
		futureImageID = "reducer_5827_upgrade_future_image"
		unrelatedID   = "reducer_5827_upgrade_unrelated"
		staleID       = "reducer_5827_upgrade_stale"
		runningID     = "reducer_5827_upgrade_package_running"
		runningOwner  = "upgrade-old-reducer"
		upgradeMarker = "provenance_edge_identity_upgrade_096"
	)
	// ApplyBootstrap has already installed 096 in the isolated schema. Remove
	// its triggers so the fixtures below reproduce rows that existed before the
	// pre-upgrade hook; executing MigrationSQL recreates the production shape.
	if _, err := db.ExecContext(ctx, `
DROP TRIGGER fact_work_items_require_provenance_edge_identity_insert ON fact_work_items;
DROP TRIGGER fact_work_items_require_provenance_edge_identity_update ON fact_work_items;
DROP TRIGGER fact_work_items_enforce_provenance_edge_identity_upgrade ON fact_work_items
`); err != nil {
		t.Fatalf("remove preinstalled provenance upgrade triggers: %v", err)
	}
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES ($2, $1, 'synthetic', FALSE, $3, $3, 'superseded')
`, scopeID, staleGen, now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed stale provenance-upgrade generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate provenance-upgrade scope: %v", err)
	}
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, identityID, scopeID, generation,
		reducer.DomainContainerImageIdentity, now,
	)
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, packageID, scopeID, generation,
		reducer.DomainPackageSourceCorrelation, now,
	)
	for _, fixture := range []struct {
		id     string
		gen    string
		domain reducer.Domain
	}{
		{id: pendingID, gen: generation, domain: reducer.DomainPackageSourceCorrelation},
		{id: retryingID, gen: generation, domain: reducer.DomainPackageSourceCorrelation},
		{id: unrelatedID, gen: generation, domain: reducer.DomainOwnership},
		{id: staleID, gen: staleGen, domain: reducer.DomainPackageSourceCorrelation},
	} {
		insertCrossScopeCompletionBaseConsumer(
			t, ctx, db, fixture.id, scopeID, fixture.gen, fixture.domain, now,
		)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = CASE WHEN work_item_id = $1 THEN 'pending' ELSE 'retrying' END
WHERE work_item_id IN ($1, $2)
`, pendingID, retryingID); err != nil {
		t.Fatalf("seed pre-upgrade pending and retrying provenance work: %v", err)
	}
	insertCrossScopeCompletionRunningProducer(
		t, ctx, db, runningID, scopeID, generation,
		reducer.DomainPackageSourceCorrelation, runningOwner, now,
	)
	if _, err := db.ExecContext(ctx, `
DELETE FROM cross_scope_completion_upgrade_markers WHERE marker_name = $1
`, upgradeMarker); err != nil {
		t.Fatalf("reset isolated provenance upgrade marker: %v", err)
	}

	if _, err := db.ExecContext(ctx, MigrationSQL("provenance_edge_identity_upgrade_seed")); err != nil {
		t.Fatalf("apply provenance identity upgrade seed: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, identityID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, packageID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, pendingID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, retryingID, "retrying", false)
	assertCrossScopeConsumerState(t, ctx, db, runningID, "running", true)
	assertCrossScopeConsumerState(t, ctx, db, unrelatedID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, staleID, "succeeded", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, packageID, true)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, identityID, true)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, pendingID, true)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, retryingID, true)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, unrelatedID, false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, staleID, false)

	// Work created by an old pod after the hook is fenced at INSERT time.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', 'package_source_correlation', 'pending',
          'intent', $1, '{}'::jsonb, $4, $4)
`, futureID, scopeID, generation, now); err != nil {
		t.Fatalf("insert post-migration provenance work: %v", err)
	}
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, futureID, true)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', 'container_image_identity', 'pending',
          'intent', $1, '{}'::jsonb, $4, $4)
`, futureImageID, scopeID, generation, now); err != nil {
		t.Fatalf("insert post-migration container identity work: %v", err)
	}
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, futureImageID, true)
	assertProvenanceIdentityUpgradeStatus(t, ctx, db, now, true, 7)

	for index, workItemID := range []string{pendingID, retryingID, futureID} {
		proveProvenanceUpgradeOldAckFencedThenNewAckSucceeds(
			t, ctx, db, workItemID, reducer.DomainPackageSourceCorrelation,
			now.Add(time.Duration(index)*time.Minute),
		)
	}
	proveProvenanceUpgradeOldAckFencedThenNewAckSucceeds(
		t, ctx, db, futureImageID, reducer.DomainContainerImageIdentity,
		now.Add(3*time.Minute),
	)

	// An old reducer can also exhaust its retries after the hook. Because its
	// terminal failure preserves the capability flag, the trigger must retain
	// the repair as pending instead of stranding it in dead_letter.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'old-reducer-failure', claim_until = clock_timestamp() + interval '1 minute'
WHERE work_item_id = $1
`, identityID); err != nil {
		t.Fatalf("claim provenance upgrade for old reducer failure: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'dead_letter', lease_owner = NULL, claim_until = NULL,
    failure_class = 'synthetic_old_binary_failure'
WHERE work_item_id = $1
`, identityID); err != nil {
		t.Fatalf("simulate post-migration old reducer dead letter: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, identityID, "pending", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, identityID, true)

	// A new reducer explicitly clears the capability flag on a genuine terminal
	// failure, preserving normal dead-letter visibility and operator replay.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'new-reducer-failure', claim_until = clock_timestamp() + interval '1 minute'
WHERE work_item_id = $1
`, identityID); err != nil {
		t.Fatalf("claim provenance upgrade for new reducer failure: %v", err)
	}
	newFailureQueue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "new-reducer-failure",
		LeaseDuration: time.Minute,
		MaxAttempts:   1,
		Now:           func() time.Time { return now.Add(time.Minute) },
	}
	if err := newFailureQueue.Fail(ctx, reducer.Intent{
		IntentID:     identityID,
		Domain:       reducer.DomainContainerImageIdentity,
		AttemptCount: 1,
	}, errors.New("synthetic new binary failure")); err != nil {
		t.Fatalf("new reducer dead letter: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, identityID, "dead_letter", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, identityID, false)

	// A pre-upgrade hook can seed the replay while an old reducer still runs.
	// Its claim preserves the new capability flag, and its ACK cannot clear it.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'old-reducer-after-hook', claim_until = clock_timestamp() + interval '1 minute'
WHERE work_item_id = $1
`, packageID); err != nil {
		t.Fatalf("claim provenance upgrade for old reducer ACK: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL
WHERE work_item_id = $1
`, packageID); err != nil {
		t.Fatalf("simulate post-migration old reducer claim and ACK: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, packageID, "pending", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, packageID, true)

	// The new binary explicitly clears the capability flag only on success.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'new-reducer', claim_until = clock_timestamp() + interval '1 minute'
WHERE work_item_id = $1
`, packageID); err != nil {
		t.Fatalf("claim provenance upgrade for new reducer ACK: %v", err)
	}
	newSuccessQueue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "new-reducer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(2 * time.Minute) },
	}
	if err := newSuccessQueue.Ack(ctx, reducer.Intent{
		IntentID: packageID,
		Domain:   reducer.DomainPackageSourceCorrelation,
	}, reducer.Result{}); err != nil {
		t.Fatalf("new reducer ACK: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, packageID, "succeeded", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, packageID, false)

	if _, err := db.ExecContext(ctx, MigrationSQL("provenance_edge_identity_upgrade_seed")); err != nil {
		t.Fatalf("reapply provenance identity upgrade seed: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, identityID, "dead_letter", false)
	assertCrossScopeConsumerState(t, ctx, db, packageID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, runningID, "running", true)
	assertCrossScopeConsumerState(t, ctx, db, pendingID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, retryingID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, futureID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, futureImageID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, unrelatedID, "succeeded", false)
	assertCrossScopeConsumerState(t, ctx, db, staleID, "succeeded", false)
	assertProvenanceIdentityUpgradeStatus(t, ctx, db, now, true, 1)
}

func assertProvenanceIdentityUpgradeStatus(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	asOf time.Time,
	wantApplied bool,
	wantRequired int,
) {
	t.Helper()

	snapshot, err := readQueueSnapshot(ctx, SQLDB{DB: db}, asOf)
	if err != nil {
		t.Fatalf("read provenance identity upgrade status: %v", err)
	}
	if got := snapshot.ProvenanceEdgeIdentityUpgradeApplied; got != wantApplied {
		t.Fatalf("provenance identity upgrade applied = %t, want %t", got, wantApplied)
	}
	if got := snapshot.ProvenanceEdgeIdentityUpgradeRequired; got != wantRequired {
		t.Fatalf("provenance identity upgrade required = %d, want %d", got, wantRequired)
	}
}

func proveProvenanceUpgradeOldAckFencedThenNewAckSucceeds(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	domain reducer.Domain,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = 'old-reducer', claim_until = clock_timestamp() + interval '1 minute'
WHERE work_item_id = $1
`, workItemID); err != nil {
		t.Fatalf("claim %s for old reducer: %v", workItemID, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL
WHERE work_item_id = $1
`, workItemID); err != nil {
		t.Fatalf("simulate old reducer ACK for %s: %v", workItemID, err)
	}
	assertCrossScopeConsumerState(t, ctx, db, workItemID, "pending", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, workItemID, true)

	const newOwner = "new-reducer-capability-proof"
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = $2, claim_until = clock_timestamp() + interval '1 minute'
WHERE work_item_id = $1
`, workItemID, newOwner); err != nil {
		t.Fatalf("claim %s for new reducer: %v", workItemID, err)
	}
	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    newOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := queue.AckBatch(ctx, []reducer.Intent{{
		IntentID: workItemID,
		Domain:   domain,
	}}, []reducer.Result{{}}); err != nil {
		t.Fatalf("new reducer ACK for %s: %v", workItemID, err)
	}
	assertCrossScopeConsumerState(t, ctx, db, workItemID, "succeeded", false)
	assertProvenanceIdentityUpgradeRequired(t, ctx, db, workItemID, false)
}

func assertProvenanceIdentityUpgradeRequired(
	t *testing.T,
	ctx context.Context,
	db interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	workItemID string,
	want bool,
) {
	t.Helper()
	var got bool
	if err := db.QueryRowContext(ctx, `
SELECT provenance_edge_identity_upgrade_required
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&got); err != nil {
		t.Fatalf("query provenance identity upgrade flag: %v", err)
	}
	if got != want {
		t.Fatalf("provenance identity upgrade required = %t, want %t", got, want)
	}
}
