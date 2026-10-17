// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// The #8283 live proofs run the production fingerprint recorder and the
// production reopen listing against the real bootstrapped schema. They never
// copy the fingerprint SQL: a recorded value comes from the fused fenced read
// (GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint), the
// store that writes it, and the reopen pass that compares it. Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://eshu@localhost:<port>/eshu_proof?sslmode=disable \
//	  go test ./internal/storage/postgres -run InputFingerprint -count=1

const (
	fingerprintDomain    = "deployable_unit_correlation"
	fingerprintItemID    = "work-duc-a"
	fingerprintOwnScope  = "git:scope-a"
	fingerprintOwnGen    = "gen-a"
	fingerprintDepRepoID = "repo-dep"
)

var fingerprintCandidateRepoIDs = []string{"repo-a", fingerprintDepRepoID}

// fingerprintFixture is one isolated real schema holding an own scope A with a
// succeeded deployable_unit_correlation item, a foreign scope B whose resolved
// row touches the candidate deployment repository, and an unrelated scope C.
type fingerprintFixture struct {
	ctx       context.Context
	database  *sql.DB
	relations *RelationshipStore
	recorder  InputFingerprintStore
	ingestion IngestionStore
	ackedAt   time.Time
}

func newFingerprintFixture(t *testing.T) *fingerprintFixture {
	t.Helper()
	bridgeRetentionFamilyDSN(t)
	ctx, database := openCorpusFenceSnapshotSchema(t)
	f := &fingerprintFixture{
		ctx:       ctx,
		database:  database,
		relations: NewRelationshipStore(SQLDB{DB: database}),
		recorder:  NewInputFingerprintStore(SQLDB{DB: database}),
		ingestion: NewIngestionStore(SQLDB{DB: database}),
		ackedAt:   time.Now().UTC().Add(time.Hour),
	}
	f.seedScope(t, fingerprintOwnScope, fingerprintOwnGen, "repo-a", fingerprintDepRepoID)
	f.seedScope(t, "git:scope-b", "gen-b1", "repo-b", fingerprintDepRepoID)
	f.seedScope(t, "git:scope-c", "gen-c1", "repo-c", "repo-z")
	return f
}

// seedScope creates an active scope with an active scope generation, an active
// relationship generation, and one resolved row source -> target.
func (f *fingerprintFixture) seedScope(t *testing.T, scope, generation, source, target string) {
	t.Helper()
	now := time.Now().UTC()
	f.exec(t, `INSERT INTO ingestion_scopes
	   (scope_id, scope_kind, source_system, source_key, collector_kind,
	    partition_key, observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scope, now, generation)
	f.exec(t, `INSERT INTO scope_generations
	   (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	  VALUES ($1, $2, 'snapshot', $3, $3, 'active')`, generation, scope, now)
	f.insertResolved(t, generation, source, target)
	f.exec(t, `INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
	  VALUES ($1, $2, 'active', $3, $3)`, generation, scope, now)
}

func (f *fingerprintFixture) insertResolved(t *testing.T, generation, source, target string) {
	t.Helper()
	f.exec(t, `INSERT INTO resolved_relationships
	  (resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
	   confidence, evidence_count, rationale, resolution_source, details)
	 VALUES ($1, $2, $3, $4, 'DEPLOYS_FROM', 0.9, 1, 'fixture', 'inferred', '{}'::jsonb)`,
		generation+":"+source+">"+target, generation, source, target)
}

// reingestScope models a foreign scope re-ingest: a new scope generation
// becomes the scope's active one, and the production activation path publishes
// its relationship generation (superseding the previous one).
func (f *fingerprintFixture) reingestScope(t *testing.T, scope, generation, source, target string) {
	t.Helper()
	now := time.Now().UTC()
	f.exec(t, `UPDATE scope_generations SET status = 'superseded' WHERE scope_id = $1 AND status = 'active'`, scope)
	f.exec(t, `INSERT INTO scope_generations
	   (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	  VALUES ($1, $2, 'snapshot', $3, $3, 'active')`, generation, scope, now)
	f.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, generation)
	rows := []relationships.ResolvedRelationship{{
		SourceRepoID: source, TargetRepoID: target,
		RelationshipType: relationships.RelDeploysFrom, Confidence: 0.9, EvidenceCount: 1,
		Rationale: "fixture", ResolutionSource: relationships.ResolutionSourceInferred,
	}}
	if err := f.relations.UpsertResolved(f.ctx, generation, rows); err != nil {
		t.Fatalf("upsert resolved for %s: %v", generation, err)
	}
	if err := f.relations.ActivateResolutionGeneration(f.ctx, generation, scope); err != nil {
		t.Fatalf("activate %s: %v", generation, err)
	}
}

// succeedItem inserts the succeeded deployable_unit_correlation item with the
// ACK time an hour in the future, so any activation stamped "now" sits before
// the item's updated_at: the shape that defeats an activated_at > updated_at
// predicate.
func (f *fingerprintFixture) succeedItem(t *testing.T) {
	t.Helper()
	seedSucceededReopenWorkItem(t, f.ctx, f.database, fingerprintItemID,
		fingerprintOwnScope, fingerprintOwnGen, fingerprintDomain, f.ackedAt)
}

// recordFingerprint takes the fingerprint exactly as the handler does: from the
// fused fenced read, then stores it through the production recorder.
func (f *fingerprintFixture) recordFingerprint(t *testing.T) string {
	t.Helper()
	_, complete, fingerprint, err := f.relations.GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint(
		f.ctx, fingerprintOwnGen, fingerprintCandidateRepoIDs,
	)
	if err != nil {
		t.Fatalf("fused fenced read: %v", err)
	}
	if !complete || fingerprint == "" {
		t.Fatalf("fused read complete=%v fingerprint=%q, want complete with a fingerprint", complete, fingerprint)
	}
	if err := f.recorder.RecordDeployableUnitInputFingerprint(
		f.ctx, fingerprintItemID, fingerprintCandidateRepoIDs, fingerprint,
	); err != nil {
		t.Fatalf("record fingerprint: %v", err)
	}
	return fingerprint
}

// runPass runs the ingester's own maintenance entrypoint.
func (f *fingerprintFixture) runPass(t *testing.T) {
	t.Helper()
	if err := f.ingestion.RunDeferredRelationshipMaintenance(f.ctx, nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v", err)
	}
}

func (f *fingerprintFixture) status(t *testing.T) string {
	t.Helper()
	return workItemStatus(t, f.ctx, f.database, fingerprintItemID)
}

func (f *fingerprintFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatalf("exec %.60q: %v", query, err)
	}
}
