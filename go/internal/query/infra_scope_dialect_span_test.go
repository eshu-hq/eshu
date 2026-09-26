// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestInfraScopeDialectSpanAttributes proves an operator can tell from the
// request span which scope dialect a read used and, on the Neo4j
// relationships route, how many probes and scoped reads it issued (#7215).
func TestInfraScopeDialectSpanAttributes(t *testing.T) {
	// Not t.Parallel(): swaps the package-global queryHandlerTracer, like
	// TestInfraRelationshipsSpanRecordsLabelsTried.
	grant := dialectGrants()[1].auth()
	cases := []struct {
		name    string
		backend querycontract.GraphBackend
		auth    *AuthContext
		path    string
		body    string
		want    map[string]any
	}{
		{
			name: "neo4j_search", backend: querycontract.GraphBackendNeo4j, auth: &grant,
			path: infraSearchPath, body: infraSearchBody,
			want: map[string]any{"eshu.infra_scope_dialect": infraScopeDialectNeo4jLists},
		},
		{
			name: "nornicdb_search", backend: querycontract.GraphBackendNornicDB, auth: &grant,
			path: infraSearchPath, body: infraSearchBody,
			want: map[string]any{"eshu.infra_scope_dialect": infraScopeDialectShapeA},
		},
		{
			name: "unscoped_search", backend: querycontract.GraphBackendNeo4j,
			path: infraSearchPath, body: infraSearchBody,
			want: map[string]any{"eshu.infra_scope_dialect": infraScopeDialectUnscoped},
		},
		{
			// The id exists on the 3rd anchor label but is ungranted, so the
			// loop probes every label, runs 1 scoped labeled read plus the
			// scoped unlabeled fallback.
			name: "neo4j_relationships", backend: querycontract.GraphBackendNeo4j, auth: &grant,
			path: infraRelationshipsPath, body: infraRelationshipsBody,
			want: map[string]any{
				"eshu.infra_scope_dialect":        infraScopeDialectNeo4jLists,
				"eshu.entity_anchor_probes":       int64(len(impactRelationshipAnchorLabels)),
				"eshu.entity_anchor_scoped_reads": int64(2),
				"eshu.entity_anchor_labels_tried": int64(len(impactRelationshipAnchorLabels) + 1),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previous := queryHandlerTracer
			queryHandlerTracer = provider.Tracer("infra-scope-dialect-span-test")
			t.Cleanup(func() { queryHandlerTracer = previous })

			hitLabel := "(n:" + impactRelationshipAnchorLabels[2] + ")"
			graph := &dialectRecordingGraph{single: func(cypher string, _ map[string]any) map[string]any {
				if isAnchorProbe(cypher) && strings.Contains(cypher, hitLabel) {
					return map[string]any{"hit": int64(1)}
				}
				return nil
			}}
			serveInfraDialect(t, tc.backend, tc.auth, graph, tc.path, tc.body)

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			got := map[string]any{}
			for _, kv := range spans[0].Attributes() {
				got[string(kv.Key)] = kv.Value.AsInterface()
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Fatalf("span %s = %#v, want %#v (all: %#v)", key, got[key], want, got)
				}
			}
		})
	}
}
