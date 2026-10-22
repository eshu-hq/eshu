// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestGraphEntityKindCountsCypherUsesOneSafeScalarRead(t *testing.T) {
	t.Parallel()

	cypher := graphEntityKindCountsCypher(graphEntityKinds)
	if !strings.HasPrefix(strings.TrimSpace(cypher), "CALL {") {
		t.Fatalf("count cypher = %q, want scalar CALL subqueries", cypher)
	}
	if strings.Contains(cypher, "MATCH (n) ") {
		t.Fatalf("count cypher = %q, must not contain an all-node MATCH", cypher)
	}
	if got, want := strings.Count(cypher, "CALL {\n"), len(graphEntityKinds); got != want {
		t.Fatalf("CALL count = %d, want %d", got, want)
	}
	if strings.Contains(cypher, "UNION") {
		t.Fatalf("count cypher = %q, must not use a union that corrupts empty-label metadata", cypher)
	}
	for _, kind := range graphEntityKinds {
		for _, want := range []string{
			"MATCH (n:" + kind.label + ")",
			"RETURN count(n) AS " + kind.key,
		} {
			if !strings.Contains(cypher, want) {
				t.Fatalf("count cypher missing %q:\n%s", want, cypher)
			}
		}
	}
	if tail := cypher[strings.LastIndex(cypher, "}")+1:]; strings.Contains(tail, "count(") || strings.Contains(tail, "sum(") {
		t.Fatalf("outer projection must not aggregate NornicDB CALL rows: %s", tail)
	}
}

func TestDecodeGraphEntityKindCountsRestoresCatalogOrder(t *testing.T) {
	t.Parallel()

	row := make(map[string]any, len(graphEntityKinds))
	for i := len(graphEntityKinds) - 1; i >= 0; i-- {
		kind := graphEntityKinds[i]
		row[kind.key] = i
	}
	row[graphEntityKinds[0].key] = float64(0)
	kinds, total, err := decodeGraphEntityKindCounts([]map[string]any{row})
	if err != nil {
		t.Fatalf("decodeGraphEntityKindCounts() error = %v, want nil", err)
	}
	if got, want := total, 28; got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	for i, kind := range graphEntityKinds {
		if got := StringVal(kinds[i], "kind"); got != kind.key {
			t.Fatalf("kinds[%d].kind = %q, want %q", i, got, kind.key)
		}
		if got := StringVal(kinds[i], "label"); got != kind.label {
			t.Fatalf("kinds[%d].label = %q, want %q", i, got, kind.label)
		}
	}
}

func TestDecodeGraphEntityKindCountsFailsClosed(t *testing.T) {
	t.Parallel()

	validRows := func() []map[string]any {
		row := make(map[string]any, len(graphEntityKinds))
		for _, kind := range graphEntityKinds {
			row[kind.key] = 0
		}
		return []map[string]any{row}
	}
	tests := []struct {
		name   string
		mutate func([]map[string]any) []map[string]any
		want   string
	}{
		{name: "missing row", mutate: func(_ []map[string]any) []map[string]any { return nil }, want: "0 rows, want 1"},
		{name: "extra row", mutate: func(rows []map[string]any) []map[string]any { return append(rows, rows[0]) }, want: "2 rows, want 1"},
		{name: "unknown", mutate: func(rows []map[string]any) []map[string]any { rows[0]["unknown"] = 0; return rows }, want: "unknown key"},
		{name: "missing count", mutate: func(rows []map[string]any) []map[string]any { delete(rows[0], graphEntityKinds[0].key); return rows }, want: "count is missing"},
		{name: "wrong count type", mutate: func(rows []map[string]any) []map[string]any { rows[0][graphEntityKinds[0].key] = "0"; return rows }, want: "want integer"},
		{name: "fractional count", mutate: func(rows []map[string]any) []map[string]any { rows[0][graphEntityKinds[0].key] = 0.5; return rows }, want: "not a finite integer"},
		{name: "negative", mutate: func(rows []map[string]any) []map[string]any { rows[0][graphEntityKinds[0].key] = -1; return rows }, want: "negative count"},
		{name: "total overflow", mutate: func(rows []map[string]any) []map[string]any {
			rows[0][graphEntityKinds[0].key] = int(^uint(0) >> 1)
			rows[0][graphEntityKinds[1].key] = 1
			return rows
		}, want: "total overflows int"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := decodeGraphEntityKindCounts(tt.mutate(validRows()))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeGraphEntityKindCounts() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestGraphEntityInventoryRecordsBoundedOutcomeTelemetry(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("graph-entity-inventory-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

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
	decodeGraphEntityBody(t, rec)

	spans := recorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("ended spans = %d, want %d", got, want)
	}
	if got, want := spans[0].Name(), "query.graph_entity_inventory"; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
	attributes := map[string]any{}
	for _, item := range spans[0].Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	for key, want := range map[string]any{
		"eshu.query.graph_entity_inventory.round_trip_count": int64(2),
		"eshu.query.graph_entity_inventory.facet_row_count":  int64(1),
		"eshu.query.graph_entity_inventory.result_count":     int64(2),
		"eshu.query.graph_entity_inventory.truncated":        true,
	} {
		if got := attributes[key]; got != want {
			t.Fatalf("span attribute %s = %#v, want %#v; attributes=%#v", key, got, want, attributes)
		}
	}
}
