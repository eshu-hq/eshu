// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

package language

import (
	"context"
	"crypto/rand"
	"fmt"
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
	t.Cleanup(func() { _ = graph.Close(context.Background()) })

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("create fixture nonce: %v", err)
	}
	fixture := tornDirectoryFixture{
		runID: fmt.Sprintf("live6703-%x", nonce),
	}
	t.Cleanup(func() { cleanupTornDirectoryOwnership(t, graph, fixture.runID) })

	seedTornDirectoryOwnership(ctx, t, graph, fixture)
	rows, err := live6541Handler(graph).directoryRowsByLanguage(
		ctx, "go", "", "", 50, live6541Grant(fixture.alphaRepo()),
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
	if got := querycontract.StringVal(rows[0], "repo_id"); got != fixture.alphaRepo() {
		t.Fatalf("directory repo_id = %q, want alpha", got)
	}
}

type tornDirectoryFixture struct {
	runID string
}

func (f tornDirectoryFixture) alphaRepo() string { return "repo://" + f.runID + "/alpha" }
func (f tornDirectoryFixture) betaRepo() string  { return "repo://" + f.runID + "/beta" }
func (f tornDirectoryFixture) directory() string { return "/" + f.runID + "/alpha/src" }
func (f tornDirectoryFixture) alphaFile() string { return f.directory() + "/a.go" }
func (f tornDirectoryFixture) betaFile() string  { return "/" + f.runID + "/beta/src/b.go" }
func (f tornDirectoryFixture) ownerless() string { return "/" + f.runID + "/unknown/x.go" }

// seedTornDirectoryOwnership creates two independently owned Files and one
// ownerless File, then adds stale edges visible between projection phases.
func seedTornDirectoryOwnership(ctx context.Context, t *testing.T, graph neo4jdriver.DriverWithContext, fixture tornDirectoryFixture) {
	t.Helper()
	session := graph.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	for _, statement := range []string{
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name='alpha-6703', r.proof_run_id=%q`, fixture.alphaRepo(), fixture.runID),
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name='beta-6703', r.proof_run_id=%q`, fixture.betaRepo(), fixture.runID),
		fmt.Sprintf(`MERGE (d:Directory {path:%q}) SET d.name='src-6703', d.repo_id=%q, d.proof_run_id=%q`, fixture.directory(), fixture.alphaRepo(), fixture.runID),
		fmt.Sprintf(`MERGE (f:File {path:%q}) SET f.name='a.go', f.language='go', f.repo_id=%q, f.proof_run_id=%q`, fixture.alphaFile(), fixture.alphaRepo(), fixture.runID),
		fmt.Sprintf(`MERGE (f:File {path:%q}) SET f.name='b.go', f.language='go', f.repo_id=%q, f.proof_run_id=%q`, fixture.betaFile(), fixture.betaRepo(), fixture.runID),
		fmt.Sprintf(`MERGE (f:File {path:%q}) SET f.name='x.go', f.language='go', f.proof_run_id=%q`, fixture.ownerless(), fixture.runID),
		fmt.Sprintf(`MATCH (d:Directory {path:%q}) MATCH (f:File {path:%q}) MERGE (d)-[:CONTAINS]->(f)`, fixture.directory(), fixture.alphaFile()),
		fmt.Sprintf(`MATCH (d:Directory {path:%q}) MATCH (f:File {path:%q}) MERGE (d)-[:CONTAINS]->(f)`, fixture.directory(), fixture.betaFile()),
		fmt.Sprintf(`MATCH (d:Directory {path:%q}) MATCH (f:File {path:%q}) MERGE (d)-[:CONTAINS]->(f)`, fixture.directory(), fixture.ownerless()),
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

// cleanupTornDirectoryOwnership removes only nodes tagged by this test run.
func cleanupTornDirectoryOwnership(t *testing.T, graph neo4jdriver.DriverWithContext, runID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session := graph.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, `MATCH (n {proof_run_id: $run_id}) DETACH DELETE n`, map[string]any{"run_id": runID})
	if err != nil {
		t.Errorf("cleanup torn ownership fixture %q: %v", runID, err)
		return
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Errorf("commit torn ownership cleanup %q: %v", runID, err)
		return
	}
	remaining, err := session.Run(ctx, `MATCH (n {proof_run_id: $run_id}) RETURN n`, map[string]any{"run_id": runID})
	if err != nil {
		t.Errorf("check torn ownership cleanup %q: %v", runID, err)
		return
	}
	if remaining.Next(ctx) {
		t.Errorf("torn ownership fixture %q remains after cleanup", runID)
	}
	if err := remaining.Err(); err != nil {
		t.Errorf("read torn ownership cleanup check %q: %v", runID, err)
	}
}
