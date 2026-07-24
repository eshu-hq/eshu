// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func BenchmarkNeo4jReaderHealthyPolicyOverhead(b *testing.B) {
	records := []*neo4jdriver.Record{{Keys: []string{"value"}, Values: []any{int64(1)}}}
	factory := benchmarkReadSessionFactory(records, nil)
	reader := newPolicyTestNeo4jReader(factory)
	ctx := context.Background()
	const cypher = "RETURN 1 AS value"

	b.Run("unbounded_reader", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rows, err := benchmarkUnboundedReaderRead(reader, ctx, cypher, nil)
			if err != nil || len(rows) != 1 {
				b.Fatalf("unbounded read = (%d rows, %v), want one row", len(rows), err)
			}
		}
	})

	b.Run("bounded_reader", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rows, err := reader.Run(ctx, cypher, nil)
			if err != nil || len(rows) != 1 {
				b.Fatalf("bounded read = (%d rows, %v), want one row", len(rows), err)
			}
		}
	})
}

func TestBenchmarkReaderShapesUseIdenticalSessionLifecycle(t *testing.T) {
	records := []*neo4jdriver.Record{{Keys: []string{"value"}, Values: []any{int64(1)}}}
	for _, test := range []struct {
		name string
		run  func(*Neo4jReader) ([]map[string]any, error)
	}{
		{
			name: "unbounded_reader",
			run: func(reader *Neo4jReader) ([]map[string]any, error) {
				return benchmarkUnboundedReaderRead(reader, context.Background(), "RETURN 1 AS value", nil)
			},
		},
		{
			name: "bounded_reader",
			run: func(reader *Neo4jReader) ([]map[string]any, error) {
				return reader.Run(context.Background(), "RETURN 1 AS value", nil)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			lifecycle := &benchmarkReadLifecycle{}
			reader := newPolicyTestNeo4jReader(benchmarkReadSessionFactory(records, lifecycle))
			rows, err := test.run(reader)
			if err != nil || len(rows) != 1 || IntVal(rows[0], "value") != 1 {
				t.Fatalf("read = (%#v, %v), want one identical row", rows, err)
			}
			if *lifecycle != (benchmarkReadLifecycle{sessions: 1, runs: 1, collects: 1, closes: 1}) {
				t.Fatalf("lifecycle = %#v, want one session/run/collect/close", lifecycle)
			}
		})
	}
}

type benchmarkReadLifecycle struct {
	sessions int
	runs     int
	collects int
	closes   int
}

func benchmarkReadSessionFactory(
	records []*neo4jdriver.Record,
	lifecycle *benchmarkReadLifecycle,
) neo4jReadSessionFactory {
	return func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		if lifecycle != nil {
			lifecycle.sessions++
		}
		return &fakeNeo4jReadSession{
			run: func(
				context.Context,
				string,
				map[string]any,
				...func(*neo4jdriver.TransactionConfig),
			) (neo4jReadResult, error) {
				if lifecycle != nil {
					lifecycle.runs++
				}
				return &fakeNeo4jReadResult{collect: func(context.Context) ([]*neo4jdriver.Record, error) {
					if lifecycle != nil {
						lifecycle.collects++
					}
					return records, nil
				}}, nil
			},
			close: func(context.Context) error {
				if lifecycle != nil {
					lifecycle.closes++
				}
				return nil
			},
		}
	}
}

// benchmarkUnboundedReaderRead is the control reader shape immediately before
// the deadline policy: it uses the same fake driver/session, input, row
// conversion, tracing, and policy-result telemetry as the bounded reader, but
// no client or transaction deadline or retry.
func benchmarkUnboundedReaderRead(
	reader *Neo4jReader,
	ctx context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	parentCtx := ctx
	started := time.Now()
	ctx, span := reader.tracer.Start(
		ctx,
		"neo4j.query",
		trace.WithAttributes(
			attribute.String("db.system", "neo4j"),
			attribute.String("db.name", reader.database),
			attribute.Int64(telemetry.SpanAttrGraphReadConfiguredDeadlineMS, reader.policy.readTimeout.Milliseconds()),
		),
	)
	defer span.End()

	session := reader.newReadSession(ctx)
	if session == nil {
		return nil, errors.New("neo4j read session is required")
	}
	defer reader.closeNeo4jReadSession(ctx, session)
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for index, key := range record.Keys {
			row[key] = record.Values[index]
		}
		rows = append(rows, row)
	}
	duration := time.Since(started)
	outcome, publicErr := graphReadResult(
		parentCtx,
		ctx,
		nil,
		1,
		duration,
		reader.policy.slowThreshold,
	)
	reader.recordGraphReadTelemetry(parentCtx, span, outcome, 1, duration, publicErr)
	return rows, nil
}
