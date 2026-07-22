// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func BenchmarkNeo4jReaderHealthyPolicyOverhead(b *testing.B) {
	records := []*neo4jdriver.Record{{Keys: []string{"value"}, Values: []any{int64(1)}}}
	tracer := noop.NewTracerProvider().Tracer("benchmark")
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: records}}
	})

	b.Run("pre_policy_shape", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rows, err := benchmarkPrePolicyRead(context.Background(), tracer, records)
			if err != nil || len(rows) != 1 {
				b.Fatalf("pre-policy read = (%d rows, %v), want one row", len(rows), err)
			}
		}
	})

	b.Run("bounded_policy", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rows, err := reader.Run(context.Background(), "RETURN 1 AS value", nil)
			if err != nil || len(rows) != 1 {
				b.Fatalf("bounded read = (%d rows, %v), want one row", len(rows), err)
			}
		}
	})
}

// benchmarkPrePolicyRead reproduces the former one-span, one-result conversion
// shape only for a same-input fixed-overhead comparison. Correctness tests call
// the production reader instead.
func benchmarkPrePolicyRead(
	ctx context.Context,
	tracer trace.Tracer,
	records []*neo4jdriver.Record,
) ([]map[string]any, error) {
	_, span := tracer.Start(ctx, "neo4j.query")
	defer span.End()

	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for index, key := range record.Keys {
			row[key] = record.Values[index]
		}
		rows = append(rows, row)
	}
	return rows, nil
}
