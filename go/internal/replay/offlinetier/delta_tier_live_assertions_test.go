// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func assertDeltaDirectoryContainsEdgeCount(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	parentPath string,
	childPath string,
	want int64,
	msg string,
) {
	t.Helper()
	var count int64
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		count, err = exec.count(
			ctx,
			`MATCH (p:Directory {path: $parent_path})-[r:CONTAINS]->(d:Directory {path: $child_path}) RETURN count(r)`,
			map[string]any{"parent_path": parentPath, "child_path": childPath},
		)
		if err != nil {
			t.Fatalf("count CONTAINS edge %q -> %q: %v", parentPath, childPath, err)
		}
		if count == want || want != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count != want {
		t.Fatalf("%s: CONTAINS edge %q -> %q count = %d, want %d", msg, parentPath, childPath, count, want)
	}
	t.Logf("CONTAINS edge %q -> %q count=%d (want %d) — %s", parentPath, childPath, count, want, msg)
}

func assertDeltaDirCount(ctx context.Context, t *testing.T, exec liveExecutor, path string, want int64, msg string) {
	t.Helper()
	var count int64
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		count, err = exec.count(
			ctx,
			`MATCH (d:Directory {path: $path}) RETURN count(d)`,
			map[string]any{"path": path},
		)
		if err != nil {
			t.Fatalf("count directory %q: %v", path, err)
		}
		if count == want || want != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count != want {
		matchingCanonical, _ := exec.count(
			ctx,
			`MATCH (d:Directory {path: $path})
WHERE d.repo_id = $repo_id AND d.evidence_source = 'projector/canonical'
RETURN count(d)`,
			map[string]any{"path": path, "repo_id": deltaRepoID},
		)
		incoming, _ := exec.count(
			ctx,
			`MATCH ()-[:CONTAINS]->(d:Directory {path: $path}) RETURN count(d)`,
			map[string]any{"path": path},
		)
		outgoing, _ := exec.count(
			ctx,
			`MATCH (d:Directory {path: $path})-[:CONTAINS]->() RETURN count(d)`,
			map[string]any{"path": path},
		)
		t.Fatalf(
			"%s: directory %q count = %d, want %d (canonical_matches=%d incoming_contains=%d outgoing_contains=%d)",
			msg,
			path,
			count,
			want,
			matchingCanonical,
			incoming,
			outgoing,
		)
	}
	t.Logf("directory %q count=%d (want %d) — %s", path, count, want, msg)
}

func assertDeltaIncomingContainsCount(ctx context.Context, t *testing.T, exec liveExecutor, path string, want int64, msg string) {
	t.Helper()
	count, err := exec.count(
		ctx,
		`MATCH ()-[r:CONTAINS]->(d:Directory {path: $path}) RETURN count(r)`,
		map[string]any{"path": path},
	)
	if err != nil {
		t.Fatalf("count incoming CONTAINS edge for %q: %v", path, err)
	}
	if count != want {
		t.Fatalf("%s: incoming CONTAINS edge for %q count = %d, want %d", msg, path, count, want)
	}
	t.Logf("incoming CONTAINS edge for %q count=%d (want %d) — %s", path, count, want, msg)
}

func assertNoAnonymousDeltaDirectoryShells(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	count, err := exec.count(
		ctx,
		`MATCH (d:Directory)
WHERE d.repo_id IS NULL
RETURN count(d)`,
		nil,
	)
	if err != nil {
		t.Fatalf("count anonymous Directory shells: %v", err)
	}
	if count != 0 {
		t.Fatalf("anonymous Directory shell count = %d, want 0", count)
	}
	t.Log("anonymous Directory shell count=0")
}

func cleanupDeltaScope(ctx context.Context, t *testing.T, exec deltaCleanupExecutor) {
	t.Helper()
	statements := []cypher.Statement{
		{Cypher: `MATCH (j:GitlabJob {repo_id: $repo_id}) DETACH DELETE j`, Parameters: map[string]any{"repo_id": deltaRepoID}},
		{Cypher: `MATCH (p:GitlabPipeline {repo_id: $repo_id}) DETACH DELETE p`, Parameters: map[string]any{"repo_id": deltaRepoID}},
		{
			Cypher: `MATCH (f:File)
WHERE f.repo_id = $repo_id OR f.path STARTS WITH $repo_path_prefix
DETACH DELETE f`,
			Parameters: map[string]any{"repo_id": deltaRepoID, "repo_path_prefix": deltaRepoPath + "/"},
		},
		{Cypher: `MATCH (d:Directory {repo_id: $repo_id}) DETACH DELETE d`, Parameters: map[string]any{"repo_id": deltaRepoID}},
		{Cypher: `MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`, Parameters: map[string]any{"repo_id": deltaRepoID}},
	}
	for index, statement := range statements {
		if err := exec.Execute(ctx, statement); err != nil {
			t.Fatalf("cleanup delta scope statement %d/%d: %v", index+1, len(statements), err)
		}
	}
}
