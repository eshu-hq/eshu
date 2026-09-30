// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type readOnlyQueryStub struct {
	called bool
	err    error
}

func (stub *readOnlyQueryStub) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	stub.called = true
	return nil, stub.err
}

func TestInstrumentedQueryerRecordsSummaryAndError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("read failed")
	stub := &readOnlyQueryStub{err: wantErr}
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := &InstrumentedQueryer{Inner: stub, Tracer: provider.Tracer("test"), StoreName: "content"}
	ctx := db.WithQuerySummary(context.Background(), "content_lookup")
	_, err := reader.QueryContext(ctx, "SELECT 1")
	if !errors.Is(err, wantErr) || !stub.called {
		t.Fatalf("QueryContext() = %v, called %v", err, stub.called)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "postgres.query" || spans[0].Status().Code != codes.Error {
		t.Fatalf("spans = %+v, want one failed postgres.query span", spans)
	}
	foundSummary := false
	for _, attr := range spans[0].Attributes() {
		if string(attr.Key) == "db.query.summary" && attr.Value.AsString() == "content_lookup" {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatal("missing query summary on read span")
	}
}
