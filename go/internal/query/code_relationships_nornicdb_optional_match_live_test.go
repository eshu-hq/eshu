// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_relationships_proof

// Live regression proof for the NornicDB OPTIONAL-MATCH function-projection
// corruption (#5681). The fake-graph-reader unit tests cannot catch this: the
// defect lives in the pinned NornicDB Cypher executor, which only the real Bolt
// path exercises. This test seeds a Class inheritance graph, drives the actual
// relationship route entry point against a live NornicDB, and asserts the
// computed relationship type and enrichment survive the handler's exact-string
// type filter — which they did not before the split (every edge was dropped
// because type(rel) came back as the literal string "type(rel)").
//
// Run against the pinned image (see docs/public/run-locally/docker-compose.yaml):
//
//	docker run -d --name nornic-rel-proof -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17687:7687 eshu-nornicdb-pr261:149245885258
//	cd go && go test ./internal/query -tags live_nornicdb_relationships_proof \
//	  -run TestLiveNornicDBRelationshipsSurviveOptionalMatchProjection -count=1 -v
package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestLiveNornicDBRelationshipsSurviveOptionalMatchProjection(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17687"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}

	seedLiveNornicDBInheritanceGraph(ctx, t, driver)

	handler := &CodeHandler{
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        NewNeo4jReader(driver, "nornic"),
	}

	row, err := handler.nornicDBRelationshipsGraphRow(ctx, "cls:ServiceDog", "", "", "outgoing", "INHERITS")
	if err != nil {
		t.Fatalf("nornicDBRelationshipsGraphRow() error = %v", err)
	}
	if row == nil {
		t.Fatal("nornicDBRelationshipsGraphRow() = nil, want metadata row for ServiceDog")
	}

	// The handler applies filterRelationshipResponse to the graph row; before
	// the split this dropped every edge because the corrupt type column never
	// equalled "INHERITS".
	filtered := filterRelationshipResponse(row, "outgoing", "INHERITS")
	outgoing := mapRelationships(filtered["outgoing"])
	if len(outgoing) != 3 {
		t.Fatalf("outgoing INHERITS edges = %d, want 3 (Dog, LogMixin, SerializeMixin); rows=%#v", len(outgoing), outgoing)
	}

	byTarget := make(map[string]map[string]any, len(outgoing))
	for _, rel := range outgoing {
		if got := StringVal(rel, "type"); got != "INHERITS" {
			t.Fatalf("relationship type = %q, want evaluated \"INHERITS\" (literal \"type(rel)\" means the projection is still corrupt)", got)
		}
		byTarget[StringVal(rel, "target_name")] = rel
	}
	for _, name := range []string{"Dog", "LogMixin", "SerializeMixin"} {
		if _, ok := byTarget[name]; !ok {
			t.Fatalf("missing INHERITS edge to %q; got targets %v", name, targetNames(outgoing))
		}
	}

	// Enrichment: Dog is contained by a File in a Repository, so its file/repo
	// metadata must be present and evaluated (not literal expression text).
	dog := byTarget["Dog"]
	if got := StringVal(dog, "target_id"); got != "cls:Dog" {
		t.Fatalf("Dog target_id = %q, want evaluated \"cls:Dog\"", got)
	}
	if got := StringVal(dog, "target_repo_id"); got != "repo:1" {
		t.Fatalf("Dog target_repo_id = %q, want enriched \"repo:1\"", got)
	}
	if got := StringVal(dog, "target_file_path"); got != "svc.py" {
		t.Fatalf("Dog target_file_path = %q, want enriched \"svc.py\"", got)
	}
	if got := StringVal(dog, "target_type"); got != "Class" {
		t.Fatalf("Dog target_type = %q, want evaluated \"Class\"", got)
	}

	// SerializeMixin has no File; enrichment must be absent, not fabricated,
	// and its identity/type must still evaluate.
	serialize := byTarget["SerializeMixin"]
	if got := StringVal(serialize, "target_id"); got != "cls:SerializeMixin" {
		t.Fatalf("SerializeMixin target_id = %q, want \"cls:SerializeMixin\"", got)
	}
	if _, ok := serialize["target_file_path"]; ok {
		t.Fatalf("SerializeMixin target_file_path = %#v, want absent (no File)", serialize["target_file_path"])
	}
	if _, ok := serialize["target_repo_id"]; ok {
		t.Fatalf("SerializeMixin target_repo_id = %#v, want absent (no File/Repository)", serialize["target_repo_id"])
	}
}

func targetNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, StringVal(row, "target_name"))
	}
	return names
}

func seedLiveNornicDBInheritanceGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	// MERGE-based so repeated runs against a retained store stay idempotent.
	statements := []string{
		`MERGE (c:Class {uid:"cls:ServiceDog"}) SET c.id="cls:ServiceDog", c.name="ServiceDog", c.language="python", c.start_line=10, c.end_line=40`,
		`MERGE (d:Class {uid:"cls:Dog"}) SET d.id="cls:Dog", d.name="Dog", d.language="python", d.start_line=1, d.end_line=5`,
		`MERGE (m:Class {uid:"cls:LogMixin"}) SET m.name="LogMixin", m.language="python", m.start_line=6, m.end_line=9`,
		`MERGE (s:Class {uid:"cls:SerializeMixin"}) SET s.name="SerializeMixin"`,
		`MERGE (f:File {uid:"file:svc"}) SET f.relative_path="svc.py", f.language="python"`,
		`MERGE (r:Repository {id:"repo:1"}) SET r.name="python_comprehensive"`,
		`MATCH (c:Class {uid:"cls:ServiceDog"}), (d:Class {uid:"cls:Dog"}) MERGE (c)-[:INHERITS]->(d)`,
		`MATCH (c:Class {uid:"cls:ServiceDog"}), (m:Class {uid:"cls:LogMixin"}) MERGE (c)-[:INHERITS]->(m)`,
		`MATCH (c:Class {uid:"cls:ServiceDog"}), (s:Class {uid:"cls:SerializeMixin"}) MERGE (c)-[:INHERITS]->(s)`,
		`MATCH (f:File {uid:"file:svc"}), (c:Class {uid:"cls:ServiceDog"}) MERGE (f)-[:CONTAINS]->(c)`,
		`MATCH (f:File {uid:"file:svc"}), (d:Class {uid:"cls:Dog"}) MERGE (f)-[:CONTAINS]->(d)`,
		`MATCH (f:File {uid:"file:svc"}), (m:Class {uid:"cls:LogMixin"}) MERGE (f)-[:CONTAINS]->(m)`,
		`MATCH (r:Repository {id:"repo:1"}), (f:File {uid:"file:svc"}) MERGE (r)-[:REPO_CONTAINS]->(f)`,
	}
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}
