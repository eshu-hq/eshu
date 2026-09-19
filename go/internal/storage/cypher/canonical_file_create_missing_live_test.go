// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

const fileCreateMissingRepoID = "live-6798-file-create-missing"

// TestCanonicalFileCreateMissingSkipsExistingFilesLive proves the
// create_missing File templates create only files that do not exist yet, on
// the pinned graph backend. NornicDB v1.3.3 evaluates an uncorrelated
// NOT EXISTS { MATCH (:File {path: row.path}) } by loading every File node per
// row and never matching row.path, so the guard both scanned the whole label
// (#6798: 300 s timeouts at 1-22 rows) and re-stamped existing files.
func TestCanonicalFileCreateMissingSkipsExistingFilesLive(t *testing.T) {
	runner := openBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Cleanup runs LIFO: register the driver close first so graph cleanup
	// still has an open driver.
	t.Cleanup(func() { runner.close(context.Background()) })

	params := map[string]any{"repo_id": fileCreateMissingRepoID}
	cleanup := func() {
		for _, stmt := range []string{
			`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`,
			`MATCH (d:Directory) WHERE d.repo_id = $repo_id DETACH DELETE d`,
			`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		} {
			if err := boltWriteStatement(context.Background(), runner, stmt, params); err != nil {
				t.Fatalf("cleanup %q: %v", stmt, err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, seed := range []string{
		`CREATE (:Repository {id: $repo_id})`,
		`CREATE (:Directory {path: '/live-6798/src', repo_id: $repo_id})`,
		`CREATE (:File {path: '/live-6798/src/existing.go', repo_id: $repo_id, generation_id: 'gen-1'})`,
		`CREATE (:File {path: '/live-6798/existing-root.go', repo_id: $repo_id, generation_id: 'gen-1'})`,
	} {
		if err := boltWriteStatement(ctx, runner, seed, params); err != nil {
			t.Fatalf("seed %q: %v", seed, err)
		}
	}

	row := func(path, dirPath string) map[string]any {
		return map[string]any{
			"path": path, "dir_path": dirPath, "name": "n", "relative_path": "r",
			"uid": path, "language": "go", "repo_id": fileCreateMissingRepoID,
			"scope_id": "scope-6798", "generation_id": "gen-2",
		}
	}
	// A slice, not a map, so the batches run in a fixed order.
	for _, batch := range []struct {
		name string
		stmt Statement
	}{
		// File rows are not de-duplicated by path, so one batch can carry the
		// same missing path twice; it must still create a single node.
		{name: "nested", stmt: Statement{Cypher: canonicalNodeFileCreateMissingCypher, Parameters: map[string]any{"rows": []any{
			row("/live-6798/src/existing.go", "/live-6798/src"),
			row("/live-6798/src/missing.go", "/live-6798/src"),
			row("/live-6798/src/missing.go", "/live-6798/src"),
		}}}},
		// The common re-ingest batch: every file already exists.
		{name: "all existing", stmt: Statement{Cypher: canonicalNodeFileCreateMissingCypher, Parameters: map[string]any{"rows": []any{
			row("/live-6798/src/existing.go", "/live-6798/src"),
		}}}},
		{name: "root", stmt: Statement{Cypher: canonicalNodeRootFileCreateMissingCypher, Parameters: map[string]any{"rows": []any{
			row("/live-6798/existing-root.go", ""),
			row("/live-6798/missing-root.go", ""),
		}}}},
	} {
		if err := runner.runCypherGroup(ctx, batch.stmt); err != nil {
			t.Fatalf("%s create_missing: %v", batch.name, err)
		}
	}

	checks := []struct {
		name  string
		query string
		want  int64
	}{
		{"existing nested file keeps its generation", `MATCH (f:File {path: '/live-6798/src/existing.go'}) WHERE f.generation_id = 'gen-1' RETURN count(f) AS count`, 1},
		{"existing root file keeps its generation", `MATCH (f:File {path: '/live-6798/existing-root.go'}) WHERE f.generation_id = 'gen-1' RETURN count(f) AS count`, 1},
		{"one existing nested node", `MATCH (f:File {path: '/live-6798/src/existing.go'}) RETURN count(f) AS count`, 1},
		{"one existing root node", `MATCH (f:File {path: '/live-6798/existing-root.go'}) RETURN count(f) AS count`, 1},
		{"missing nested file created", `MATCH (f:File {path: '/live-6798/src/missing.go'}) WHERE f.generation_id = 'gen-2' RETURN count(f) AS count`, 1},
		{"duplicate missing row creates one node", `MATCH (f:File {path: '/live-6798/src/missing.go'}) RETURN count(f) AS count`, 1},
		{"duplicate missing row creates one directory edge", `MATCH (:Directory {path: '/live-6798/src'})-[r:CONTAINS]->(:File {path: '/live-6798/src/missing.go'}) RETURN count(r) AS count`, 1},
		{"missing nested file repo edge", `MATCH (:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File {path: '/live-6798/src/missing.go'}) RETURN count(f) AS count`, 1},
		{"missing nested file directory edge", `MATCH (:Directory {path: '/live-6798/src'})-[:CONTAINS]->(f:File {path: '/live-6798/src/missing.go'}) RETURN count(f) AS count`, 1},
		{"missing root file repo edge", `MATCH (:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File {path: '/live-6798/missing-root.go'}) WHERE f.generation_id = 'gen-2' RETURN count(f) AS count`, 1},
	}
	for _, check := range checks {
		got, err := boltCount(ctx, runner, check.query, params)
		if err != nil {
			t.Fatalf("%s: %v", check.name, err)
		}
		if got != check.want {
			t.Errorf("%s: count = %d, want %d", check.name, got, check.want)
		}
	}
}
