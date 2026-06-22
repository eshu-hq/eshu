package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTraceResourceToCodeAnchorsLabeledStart proves the resource-to-code start
// anchor is label-seeded (issue #3567). On the Neo4j-compat path an unlabeled
// `MATCH (start) WHERE start.id = $start_id` cannot use a label/index seek, so
// the planner scans every node in the graph. The fix anchors the start MATCH on
// the bounded impact-anchor label disjunction while keeping the exact id
// predicate, projection, ordering, and LIMIT, so results are unchanged but the
// scan is bounded to the labeled, id-indexed populations.
func TestTraceResourceToCodeAnchorsLabeledStart(t *testing.T) {
	t.Parallel()

	var seenCypher string
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				seenCypher = cypher
				return []map[string]any{
					{"start_id": "resource:queue", "start_name": "queue", "start_labels": []any{"CloudResource"}, "repo_id": "repo-a", "repo_name": "api", "depth": int64(1)},
				}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-resource-to-code",
		bytes.NewBufferString(`{"start":"resource:queue","max_depth":4,"limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.traceResourceToCode(rec, req)

	decodeImpactEnvelopeData(t, rec)
	if strings.Contains(seenCypher, "MATCH (start) WHERE start.id") {
		t.Fatalf("start anchor must not be an unlabeled all-node scan: %s", seenCypher)
	}
	if !strings.Contains(seenCypher, "MATCH (start:"+impactAnchorLabelDisjunction+")") {
		t.Fatalf("start anchor must be label-seeded with the impact-anchor disjunction: %s", seenCypher)
	}
	// Semantics preserved: the exact id predicate, repo target, and server-side
	// LIMIT parameter must remain.
	if !strings.Contains(seenCypher, "start.id = $start_id") {
		t.Fatalf("start id predicate must be preserved: %s", seenCypher)
	}
	if !strings.Contains(seenCypher, "(repo:Repository)") {
		t.Fatalf("repo target label must be preserved: %s", seenCypher)
	}
	if !strings.Contains(seenCypher, "LIMIT $limit") {
		t.Fatalf("server-side LIMIT must be preserved: %s", seenCypher)
	}
}

// TestTraceResourceToCodeFallbackHydrationAnchorsLabeled proves the fallback
// start-node hydration (issue #3567, impact.go ~line 233) is label-seeded too.
// When the traversal returns no rows the handler hydrates the start node by id;
// the prior `MATCH (n) WHERE n.id = $id` was a second all-node scan.
func TestTraceResourceToCodeFallbackHydrationAnchorsLabeled(t *testing.T) {
	t.Parallel()

	var hydrationCypher string
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReaderWithSingle{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				// Traversal returns no paths so the handler falls back to hydration.
				return nil, nil
			},
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				hydrationCypher = cypher
				return map[string]any{"id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-resource-to-code",
		bytes.NewBufferString(`{"start":"resource:queue"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.traceResourceToCode(rec, req)

	decodeImpactEnvelopeData(t, rec)
	if strings.Contains(hydrationCypher, "MATCH (n) WHERE n.id") {
		t.Fatalf("fallback hydration must not be an unlabeled all-node scan: %s", hydrationCypher)
	}
	if !strings.Contains(hydrationCypher, "MATCH (n:"+impactAnchorLabelDisjunction+")") {
		t.Fatalf("fallback hydration must be label-seeded: %s", hydrationCypher)
	}
	if !strings.Contains(hydrationCypher, "n.id = $id") {
		t.Fatalf("fallback hydration id predicate must be preserved: %s", hydrationCypher)
	}
}

// TestExplainDependencyPathAnchorsLabeledEndpoints proves the dependency-path
// source and target anchors are label-seeded (issue #3567, impact.go ~lines
// 312-313). Both endpoints previously used unlabeled `MATCH (x) WHERE x.id` that
// the planner could only satisfy with an all-node scan per endpoint.
func TestExplainDependencyPathAnchorsLabeledEndpoints(t *testing.T) {
	t.Parallel()

	var seenCypher string
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReaderWithSingle{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				seenCypher = cypher
				return map[string]any{
					"source_id": "resource:queue", "source_name": "queue", "source_labels": []any{"CloudResource"},
					"target_id": "repo:api", "target_name": "api", "target_labels": []any{"Repository"},
					"depth": int64(-1),
				}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/explain-dependency-path",
		bytes.NewBufferString(`{"source":"resource:queue","target":"repo:api"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.explainDependencyPath(rec, req)

	if got := rec.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", got, rec.Body.String())
	}
	if strings.Contains(seenCypher, "MATCH (source) WHERE source.id") {
		t.Fatalf("source anchor must not be an unlabeled all-node scan: %s", seenCypher)
	}
	if strings.Contains(seenCypher, "MATCH (target) WHERE target.id") {
		t.Fatalf("target anchor must not be an unlabeled all-node scan: %s", seenCypher)
	}
	if !strings.Contains(seenCypher, "MATCH (source:"+impactAnchorLabelDisjunction+")") {
		t.Fatalf("source anchor must be label-seeded: %s", seenCypher)
	}
	if !strings.Contains(seenCypher, "MATCH (target:"+impactAnchorLabelDisjunction+")") {
		t.Fatalf("target anchor must be label-seeded: %s", seenCypher)
	}
	// Semantics preserved: exact id predicates and shortestPath stay intact.
	if !strings.Contains(seenCypher, "source.id = $source_id") || !strings.Contains(seenCypher, "target.id = $target_id") {
		t.Fatalf("id predicates must be preserved: %s", seenCypher)
	}
	if !strings.Contains(seenCypher, "shortestPath((source)-[*1..8]-(target))") {
		t.Fatalf("shortestPath traversal must be preserved: %s", seenCypher)
	}
}

// fakeGraphReaderWithSingle is a GraphQuery test double that scripts both Run
// and RunSingle so the dependency-path and resource-to-code fallback paths can
// be exercised independently.
type fakeGraphReaderWithSingle struct {
	run       func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f fakeGraphReaderWithSingle) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.run == nil {
		return nil, nil
	}
	return f.run(ctx, cypher, params)
}

func (f fakeGraphReaderWithSingle) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.runSingle == nil {
		return nil, nil
	}
	return f.runSingle(ctx, cypher, params)
}
