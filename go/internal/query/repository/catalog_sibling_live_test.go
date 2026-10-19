// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Live positive proof for the #5385 catalog SAME_NAME sibling read
// (CatalogWorkloadSiblingCypher). The differential B-7 corpus holds no
// cross-repo same-name workload pair, so the sibling enrichment read is
// always empty there; this test seeds the pair it needs and asserts the
// exact production Cypher resolves both directions plus a negative
// control. It runs on both live backends under the same tag (the CI
// live-backend job points ESHU_NEO4J_URI at each backend in turn and pins
// ESHU_NEO4J_DATABASE to that backend's database, "nornic" or "neo4j").
//
// Seed uses literal property values only (see the pinned NornicDB
// literal-values note on repoAnswerTruthSeed). Ground truth:
//   - repo A defines workload sib-live-wl-a named sib-live-shared.
//   - repo B defines workload sib-live-wl-b named sib-live-shared.
//   - repo A defines workload sib-live-wl-solo named sib-live-solo.
//   - one SAME_NAME edge wl-a -> wl-b (the read pattern is undirected).
var siblingLiveSeed = []string{
	`CREATE (:Repository {id: 'sib-live-repo-a', name: 'sib-live-a'})`,
	`CREATE (:Repository {id: 'sib-live-repo-b', name: 'sib-live-b'})`,
	`CREATE (:Workload {id: 'sib-live-wl-a', name: 'sib-live-shared'})`,
	`CREATE (:Workload {id: 'sib-live-wl-b', name: 'sib-live-shared'})`,
	`CREATE (:Workload {id: 'sib-live-wl-solo', name: 'sib-live-solo'})`,
	`MATCH (r:Repository {id: 'sib-live-repo-a'}) MATCH (w:Workload {id: 'sib-live-wl-a'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (r:Repository {id: 'sib-live-repo-b'}) MATCH (w:Workload {id: 'sib-live-wl-b'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (r:Repository {id: 'sib-live-repo-a'}) MATCH (w:Workload {id: 'sib-live-wl-solo'}) CREATE (r)-[:DEFINES]->(w)`,
	`MATCH (a:Workload {id: 'sib-live-wl-a'}) MATCH (b:Workload {id: 'sib-live-wl-b'}) CREATE (a)-[:SAME_NAME]->(b)`,
}

const siblingLiveCleanup = `MATCH (n) WHERE n.id STARTS WITH 'sib-live-' DETACH DELETE n`

// siblingLiveReader is a file-local live reader that honors the
// per-backend database pin (several tests default the database to "nornic"
// when the env is empty, which does not exist on Neo4j). It implements
// querycontract.GraphQuery so the production handler enrichment path runs
// against the same seeded backend.
type siblingLiveReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (r siblingLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.database})
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

func (r siblingLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r siblingLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}

func TestLiveCatalogSameNameSiblings(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}

	reader := siblingLiveReader{driver: driver, database: database}
	reader.write(ctx, t, siblingLiveCleanup)
	for _, stmt := range siblingLiveSeed {
		reader.write(ctx, t, stmt)
	}
	defer reader.write(context.Background(), t, siblingLiveCleanup)

	ids := []string{"sib-live-wl-a", "sib-live-wl-b", "sib-live-wl-solo"}
	rows, err := reader.Run(ctx, CatalogWorkloadSiblingCypher, map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("sibling cypher: %v", err)
	}
	type siblingRow struct{ id, sibling, repo string }
	var got []siblingRow
	for _, row := range rows {
		id, _ := row["id"].(string)
		sib, _ := row["sibling_id"].(string)
		repo, _ := row["sibling_repo_id"].(string)
		got = append(got, siblingRow{id, sib, repo})
	}
	want := []siblingRow{
		{"sib-live-wl-a", "sib-live-wl-b", "sib-live-repo-b"},
		{"sib-live-wl-b", "sib-live-wl-a", "sib-live-repo-a"},
	}
	if len(got) != len(want) {
		t.Fatalf("sibling rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sibling rows = %v, want %v", got, want)
		}
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	enrichments, err := handler.catalogWorkloadEnrichments(ctx, ids)
	if err != nil {
		t.Fatalf("enrichments: %v", err)
	}
	if enrichments["sib-live-wl-a"].sameNameSiblings != 1 {
		t.Fatalf("wl-a sameNameSiblings = %d, want 1", enrichments["sib-live-wl-a"].sameNameSiblings)
	}
	solo, ok := enrichments["sib-live-wl-solo"]
	if !ok {
		t.Fatalf("no enrichment for the sibling-less workload: %v", enrichments)
	}
	if solo.sameNameSiblings != 0 {
		t.Fatalf("solo sameNameSiblings = %d, want 0", solo.sameNameSiblings)
	}
}
