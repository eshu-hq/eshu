// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const repoDependencyIfaMismatchHTTPBaseURLEnv = "ESHU_REPO_DEPENDENCY_PROOF_MISMATCH_HTTP_URL"

func TestRepoDependencyIfaProofRejectsNonDisposableDatabase(t *testing.T) {
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)

	artifactIDs := repoDependencyIfaArtifactIDs(t, repoDependencyIfaRows(t, mustRepoDependencyIfaOdu(t)))
	if err := validateRepoDependencyIfaDisposableBackend(ctx, exec, artifactIDs); err != nil {
		t.Fatalf("hostile guard test requires a clean disposable graph database: %v", err)
	}
	collidingArtifactID := artifactIDs[0]
	const (
		collidingRepositoryID = "repository:source-01"
		collidingEnvironment  = "ifa-prod-proof"
		collisionMarker       = "unowned-canonical-sentinel"
	)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, err := runRepoDependencyIfaHTTP(cleanupCtx, exec, []map[string]any{
			{
				"statement":  `MATCH (repo:Repository {id: $id, collision_marker: $marker}) DETACH DELETE repo`,
				"parameters": map[string]any{"id": collidingRepositoryID, "marker": collisionMarker},
			},
			{
				"statement":  `MATCH (env:Environment {name: $name, collision_marker: $marker}) DETACH DELETE env`,
				"parameters": map[string]any{"name": collidingEnvironment, "marker": collisionMarker},
			},
			{
				"statement":  `MATCH (artifact:EvidenceArtifact {id: $id, collision_marker: $marker}) DETACH DELETE artifact`,
				"parameters": map[string]any{"id": collidingArtifactID, "marker": collisionMarker},
			},
		})
		if err != nil {
			t.Fatalf("cleanup hostile disposable-database sentinel: %v", err)
		}
	})

	_, err := runRepoDependencyIfaHTTP(ctx, exec, []map[string]any{{
		"statement": `MERGE (repo:Repository {id: $repository_id})
			SET repo.collision_marker = $marker
			MERGE (artifact:EvidenceArtifact {id: $artifact_id})
			SET artifact.collision_marker = $marker
			MERGE (env:Environment {name: $environment})
			SET env.collision_marker = $marker
			MERGE (repo)-[:HAS_DEPLOYMENT_EVIDENCE {collision_marker: $marker}]->(artifact)
			MERGE (artifact)-[:TARGETS_ENVIRONMENT {collision_marker: $marker}]->(env)`,
		"parameters": map[string]any{
			"repository_id": collidingRepositoryID,
			"artifact_id":   collidingArtifactID,
			"environment":   collidingEnvironment,
			"marker":        collisionMarker,
		},
	}})
	if err != nil {
		t.Fatalf("seed hostile disposable-database collision: %v", err)
	}

	_, err = tryAcquireRepoDependencyIfaExclusiveBackend(ctx, exec, artifactIDs)
	if err == nil {
		t.Fatal("exclusive backend acquisition accepted unowned identity collisions")
	}
	for _, identity := range []string{collidingRepositoryID, collidingEnvironment, collidingArtifactID} {
		if !strings.Contains(err.Error(), identity) {
			t.Fatalf("disposable-database error %q does not name collision %q", err, identity)
		}
	}
	parameters := map[string]any{
		"repository_id": collidingRepositoryID,
		"environment":   collidingEnvironment,
		"artifact_id":   collidingArtifactID,
		"marker":        collisionMarker,
	}
	results, queryErr := runRepoDependencyIfaHTTP(ctx, exec, []map[string]any{
		{"statement": `MATCH (node:Repository {id: $repository_id, collision_marker: $marker}) RETURN count(node)`, "parameters": parameters},
		{"statement": `MATCH (node:Environment {name: $environment, collision_marker: $marker}) RETURN count(node)`, "parameters": parameters},
		{"statement": `MATCH (node:EvidenceArtifact {id: $artifact_id, collision_marker: $marker}) RETURN count(node)`, "parameters": parameters},
		{
			"statement": `MATCH (:Repository {id: $repository_id})-[rel:HAS_DEPLOYMENT_EVIDENCE {collision_marker: $marker}]->
				(:EvidenceArtifact {id: $artifact_id}) RETURN count(rel)`,
			"parameters": parameters,
		},
		{
			"statement": `MATCH (:EvidenceArtifact {id: $artifact_id})-[rel:TARGETS_ENVIRONMENT {collision_marker: $marker}]->
				(:Environment {name: $environment}) RETURN count(rel)`,
			"parameters": parameters,
		},
		{
			"statement":  `MATCH (lock:Repository {id: $lock_id}) RETURN count(lock)`,
			"parameters": map[string]any{"lock_id": repoDependencyIfaExclusiveLockID},
		},
	})
	if queryErr != nil {
		t.Fatalf("read preserved collision snapshot: %v", queryErr)
	}
	for index, want := range []int64{1, 1, 1, 1, 1, 0} {
		if len(results[index].rows) != 1 || len(results[index].rows[0]) != 1 {
			t.Fatalf("preserved collision result %d shape=%v, want one scalar", index, results[index].rows)
		}
		got, parseErr := strconv.ParseFloat(fmt.Sprint(results[index].rows[0][0]), 64)
		if parseErr != nil {
			t.Fatalf(
				"preserved collision result %d value=%v type=%T, want number: %v",
				index,
				results[index].rows[0][0],
				results[index].rows[0][0],
				parseErr,
			)
		}
		if int64(got) != want {
			t.Fatalf("preserved collision result %d=%d, want %d", index, int64(got), want)
		}
	}
}

func TestRepoDependencyIfaProofRejectsMismatchedHTTPBackend(t *testing.T) {
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}
	mismatchHTTPURL := strings.TrimSpace(os.Getenv(repoDependencyIfaMismatchHTTPBaseURLEnv))
	if mismatchHTTPURL == "" {
		t.Skipf("set %s to a different clean disposable NornicDB HTTP endpoint", repoDependencyIfaMismatchHTTPBaseURLEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)
	artifactIDs := repoDependencyIfaArtifactIDs(t, repoDependencyIfaRows(t, mustRepoDependencyIfaOdu(t)))
	if err := validateRepoDependencyIfaDisposableBackend(ctx, exec, artifactIDs); err != nil {
		t.Fatalf("mismatch guard test requires a clean Bolt target: %v", err)
	}

	const (
		collidingRepositoryID = "repository:source-01"
		collidingEnvironment  = "ifa-prod-proof"
		collisionMarker       = "unowned-bolt-mismatch-sentinel"
	)
	collidingArtifactID := artifactIDs[0]
	parameters := map[string]any{
		"repository_id": collidingRepositoryID,
		"artifact_id":   collidingArtifactID,
		"environment":   collidingEnvironment,
		"marker":        collisionMarker,
	}
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher: `MERGE (repo:Repository {id: $repository_id})
			SET repo.collision_marker = $marker
			MERGE (artifact:EvidenceArtifact {id: $artifact_id})
			SET artifact.collision_marker = $marker
			MERGE (env:Environment {name: $environment})
			SET env.collision_marker = $marker
			MERGE (repo)-[:HAS_DEPLOYMENT_EVIDENCE {collision_marker: $marker}]->(artifact)
			MERGE (artifact)-[:TARGETS_ENVIRONMENT {collision_marker: $marker}]->(env)`,
		Parameters: parameters,
	}); err != nil {
		t.Fatalf("seed Bolt-only hostile collision: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, statement := range []cypher.Statement{
			{Cypher: `MATCH (repo:Repository {id: $repository_id, collision_marker: $marker}) DETACH DELETE repo`, Parameters: parameters},
			{Cypher: `MATCH (env:Environment {name: $environment, collision_marker: $marker}) DETACH DELETE env`, Parameters: parameters},
			{Cypher: `MATCH (artifact:EvidenceArtifact {id: $artifact_id, collision_marker: $marker}) DETACH DELETE artifact`, Parameters: parameters},
		} {
			if err := exec.Execute(cleanupCtx, statement); err != nil {
				t.Fatalf("cleanup Bolt-only hostile collision: %v", err)
			}
		}
	})

	t.Setenv(repoDependencyIfaHTTPBaseURLEnv, mismatchHTTPURL)
	_, err := tryAcquireRepoDependencyIfaExclusiveBackend(ctx, exec, artifactIDs)
	if err == nil {
		t.Fatal("exclusive backend acquisition trusted a clean mismatched HTTP endpoint over a dirty Bolt target")
	}
	for _, identity := range []string{collidingRepositoryID, collidingEnvironment, collidingArtifactID} {
		if !strings.Contains(err.Error(), identity) {
			t.Fatalf("mismatched-endpoint error %q does not name Bolt collision %q", err, identity)
		}
	}
	assertRepoDependencyIfaBoltCollisionPreserved(ctx, t, exec, parameters)
}

func TestRepoDependencyIfaProofCleansAmbiguousCommittedLock(t *testing.T) {
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)
	artifactIDs := repoDependencyIfaArtifactIDs(t, repoDependencyIfaRows(t, mustRepoDependencyIfaOdu(t)))
	if err := validateRepoDependencyIfaDisposableBackend(ctx, exec, artifactIDs); err != nil {
		t.Fatalf("ambiguous lock test requires a clean disposable graph database: %v", err)
	}

	_, err := tryAcquireRepoDependencyIfaExclusiveBackendWithLockWriter(
		ctx,
		exec,
		artifactIDs,
		func(writeCtx context.Context, statement cypher.Statement) error {
			if err := exec.Execute(writeCtx, statement); err != nil {
				return err
			}
			return errors.New("consume write: connection lost after commit")
		},
	)
	if err == nil {
		t.Fatal("exclusive backend acquisition error = nil, want ambiguous committed lock response")
	}
	lockCount, countErr := exec.count(
		ctx,
		`MATCH (lock:Repository {id: $id}) RETURN count(lock)`,
		map[string]any{"id": repoDependencyIfaExclusiveLockID},
	)
	if countErr != nil {
		t.Fatalf("read proof lock after ambiguous response: %v", countErr)
	}
	if lockCount != 0 {
		t.Fatalf("proof locks after ambiguous committed response = %d, want 0", lockCount)
	}
}

func assertRepoDependencyIfaBoltCollisionPreserved(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	parameters map[string]any,
) {
	t.Helper()
	checks := []struct {
		query string
		want  int64
	}{
		{`MATCH (node:Repository {id: $repository_id, collision_marker: $marker}) RETURN count(node)`, 1},
		{`MATCH (node:Environment {name: $environment, collision_marker: $marker}) RETURN count(node)`, 1},
		{`MATCH (node:EvidenceArtifact {id: $artifact_id, collision_marker: $marker}) RETURN count(node)`, 1},
		{`MATCH (:Repository {id: $repository_id})-[rel:HAS_DEPLOYMENT_EVIDENCE {collision_marker: $marker}]->(:EvidenceArtifact {id: $artifact_id}) RETURN count(rel)`, 1},
		{`MATCH (:EvidenceArtifact {id: $artifact_id})-[rel:TARGETS_ENVIRONMENT {collision_marker: $marker}]->(:Environment {name: $environment}) RETURN count(rel)`, 1},
		{`MATCH (lock:Repository {id: $lock_id}) RETURN count(lock)`, 0},
	}
	parameters = maps.Clone(parameters)
	parameters["lock_id"] = repoDependencyIfaExclusiveLockID
	for index, check := range checks {
		got, err := exec.count(ctx, check.query, parameters)
		if err != nil {
			t.Fatalf("read preserved Bolt collision %d: %v", index, err)
		}
		if got != check.want {
			t.Fatalf("preserved Bolt collision %d=%d, want %d", index, got, check.want)
		}
	}
}
