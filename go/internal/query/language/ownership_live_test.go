// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

package language

import (
	"context"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestLiveNornicDBDirectoryLanguageQueryCountsOnlyOwnedFiles reproduces the
// phase-group window where a Directory has its new owner while a CONTAINS edge
// still points to a File owned by another repository.
func TestLiveNornicDBDirectoryLanguageQueryCountsOnlyOwnedFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	graph := openLive6541Driver(ctx, t)
	defer func() { _ = graph.Close(context.Background()) }()

	seedTornDirectoryOwnership(ctx, t, graph)
	rows, err := live6541Handler(graph).directoryRowsByLanguage(
		ctx, "go", "", "", 50, live6541Grant("repo://live6703/alpha"),
	)
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("directory rows = %#v, want one alpha directory", rows)
	}
	if got := querycontract.IntVal(rows[0], "file_count"); got != 1 {
		t.Fatalf("alpha directory file_count = %d, want 1; stale cross-repository and ownerless Files must not enter the count", got)
	}
	if got := querycontract.StringVal(rows[0], "repo_id"); got != "repo://live6703/alpha" {
		t.Fatalf("directory repo_id = %q, want alpha", got)
	}
}

// seedTornDirectoryOwnership creates two independently owned Files and one
// ownerless File, then adds stale edges visible between projection phases.
func seedTornDirectoryOwnership(ctx context.Context, t *testing.T, graph neo4jdriver.DriverWithContext) {
	t.Helper()
	session := graph.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	for _, statement := range []string{
		`MERGE (r:Repository {id:'repo://live6703/alpha'}) SET r.name='alpha-6703'`,
		`MERGE (r:Repository {id:'repo://live6703/beta'}) SET r.name='beta-6703'`,
		`MERGE (d:Directory {path:'/live6703/alpha/src'}) SET d.name='src-6703', d.repo_id='repo://live6703/alpha'`,
		`MERGE (f:File {path:'/live6703/alpha/src/a.go'}) SET f.name='a.go', f.language='go', f.repo_id='repo://live6703/alpha'`,
		`MERGE (f:File {path:'/live6703/beta/src/b.go'}) SET f.name='b.go', f.language='go', f.repo_id='repo://live6703/beta'`,
		`MERGE (f:File {path:'/live6703/unknown/x.go'}) SET f.name='x.go', f.language='go'`,
		`MATCH (d:Directory {path:'/live6703/alpha/src'}) MATCH (f:File {path:'/live6703/alpha/src/a.go'}) MERGE (d)-[:CONTAINS]->(f)`,
		`MATCH (d:Directory {path:'/live6703/alpha/src'}) MATCH (f:File {path:'/live6703/beta/src/b.go'}) MERGE (d)-[:CONTAINS]->(f)`,
		`MATCH (d:Directory {path:'/live6703/alpha/src'}) MATCH (f:File {path:'/live6703/unknown/x.go'}) MERGE (d)-[:CONTAINS]->(f)`,
	} {
		result, err := session.Run(ctx, statement, nil)
		if err != nil {
			t.Fatalf("seed torn ownership: %v: %s", err, statement)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("commit torn ownership seed: %v: %s", err, statement)
		}
	}
}
