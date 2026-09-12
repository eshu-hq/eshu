// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file is this package's own copy of small test doubles iac/resources_test.go
// and iac/resources_current_test.go declare (#6642 Part A). Both copies exist
// because resources_scope_auth_test.go needs the genuine production
// AuthMiddlewareWithScopedTokens (package query, not importable by the iac/
// leaf) alongside a graph/inventory double for IaCHandler, so that test
// cannot move with its route's own handler tests. IaCInventoryStore's method
// signatures now use iac's exported InventoryCandidate/InventorySearch/
// InventorySummary types (exported from this move so an external adapter can
// implement the interface at all), which is what makes this root-side double
// possible.

// iacResourceNode is iac/resources_test.go's identically named fixture,
// duplicated here.
func iacResourceNode(id, name, resourceType, provider string) map[string]any {
	return map[string]any{
		"id":            id,
		"name":          name,
		"resource_name": name,
		"type":          resourceType,
		"provider":      provider,
		"line_number":   int64(1),
	}
}

// stubIaCResourceGraph is iac/resources_test.go's identically named fixture,
// duplicated here.
type stubIaCResourceGraph struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
	calls      int
}

func (s *stubIaCResourceGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.calls++
	s.lastCypher = cypher
	s.lastParams = params
	if s.err != nil {
		return nil, s.err
	}
	return append([]map[string]any(nil), s.rows...), nil
}

func (s *stubIaCResourceGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle not used by IaC resource list")
}

// stubIaCInventoryStore is iac/resources_current_test.go's identically named
// fixture, duplicated here. Only the fields this package's tests read
// (lastAccess) are exercised; the rest exist so the type satisfies
// IaCInventoryStore identically to the leaf's copy.
type stubIaCInventoryStore struct {
	candidates   []IaCInventoryCandidate
	lastSearch   IaCInventorySearch
	lastAccess   querycontract.RepositoryAccessFilter
	searchCalls  int
	summaryCalls int
}

func (s *stubIaCInventoryStore) SearchActive(
	_ context.Context,
	search IaCInventorySearch,
	access querycontract.RepositoryAccessFilter,
) ([]IaCInventoryCandidate, error) {
	s.searchCalls++
	s.lastSearch = search
	s.lastAccess = access
	return append([]IaCInventoryCandidate(nil), s.candidates...), nil
}

func (s *stubIaCInventoryStore) Summary(
	_ context.Context,
	access querycontract.RepositoryAccessFilter,
	_ int,
) (IaCInventorySummary, error) {
	s.summaryCalls++
	s.lastAccess = access
	return IaCInventorySummary{}, nil
}

// newIaCResourceTestHandler is iac/resources_current_test.go's identically
// named fixture, duplicated here.
func newIaCResourceTestHandler(graph querycontract.GraphQuery) *IaCHandler {
	inventory := &stubIaCInventoryStore{}
	if stub, ok := graph.(*stubIaCResourceGraph); ok {
		for _, row := range stub.rows {
			if querycontract.StringVal(row, "generation_id") == "" {
				row["generation_id"] = "generation-active"
			}
			inventory.candidates = append(inventory.candidates, IaCInventoryCandidate{
				ID:           querycontract.StringVal(row, "id"),
				Name:         querycontract.StringVal(row, "name"),
				GenerationID: querycontract.StringVal(row, "generation_id"),
			})
		}
	}
	return &IaCHandler{
		Graph:     graph,
		Inventory: inventory,
	}
}

// iacResourceListResponseForTest decodes only the fields this package's tests
// read, through a package-local struct rather than the leaf's unexported
// response row type -- this package cannot spell iac's unexported resourceRow.
type iacResourceListResponseForTest struct {
	Count     int              `json:"count"`
	Limit     int              `json:"limit"`
	Truncated bool             `json:"truncated"`
	Resources []map[string]any `json:"resources"`
}

// decodeIaCResourceList is iac/resources_test.go's identically named fixture,
// duplicated here with a package-local response shape.
func decodeIaCResourceList(t *testing.T, w *httptest.ResponseRecorder) iacResourceListResponseForTest {
	t.Helper()
	var body iacResourceListResponseForTest
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw = %s", err, w.Body.String())
	}
	return body
}
