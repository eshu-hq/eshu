// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocumentationFindingAggregateScopedEmptyGrantReturnsZeroWithoutRead(t *testing.T) {
	t.Parallel()

	store := &stubDocumentationFindingAggregateStore{
		countErr:     errors.New("broad documentation aggregate count read"),
		inventoryErr: errors.New("broad documentation aggregate inventory read"),
	}
	handler := &DocumentationHandler{Aggregates: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "count", path: "/api/v0/documentation/findings/count"},
		{name: "inventory", path: "/api/v0/documentation/findings/inventory?group_by=status&limit=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
				Mode:        AuthModeScoped,
				TenantID:    "tenant_a",
				WorkspaceID: "workspace_a",
			}))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			var envelope ResponseEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
			}
			if envelope.Truth == nil {
				t.Fatalf("truth envelope missing: %#v", envelope)
			}
			data, ok := envelope.Data.(map[string]any)
			if !ok {
				t.Fatalf("envelope data = %#v, want object", envelope.Data)
			}
			switch tc.name {
			case "count":
				if got := data["total_findings"]; got != float64(0) {
					t.Fatalf("total_findings = %v, want 0", got)
				}
			case "inventory":
				if got := data["count"]; got != float64(0) {
					t.Fatalf("count = %v, want 0", got)
				}
				if got := data["truncated"]; got != false {
					t.Fatalf("truncated = %v, want false", got)
				}
			}
		})
	}
	if store.countCalls != 0 {
		t.Fatalf("count store calls = %d, want 0", store.countCalls)
	}
	if store.invCalls != 0 {
		t.Fatalf("inventory store calls = %d, want 0", store.invCalls)
	}
}

func TestDocumentationFindingAggregateSQLAppliesScopedAuthorizationBeforeGrouping(t *testing.T) {
	t.Parallel()

	filter := DocumentationFindingAggregateFilter{
		AllowedRepositoryIDs: []string{"repository:team-a", "repository:team-a"},
		AllowedScopeIDs:      []string{"scope:team-a"},
	}
	groupExpr, err := documentationFindingInventoryGroupExpression(DocumentationFindingInventoryByStatus)
	if err != nil {
		t.Fatalf("documentationFindingInventoryGroupExpression() error = %v", err)
	}
	totalSQL, totalArgs := buildDocumentationFindingAggregateTotalSQL(filter)
	groupSQL, groupArgs := buildDocumentationFindingAggregateGroupSQL(filter, groupExpr)
	inventorySQL, inventoryArgs := buildDocumentationFindingInventorySQL(filter, groupExpr, 10, 0)
	for _, tc := range []struct {
		name          string
		query         string
		args          []any
		orderBoundary string
		wantArgs      int
	}{
		{name: "total", query: totalSQL, args: totalArgs, orderBoundary: ";", wantArgs: 2},
		{name: "group", query: groupSQL, args: groupArgs, orderBoundary: "GROUP BY", wantArgs: 2},
		{name: "inventory", query: inventorySQL, args: inventoryArgs, orderBoundary: "GROUP BY", wantArgs: 4},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertDocumentationAuthorizationPredicate(t, tc.query, "fact_records", "ingestion_scopes")
			if got := len(tc.args); got != tc.wantArgs {
				t.Fatalf("args len = %d, want %d", got, tc.wantArgs)
			}
			if strings.Index(tc.query, "fact_records.scope_id IN (") > strings.Index(tc.query, tc.orderBoundary) {
				t.Fatalf("scoped authorization predicate appears after %s:\n%s", tc.orderBoundary, tc.query)
			}
		})
	}
}
