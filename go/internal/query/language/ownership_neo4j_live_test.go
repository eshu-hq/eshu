// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_neo4j_directory_ownership

package language

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestLiveNeo4jDirectoryLanguageQueryCountsOnlyOwnedFiles proves that a
// stale cross-repository edge and an ownerless File cannot inflate the
// directory count on the Neo4j backend.
func TestLiveNeo4jDirectoryLanguageQueryCountsOnlyOwnedFiles(t *testing.T) {
	uri := os.Getenv("ESHU_NEO4J_URI")
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI must point to a disposable Neo4j store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open Neo4j driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify Neo4j connectivity: %v", err)
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("create fixture nonce: %v", err)
	}
	runID := fmt.Sprintf("live6703-neo4j-%x", nonce)
	alphaRepo := "repo://" + runID + "/alpha"
	betaRepo := "repo://" + runID + "/beta"
	t.Cleanup(func() { cleanupNeo4jDirectoryOwnership(t, driver, runID) })

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "neo4j",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	seed, err := session.Run(ctx, `
CREATE (r:Repository {id:$alpha, name:'alpha', proof_run_id:$run_id}),
       (d:Directory {path:$dir, name:'src', repo_id:$alpha, proof_run_id:$run_id}),
       (owned:File {path:$owned, name:'a.go', language:'go', repo_id:$alpha, proof_run_id:$run_id}),
       (stale:File {path:$stale, name:'b.go', language:'go', repo_id:$beta, proof_run_id:$run_id}),
       (ownerless:File {path:$ownerless, name:'x.go', language:'go', proof_run_id:$run_id}),
       (d)-[:CONTAINS]->(owned),
       (d)-[:CONTAINS]->(stale),
       (d)-[:CONTAINS]->(ownerless)
`, map[string]any{
		"alpha": alphaRepo, "beta": betaRepo, "run_id": runID,
		"dir":       "/" + runID + "/alpha/src",
		"owned":     "/" + runID + "/alpha/src/a.go",
		"stale":     "/" + runID + "/beta/src/b.go",
		"ownerless": "/" + runID + "/unknown/x.go",
	})
	if err != nil {
		_ = session.Close(ctx)
		t.Fatalf("seed Neo4j ownership fixture: %v", err)
	}
	if _, err := seed.Consume(ctx); err != nil {
		_ = session.Close(ctx)
		t.Fatalf("commit Neo4j ownership fixture: %v", err)
	}
	if err := session.Close(ctx); err != nil {
		t.Fatalf("close Neo4j seed session: %v", err)
	}

	handler := &Handler{Neo4j: neo4jDirectoryOwnershipReader{driver: driver}}
	grant := codequery.LanguageQueryGrant{Access: querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{alphaRepo},
	}}
	rows, err := handler.directoryRowsByLanguage(ctx, "go", "", "", 50, grant)
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}
	if len(rows) != 1 || querycontract.IntVal(rows[0], "file_count") != 1 {
		t.Fatalf("Neo4j directory rows = %#v, want one owned File", rows)
	}
	if got := querycontract.StringVal(rows[0], "repo_id"); got != alphaRepo {
		t.Fatalf("directory repo_id = %q, want %q", got, alphaRepo)
	}
}

type neo4jDirectoryOwnershipReader struct {
	driver neo4jdriver.DriverWithContext
}

func (r neo4jDirectoryOwnershipReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "neo4j",
		AccessMode:   neo4jdriver.AccessModeRead,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r neo4jDirectoryOwnershipReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func cleanupNeo4jDirectoryOwnership(t *testing.T, driver neo4jdriver.DriverWithContext, runID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "neo4j",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()
	params := map[string]any{"run_id": runID}
	deleted, err := session.Run(ctx, `MATCH (n {proof_run_id:$run_id}) DETACH DELETE n`, params)
	if err != nil {
		t.Errorf("cleanup Neo4j ownership fixture: %v", err)
		return
	}
	if _, err := deleted.Consume(ctx); err != nil {
		t.Errorf("commit Neo4j ownership cleanup: %v", err)
		return
	}
	remaining, err := session.Run(ctx, `MATCH (n {proof_run_id:$run_id}) RETURN n`, params)
	if err != nil {
		t.Errorf("check Neo4j ownership cleanup: %v", err)
		return
	}
	if remaining.Next(ctx) {
		t.Error("Neo4j ownership fixture remains after cleanup")
	}
	if err := remaining.Err(); err != nil {
		t.Errorf("read Neo4j ownership cleanup check: %v", err)
	}
}
