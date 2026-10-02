// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRepositoryWorkloadNamesRecordsBoundedPostgresSpanAndErrors(t *testing.T) {
	// The shared fake driver returns io.EOF after its rows, so it cannot inject a rows.Err() iteration failure.
	for _, tc := range []struct {
		name   string
		result contentReaderQueryResult
	}{
		{
			name: "query error",
			result: contentReaderQueryResult{
				columns: []string{"name"},
				err:     errors.New("database unavailable"),
			},
		},
		{
			name: "scan error",
			result: contentReaderQueryResult{
				columns: []string{"name"},
				rows:    [][]driver.Value{{struct{}{}}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

			db := openContentReaderTestDB(t, []contentReaderQueryResult{tc.result})
			reader := NewContentReader(db)
			reader.tracer = provider.Tracer("repository-workload-names-test")

			_, err := reader.repositoryWorkloadNames(context.Background(), "scope-private-value")
			if err == nil {
				t.Fatal("repositoryWorkloadNames() error = nil, want non-nil")
			}

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			span := spans[0]
			if got, want := span.Name(), "postgres.query"; got != want {
				t.Errorf("span name = %q, want %q", got, want)
			}
			attributes := make(map[string]string, len(span.Attributes()))
			for _, attr := range span.Attributes() {
				attributes[string(attr.Key)] = attr.Value.AsString()
			}
			for key, want := range map[string]string{
				"db.system":    "postgresql",
				"db.operation": "repository_workload_names",
				"db.sql.table": "fact_records",
			} {
				if got := attributes[key]; got != want {
					t.Errorf("span attribute %q = %q, want %q", key, got, want)
				}
			}
			if _, ok := attributes["scope_id"]; ok {
				t.Error("span contains scope_id attribute; want bounded attributes only")
			}
			if got := span.Events(); len(got) != 1 || got[0].Name != "exception" {
				t.Errorf("span events = %+v, want one exception event", got)
			}
		})
	}
}

func TestRepositoryWorkloadNamesEmptyScopeSkipsPostgresSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	reader := NewContentReader(openContentReaderTestDB(t, nil))
	reader.tracer = provider.Tracer("repository-workload-names-test")
	names, err := reader.repositoryWorkloadNames(context.Background(), "")
	if err != nil {
		t.Fatalf("repositoryWorkloadNames() error = %v, want nil", err)
	}
	if len(names) != 0 {
		t.Fatalf("repositoryWorkloadNames() = %v, want no names", names)
	}
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}
