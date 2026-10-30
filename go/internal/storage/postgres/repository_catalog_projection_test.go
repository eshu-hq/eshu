// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestListActiveRepositoryFactsQueryProjectsHeavyKeysOut pins the #9567
// projection: the catalog read subtracts exactly the omitted payload keys in
// SQL, so the reducer never scans or decodes imports_map, git_refs, or the
// delta path arrays, and it subtracts only from object payloads.
func TestListActiveRepositoryFactsQueryProjectsHeavyKeysOut(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	if _, err := NewFactStore(db).ListActiveRepositoryFacts(context.Background()); err != nil {
		t.Fatalf("ListActiveRepositoryFacts() error = %v", err)
	}
	query := db.queries[0].query

	subtracted := "detoasted.payload"
	for _, key := range facts.RepositoryCatalogOmittedPayloadKeys() {
		subtracted += " - '" + key + "'"
	}
	// jsonb "-" raises "cannot delete from scalar" on a scalar or JSON null
	// payload, which would fail the whole catalog load for every consumer
	// domain. Only an object payload is subtracted; anything else passes
	// through unchanged, as the unprojected read returned it.
	projection := "CASE WHEN jsonb_typeof(detoasted.payload) = 'object'\n" +
		"         THEN " + subtracted + "\n" +
		"         ELSE detoasted.payload END"
	if !strings.Contains(query, projection+"\nFROM fact_records AS fact") {
		t.Fatalf("query must select %q as its payload column:\n%s", projection, query)
	}
	// The type check and the subtraction each detoast a toasted argument, so
	// reading fact.payload in both made page 1 69% slower than the unprojected
	// read on PG18. One lateral expression detoasts it once for both.
	lateral := "CROSS JOIN LATERAL (SELECT fact.payload #> '{}' AS payload OFFSET 0) AS detoasted\nWHERE"
	if !strings.Contains(query, lateral) {
		t.Fatalf("query must detoast the payload once through %q:\n%s", lateral, query)
	}
	if got, want := strings.Count(query, "fact.payload"), 1; got != want {
		t.Fatalf("query references fact.payload %d times, want %d (the single detoast):\n%s", got, want, query)
	}
}

// TestListActiveRepositoryFactsRecordsCatalogLoadHistograms proves the load
// records rows and raw payload bytes once per call, labeled by the calling
// reducer domain from the context, and folds an unlisted domain to
// "unknown" so the label set stays closed.
func TestListActiveRepositoryFactsRecordsCatalogLoadHistograms(t *testing.T) {
	t.Parallel()

	payloads := [][]byte{
		[]byte(`{"repo_id":"repo-1","name":"one"}`),
		[]byte(`{"repo_id":"repo-2","name":"two","remote_url":"https://github.com/acme/two"}`),
	}
	wantBytes := int64(len(payloads[0]) + len(payloads[1]))
	pageRows := func() [][]any {
		rows := make([][]any, 0, len(payloads))
		for i, payload := range payloads {
			rows = append(rows, []any{
				fmt.Sprintf("fact-repo-%d", i), "repository:repo", "generation-1", "repository",
				"repository:repo", "1.0.0", "git", int64(0), "unknown", "git", "repository:repo",
				"", "", time.Date(2026, time.October, 29, 10, 0, i, 0, time.UTC), false, payload,
			})
		}
		return rows
	}

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: pageRows()}, {rows: pageRows()}}}
	store := NewFactStore(db).WithInstruments(instruments)

	ctx := telemetry.ContextWithRepositoryCatalogDomain(context.Background(), string(reducercontract.DomainSupplyChainImpact))
	if _, err := store.ListActiveRepositoryFacts(ctx); err != nil {
		t.Fatalf("ListActiveRepositoryFacts() error = %v", err)
	}
	unlisted := telemetry.ContextWithRepositoryCatalogDomain(context.Background(), "not_a_catalog_domain")
	if _, err := store.ListActiveRepositoryFacts(unlisted); err != nil {
		t.Fatalf("ListActiveRepositoryFacts(unlisted) error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, tc := range []struct {
		metric string
		domain string
		sum    int64
	}{
		{"eshu_dp_repository_catalog_load_rows", "supply_chain_impact", 2},
		{"eshu_dp_repository_catalog_load_payload_bytes", "supply_chain_impact", wantBytes},
		{"eshu_dp_repository_catalog_load_rows", "unknown", 2},
		{"eshu_dp_repository_catalog_load_payload_bytes", "unknown", wantBytes},
	} {
		point, ok := catalogHistogramPoint(rm, tc.metric, tc.domain)
		if !ok {
			t.Fatalf("%s{domain=%q} not recorded", tc.metric, tc.domain)
		}
		if point.Count != 1 || point.Sum != tc.sum {
			t.Fatalf("%s{domain=%q} count=%d sum=%d, want count=1 sum=%d", tc.metric, tc.domain, point.Count, point.Sum, tc.sum)
		}
	}
}

func catalogHistogramPoint(rm metricdata.ResourceMetrics, name, domain string) (metricdata.HistogramDataPoint[int64], bool) {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[int64])
			if !ok {
				continue
			}
			for _, point := range hist.DataPoints {
				if value, ok := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain)); ok && value.AsString() == domain {
					return point, true
				}
			}
		}
	}
	return metricdata.HistogramDataPoint[int64]{}, false
}
