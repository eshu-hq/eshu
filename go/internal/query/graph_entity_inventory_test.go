// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// graphEntityCountReader returns one fixed scalar count row and serves the
// bounded list rows from listRows for any list query.
type graphEntityCountReader struct {
	countByLabel map[string]int
	listRows     []map[string]any
	countRows    []map[string]any
	runCalls     int
	singleCalls  int
	lastCountCy  string
	lastListCy   string
	lastParams   map[string]any
}

func (f *graphEntityCountReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.runCalls++
	if strings.Contains(cypher, "AS "+graphEntityKinds[0].key) {
		f.lastCountCy = cypher
		if f.countRows != nil {
			return f.countRows, nil
		}
		row := make(map[string]any, len(graphEntityKinds))
		for _, kind := range graphEntityKinds {
			row[kind.key] = f.countByLabel[kind.label]
		}
		return []map[string]any{row}, nil
	}
	f.lastListCy = cypher
	f.lastParams = params
	return f.listRows, nil
}

func (f *graphEntityCountReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	f.singleCalls++
	for label, count := range f.countByLabel {
		if strings.Contains(cypher, "(n:"+label+")") {
			return map[string]any{"c": count}, nil
		}
	}
	return map[string]any{"c": 0}, nil
}

func TestGraphEntityInventoryUsesOneFacetCountRoundTrip(t *testing.T) {
	t.Parallel()

	reader := &graphEntityCountReader{
		countByLabel: map[string]int{"Workload": 15, "Repository": 21, "Module": 6},
	}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	data := decodeGraphEntityBody(t, rec)
	if got, want := reader.runCalls, 1; got != want {
		t.Fatalf("Run calls = %d, want %d (one facet-count round trip)", got, want)
	}
	if got := reader.singleCalls; got != 0 {
		t.Fatalf("RunSingle calls = %d, want 0 (serial per-label counts removed)", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(reader.lastCountCy), "CALL {") {
		t.Fatalf("count cypher = %q, want scalar CALL subqueries", reader.lastCountCy)
	}
	if strings.Contains(reader.lastCountCy, "MATCH (n) ") {
		t.Fatalf("count cypher = %q, must not contain an all-node MATCH", reader.lastCountCy)
	}
	if got, want := strings.Count(reader.lastCountCy, "CALL {\n"), len(graphEntityKinds); got != want {
		t.Fatalf("count CALL subqueries = %d, want %d", got, want)
	}
	if got := data["total"].(float64); got != 42 {
		t.Fatalf("total = %v, want 42", got)
	}
}

func decodeGraphEntityBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("error = %#v, want nil", envelope.Error)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object", envelope.Data)
	}
	return data
}

func TestGraphEntityInventoryReturnsPerKindCountsWithoutFilter(t *testing.T) {
	t.Parallel()

	reader := &graphEntityCountReader{
		countByLabel: map[string]int{"Workload": 15, "Repository": 21, "Module": 6},
	}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	data := decodeGraphEntityBody(t, rec)
	kinds, ok := data["kinds"].([]any)
	if !ok {
		t.Fatalf("kinds = %#v, want array", data["kinds"])
	}
	if len(kinds) != len(graphEntityKinds) {
		t.Fatalf("len(kinds) = %d, want %d", len(kinds), len(graphEntityKinds))
	}
	// services facet maps to the Workload label count.
	first, ok := kinds[0].(map[string]any)
	if !ok {
		t.Fatalf("kinds[0] = %#v, want object", kinds[0])
	}
	if got, want := first["kind"], "services"; got != want {
		t.Fatalf("kinds[0].kind = %v, want %v", got, want)
	}
	if got := first["count"].(float64); got != 15 {
		t.Fatalf("services count = %v, want 15", got)
	}
	// No kind filter -> no entities listed yet.
	if entities := data["entities"].([]any); len(entities) != 0 {
		t.Fatalf("entities = %#v, want empty without kind filter", entities)
	}
	if got := data["total"].(float64); got != 42 {
		t.Fatalf("total = %v, want 42", got)
	}
}

func TestGraphEntityInventoryCountsEmptyAndSingleLabelGraphs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		countByLabel  map[string]int
		wantTotal     float64
		populatedKind string
		wantCount     float64
	}{
		{name: "empty graph", countByLabel: map[string]int{}},
		{
			name:          "one populated label",
			countByLabel:  map[string]int{"Repository": 9},
			wantTotal:     9,
			populatedKind: "repositories",
			wantCount:     9,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := &graphEntityCountReader{countByLabel: tt.countByLabel}
			handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			handler.listEntities(rec, req)

			data := decodeGraphEntityBody(t, rec)
			if got := data["total"].(float64); got != tt.wantTotal {
				t.Fatalf("total = %v, want %v", got, tt.wantTotal)
			}
			kinds := data["kinds"].([]any)
			if got, want := len(kinds), len(graphEntityKinds); got != want {
				t.Fatalf("len(kinds) = %d, want %d", got, want)
			}
			for _, rawKind := range kinds {
				kind := rawKind.(map[string]any)
				want := float64(0)
				if kind["kind"] == tt.populatedKind {
					want = tt.wantCount
				}
				if got := kind["count"].(float64); got != want {
					t.Fatalf("%s count = %v, want %v", kind["kind"], got, want)
				}
			}
		})
	}
}

func TestGraphEntityInventoryListsEntitiesForSelectedKind(t *testing.T) {
	t.Parallel()

	reader := &graphEntityCountReader{
		countByLabel: map[string]int{"Workload": 2},
		listRows: []map[string]any{
			{"id": "workload:eshu-api", "name": "eshu-api", "account": "repo://eshu"},
			{"id": "workload:eshu-mcp", "name": "eshu-mcp", "account": "repo://eshu"},
		},
	}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=services", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	data := decodeGraphEntityBody(t, rec)
	entities := data["entities"].([]any)
	if len(entities) != 2 {
		t.Fatalf("len(entities) = %d, want 2", len(entities))
	}
	first := entities[0].(map[string]any)
	if got, want := first["name"], "eshu-api"; got != want {
		t.Fatalf("entities[0].name = %v, want %v", got, want)
	}
	if got, want := first["kind"], "services"; got != want {
		t.Fatalf("entities[0].kind = %v, want %v", got, want)
	}
	if got, want := first["id"], "workload:eshu-api"; got != want {
		t.Fatalf("entities[0].id = %v, want %v", got, want)
	}
	// The list query must anchor on the Workload label, not an unlabelled scan.
	if !strings.Contains(reader.lastListCy, "MATCH (n:Workload)") {
		t.Fatalf("list cypher = %q, want Workload anchor", reader.lastListCy)
	}
}

func TestGraphEntityInventoryIdentityIAMProjectsPopulatedProperties(t *testing.T) {
	t.Parallel()

	// The ExternalPrincipal writer sets principal_value and principal_account_id,
	// not name/account_id. The identity_iam projection must read those populated
	// properties so rows carry a meaningful name + account instead of empty
	// strings (regression for empty identity_iam name/account).
	reader := &graphEntityCountReader{
		countByLabel: map[string]int{"ExternalPrincipal": 1},
		listRows: []map[string]any{
			{"id": "principal:hash", "name": "arn:aws:iam::PLACEHOLDER:role/example", "account": "PLACEHOLDER"},
		},
	}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=identity_iam", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	data := decodeGraphEntityBody(t, rec)

	// The list cypher must select the properties the writer actually populates.
	if !strings.Contains(reader.lastListCy, "n.principal_value") {
		t.Fatalf("list cypher = %q, want n.principal_value name source", reader.lastListCy)
	}
	if !strings.Contains(reader.lastListCy, "n.principal_account_id") {
		t.Fatalf("list cypher = %q, want n.principal_account_id account source", reader.lastListCy)
	}

	entities := data["entities"].([]any)
	if len(entities) != 1 {
		t.Fatalf("len(entities) = %d, want 1", len(entities))
	}
	first := entities[0].(map[string]any)
	if got := first["name"].(string); got == "" {
		t.Fatalf("identity_iam name = empty, want non-empty principal name")
	}
	if got := first["account"].(string); got == "" {
		t.Fatalf("identity_iam account = empty, want non-empty principal account")
	}
}

func TestGraphEntityInventoryNetworkingProjectsPopulatedName(t *testing.T) {
	t.Parallel()

	// The SecurityGroupRule writer synthesizes a human-readable name from
	// direction/ip_protocol/from_port-to_port and stores it as the "name"
	// property. The networking projection must select n.name so rows carry a
	// meaningful display name instead of an empty string (regression for empty
	// networking name). SecurityGroupRule nodes carry no account_id, so the
	// account column is always blank.
	reader := &graphEntityCountReader{
		countByLabel: map[string]int{"SecurityGroupRule": 1},
		listRows: []map[string]any{
			{"id": "rule:hash", "name": "ingress/tcp/443-443", "account": ""},
		},
	}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=networking", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	data := decodeGraphEntityBody(t, rec)

	// The list cypher must anchor on SecurityGroupRule and select n.name.
	if !strings.Contains(reader.lastListCy, "MATCH (n:SecurityGroupRule)") {
		t.Fatalf("list cypher = %q, want SecurityGroupRule anchor", reader.lastListCy)
	}
	if !strings.Contains(reader.lastListCy, "n.name") {
		t.Fatalf("list cypher = %q, want n.name name source", reader.lastListCy)
	}

	entities := data["entities"].([]any)
	if len(entities) != 1 {
		t.Fatalf("len(entities) = %d, want 1", len(entities))
	}
	first := entities[0].(map[string]any)
	if got := first["name"].(string); got == "" {
		t.Fatalf("networking name = empty, want non-empty rule name")
	}
}

func TestGraphEntityInventoryNameSearchBindsLoweredParam(t *testing.T) {
	t.Parallel()

	reader := &graphEntityCountReader{countByLabel: map[string]int{"Workload": 1}}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=services&q=API", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	decodeGraphEntityBody(t, rec)
	if !strings.Contains(reader.lastListCy, "CONTAINS $q") {
		t.Fatalf("list cypher = %q, want CONTAINS $q predicate", reader.lastListCy)
	}
	if got, want := reader.lastParams["q"], "api"; got != want {
		t.Fatalf("q param = %v, want lowered %v", got, want)
	}
}

func TestGraphEntityInventoryRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	reader := &graphEntityCountReader{}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=bogus", nil)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGraphEntityInventoryPaginationTruncates(t *testing.T) {
	t.Parallel()

	// Three rows returned for a limit of 2 -> truncated, only 2 surfaced.
	reader := &graphEntityCountReader{
		countByLabel: map[string]int{"Module": 3},
		listRows: []map[string]any{
			{"id": "module:a", "name": "a"},
			{"id": "module:b", "name": "b"},
			{"id": "module:c", "name": "c"},
		},
	}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities?kind=libraries&limit=2", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	data := decodeGraphEntityBody(t, rec)
	if entities := data["entities"].([]any); len(entities) != 2 {
		t.Fatalf("len(entities) = %d, want 2 (limit)", len(entities))
	}
	if truncated := data["truncated"].(bool); !truncated {
		t.Fatal("truncated = false, want true")
	}
	// The handler asks for limit+1 to detect truncation.
	if got := reader.lastParams["limit"]; got != 3 {
		t.Fatalf("list limit param = %v, want 3 (limit+1)", got)
	}
}

func TestGraphEntityInventoryGatesUnsupportedProfile(t *testing.T) {
	t.Parallel()

	reader := &graphEntityCountReader{}
	handler := &GraphEntityInventoryHandler{Neo4j: reader, Profile: ProfileLocalLightweight}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/graph/entities", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.listEntities(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
}
