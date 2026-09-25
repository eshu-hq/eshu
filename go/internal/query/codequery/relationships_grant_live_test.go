// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_code_relationships_grant

// Live two-tenant grant proof for POST /api/v0/code/relationships (#5167).
//
// Every case drives the real route through CodeHandler.Mount with a scoped
// caller granted exactly one of the two seeded repositories, on the backend
// named by ESHU_LIVE_GRAPH_BACKEND, and asserts on the decoded response. Each
// leak case also asserts that the granted neighbour IS present, so an empty
// answer cannot pass: the pinned NornicDB silently ignores a predicate it
// cannot parse and returns every row, and a predicate that matches nothing
// would look like a fix on an out-of-grant-only fixture.
//
//	docker run -d --name nornic-5167rel --platform linux/amd64 -p 127.0.0.1:17995:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_ASYNC_WRITES_ENABLED=false \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -e NORNICDB_HEIMDALL_ENABLED=false \
//	  -e NORNICDB_SEARCH_BM25_ENABLED=false -e NORNICDB_SEARCH_VECTOR_ENABLED=false \
//	  -e NORNICDB_QDRANT_GRPC_ENABLED=false -e NORNICDB_PERSIST_SEARCH_INDEXES=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17995 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/codequery -tags live_code_relationships_grant \
//	  -run TestLiveCodeRelationshipsGrant -count=1 -v
//
//	docker run -d --name neo4j-5167rel -p 127.0.0.1:17996:7687 -e NEO4J_AUTH=none \
//	  neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17996 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/codequery -tags live_code_relationships_grant \
//	  -run TestLiveCodeRelationshipsGrant -count=1 -v
package codequery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

type relLiveCase struct {
	name      string
	body      map[string]any
	field     string
	nameKey   string
	keep      []string
	leaks     []string
	leakMatch string // any name with this prefix is a leak
}

func relLiveBackend(t *testing.T) (GraphBackend, string) {
	t.Helper()
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))) {
	case "nornicdb":
		return GraphBackendNornicDB, "nornic"
	case "neo4j":
		return GraphBackendNeo4j, "neo4j"
	default:
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
		return "", ""
	}
}

func relLiveServe(t *testing.T, handler *CodeHandler, body map[string]any, auth *AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newCodeGrantRouteRequest(t, "/api/v0/code/relationships", body, auth))
	return rec
}

func relLiveData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
	}
	return envelope.Data
}

func relLiveNames(data map[string]any, field, key string) []string {
	rows, _ := data[field].([]any)
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok {
			if name, _ := m[key].(string); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func relLiveCases() []relLiveCase {
	cases := []relLiveCase{
		{
			name:  "one_hop_outgoing_calls",
			body:  map[string]any{"entity_id": liveClauseAnchorUID, "direction": "outgoing", "relationship_type": "CALLS"},
			field: "outgoing", nameKey: "target_name",
			keep:  []string{liveClauseGrantedCallee},
			leaks: []string{liveClauseUngrantedCallee, liveClauseOrphanCallee},
		},
		{
			name:  "one_hop_incoming_calls",
			body:  map[string]any{"entity_id": liveClauseAnchorUID, "direction": "incoming", "relationship_type": "CALLS"},
			field: "incoming", nameKey: "source_name",
			keep:  []string{liveClauseGrantedCaller},
			leaks: []string{liveClauseUngrantedCaller},
		},
		{
			name:  "one_hop_all_types_outgoing",
			body:  map[string]any{"entity_id": liveClauseAnchorUID},
			field: "outgoing", nameKey: "target_name",
			keep:  []string{liveClauseGrantedCallee},
			leaks: []string{liveClauseUngrantedCallee, liveClauseOrphanCallee},
		},
		{
			name:  "one_hop_all_types_incoming",
			body:  map[string]any{"entity_id": liveClauseAnchorUID},
			field: "incoming", nameKey: "source_name",
			keep:  []string{liveClauseGrantedCaller},
			leaks: []string{liveClauseUngrantedCaller},
		},
		{
			name:  "hub_grant_binds_before_limit",
			body:  map[string]any{"entity_id": relLiveHubUID, "direction": "outgoing", "relationship_type": "CALLS"},
			field: "outgoing", nameKey: "target_name",
			keep:      []string{relLiveHubGrantedName + "00", relLiveHubGrantedName + "39"},
			leakMatch: relLiveHubOtherName,
		},
		{
			name:  "transitive_outgoing_never_crosses_ungranted",
			body:  map[string]any{"entity_id": liveClauseChainStartUID, "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 5},
			field: "outgoing", nameKey: "target_name",
			leaks: []string{liveClauseChainBridge, liveClauseChainEnd},
		},
		{
			name:  "transitive_incoming_never_crosses_ungranted",
			body:  map[string]any{"entity_id": liveClauseChainEndUID, "direction": "incoming", "relationship_type": "CALLS", "transitive": true, "max_depth": 5},
			field: "incoming", nameKey: "source_name",
			leaks: []string{liveClauseChainBridge, liveClauseChainStart},
		},
		{
			name:  "transitive_outgoing_clean_chain_admitted",
			body:  map[string]any{"entity_id": liveClauseCleanStartUID, "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 5},
			field: "outgoing", nameKey: "target_name",
			keep: []string{liveClauseCleanMid, liveClauseCleanEnd},
		},
	}
	for _, relType := range relLiveOtherTypes {
		cases = append(cases,
			relLiveCase{
				name:  "one_hop_outgoing_" + relType,
				body:  map[string]any{"entity_id": relLiveTypesAnchorUID, "direction": "outgoing", "relationship_type": relType},
				field: "outgoing", nameKey: "target_name",
				keep:  []string{relLiveTypeNodeName(relLiveTypeGrantedName, relType, "out")},
				leaks: []string{relLiveTypeNodeName(relLiveTypeOtherName, relType, "out")},
			},
			relLiveCase{
				name:  "one_hop_incoming_" + relType,
				body:  map[string]any{"entity_id": relLiveTypesAnchorUID, "direction": "incoming", "relationship_type": relType},
				field: "incoming", nameKey: "source_name",
				keep:  []string{relLiveTypeNodeName(relLiveTypeGrantedName, relType, "in")},
				leaks: []string{relLiveTypeNodeName(relLiveTypeOtherName, relType, "in")},
			},
		)
	}
	return cases
}

// TestLiveCodeRelationshipsGrant runs every read path of the route as a scoped
// caller, then the same anchors as a shared-key caller to prove the unscoped
// answer did not change.
func TestLiveCodeRelationshipsGrant(t *testing.T) {
	backend, database := relLiveBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	driver := openLiveClauseDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedRelLiveGraph(ctx, t, driver, database)
	reader := newLiveNornicDBReader(driver, database)
	handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative, GraphBackend: backend}
	scoped := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})

	for _, tc := range relLiveCases() {
		t.Run("scoped/"+tc.name, func(t *testing.T) {
			data := relLiveData(t, relLiveServe(t, handler, tc.body, &scoped))
			names := relLiveNames(data, tc.field, tc.nameKey)
			leaked := 0
			for _, name := range names {
				if tc.leakMatch != "" && strings.HasPrefix(name, tc.leakMatch) {
					leaked++
				}
			}
			t.Logf("%s: %d %s rows, %d matching %q, truncated=%v", tc.name, len(names), tc.field, leaked, tc.leakMatch, data[tc.field+"_truncated"])
			for _, want := range tc.keep {
				if !liveClauseContainsName(names, want) {
					t.Errorf("granted neighbour %q missing from %s: %v", want, tc.field, relLiveHead(names))
				}
			}
			for _, bad := range tc.leaks {
				if liveClauseContainsName(names, bad) {
					t.Errorf("out-of-grant neighbour %q returned in %s: %v", bad, tc.field, relLiveHead(names))
				}
			}
			if leaked > 0 {
				t.Errorf("%d out-of-grant %q rows returned in %s", leaked, tc.leakMatch, tc.field)
			}
			if tc.name == "hub_grant_binds_before_limit" && len(names) != relLiveHubGranted {
				t.Errorf("hub returned %d rows, want exactly the %d granted neighbours", len(names), relLiveHubGranted)
			}
			// Far-end repository metadata comes from the enrichment reads,
			// which share the core read's row ceiling. On the hub an
			// enrichment read without the grant spends that ceiling on the
			// ungranted neighbours and leaves the granted rows unenriched.
			repoKey := strings.TrimSuffix(tc.nameKey, "_name") + "_repo_id"
			if !strings.HasPrefix(tc.name, "transitive") {
				rows, _ := data[tc.field].([]any)
				for _, row := range rows {
					m, _ := row.(map[string]any)
					if name, _ := m[tc.nameKey].(string); name == "" {
						continue // the all-types read also returns the File CONTAINS edge
					}
					if got, _ := m[repoKey].(string); got != codeGrantGrantedRepo {
						t.Errorf("%s row %v: %s = %q, want the granted repository", tc.name, m[tc.nameKey], repoKey, got)
					}
				}
			}
		})
	}

	t.Run("shared_key_unchanged", func(t *testing.T) {
		data := relLiveData(t, relLiveServe(t, handler, map[string]any{"entity_id": liveClauseAnchorUID}, nil))
		out := relLiveNames(data, "outgoing", "target_name")
		in := relLiveNames(data, "incoming", "source_name")
		for _, want := range []string{liveClauseGrantedCallee, liveClauseUngrantedCallee, liveClauseOrphanCallee} {
			if !liveClauseContainsName(out, want) {
				t.Errorf("shared-key outgoing lost %q: %v", want, out)
			}
		}
		for _, want := range []string{liveClauseGrantedCaller, liveClauseUngrantedCaller} {
			if !liveClauseContainsName(in, want) {
				t.Errorf("shared-key incoming lost %q: %v", want, in)
			}
		}
		chain := relLiveData(t, relLiveServe(t, handler, map[string]any{"entity_id": liveClauseChainStartUID, "direction": "outgoing", "relationship_type": "CALLS", "transitive": true, "max_depth": 5}, nil))
		reached := relLiveNames(chain, "outgoing", "target_name")
		for _, want := range []string{liveClauseChainBridge, liveClauseChainEnd} {
			if !liveClauseContainsName(reached, want) {
				t.Errorf("shared-key transitive lost %q: %v", want, reached)
			}
		}
	})

	// Name lookups without repo_id: the NornicDB metadata read and the Neo4j
	// `MATCH (e) WHERE e.name = $name` scan must both resolve among the granted
	// repositories only.
	t.Run("scoped/name_scan_resolves_granted_copy", func(t *testing.T) {
		data := relLiveData(t, relLiveServe(t, handler, map[string]any{"name": relLiveSharedName}, &scoped))
		if got, _ := data["entity_id"].(string); got != relLiveSharedGrantedUID {
			t.Fatalf("scoped shared-name lookup resolved %q, want %q", got, relLiveSharedGrantedUID)
		}
		if got, _ := data["repo_id"].(string); got != codeGrantGrantedRepo {
			t.Fatalf("scoped shared-name lookup repo_id = %q, want the granted repository", got)
		}
	})
	t.Run("scoped/name_scan_ungranted_only_is_not_found", func(t *testing.T) {
		ungranted := relLiveServe(t, handler, map[string]any{"name": liveClauseUngrantedCallee}, &scoped)
		unknown := relLiveServe(t, handler, map[string]any{"name": "RelLiveNoSuchSymbol"}, &scoped)
		if ungranted.Code != http.StatusNotFound || ungranted.Body.String() != unknown.Body.String() {
			t.Fatalf("ungranted name: status %d body %s; unknown: status %d body %s",
				ungranted.Code, ungranted.Body.String(), unknown.Code, unknown.Body.String())
		}
	})
	t.Run("shared_key/name_scan_stays_ambiguous", func(t *testing.T) {
		if rec := relLiveServe(t, handler, map[string]any{"name": relLiveSharedName}, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("unscoped shared-name lookup status = %d, want 404 (two matches); body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("scoped/ungranted_anchor_is_not_found", func(t *testing.T) {
		content := &relGrantContentStore{entities: []EntityContent{{
			EntityID: liveClauseUngrantedCalleeUID, EntityName: liveClauseUngrantedCallee, EntityType: "Function",
			RepoID: codeGrantOtherRepo, RelativePath: "internal/neighbor.go", Language: "go",
		}}}
		withContent := &CodeHandler{
			Neo4j: reader, Profile: ProfileLocalAuthoritative, GraphBackend: backend,
			Content: content, ContentRelationships: &relGrantContentBuilder{},
		}
		ungranted := relLiveServe(t, withContent, map[string]any{"entity_id": liveClauseUngrantedCalleeUID}, &scoped)
		unknown := relLiveServe(t, withContent, map[string]any{"entity_id": "fn:does-not-exist"}, &scoped)
		if ungranted.Code != http.StatusNotFound || ungranted.Body.String() != unknown.Body.String() {
			t.Fatalf("ungranted anchor: status %d body %s; unknown: status %d body %s",
				ungranted.Code, ungranted.Body.String(), unknown.Code, unknown.Body.String())
		}
	})
}

// TestLiveCodeRelationshipsGrantTiming measures the route end to end for the
// two anchors that bound the cost: an ordinary function (all types, both
// directions) and the 1240-neighbour hub. "before" is the unscoped request:
// for NornicDB and Neo4j alike its statements are byte-identical to the ones
// every caller -- scoped or not -- ran before #5167, because the grant text
// renders only for a scoped caller. "after" is the scoped request, which runs
// the grant-bound statements. Medians of relLiveTimingRuns warm runs.
func TestLiveCodeRelationshipsGrantTiming(t *testing.T) {
	backend, database := relLiveBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	driver := openLiveClauseDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedRelLiveGraph(ctx, t, driver, database)
	handler := &CodeHandler{Neo4j: newLiveNornicDBReader(driver, database), Profile: ProfileLocalAuthoritative, GraphBackend: backend}
	scoped := testutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	const relLiveTimingRuns = 25
	for _, anchor := range []struct {
		name string
		body map[string]any
	}{
		{"function_all_types", map[string]any{"entity_id": liveClauseAnchorUID}},
		{"hub_outgoing_calls", map[string]any{"entity_id": relLiveHubUID, "direction": "outgoing", "relationship_type": "CALLS"}},
	} {
		for _, mode := range []struct {
			name string
			auth *AuthContext
		}{{"before_unscoped", nil}, {"after_scoped", &scoped}} {
			samples := make([]time.Duration, 0, relLiveTimingRuns)
			relLiveData(t, relLiveServe(t, handler, anchor.body, mode.auth)) // warm
			for i := 0; i < relLiveTimingRuns; i++ {
				start := time.Now()
				relLiveData(t, relLiveServe(t, handler, anchor.body, mode.auth))
				samples = append(samples, time.Since(start))
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			t.Logf("TIMING backend=%s anchor=%s mode=%s runs=%d median=%s p90=%s",
				backend, anchor.name, mode.name, relLiveTimingRuns, samples[len(samples)/2], samples[len(samples)*9/10])
		}
	}
}

func relLiveHead(names []string) []string {
	if len(names) > 8 {
		return append(append([]string(nil), names[:8]...), "...")
	}
	return names
}
