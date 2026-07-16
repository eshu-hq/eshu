// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestSearchInfraResourcesNeverUsesUnlabeledWholeGraphScan is the regression
// guard for issue #5271: a query-shape defect, not a NornicDB plan-cache
// problem, explained the reported cold/warm latency gap for
// infra/resources/search. The handler previously anchored on a bare
// MATCH (n) filtered by an (n:A OR n:B OR ...) predicate — that forces a
// whole-graph scan on every call regardless of corpus size, since no label
// or index can narrow the anchor. This proves the worst case (no category
// filter, so every one of the ~27 candidate labels is a branch) never
// regresses back to that shape, for both the free-text and structured-filter
// paths.
func TestSearchInfraResourcesNeverUsesUnlabeledWholeGraphScan(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "free-text query", body: `{"query":"orders","limit":5}`},
		{name: "structured filter only", body: `{"kind":"aws_instance","limit":5}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := &recordingInfraGraphReader{runRows: []map[string]any{}}
			handler := &InfraHandler{Neo4j: reader}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if strings.Contains(reader.lastCypher, "MATCH (n)\n") {
				t.Fatalf("cypher = %q, regressed to an unlabeled whole-graph MATCH(n) scan", reader.lastCypher)
			}
			if !strings.Contains(reader.lastCypher, "MATCH (n:CloudResource)") {
				t.Fatalf("cypher = %q, want a label-anchored branch for every candidate label", reader.lastCypher)
			}
			if got, want := strings.Count(reader.lastCypher, "\nUNION"), len(allInfraLabels)-1; got != want {
				t.Fatalf("union branch count = %d, want %d (one branch per candidate label)", got, want)
			}
			if got, want := strings.Count(reader.lastCypher, "ORDER BY name"), 1; got != want {
				t.Fatalf("ORDER BY name occurrences = %d, want exactly %d (applied once, after the final unioned branch)", got, want)
			}
		})
	}
}

// TestSearchInfraResourcesWrapsUnionInCallSubquery is the regression guard
// for the correctness bug found while proving #5271's fix at realistic
// cardinality: on the pinned NornicDB backend, a bare top-level UNION chain
// returns zero rows for the ENTIRE query whenever its first branch matches
// zero rows, even when later branches have real matches. Confirmed directly
// against the live backend with UNION, UNION ALL, and multiple branch
// orderings; only wrapping the union in CALL {...} RETURN ... avoided it.
// allInfraLabels starts with CloudResource, so any search on a corpus with
// zero matching CloudResource nodes would otherwise silently return an empty
// result for every other label. This asserts the CALL wrapper is present so
// the fix cannot silently regress back to a bare UNION chain.
func TestSearchInfraResourcesWrapsUnionInCallSubquery(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{runRows: []map[string]any{}}
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", bytes.NewBufferString(`{"query":"orders","limit":5}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	cypher := reader.lastCypher
	if !strings.HasPrefix(strings.TrimSpace(cypher), "CALL {") {
		t.Fatalf("cypher = %q, want the per-label UNION wrapped in a CALL {...} subquery", cypher)
	}
	// Split on the CALL block's closing brace followed by the outer RETURN,
	// tolerant of exact indentation/whitespace (a gofumpt reformat or a
	// future template restructure must not break this on formatting alone —
	// only a structural regression should).
	boundary := regexp.MustCompile(`\}\s*RETURN\s`).FindStringIndex(cypher)
	if boundary == nil {
		t.Fatalf("cypher = %q, want a closing CALL block followed by an outer RETURN", cypher)
	}
	beforeCall, afterCall := cypher[:boundary[0]], cypher[boundary[1]:]
	if !strings.Contains(beforeCall, "MATCH (n:CloudResource)") {
		t.Fatalf("cypher = %q, want the CALL block to contain the per-label MATCH branches", cypher)
	}
	if strings.Contains(afterCall, "MATCH (n:") {
		t.Fatalf("cypher = %q, want no per-label MATCH branches outside the CALL block", cypher)
	}
	if !strings.Contains(afterCall, "ORDER BY name") || !strings.Contains(afterCall, "LIMIT $limit") {
		t.Fatalf("cypher = %q, want ORDER BY/LIMIT applied once, outside the CALL block", cypher)
	}
}

// TestInfraSearchReturnColumnsHaveExprs guards the invariant
// infraSearchReturnExprs relies on: every column in infraSearchReturnColumns
// must have a non-empty entry in infraSearchReturnColumnExprs, and the map
// must not carry orphan entries for columns no longer in the slice. A
// missing entry would silently render as "" + " as " + col (e.g. " as
// region", invalid Cypher with no expression before "as") rather than a
// visible Go compile error, so this is a runtime-only failure mode without
// this test.
func TestInfraSearchReturnColumnsHaveExprs(t *testing.T) {
	t.Parallel()

	if got, want := len(infraSearchReturnColumnExprs), len(infraSearchReturnColumns); got != want {
		t.Fatalf("infraSearchReturnColumnExprs has %d entries, want %d (one per infraSearchReturnColumns column, no orphans)", got, want)
	}
	for _, col := range infraSearchReturnColumns {
		if expr := infraSearchReturnColumnExprs[col]; expr == "" {
			t.Fatalf("infraSearchReturnColumnExprs[%q] is empty; every column in infraSearchReturnColumns needs a Cypher expression", col)
		}
	}
}
