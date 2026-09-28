// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for issue #7380 on a real Neo4j with Eshu's schema applied: the
// single CALL () entity-context anchor returns the same answer as the
// 16-statement per-label loop it replaces, on every scenario the loop was
// specified by, while sending at most two statements. The loop is the oracle:
// the same seeded graph is read through a NornicDB-dialect handler (the loop,
// which is valid Cypher on Neo4j) and a Neo4j-dialect handler (the anchor), and
// the two responses must be identical. The plan is also checked: the anchor
// seeks an index for every label and never scans.
//
// This proves Neo4j only. The anchor is not used on NornicDB (#7006), so the
// test skips on that backend. Run it with, for example:
//
//	docker run -d --name 7380-neo4j -e NEO4J_AUTH=none \
//	  -p 127.0.0.1:<bolt-port>:7687 neo4j:2026-community
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<bolt-port> \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/entity -tags live_nornicdb_answer_truth \
//	  -run TestLiveNeo4jEntityContextAnchorMatchesLoop -count=1 -v
package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	anchorSeedPrefix        = "anchor-7380:"
	anchorSeedCleanupByID   = `MATCH (n) WHERE n.id STARTS WITH 'anchor-7380:' DETACH DELETE n`
	anchorSeedCleanupByUID  = `MATCH (n) WHERE n.uid STARTS WITH 'anchor-7380:' DETACH DELETE n`
	anchorSeedRepoID        = anchorSeedPrefix + "repo"
	anchorSeedOtherRepoID   = anchorSeedPrefix + "repo-other"
	anchorSeedFileUID       = anchorSeedPrefix + "file"
	anchorSeedOtherFileUID  = anchorSeedPrefix + "file-other"
	anchorSeedFileUIDOnly   = anchorSeedPrefix + "file-uid-only"
	anchorSeedDupID         = anchorSeedPrefix + "dup"
	anchorSeedDupLateFirst  = anchorSeedPrefix + "dup-late-first"
	anchorSeedOtherRepoFn   = anchorSeedPrefix + "fn-other-repo"
	anchorReadStatementMark = "OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)"
)

// anchorSeed builds one graph covering the scenarios of the theory shim. Every
// entity that the loop resolves through a labeled read carries id == uid (the
// canonical shape); the fallback cases carry no uid or a label outside the
// anchor set; the two dup ids are shared by two nodes on different labels.
var anchorSeed = []string{
	`CREATE (:Repository {id: '` + anchorSeedRepoID + `', name: 'anchor-repo'})`,
	`CREATE (:Repository {id: '` + anchorSeedOtherRepoID + `', name: 'anchor-repo-other'})`,
	`CREATE (:File {uid: '` + anchorSeedFileUID + `', relative_path: 'a.go', language: 'go'})`,
	`CREATE (:File {uid: '` + anchorSeedOtherFileUID + `', relative_path: 'b.go', language: 'go'})`,
	`CREATE (:File {uid: '` + anchorSeedFileUIDOnly + `', relative_path: 'c.go', language: 'go'})`,
	`CREATE (:Function {id: '` + anchorSeedPrefix + `fn0', uid: '` + anchorSeedPrefix + `fn0', name: 'Fn0', language: 'go'})`,
	`CREATE (:Function {id: '` + anchorSeedPrefix + `fn1', uid: '` + anchorSeedPrefix + `fn1', name: 'Fn1', language: 'go'})`,
	`CREATE (:Function {id: '` + anchorSeedOtherRepoFn + `', uid: '` + anchorSeedOtherRepoFn + `', name: 'FnOther', language: 'go'})`,
	`CREATE (:Class {id: '` + anchorSeedPrefix + `class0', uid: '` + anchorSeedPrefix + `class0', name: 'Class0', language: 'go'})`,
	`CREATE (:Enum {id: '` + anchorSeedPrefix + `enum0', uid: '` + anchorSeedPrefix + `enum0', name: 'Enum0', language: 'go'})`,
	`CREATE (:Workload {id: '` + anchorSeedPrefix + `workload0', name: 'workload0', repo_id: '` + anchorSeedRepoID + `'})`,
	`CREATE (:WorkloadInstance {id: '` + anchorSeedPrefix + `wi0', name: 'wi0', repo_id: '` + anchorSeedRepoID + `'})`,
	`CREATE (:Directory {id: '` + anchorSeedPrefix + `dir0', path: '` + anchorSeedPrefix + `dir0'})`,
	`CREATE (:Function {id: '` + anchorSeedPrefix + `fn-id-only', name: 'IDOnly', language: 'go'})`,
	`CREATE (:Trait {id: '` + anchorSeedPrefix + `trait0', uid: '` + anchorSeedPrefix + `trait0', name: 'Trait0'})`,
	// dup: one id on Function (rank 0) and Class (rank 1); the loop resolves the Function.
	`CREATE (:Class {id: '` + anchorSeedDupID + `', uid: '` + anchorSeedDupID + `', name: 'DupClass'})`,
	`CREATE (:Function {id: '` + anchorSeedDupID + `', uid: '` + anchorSeedDupID + `', name: 'DupFunction'})`,
	// dup-late-first: the later-ranked WorkloadInstance is created before the
	// earlier-ranked Enum, so creation order cannot explain a passing result.
	`CREATE (:WorkloadInstance {id: '` + anchorSeedDupLateFirst + `', name: 'DupInstance'})`,
	`CREATE (:Enum {id: '` + anchorSeedDupLateFirst + `', uid: '` + anchorSeedDupLateFirst + `', name: 'DupEnum'})`,
	`MATCH (r:Repository {id: '` + anchorSeedRepoID + `'}) MATCH (f:File {uid: '` + anchorSeedFileUID + `'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (r:Repository {id: '` + anchorSeedOtherRepoID + `'}) MATCH (f:File {uid: '` + anchorSeedOtherFileUID + `'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (f:File {uid: '` + anchorSeedFileUID + `'}) MATCH (e:Function {uid: '` + anchorSeedPrefix + `fn0'}) CREATE (f)-[:CONTAINS]->(e)`,
	`MATCH (f:File {uid: '` + anchorSeedFileUID + `'}) MATCH (e:Enum {uid: '` + anchorSeedPrefix + `enum0'}) CREATE (f)-[:CONTAINS]->(e)`,
	`MATCH (f:File {uid: '` + anchorSeedOtherFileUID + `'}) MATCH (e:Function {uid: '` + anchorSeedOtherRepoFn + `'}) CREATE (f)-[:CONTAINS]->(e)`,
	`MATCH (a:Function {uid: '` + anchorSeedPrefix + `fn0'}) MATCH (b:Function {uid: '` + anchorSeedPrefix + `fn1'}) CREATE (a)-[:CALLS]->(b)`,
}

// anchorScenario is one shim truth-table row: the entity id, whether the
// request resolves, and the first label of the row it must resolve to.
type anchorScenario struct {
	name       string
	id         string
	wantStatus int
	wantLabel  string
	// wantStatements is the number of statements the Neo4j-dialect handler
	// may send: 1 when the CALL () anchor resolves the row, 2 when only the
	// unlabeled fallback does or nothing does.
	wantStatements int
}

var anchorScenarios = []anchorScenario{
	{"Function label position 1", anchorSeedPrefix + "fn0", http.StatusOK, "Function", 1},
	{"Class label position 2", anchorSeedPrefix + "class0", http.StatusOK, "Class", 1},
	{"Enum label position 10", anchorSeedPrefix + "enum0", http.StatusOK, "Enum", 1},
	{"Workload id-keyed", anchorSeedPrefix + "workload0", http.StatusOK, "Workload", 1},
	{"WorkloadInstance label position 15", anchorSeedPrefix + "wi0", http.StatusOK, "WorkloadInstance", 1},
	{"Repository id-keyed", anchorSeedRepoID, http.StatusOK, "Repository", 1},
	{"Directory with an id resolves through the fallback", anchorSeedPrefix + "dir0", http.StatusOK, "Directory", 2},
	{"id-only Function resolves through the fallback", anchorSeedPrefix + "fn-id-only", http.StatusOK, "Function", 2},
	{"off-list Trait resolves through the fallback", anchorSeedPrefix + "trait0", http.StatusOK, "Trait", 2},
	{"File uid-only must not match", anchorSeedFileUIDOnly, http.StatusNotFound, "", 2},
	{"File uid must not match", anchorSeedFileUID, http.StatusNotFound, "", 2},
	{"not found", anchorSeedPrefix + "nope", http.StatusNotFound, "", 2},
	{"id shared by Function and Class resolves to the Function", anchorSeedDupID, http.StatusOK, "Function", 1},
	{"id shared by WorkloadInstance and Enum resolves to the Enum", anchorSeedDupLateFirst, http.StatusOK, "Enum", 1},
}

// statementRecorder counts the anchor-statement reads GetEntityContext sends
// through the live reader.
type statementRecorder struct {
	entityLiveReader
	mu    sync.Mutex
	count int
}

func (r *statementRecorder) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if strings.Contains(cypher, anchorReadStatementMark) {
		r.mu.Lock()
		r.count++
		r.mu.Unlock()
	}
	return r.entityLiveReader.RunSingle(ctx, cypher, params)
}

func (r *statementRecorder) take() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.count
	r.count = 0
	return n
}

func TestLiveNeo4jEntityContextAnchorMatchesLoop(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend, database := liveGraphBackend()
	if backend != graph.SchemaBackendNeo4j {
		t.Skip("the single CALL () anchor is Neo4j-only; NornicDB keeps the per-label loop (#7006)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
		base.write(context.Background(), t, anchorSeedCleanupByID)
		base.write(context.Background(), t, anchorSeedCleanupByUID)
	}
	cleanup()
	for _, stmt := range anchorSeed {
		base.write(ctx, t, stmt)
	}
	defer cleanup()

	loopReader := &statementRecorder{entityLiveReader: base}
	anchorReader := &statementRecorder{entityLiveReader: base}
	loopHandler := &Handler{Neo4j: loopReader, Profile: querycontract.ProfileLocalAuthoritative}
	anchorHandler := &Handler{GraphBackend: querycontract.GraphBackendNeo4j, Neo4j: anchorReader, Profile: querycontract.ProfileLocalAuthoritative}

	get := func(h *Handler, id string, grant []string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+id+"/context", nil)
		if grant != nil {
			req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
				Mode:                 auth.AuthModeScoped,
				AllowedRepositoryIDs: grant,
			}))
		}
		req.SetPathValue("entity_id", id)
		rec := httptest.NewRecorder()
		h.GetEntityContext(rec, req)
		return rec
	}
	compare := func(t *testing.T, id string, grant []string) (loopRec, anchorRec *httptest.ResponseRecorder) {
		t.Helper()
		loopRec = get(loopHandler, id, grant)
		anchorRec = get(anchorHandler, id, grant)
		if loopRec.Code != anchorRec.Code {
			t.Fatalf("status: loop = %d, anchor = %d; loop body = %s; anchor body = %s",
				loopRec.Code, anchorRec.Code, loopRec.Body.String(), anchorRec.Body.String())
		}
		if got, want := normalizedAnchorBody(t, anchorRec), normalizedAnchorBody(t, loopRec); !reflect.DeepEqual(got, want) {
			t.Fatalf("anchor response differs from the loop response\nanchor = %v\nloop   = %v", got, want)
		}
		return loopRec, anchorRec
	}

	for _, scenario := range anchorScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			loopReader.take()
			anchorReader.take()
			_, anchorRec := compare(t, scenario.id, nil)
			anchorReads := anchorReader.take()
			if anchorRec.Code != scenario.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", anchorRec.Code, scenario.wantStatus, anchorRec.Body.String())
			}
			if scenario.wantLabel != "" {
				var body struct {
					Labels []string `json:"labels"`
				}
				if err := json.Unmarshal(anchorRec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(body.Labels) == 0 || body.Labels[0] != scenario.wantLabel {
					t.Fatalf("resolved labels = %v, want first label %q", body.Labels, scenario.wantLabel)
				}
			}
			if anchorReads != scenario.wantStatements {
				t.Fatalf("anchor handler sent %d statements, want %d", anchorReads, scenario.wantStatements)
			}
			if anchorReads > 2 {
				t.Fatalf("anchor handler sent %d statements, the contract is at most 2", anchorReads)
			}
		})
	}

	// The scoped caller shape has its own statement text; its answer must also
	// match the loop, in grant (resolves) and out of grant (404).
	t.Run("scoped in grant", func(t *testing.T) {
		_, rec := compare(t, anchorSeedPrefix+"fn0", []string{anchorSeedRepoID})
		if rec.Code != http.StatusOK {
			t.Fatalf("in-grant status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("scoped out of grant", func(t *testing.T) {
		_, rec := compare(t, anchorSeedPrefix+"fn0", []string{anchorSeedOtherRepoID})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("out-of-grant status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("plan seeks every label", func(t *testing.T) {
		scopedCtx := auth.ContextWithAuthContext(ctx, auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			AllowedRepositoryIDs: []string{anchorSeedRepoID},
		})
		for name, access := range map[string]querycontract.RepositoryAccessFilter{
			"unscoped": {AllScopes: true},
			"scoped":   querycontract.RepositoryAccessFilterFromContext(scopedCtx),
		} {
			params := access.GraphParams(map[string]any{"entity_id": anchorSeedPrefix + "nope"})
			ops := explainOperators(ctx, t, driver, database, entityContextStatement(neo4jEntityContextAnchor(), access), params)
			if !strings.Contains(ops, "NodeUniqueIndexSeek") {
				t.Errorf("%s anchor plan has no NodeUniqueIndexSeek:\n%s", name, ops)
			}
			for _, scan := range []string{"NodeByLabelScan", "AllNodesScan", "UnionNodeByLabelsScan"} {
				if strings.Contains(ops, scan) {
					t.Errorf("%s anchor plan contains %s:\n%s", name, scan, ops)
				}
			}
		}
	})
}

// normalizedAnchorBody decodes a response into a comparable value: the
// relationships list, whose order is not part of the contract, is sorted.
func normalizedAnchorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	// The success envelope wraps the payload under data; the error envelope
	// has no data, so compare the whole body then.
	if data, ok := body["data"].(map[string]any); ok {
		body = data
	}
	if rels, ok := body["relationships"].([]any); ok {
		keys := make([]string, len(rels))
		for i, rel := range rels {
			raw, _ := json.Marshal(rel)
			keys[i] = string(raw)
		}
		sort.Strings(keys)
		body["relationships"] = keys
	}
	return body
}
