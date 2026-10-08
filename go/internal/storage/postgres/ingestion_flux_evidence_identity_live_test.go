// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Native Flux evidence identity proof for issue #7543.
//
// The Flux GitRepository producer derives SourceEntityID with NUL separators
// (flux_evidence.go), and insertEvidenceFactBatch binds it unchanged to the
// source_entity_id TEXT column, so a native commit carrying Flux evidence
// fails with SQLSTATE 22021. This test drives the real
// IngestionStore.CommitScopeGeneration -> DiscoverEvidenceWithStats ->
// RelationshipStore.UpsertEvidenceFacts path against a live Postgres and
// requires the commit to succeed with one NUL-free persisted evidence row.

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestIngestionStoreCommitScopeGenerationPersistsFluxEvidenceNatively(t *testing.T) {
	dsn := factCrossBatchFencingProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the native Flux evidence identity proof")
	}

	ctx := context.Background()
	db := openDerivedEvidenceFencingSchema(t, ctx, dsn)
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
		testFactChannel([]facts.Envelope{fluxEnvelope}),
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
		testFactChannel([]facts.Envelope{envelope}),
	); err != nil {
		t.Fatalf("onboard repository %q: CommitScopeGeneration() error = %v, want nil", repoID, err)
	}
}
