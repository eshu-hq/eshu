// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for issue #7212 PR-B on a real Neo4j with Eshu's schema applied:
// the schema-closed entity-context anchor, sent alone with no unlabeled
// fallback, answers exactly what the id-anchor census definition
// (anchor.Classify) says it can reach, on every label the census knows.
//
// The seed holds one canonical node per uid- or id-constrained label (uid labels
// carry id == uid, id labels carry id), which is the shape the writers produce,
// plus the three shapes the census counts as unreachable: a Directory with an
// id, an id-only node on a uid label, and a node whose id differs from its uid.
// Each node is read back from Neo4j and classified. A reachable node must
// answer, in one statement, the same body the per-label loop oracle returns.
// An unreachable node must still resolve through the loop's unlabeled fallback
// (so the shape is real) and answer the not-found shape on Neo4j, in one
// statement. On a deployment, the census gauge
// eshu_dp_graph_id_anchor_unreachable_nodes reading 0 is then the proof that
// removing the fallback lost no answer there.
//
// This proves Neo4j only and skips on NornicDB, which keeps the per-label loop
// and its fallback (#7006). Run it with, for example:
//
//	docker run -d --name 7212-neo4j -e NEO4J_AUTH=none \
//	  -p 127.0.0.1:<bolt-port>:7687 neo4j:2026-community
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<bolt-port> \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/entity -tags live_nornicdb_answer_truth \
//	  -run TestLiveNeo4jSchemaClosedAnchorAgreesWithTheCensus -count=1 -v
package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	censusSeedPrefix       = "census-7212:"
	censusSeedCleanupByID  = `MATCH (n) WHERE n.id STARTS WITH 'census-7212:' DETACH DELETE n`
	censusSeedCleanupByUID = `MATCH (n) WHERE n.uid STARTS WITH 'census-7212:' DETACH DELETE n`
	censusSeedReadBack     = `MATCH (n) WHERE n.id STARTS WITH 'census-7212:' RETURN labels(n) AS labels, n.id AS id, n.uid AS uid ORDER BY n.id`
)

// censusSeed returns one CREATE per seeded node: a canonical node for every
// census label, then the three census-unreachable shapes. Labels come from the
// schema tables (anchor.UIDLabels, anchor.IDLabels), never from the anchor
// text under test.
func censusSeed() []string {
	var stmts []string
	for _, label := range anchor.UIDLabels() {
		id := censusSeedPrefix + label
		stmts = append(stmts, `CREATE (:`+label+` {id: '`+id+`', uid: '`+id+`', name: '`+label+`'})`)
	}
	for _, label := range anchor.IDLabels() {
		id := censusSeedPrefix + label
		stmts = append(stmts, `CREATE (:`+label+` {id: '`+id+`', name: '`+label+`'})`)
	}
	return append(stmts,
		`CREATE (:Directory {id: '`+censusSeedPrefix+`unreachable-dir', path: '`+censusSeedPrefix+`unreachable-dir'})`,
		`CREATE (:Function {id: '`+censusSeedPrefix+`unreachable-id-only', name: 'IDOnly'})`,
		`CREATE (:TerraformVariable {id: '`+censusSeedPrefix+`unreachable-split', uid: '`+censusSeedPrefix+`unreachable-split-uid', name: 'Split'})`,
	)
}

func TestLiveNeo4jSchemaClosedAnchorAgreesWithTheCensus(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend, database := liveGraphBackend()
	if backend != graph.SchemaBackendNeo4j {
		t.Skip("the schema-closed anchor is Neo4j-only; NornicDB keeps the per-label loop and its fallback (#7006)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	if err := graph.EnsureSchemaWithBackendStrict(ctx, liveSchemaExecutor{driver: driver, database: database}, nil, backend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	base := entityLiveReader{driver: driver, database: database}
	cleanup := func() {
		base.write(context.Background(), t, censusSeedCleanupByID)
		base.write(context.Background(), t, censusSeedCleanupByUID)
	}
	cleanup()
	for _, stmt := range censusSeed() {
		base.write(ctx, t, stmt)
	}
	defer cleanup()

	rows, err := base.Run(ctx, censusSeedReadBack, nil)
	if err != nil {
		t.Fatalf("read back the seed: %v", err)
	}
	if want := len(anchor.Labels()) + 3; len(rows) != want {
		t.Fatalf("seeded nodes read back = %d, want %d", len(rows), want)
	}

	uidSet, idSet := map[string]bool{}, map[string]bool{}
	for _, label := range anchor.UIDLabels() {
		uidSet[label] = true
	}
	for _, label := range anchor.IDLabels() {
		idSet[label] = true
	}

	loopReader := &statementRecorder{entityLiveReader: base}
	anchorReader := &statementRecorder{entityLiveReader: base}
	loopHandler := &Handler{Neo4j: loopReader, Profile: querycontract.ProfileLocalAuthoritative}
	anchorHandler := &Handler{GraphBackend: querycontract.GraphBackendNeo4j, Neo4j: anchorReader, Profile: querycontract.ProfileLocalAuthoritative}

	reachableLabels := map[string]bool{}
	unreachable := 0
	for _, row := range rows {
		node := anchor.Node{Labels: querycontract.StringSliceVal(row, "labels")}
		id := querycontract.StringVal(row, "id")
		node.ID = &id
		if uid, ok := row["uid"].(string); ok {
			node.UID = &uid
		}
		reachable := anchor.Classify(node, uidSet, idSet) != anchor.Unreachable

		loopReader.take()
		anchorReader.take()
		loopRec := getEntityContext(loopHandler, id, auth.AuthContext{})
		anchorRec := getEntityContext(anchorHandler, id, auth.AuthContext{})
		if reads := anchorReader.take(); reads != 1 {
			t.Errorf("%v %s: anchor handler sent %d statements, want 1", node.Labels, id, reads)
		}
		if loopRec.Code != http.StatusOK {
			t.Errorf("%v %s: loop oracle status = %d, want 200 (every seeded node carries an id)", node.Labels, id, loopRec.Code)
			continue
		}
		if !reachable {
			unreachable++
			if anchorRec.Code != http.StatusNotFound {
				t.Errorf("%v %s: census says unreachable, Neo4j answered %d, want 404", node.Labels, id, anchorRec.Code)
			}
			continue
		}
		if anchorRec.Code != http.StatusOK {
			t.Errorf("%v %s: census says reachable, Neo4j answered %d: %s", node.Labels, id, anchorRec.Code, anchorRec.Body.String())
			continue
		}
		if got, want := normalizedAnchorBody(t, anchorRec), normalizedAnchorBody(t, loopRec); !reflect.DeepEqual(got, want) {
			t.Errorf("%v %s: anchor answer differs from the loop\nanchor = %v\nloop   = %v", node.Labels, id, got, want)
		}
		var body struct {
			Labels []string `json:"labels"`
			Data   struct {
				Labels []string `json:"labels"`
			} `json:"data"`
		}
		if err := json.Unmarshal(anchorRec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		gotLabels := body.Labels
		if len(gotLabels) == 0 {
			gotLabels = body.Data.Labels
		}
		if !slices.Equal(gotLabels, node.Labels) {
			t.Errorf("%s: resolved labels = %v, want %v", id, gotLabels, node.Labels)
		}
		for _, label := range node.Labels {
			reachableLabels[label] = true
		}
	}
	if unreachable != 3 {
		t.Errorf("census-unreachable nodes = %d, want the 3 seeded shapes", unreachable)
	}
	for label := range anchor.Labels() {
		if !reachableLabels[label] {
			t.Errorf("census label %s did not resolve on the schema-closed anchor", label)
		}
	}
}
