// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routes

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestRequestNormalizeFloorsAndCaps(t *testing.T) {
	req := Request{Path: " /orders ", Method: "get"}
	req.Normalize()
	if req.Path != "/orders" {
		t.Fatalf("Path = %q, want trimmed", req.Path)
	}
	if req.Method != "GET" {
		t.Fatalf("Method = %q, want upper-cased", req.Method)
	}
	if req.MaxDepth != 2 || req.Limit != 25 {
		t.Fatalf("floored MaxDepth/Limit = %d/%d, want 2/25", req.MaxDepth, req.Limit)
	}
	req = Request{Path: "/orders", MaxDepth: 99, Limit: 999}
	req.Normalize()
	if req.MaxDepth != 5 || req.Limit != 100 {
		t.Fatalf("capped MaxDepth/Limit = %d/%d, want 5/100", req.MaxDepth, req.Limit)
	}
}

func TestRequestValidateRequiresPathAndSelector(t *testing.T) {
	if err := (Request{RepoID: "r"}).Validate(); err == nil {
		t.Fatal("Validate without path = nil, want error")
	}
	if err := (Request{Path: "/orders"}).Validate(); err == nil {
		t.Fatal("Validate without selector = nil, want error")
	}
	if err := (Request{Path: "/orders", ServiceName: "svc"}).Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
}

func TestAllowedByScopeNeedsGrantAndMatch(t *testing.T) {
	req := Request{Path: "/orders", RepoID: "repo-a"}
	if AllowedByScope(querycontract.RepositoryAccessFilter{}, req) {
		t.Fatal("AllowedByScope with empty grant = true, want false")
	}
	grant := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}
	if !AllowedByScope(grant, req) {
		t.Fatal("AllowedByScope with matching grant = false, want true")
	}
	grant = querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-b"}}
	if AllowedByScope(grant, req) {
		t.Fatal("AllowedByScope with other-repo grant = true, want false")
	}
}

func TestSelectRouteEmptyIsNotFound(t *testing.T) {
	_, status, ok := SelectRoute(nil)
	if ok || status != "not_found" {
		t.Fatalf("SelectRoute(nil) = ok=%v status=%q, want false/not_found", ok, status)
	}
}

func TestSelectRoutePrefersHandlerRow(t *testing.T) {
	rows := []map[string]any{
		{"endpoint_id": "e1", "path": "/orders", "repo_id": "r"},
		{"endpoint_id": "e1", "path": "/orders", "repo_id": "r", "handler_id": "h1", "handler_name": "place"},
	}
	route, status, ok := SelectRoute(rows)
	if !ok || status != "ok" {
		t.Fatalf("SelectRoute = ok=%v status=%q, want true/ok", ok, status)
	}
	if route.HandlerID != "h1" {
		t.Fatalf("HandlerID = %q, want the handler row", route.HandlerID)
	}
}

func TestSelectRouteMultipleEndpointsIsAmbiguous(t *testing.T) {
	rows := []map[string]any{
		{"endpoint_id": "e1", "path": "/orders", "repo_id": "r"},
		{"endpoint_id": "e2", "path": "/orders", "repo_id": "r"},
	}
	_, status, ok := SelectRoute(rows)
	if ok || status != "ambiguous" {
		t.Fatalf("SelectRoute = ok=%v status=%q, want false/ambiguous", ok, status)
	}
}

func TestSplitRelationshipsDividesAndTruncates(t *testing.T) {
	rows := []map[string]any{
		{"entity_id": "a", "direction": "incoming"},
		{"entity_id": "b", "direction": "outgoing"},
		{"entity_id": "c", "direction": "incoming"},
	}
	callers, callees, truncated := SplitRelationships(rows, 3)
	if truncated {
		t.Fatal("truncated = true within the limit, want false")
	}
	if len(callers) != 2 || len(callees) != 1 {
		t.Fatalf("callers/callees = %d/%d, want 2/1", len(callers), len(callees))
	}
	callers, callees, truncated = SplitRelationships(rows, 2)
	if !truncated {
		t.Fatal("truncated = false past the limit, want true")
	}
	if len(callers)+len(callees) != 2 {
		t.Fatalf("kept %d rows past the limit, want 2", len(callers)+len(callees))
	}
}

func TestMergeMapsDedupesAndBounds(t *testing.T) {
	first := []map[string]any{{"id": "a"}, {"id": "b"}}
	second := []map[string]any{{"id": "b"}, {"id": "c"}}
	merged := MergeMaps(first, second, 10)
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3 deduplicated", len(merged))
	}
	merged = MergeMaps(first, second, 2)
	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want bounded to 2", len(merged))
	}
}

func TestLabelAllowedGatesUnknownLabels(t *testing.T) {
	if !LabelAllowed("Function") {
		t.Fatal("LabelAllowed(Function) = false, want true")
	}
	if LabelAllowed("Endpoint") {
		t.Fatal("LabelAllowed(Endpoint) = true, want false (not a code entity)")
	}
	if LabelAllowed("Function OR 1=1") {
		t.Fatal("LabelAllowed(injection) = true, want false")
	}
}
