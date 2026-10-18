// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// The #8283 changed-evidence gate fixture. Every step a test drives goes
// through production code: the fenced resolved read that returns the input
// fingerprint, the claim-fenced success write, the resolution upsert and
// activation that move a producer scope's resolved rows, and the maintenance
// pass that decides the reopen. Only the claim and the ACK are seeded with
// SQL, because the reducer queue's claim selection is not under test here.

// fingerprintGateLeaseOwner is the lease owner of every seeded claim.
const fingerprintGateLeaseOwner = "fingerprint-gate-proof"

// fingerprintGateScope is one seeded repository scope: an active scope
// generation whose relationship generation (same id) is active too, so the
// corpus fence is complete.
type fingerprintGateScope struct {
	scopeID string
	genID   string
	repoID  string
}

// seedFingerprintGateScope seeds an active scope, its active scope
// generation, and its active relationship generation.
func seedFingerprintGateScope(t *testing.T, ctx context.Context, db *sql.DB, scope fingerprintGateScope) {
	t.Helper()
	seedContainerImageIdentityAckScope(t, ctx, db, scope.scopeID)
	advanceFingerprintGateScopeGeneration(t, ctx, db, scope.scopeID, scope.genID)
}

// advanceFingerprintGateScopeGeneration supersedes scopeID's active scope
// generation, seeds a new active one, points the scope at it, and activates the relationship
// generation of the same id through the production activation, which
// supersedes the scope's previous relationship generation.
func advanceFingerprintGateScopeGeneration(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = clock_timestamp()
WHERE scope_id = $1 AND status = 'active'`, scopeID); err != nil {
		t.Fatalf("supersede active generation of %s: %v", scopeID, err)
	}
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, genID)
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, genID); err != nil {
		t.Fatalf("point scope %s at generation %s: %v", scopeID, genID, err)
	}
	if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGeneration(ctx, genID, scopeID); err != nil {
		t.Fatalf("activate relationship generation %s: %v", genID, err)
	}
}

// upsertFingerprintGateDeploysFrom writes one resolved DEPLOYS_FROM row into
// generationID through the production resolution upsert. Entity ids are set,
// as every resolved row on a real corpus carries them: resolved_id is a pure
// function of the unique-index identity (generation, COALESCE(entity id,
// repo id) on both ends, relationship type) with no ordinal input (#8299).
// Writing into an
// already-active generation is the append-to-active-generation shape a
// re-run deployment_mapping produces.
func upsertFingerprintGateDeploysFrom(t *testing.T, ctx context.Context, db *sql.DB, generationID, sourceRepo, targetRepo string) {
	t.Helper()
	err := NewRelationshipStore(SQLDB{DB: db}).UpsertResolved(ctx, generationID, []relationships.ResolvedRelationship{{
		SourceRepoID:     sourceRepo,
		TargetRepoID:     targetRepo,
		SourceEntityID:   "entity:" + sourceRepo,
		TargetEntityID:   "entity:" + targetRepo,
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.95,
		EvidenceCount:    1,
		Rationale:        "fingerprint gate proof",
		ResolutionSource: relationships.ResolutionSourceInferred,
	}})
	if err != nil {
		t.Fatalf("upsert resolved %s -> %s into %s: %v", sourceRepo, targetRepo, generationID, err)
	}
}

// fingerprintGateClaim is one seeded running claim of a reducer work item.
type fingerprintGateClaim struct {
	workItemID string
	claimedAt  time.Time
}

// claimFingerprintGateItem seeds one running deployable_unit_correlation (or
// other domain) work item claimed by fingerprintGateLeaseOwner at claimedAt,
// with a lease that is live for an hour from now.
func claimFingerprintGateItem(
	t *testing.T, ctx context.Context, db *sql.DB,
	workItemID, scopeID, genID string, domain reducer.Domain, claimedAt time.Time,
) fingerprintGateClaim {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    lease_owner, claim_until, last_attempt_at, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', $4, 'running', 1, $5, clock_timestamp() + interval '1 hour', $6, '{}'::jsonb, $6, $6)
ON CONFLICT (work_item_id) DO UPDATE
SET status = 'running', attempt_count = fact_work_items.attempt_count + 1,
    lease_owner = EXCLUDED.lease_owner, claim_until = EXCLUDED.claim_until,
    last_attempt_at = EXCLUDED.last_attempt_at, updated_at = EXCLUDED.updated_at`,
		workItemID, scopeID, genID, string(domain), fingerprintGateLeaseOwner, claimedAt); err != nil {
		t.Fatalf("claim %s: %v", workItemID, err)
	}
	return fingerprintGateClaim{workItemID: workItemID, claimedAt: claimedAt}
}

// readFingerprintGateInput runs the production fenced resolved read for
// repoIDs and returns the fingerprint the same statement computed.
func readFingerprintGateInput(t *testing.T, ctx context.Context, db *sql.DB, repoIDs []string) reducercontract.ResolvedReadFingerprint {
	t.Helper()
	_, complete, fingerprint, err := NewRelationshipStore(SQLDB{DB: db}).
		GetFingerprintedResolvedRelationshipsForReposWithCorpusFence(ctx, repoIDs)
	if err != nil {
		t.Fatalf("fenced resolved read for %v: %v", repoIDs, err)
	}
	if !complete {
		t.Fatalf("fenced resolved read for %v: corpus fence incomplete, fixture must keep it complete", repoIDs)
	}
	return fingerprint
}

// recordFingerprintGateInput runs the production claim-fenced success write.
func recordFingerprintGateInput(
	ctx context.Context, db *sql.DB, claim fingerprintGateClaim, fingerprint reducercontract.ResolvedReadFingerprint,
) error {
	return NewRelationshipStore(SQLDB{DB: db}).
		RecordDeployableUnitReopenFingerprint(ctx, claim.workItemID, claim.claimedAt, fingerprint)
}

// ackFingerprintGateItem stands in for the reducer ACK: the claim ends and
// the item is succeeded.
func ackFingerprintGateItem(t *testing.T, ctx context.Context, db *sql.DB, workItemID string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL, updated_at = $2
WHERE work_item_id = $1`, workItemID, at); err != nil {
		t.Fatalf("ack %s: %v", workItemID, err)
	}
}

// succeedFingerprintGateItem drives one deployable_unit_correlation run to
// success: claim, fenced read, recorded fingerprint, ACK.
func succeedFingerprintGateItem(
	t *testing.T, ctx context.Context, db *sql.DB,
	workItemID string, scope fingerprintGateScope, repoIDs []string, at time.Time,
) {
	t.Helper()
	claim := claimFingerprintGateItem(t, ctx, db, workItemID, scope.scopeID, scope.genID,
		reducer.DomainDeployableUnitCorrelation, at)
	if err := recordFingerprintGateInput(ctx, db, claim, readFingerprintGateInput(t, ctx, db, repoIDs)); err != nil {
		t.Fatalf("record fingerprint for %s: %v", workItemID, err)
	}
	ackFingerprintGateItem(t, ctx, db, workItemID, at)
}

// fingerprintRowCount counts the fingerprint rows of one work item.
func fingerprintRowCount(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM deployable_unit_reopen_fingerprints WHERE work_item_id = $1`, workItemID,
	).Scan(&count); err != nil {
		t.Fatalf("count fingerprint rows for %s: %v", workItemID, err)
	}
	return count
}

// correlationReopenSkippedValue reads one domain's
// eshu_dp_correlation_reopen_skipped_total{reason="input_unchanged"}, 0 when
// never counted.
func correlationReopenSkippedValue(t *testing.T, rm metricdata.ResourceMetrics, domain string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_correlation_reopen_skipped_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("reopen skipped counter data type = %T, want Int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				d, _ := point.Attributes.Value("domain")
				r, _ := point.Attributes.Value("reason")
				if d.AsString() == domain && r.AsString() == "input_unchanged" {
					return point.Value
				}
			}
		}
	}
	return 0
}
