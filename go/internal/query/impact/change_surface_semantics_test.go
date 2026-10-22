// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	testgraph "github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const (
	changeSurfaceConsumerRepoID   = "repository:orders"
	changeSurfaceDependencyRepoID = "repository:lib-common"
)

func TestChangeSurfaceImpactRowsUseConsumerDirectionForDependencies(t *testing.T) {
	t.Parallel()

	graph := semanticChangeSurfaceGraph()
	handler := &Handler{Neo4j: graph}
	target := ChangeSurfaceTargetCandidate{
		ID:     changeSurfaceDependencyRepoID,
		Name:   "lib-common",
		Labels: []string{"Repository"},
		RepoID: changeSurfaceDependencyRepoID,
	}

	rows, truncated, err := handler.ChangeSurfaceImpactRows(
		t.Context(),
		ChangeSurfaceInvestigationRequest{MaxDepth: 4, Limit: 10},
		target,
	)
	if err != nil {
		t.Fatalf("ChangeSurfaceImpactRows() error = %v", err)
	}
	if truncated {
		t.Fatal("ChangeSurfaceImpactRows() truncated = true, want false")
	}
	assertSemanticChangeSurfaceRows(t, rows)
	assertSemanticChangeSurfaceQueries(t, graph.runCalls)
}

func TestLegacyChangeSurfaceUsesConsumerDirectionAndKeepsProvenance(t *testing.T) {
	t.Parallel()

	graph := semanticChangeSurfaceGraph()
	handler := &Handler{Neo4j: graph}
	target := ChangeSurfaceTargetCandidate{
		ID:     changeSurfaceDependencyRepoID,
		Name:   "lib-common",
		Labels: []string{"Repository"},
		RepoID: changeSurfaceDependencyRepoID,
	}

	rows, truncated, err := handler.FindChangeSurfaceImpactRows(
		t.Context(), target, "", 4, 10, querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("FindChangeSurfaceImpactRows() error = %v", err)
	}
	if truncated {
		t.Fatal("FindChangeSurfaceImpactRows() truncated = true, want false")
	}
	assertSemanticChangeSurfaceRows(t, rows)
	if got := querycontract.StringVal(rows[0], "rel_type"); got != "DEFINES" {
		t.Fatalf("first row rel_type = %q, want DEFINES", got)
	}
	if got := querycontract.StringVal(rows[1], "rel_type"); got != "DEPENDS_ON" {
		t.Fatalf("second row rel_type = %q, want DEPENDS_ON", got)
	}
	assertSemanticChangeSurfaceQueries(t, graph.runCalls)
}

func TestPreChangeImpactDerivesRepositoryConsumerAnchorFromChangedFiles(t *testing.T) {
	t.Parallel()

	graph := semanticChangeSurfaceGraph()
	store := content.FakePortContentStore{Entities: []querycontract.EntityContent{{
		EntityID:     "content-entity:common",
		EntityName:   "Common",
		EntityType:   "Function",
		RepoID:       changeSurfaceDependencyRepoID,
		RelativePath: "common.go",
		Language:     "go",
		StartLine:    1,
		EndLine:      10,
	}}}
	handler := &Handler{
		Neo4j:   graph,
		Content: store,
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/pre-change",
		bytes.NewBufferString(`{"repo_id":"repository:lib-common","changed_paths":["common.go"],"limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	data := decodePreChangeEnvelope(t, rec).Data.(map[string]any)
	direct := data["direct_impact"].([]any)
	var foundConsumer bool
	for _, item := range direct {
		if item.(map[string]any)["id"] == changeSurfaceConsumerRepoID {
			foundConsumer = true
			break
		}
	}
	if !foundConsumer {
		t.Fatalf("direct impact does not contain repository consumer %q: %#v", changeSurfaceConsumerRepoID, direct)
	}
	resolution := data["target_resolution"].(map[string]any)
	if got, want := resolution["input"], changeSurfaceDependencyRepoID; got != want {
		t.Fatalf("target resolution input = %#v, want derived repository %q", got, want)
	}
}

func semanticChangeSurfaceGraph() *recordingChangeSurfaceGraph {
	return &recordingChangeSurfaceGraph{runFunc: func(cypher string, _ map[string]any) ([]map[string]any, error) {
		switch {
		case strings.Contains(cypher, "MATCH (n:Repository {id: $target})"):
			return []map[string]any{{
				"id": changeSurfaceDependencyRepoID, "name": "lib-common",
				"labels": []any{"Repository"}, "repo_id": changeSurfaceDependencyRepoID,
			}}, nil
		case strings.Contains(cypher, "<-[:DEPENDS_ON"):
			// The consumers branch reads one exact depth per statement;
			// only the depth-1 statement sees the consumer row.
			if depth, ok := changeSurfaceDepthRangeOfQuery(cypher); !ok || depth != 1 {
				return []map[string]any{}, nil
			}
			return []map[string]any{semanticChangeSurfaceRow(
				changeSurfaceConsumerRepoID, "orders-api", 1, "DEPENDS_ON",
			)}, nil
		case strings.Contains(cypher, "MATCH path"):
			// The outgoing branch reads one exact depth per statement;
			// serve only the rows at the requested depth, as the backend
			// does for an exact-depth range.
			depth, ok := changeSurfaceDepthRangeOfQuery(cypher)
			if !ok {
				return nil, nil
			}
			rows := []map[string]any{
				semanticChangeSurfaceRow("workload:lib-common", "lib-common-service", 1, "DEFINES"),
				semanticChangeSurfaceRow(changeSurfaceDependencyRepoID, "dependency", 1, "DEPENDS_ON"),
				semanticMixedChangeSurfaceRow("workload:mixed", "mixed", 2),
			}
			served := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				if querycontract.IntVal(row, "depth") == depth {
					served = append(served, row)
				}
			}
			return served, nil
		default:
			return nil, nil
		}
	}}
}

func semanticChangeSurfaceRow(id, name string, depth int64, relType string) map[string]any {
	repoID := changeSurfaceDependencyRepoID
	if strings.HasPrefix(id, "repository:") {
		repoID = id
	}
	return map[string]any{
		"id": id, "name": name, "labels": []any{"Repository"}, "repo_id": repoID, "depth": depth,
		"rels": []any{map[string]any{"type": relType, "properties": map[string]any{"reason": "fixture"}}},
	}
}

func semanticMixedChangeSurfaceRow(id, name string, depth int64) map[string]any {
	row := semanticChangeSurfaceRow(id, name, depth, "DEFINES")
	row["rels"] = append(row["rels"].([]any), map[string]any{
		"type": "DEPENDS_ON", "properties": map[string]any{"reason": "fixture"},
	})
	return row
}

func assertSemanticChangeSurfaceRows(t *testing.T, rows []map[string]any) {
	t.Helper()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("row count = %d, want %d: %#v", got, want, rows)
	}
	wantIDs := []string{"workload:lib-common", changeSurfaceConsumerRepoID}
	for index, want := range wantIDs {
		if got := querycontract.StringVal(rows[index], "id"); got != want {
			t.Fatalf("row %d id = %q, want %q; rows=%#v", index, got, want, rows)
		}
	}
}

func assertSemanticChangeSurfaceQueries(t *testing.T, calls []changeSurfaceRunCall) {
	t.Helper()
	// The target arrives resolved, so the calls are the outgoing branch at
	// depths 1..4 (depths 1 and 2 serve rows, 3 and 4 exhaust against the
	// limit+1 probe of 11) followed by the consumers branch at depths 1..4.
	if got, want := len(calls), 8; got != want {
		t.Fatalf("graph run calls = %d, want 4 outgoing and 4 dependency-consumer depth reads", got)
	}
	for i := range 4 {
		call := calls[i]
		if !strings.Contains(call.cypher, "relationships(path) as rels") {
			t.Fatalf("outgoing query %d must project raw relationships: %s", i, call.cypher)
		}
		if depth, ok := changeSurfaceDepthRangeOfQuery(call.cypher); !ok || depth != i+1 {
			t.Fatalf("outgoing query %d reads depth %d, want exact depth %d", i, depth, i+1)
		}
	}
	for i := range 4 {
		call := calls[4+i]
		if !strings.Contains(call.cypher, "(start:Repository") ||
			!strings.Contains(call.cypher, "<-[:DEPENDS_ON") {
			t.Fatalf("consumer query %d must use an incoming typed repository dependency traversal: %s", i, call.cypher)
		}
		if depth, ok := changeSurfaceDepthRangeOfQuery(call.cypher); !ok || depth != i+1 {
			t.Fatalf("consumer query %d reads depth %d, want exact depth %d", i, depth, i+1)
		}
	}
}

// TestChangeSurfaceOutgoingTraversalIsNamedForGraphReadTelemetry pins the
// graph_query_name the outgoing traversal logs under (#7246). That read is the
// expensive one: without a name its slow-read warning says "unnamed" and an
// operator needs the statement fingerprint to find the stage. The consumer
// read stays unnamed here; it is bounded and was not part of the finding.
func TestChangeSurfaceOutgoingTraversalIsNamedForGraphReadTelemetry(t *testing.T) {
	t.Parallel()

	for name, access := range map[string]querycontract.RepositoryAccessFilter{
		"unscoped": {AllScopes: true},
		"scoped": {
			AllowedRepositoryIDs: []string{"repository:owner"},
			Allowed:              map[string]struct{}{"repository:owner": {}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var outgoingName string
			handler := &Handler{Neo4j: testgraph.FakeGraphReader{RunFn: func(
				ctx context.Context, cypher string, _ map[string]any,
			) ([]map[string]any, error) {
				if strings.Contains(cypher, "RETURN impacted.id as id") || strings.Contains(cypher, "RETURN id, name") {
					outgoingName = querycontract.GraphQueryNameFromContext(ctx)
				}
				return nil, nil
			}}}
			_, _, err := handler.changeSurfaceTraversalRows(t.Context(),
				ChangeSurfaceTargetCandidate{ID: "workload:changed", Labels: []string{"Workload"}}, "", 4, 10, access)
			if err != nil {
				t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
			}
			if want := "platform_impact.change_surface.outgoing"; outgoingName != want {
				t.Fatalf("outgoing graph_query_name = %q, want %q", outgoingName, want)
			}
		})
	}
}

// TestChangeSurfaceRepositoryTargetNamesOnlyTheOutgoingTraversal pins the other
// half of the naming contract (#7246). A Repository target also runs the
// dependency-consumer read, and that read stays unnamed: it is bounded and was
// not part of the finding. The Workload-target test above never reaches the
// consumer branch, so a later change that names the consumer context would pass
// it. Split the two statements by direction: the consumer read is the only one
// that walks incoming typed DEPENDS_ON edges.
func TestChangeSurfaceRepositoryTargetNamesOnlyTheOutgoingTraversal(t *testing.T) {
	t.Parallel()

	var outgoingName, consumerName string
	var outgoingCalls, consumerCalls int
	handler := &Handler{Neo4j: testgraph.FakeGraphReader{RunFn: func(
		ctx context.Context, cypher string, _ map[string]any,
	) ([]map[string]any, error) {
		if strings.Contains(cypher, "<-[:DEPENDS_ON") {
			consumerCalls++
			consumerName = querycontract.GraphQueryNameFromContext(ctx)
			return nil, nil
		}
		outgoingCalls++
		outgoingName = querycontract.GraphQueryNameFromContext(ctx)
		return nil, nil
	}}}
	_, _, err := handler.changeSurfaceTraversalRows(t.Context(),
		ChangeSurfaceTargetCandidate{ID: "repository:changed", Labels: []string{"Repository"}},
		"", 4, 10, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("changeSurfaceTraversalRows() error = %v", err)
	}
	// The empty fake exhausts no depth, so each branch reads all four depths.
	if outgoingCalls != 4 || consumerCalls != 4 {
		t.Fatalf("graph reads = %d outgoing, %d consumer, want four exact-depth reads of each for a Repository target", outgoingCalls, consumerCalls)
	}
	if want := "platform_impact.change_surface.outgoing"; outgoingName != want {
		t.Errorf("outgoing graph_query_name = %q, want %q", outgoingName, want)
	}
	if consumerName != querycontract.DefaultGraphQueryName {
		t.Errorf("consumer graph_query_name = %q, want the unnamed default %q", consumerName, querycontract.DefaultGraphQueryName)
	}
}

// TestChangeSurfaceFilterTraversalRowsSuppressesLegacyAppSyncKeys pins the
// AppSync disclosure fix on the traversal path: suppression lives in Go,
// not in the traversal Cypher, because a labels() predicate sharing the
// repo-scope WHERE empties the whole result on the pinned NornicDB executor
// (differential nornicdb_vs_neo4j_quorum reproduced nornicdb=0 rows against
// neo4j=1 row on changeSurfaceScopedOutgoingCypher). Labels plus either type
// field must agree; a bare type value on another label is not a key, and a
// row without the projected type keys (the legacy shape) stays admitted.
func TestChangeSurfaceFilterTraversalRowsSuppressesLegacyAppSyncKeys(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	outgoing := []any{map[string]any{"type": "CALLS"}}
	row := func(id string, extra map[string]any) map[string]any {
		r := map[string]any{"id": id, "labels": []string{"CloudResource"}, "rels": outgoing}
		for key, value := range extra {
			r[key] = value
		}
		return r
	}
	rows := []map[string]any{
		row("key-by-resource-type", map[string]any{"resource_type": "aws_appsync_api_key"}),
		row("key-by-data-type", map[string]any{"data_type": "aws_appsync_api_key"}),
		row("plain-resource", map[string]any{"resource_type": "aws_s3_bucket"}),
		{
			"id": "key-value-wrong-label", "labels": []string{"Workload"},
			"resource_type": "aws_appsync_api_key", "rels": outgoing,
		},
		{
			"id": "legacy-shape-no-type-keys", "labels": []string{"Workload"},
			"rels": outgoing,
		},
	}

	got := changeSurfaceFilterTraversalRows(rows, "", access, false)

	kept := map[string]bool{}
	for _, r := range got {
		kept[querycontract.StringVal(r, "id")] = true
	}
	for _, id := range []string{"plain-resource", "key-value-wrong-label", "legacy-shape-no-type-keys"} {
		if !kept[id] {
			t.Errorf("row %q was dropped, want it kept", id)
		}
	}
	for _, id := range []string{"key-by-resource-type", "key-by-data-type"} {
		if kept[id] {
			t.Errorf("row %q was kept, want the legacy AppSync key suppressed", id)
		}
	}
}

// TestChangeSurfaceScopedOutgoingCypherSuppressesKeysInGo pins the other
// half of the disclosure fix: the scoped traversal carries no key-exclusion
// predicate (it zeroed NornicDB) and projects the type fields the Go filter
// above needs.
func TestChangeSurfaceScopedOutgoingCypherSuppressesKeysInGo(t *testing.T) {
	t.Parallel()

	if strings.Contains(changeSurfaceScopedOutgoingCypher, "aws_appsync_api_key") {
		t.Error("scoped outgoing cypher carries a legacy-key exclusion predicate: " +
			"suppression lives in changeSurfaceFilterTraversalRows, where both backends agree")
	}
	for _, col := range []string{
		"impacted.resource_type as resource_type",
		"impacted.data_type as data_type",
	} {
		if !strings.Contains(changeSurfaceScopedOutgoingCypher, col) {
			t.Errorf("scoped outgoing cypher does not project %q, which the Go key filter reads", col)
		}
	}
}
