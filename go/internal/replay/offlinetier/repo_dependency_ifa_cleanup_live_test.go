// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"testing"
	"time"
)

func TestRepoDependencyIfaCleanupPreservesUnrelatedArtifact(t *testing.T) {
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)
	artifactIDs := repoDependencyIfaArtifactIDs(t, repoDependencyIfaRows(t, mustRepoDependencyIfaOdu(t)))
	acquireRepoDependencyIfaExclusiveBackend(ctx, t, exec, artifactIDs)
	const sentinelID = "ifa-repo-dependency-cleanup-unrelated-sentinel"
	deleteArtifact := func(cleanupCtx context.Context, artifactID string) {
		if _, err := runRepoDependencyIfaHTTP(cleanupCtx, exec, []map[string]any{{
			"statement":  `MATCH (artifact:EvidenceArtifact {id: $id}) DETACH DELETE artifact`,
			"parameters": map[string]any{"id": artifactID},
		}}); err != nil {
			t.Fatalf("delete cleanup artifact %q: %v", artifactID, err)
		}
	}
	fixtureArtifactID := artifactIDs[0]
	deleteArtifact(ctx, sentinelID)
	deleteArtifact(ctx, fixtureArtifactID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		deleteArtifact(cleanupCtx, sentinelID)
		deleteArtifact(cleanupCtx, fixtureArtifactID)
	})

	for _, artifactID := range []string{sentinelID, fixtureArtifactID} {
		if _, err := runRepoDependencyIfaHTTP(ctx, exec, []map[string]any{{
			"statement": `MERGE (artifact:EvidenceArtifact {id: $id})
				SET artifact.generation_id = $generation_id, artifact.path = $path`,
			"parameters": map[string]any{
				"id":            artifactID,
				"generation_id": "generation:repo-dependency:source-01",
				"path":          "env/ifa-prod-proof/main.tf",
			},
		}}); err != nil {
			t.Fatalf("create cleanup artifact %q: %v", artifactID, err)
		}
	}

	cleanupRepoDependencyConcurrencyScope(ctx, t, exec, artifactIDs)
	assertRepoDependencyIfaCleanup(ctx, t, exec, artifactIDs)
	snapshotArtifactIDs := append(append([]string(nil), artifactIDs...), sentinelID)
	snapshot, err := readRepoDependencyIfaIdentitySnapshot(ctx, exec, snapshotArtifactIDs)
	if err != nil {
		t.Fatalf("read unrelated cleanup sentinel snapshot: %v", err)
	}
	var got int
	for _, artifactID := range snapshot.artifactIDs {
		if artifactID == sentinelID {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("unrelated cleanup sentinel count=%d, want 1", got)
	}
}
