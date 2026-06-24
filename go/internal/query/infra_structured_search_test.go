// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchInfraResourcesAcceptsStructuredFiltersWithoutQuery(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":                "cloud-resource:ec2-instance",
				"name":              "orders-api",
				"labels":            []any{"CloudResource"},
				"source_system":     "aws",
				"environment":       "prod",
				"resource_type":     "aws_instance",
				"resource_category": "compute",
				"resource_id":       "i-0123456789abcdef0",
				"service_kind":      "ec2",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"category":"cloud","kind":" aws_instance ","provider":" aws ","environment":" prod ","resource_service":" ec2 ","resource_category":" compute ","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	for _, want := range []string{
		"n:CloudResource",
		"n.source_system = $provider",
		"n.service_kind = $resource_service",
		"n.resource_category = $resource_category",
		"n.environment = $environment",
	} {
		if !strings.Contains(reader.lastCypher, want) {
			t.Fatalf("cypher = %q, want structured filter fragment %q", reader.lastCypher, want)
		}
	}
	whereClause, _, _ := strings.Cut(reader.lastCypher, "RETURN")
	for _, forbidden := range []string{
		"CONTAINS $query",
		"coalesce(n.resource_type, n.data_type, '') = $resource_type_query",
		"coalesce(n.environment",
		"coalesce(n.resource_category",
		"coalesce(n.resource_service",
	} {
		if strings.Contains(whereClause, forbidden) {
			t.Fatalf("where clause = %q, want no free-text or coalesced filter predicate for filter-only search", whereClause)
		}
	}
	for key, want := range map[string]any{
		"kind":              "aws_instance",
		"provider":          "aws",
		"environment":       "prod",
		"resource_service":  "ec2",
		"resource_category": "compute",
		"limit":             6,
	} {
		if got := reader.lastParams[key]; got != want {
			t.Fatalf("params[%s] = %#v, want %#v", key, got, want)
		}
	}
	for _, unexpected := range []string{"query", "resource_type_query"} {
		if _, ok := reader.lastParams[unexpected]; ok {
			t.Fatalf("params[%s] = %#v, want omitted for filter-only search", unexpected, reader.lastParams[unexpected])
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := int(resp["count"].(float64)), 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func TestSearchInfraResourcesRejectsUnboundedRequestWithoutQueryOrFilters(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: &recordingInfraGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"   ","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["detail"], "query or structured filter is required"; got != want {
		t.Fatalf("detail = %#v, want %#v", got, want)
	}
}
