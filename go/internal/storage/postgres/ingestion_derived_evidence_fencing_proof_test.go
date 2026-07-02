// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Derived-evidence fencing proof for issue #4444 review (codex P1).
//
// The fencing guard alone is not enough: upsertStreamingFacts's afterBatch
// closure (wired from IngestionStore.commitScopeGeneration, ingestion.go:246)
// derives repository-catalog entries and relationship evidence from the
// in-memory batch, not from what actually landed in fact_records. Before this
// fix, ExecContext for the guarded UPDATE returns nil even when the WHERE
// predicate silently no-ops a row, so afterBatch ran on every envelope in the
// batch regardless of whether its write was fenced out. A stale batch could
// lose the fact_records race yet still have its payload discovered as
// relationship evidence and durably inserted into
// relationship_evidence_facts in the very same transaction.
//
// This test drives the real IngestionStore.CommitScopeGeneration ->
// upsertStreamingFacts -> afterBatch -> relationships.DiscoverEvidence ->
// RelationshipStore.UpsertEvidenceFacts path against a live Postgres
// instance. It onboards two catalog targets, then commits the same
// evidence-triggering fact_id twice under the same generation: once with a
// higher fencing token referencing target-a (accepted), then again with a
// lower, stale fencing token referencing target-b (must be fenced out of
// fact_records AND must not leak into relationship_evidence_facts).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestIngestionStoreCommitScopeGenerationFencesDerivedRelationshipEvidence(t *testing.T) {
	dsn := factCrossBatchFencingProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the derived-evidence fencing proof")
	}

	ctx := context.Background()
	db := openDerivedEvidenceFencingSchema(t, ctx, dsn)
	store := NewIngestionStore(SQLDB{DB: db})
	now := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	// Onboard two repository catalog targets in their own generations so the
	// shared repository catalog (loaded fresh at the start of the evidence
	// commit below) already recognizes both target-a and target-b as valid
	// relationship targets. Using two prior, separate commits means any
	// evidence referencing either target is unambiguously real catalog-match
	// evidence, not a fixture artifact.
	mustCommitDerivedEvidenceRepository(t, ctx, store, "scope-target-a", "gen-target-a", "repo-target-a", now)
	mustCommitDerivedEvidenceRepository(t, ctx, store, "scope-target-b", "gen-target-b", "repo-target-b", now.Add(time.Minute))

	const sourceScopeID = "scope-evidence-source"
	const generationID = "gen-evidence-source"
	const evidenceFactID = "fact-evidence-source-main-tf"

	acceptedEnvelope := facts.Envelope{
		FactID:        evidenceFactID,
		ScopeID:       sourceScopeID,
		GenerationID:  generationID,
		FactKind:      "content_entity",
		StableFactKey: "content_entity:" + evidenceFactID,
		FencingToken:  20,
		ObservedAt:    now.Add(2 * time.Minute),
		Payload: map[string]any{
			"repo_id":       "repo-evidence-source",
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `app_repo = "repo-target-a"`,
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: evidenceFactID},
	}
	if err := store.CommitScopeGeneration(
		ctx,
		derivedEvidenceScope(sourceScopeID),
		derivedEvidenceGeneration(sourceScopeID, generationID, now.Add(2*time.Minute)),
		testFactChannel([]facts.Envelope{acceptedEnvelope}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration(accepted) error = %v, want nil", err)
	}

	acceptedEvidence := readDerivedEvidenceTargets(t, ctx, db, generationID)
	if got, want := acceptedEvidence, []string{"repo-target-a"}; !stringSlicesEqual(got, want) {
		t.Fatalf("evidence targets after accepted commit = %v, want %v", got, want)
	}
	gotToken, gotPayload := readFactCrossBatchFencingRow(t, ctx, db, evidenceFactID)
	if gotToken != 20 {
		t.Fatalf("fencing_token after accepted commit = %d, want 20", gotToken)
	}
	if !strings.Contains(gotPayload, "repo-target-a") {
		t.Fatalf("payload after accepted commit = %s, want repo-target-a reference", gotPayload)
	}

	// Same fact_id, SAME generation, but a lower (stale) fencing token
	// referencing target-b instead. The fact_records write must be fenced out
	// (proven below via readFactCrossBatchFencingRow still reporting token 20
	// and the target-a payload). The derived-evidence proof is the new
	// assertion: relationship_evidence_facts for this generation must still
	// contain ONLY the target-a row. A target-b row appearing here is exactly
	// the codex P1 — the stale batch's payload leaked into derived graph truth
	// even though its own fact_records row was correctly protected.
	staleEnvelope := facts.Envelope{
		FactID:        evidenceFactID,
		ScopeID:       sourceScopeID,
		GenerationID:  generationID,
		FactKind:      "content_entity",
		StableFactKey: "content_entity:" + evidenceFactID,
		FencingToken:  10,
		ObservedAt:    now.Add(3 * time.Minute),
		Payload: map[string]any{
			"repo_id":       "repo-evidence-source",
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `app_repo = "repo-target-b"`,
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: evidenceFactID},
	}
	if err := store.CommitScopeGeneration(
		ctx,
		derivedEvidenceScope(sourceScopeID),
		derivedEvidenceGeneration(sourceScopeID, generationID, now.Add(3*time.Minute)),
		testFactChannel([]facts.Envelope{staleEnvelope}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration(stale) error = %v, want nil", err)
	}

	gotToken, gotPayload = readFactCrossBatchFencingRow(t, ctx, db, evidenceFactID)
	if gotToken != 20 {
		t.Fatalf("fencing_token after stale commit = %d, want 20 (fact_records must reject the stale write)", gotToken)
	}
	if !strings.Contains(gotPayload, "repo-target-a") {
		t.Fatalf("payload after stale commit = %s, want repo-target-a payload preserved", gotPayload)
	}

	finalEvidence := readDerivedEvidenceTargets(t, ctx, db, generationID)
	if got, want := finalEvidence, []string{"repo-target-a"}; !stringSlicesEqual(got, want) {
		t.Fatalf(
			"evidence targets after stale commit = %v, want %v (a stale batch's payload must not leak into derived relationship evidence, issue #4444 review codex P1)",
			got, want,
		)
	}
}

// derivedEvidenceScope and derivedEvidenceGeneration build the minimal
// IngestionScope/ScopeGeneration shape CommitScopeGeneration requires.
// ScopeKind must be scope.KindRepository so shouldDiscoverStreamingRelationshipEvidence
// (ingestion.go:488) allows relationship-evidence discovery for the commit.
func derivedEvidenceScope(scopeID string) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  scopeID,
	}
}

func derivedEvidenceGeneration(scopeID, generationID string, observedAt time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

// mustCommitDerivedEvidenceRepository onboards one "repository" fact through
// the real commit path so the shared repository catalog (loaded fresh at the
// start of every commitScopeGeneration call) recognizes repoID as a valid
// relationship-evidence target for later commits.
func mustCommitDerivedEvidenceRepository(
	t *testing.T,
	ctx context.Context,
	store IngestionStore,
	scopeID, generationID, repoID string,
	observedAt time.Time,
) {
	t.Helper()
	envelope := facts.Envelope{
		FactID:        "fact-" + generationID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "repository",
		StableFactKey: "repository:" + scopeID,
		ObservedAt:    observedAt,
		Payload:       map[string]any{"repo_id": repoID, "name": repoID},
		SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "fact-" + generationID},
	}
	if err := store.CommitScopeGeneration(
		ctx,
		derivedEvidenceScope(scopeID),
		derivedEvidenceGeneration(scopeID, generationID, observedAt),
		testFactChannel([]facts.Envelope{envelope}),
	); err != nil {
		t.Fatalf("onboard repository %q: CommitScopeGeneration() error = %v, want nil", repoID, err)
	}
}

// readDerivedEvidenceTargets returns the sorted, deduplicated set of
// TargetRepoID values stored in relationship_evidence_facts for generationID.
func readDerivedEvidenceTargets(t *testing.T, ctx context.Context, db *sql.DB, generationID string) []string {
	t.Helper()
	rows, err := db.QueryContext(
		ctx,
		`SELECT DISTINCT target_repo_id FROM relationship_evidence_facts WHERE generation_id = $1 ORDER BY target_repo_id`,
		generationID,
	)
	if err != nil {
		t.Fatalf("read relationship evidence targets: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var targets []string
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatalf("scan relationship evidence target: %v", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("relationship evidence target rows: %v", err)
	}
	return targets
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// openDerivedEvidenceFencingSchema creates an isolated throwaway schema and
// applies the FULL bootstrap DDL (not just the fact-records subset), because
// this proof exercises the real commit transaction end to end: ingestion
// scopes/generations, fact_records, fact_work_items (projector enqueue), and
// relationship_evidence_facts (derived evidence).
func openDerivedEvidenceFencingSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("derived_evidence_fencing_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create derived-evidence fencing schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply full bootstrap: %v", err)
	}
	return db
}
