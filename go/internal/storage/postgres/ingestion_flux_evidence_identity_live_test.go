// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Native Flux evidence identity proof for issue #7543.
//
// Before the #7543 fix the Flux GitRepository producer derived
// SourceEntityID with NUL separators (flux_evidence.go), and
// insertEvidenceFactBatch bound it unchanged to the source_entity_id TEXT
// column, so a native commit carrying Flux evidence failed with SQLSTATE
// 22021 (RED). This test drives the real
// IngestionStore.CommitScopeGeneration -> DiscoverEvidenceWithStats ->
// RelationshipStore.UpsertEvidenceFacts path against a live Postgres and
// requires the commit to succeed with one NUL-free persisted evidence row
// (GREEN).

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openFluxEvidenceIdentitySchema opens a disposable PostgreSQL 18 database,
// creates an isolated schema with the full bootstrap, and returns a handle
// pinned to it. It runs in the live-postgres-readiness runner. Run locally
// with a disposable PostgreSQL 18 administrative database:
//
//	ESHU_FLUX_EVIDENCE_IDENTITY_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	ESHU_FLUX_EVIDENCE_IDENTITY_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres -run 'TestIngestionStoreCommitScopeGenerationPersistsFluxEvidenceNatively|TestFluxEvidenceMixedGenerationLegacyAndCurrentCoexist' -count=1 -v
func openFluxEvidenceIdentitySchema(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_FLUX_EVIDENCE_IDENTITY_PROOF_DSN")
	optIn := os.Getenv("ESHU_FLUX_EVIDENCE_IDENTITY_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)

	schemaName := fmt.Sprintf("flux_evidence_identity_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create flux evidence identity schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply full bootstrap: %v", err)
	}
	return ctx, db
}

func TestIngestionStoreCommitScopeGenerationPersistsFluxEvidenceNatively(t *testing.T) {
	ctx, db := openFluxEvidenceIdentitySchema(t)
	store := NewIngestionStore(SQLDB{DB: db})
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	const (
		targetRepoID = "repo-flux-target"
		targetURL    = "https://github.com/myorg/payments-deploy.git"
		sourceScope  = "scope-flux-source"
		generationID = "gen-flux-source"
	)
	mustCommitFluxIdentityRepository(t, ctx, store, "scope-flux-target", "gen-flux-target",
		targetRepoID, targetURL, now)

	fluxEnvelope := facts.Envelope{
		FactID:        "fact-flux-identity",
		ScopeID:       sourceScope,
		GenerationID:  generationID,
		FactKind:      "file",
		StableFactKey: "file:fact-flux-identity",
		ObservedAt:    now.Add(time.Minute),
		Payload: map[string]any{
			"repo_id":       "repo-flux-source",
			"relative_path": "clusters/prod/git-repository.yaml",
			"parsed_file_data": map[string]any{
				"flux_git_repositories": []any{
					map[string]any{
						"name":      "app-source",
						"namespace": "flux-system",
						"url":       targetURL,
					},
				},
			},
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: "fact-flux-identity"},
	}
	if err := store.CommitScopeGeneration(
		ctx,
		derivedEvidenceScope(sourceScope),
		derivedEvidenceGeneration(sourceScope, generationID, now.Add(time.Minute)),
		testfixtures.FactChannel([]facts.Envelope{fluxEnvelope}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil (SQLSTATE 22021 = NUL identity)", err)
	}

	var sourceEntityID string
	if err := db.QueryRowContext(ctx,
		`SELECT source_entity_id FROM relationship_evidence_facts WHERE generation_id = $1`,
		generationID,
	).Scan(&sourceEntityID); err != nil {
		t.Fatalf("read persisted source_entity_id: %v", err)
	}
	if strings.ContainsRune(sourceEntityID, 0) {
		t.Fatalf("persisted source_entity_id = %q, must not contain NUL", sourceEntityID)
	}
	if !strings.HasPrefix(sourceEntityID, "FluxGitRepository:v1:") ||
		!strings.Contains(sourceEntityID, hex.EncodeToString([]byte("repo-flux-source"))) {
		t.Fatalf("persisted source_entity_id = %q, want FluxGitRepository:v1:<hex(source)>... identity",
			sourceEntityID)
	}
}

// TestFluxEvidenceMixedGenerationLegacyAndCurrentCoexist is the issue
// #7543 existing-state regression: a generation carrying both a legacy
// NULL-source Flux row (historical producers omitted SourceEntityID and
// the namespace) and a current-producer row must load, resolve, and
// persist without error. The legacy row keeps resolving through the
// repository fallback; the current row resolves by explicit identity.
// Exact reconstruction of the legacy row is unavailable (no namespace
// was stored), so no rewrite is attempted: the rows coexist, and the
// graph layer converges them (DEPLOYS_FROM MERGE keys on endpoints).
func TestFluxEvidenceMixedGenerationLegacyAndCurrentCoexist(t *testing.T) {
	ctx, db := openFluxEvidenceIdentitySchema(t)
	store := NewIngestionStore(SQLDB{DB: db})
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	const (
		targetRepoID = "repo-flux-target"
		targetURL    = "https://github.com/myorg/payments-deploy.git"
		sourceRepoID = "repo-flux-source"
		sourceScope  = "scope-flux-mixed"
		generationID = "gen-flux-mixed"
	)
	mustCommitFluxIdentityRepository(t, ctx, store, "scope-flux-target", "gen-flux-target",
		targetRepoID, targetURL, now)

	// The legacy row, as a historical producer wrote it: empty entity id
	// (bound as SQL NULL) and details without a namespace.
	legacy := relationships.EvidenceFact{
		EvidenceKind:     relationships.EvidenceKindFluxGitRepositorySource,
		RelationshipType: relationships.RelDeploysFrom,
		SourceRepoID:     sourceRepoID,
		SourceEntityID:   "",
		TargetRepoID:     targetRepoID,
		Confidence:       relationships.DefaultConfidenceRegistry.ConfidenceFor(relationships.EvidenceKindFluxGitRepositorySource),
		Rationale:        "legacy flux row",
		Details:          map[string]any{"path": "sources.yaml", "extractor": "flux"},
	}
	relStore := NewRelationshipStore(SQLDB{DB: db})
	if err := relStore.UpsertEvidenceFacts(ctx, generationID, []relationships.EvidenceFact{legacy}); err != nil {
		t.Fatalf("persist legacy evidence: %v", err)
	}

	// The current-producer row for the same logical source, derived
	// through the real commit path in the same generation.
	fluxEnvelope := facts.Envelope{
		FactID:        "fact-flux-mixed",
		ScopeID:       sourceScope,
		GenerationID:  generationID,
		FactKind:      "file",
		StableFactKey: "file:fact-flux-mixed",
		ObservedAt:    now.Add(time.Minute),
		Payload: map[string]any{
			"repo_id":       sourceRepoID,
			"relative_path": "clusters/prod/git-repository.yaml",
			"parsed_file_data": map[string]any{
				"flux_git_repositories": []any{
					map[string]any{
						"name":      "app-source",
						"namespace": "flux-system",
						"url":       targetURL,
					},
				},
			},
		},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: "fact-flux-mixed"},
	}
	if err := store.CommitScopeGeneration(
		ctx,
		derivedEvidenceScope(sourceScope),
		derivedEvidenceGeneration(sourceScope, generationID, now.Add(time.Minute)),
		testfixtures.FactChannel([]facts.Envelope{fluxEnvelope}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	loaded, err := relStore.ListEvidenceFacts(ctx, generationID)
	if err != nil {
		t.Fatalf("ListEvidenceFacts() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded evidence count = %d, want 2 (legacy + current)", len(loaded))
	}

	candidates, resolved := relationships.Resolve(loaded, nil, 0)
	if len(candidates) != 2 || len(resolved) != 2 {
		t.Fatalf("candidates = %d, resolved = %d, want 2/2 (legacy by repo, current by identity)",
			len(candidates), len(resolved))
	}
	for _, c := range candidates {
		if c.SourceRepoID != sourceRepoID || c.TargetRepoID != targetRepoID {
			t.Fatalf("candidate endpoints = %s->%s, want %s->%s",
				c.SourceRepoID, c.TargetRepoID, sourceRepoID, targetRepoID)
		}
	}

	if err := relStore.UpsertResolved(ctx, generationID, resolved); err != nil {
		t.Fatalf("UpsertResolved() error = %v", err)
	}
	// Read back by generation directly: the scope-joined getter needs a
	// relationship_generations row, a separate lifecycle this proof does
	// not exercise.
	var stored int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resolved_relationships WHERE generation_id = $1`,
		generationID,
	).Scan(&stored); err != nil {
		t.Fatalf("count stored resolved: %v", err)
	}
	if stored != 2 {
		t.Fatalf("stored resolved count = %d, want 2", stored)
	}
}

// mustCommitFluxIdentityRepository onboards one "repository" fact with a
// remote_url through the real commit path so the shared repository catalog
// carries the RemoteURL the Flux strict-equality resolution matches.
func mustCommitFluxIdentityRepository(
	t *testing.T,
	ctx context.Context,
	store IngestionStore,
	scopeID, generationID, repoID, remoteURL string,
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
		Payload:       map[string]any{"repo_id": repoID, "name": repoID, "remote_url": remoteURL},
		SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "fact-" + generationID},
	}
	if err := store.CommitScopeGeneration(
		ctx,
		derivedEvidenceScope(scopeID),
		derivedEvidenceGeneration(scopeID, generationID, observedAt),
		testfixtures.FactChannel([]facts.Envelope{envelope}),
	); err != nil {
		t.Fatalf("onboard repository %q: CommitScopeGeneration() error = %v, want nil", repoID, err)
	}
}
