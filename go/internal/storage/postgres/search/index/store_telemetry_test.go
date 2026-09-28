// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

func TestEshuSearchIndexStoreTracesPersistedStagesThroughRows(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := searchIndexTracer
	searchIndexTracer = provider.Tracer("index-test")
	t.Cleanup(func() { searchIndexTracer = previousTracer })

	document := searchIndexDocumentFixture("searchdoc:runtime:payments", "repo-1", "Payments runbook")
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	database := &recordingSearchDatabase{ExecQueryer: &fake.ExecQueryer{QueryResponses: []fake.Rows{
		{Data: [][]any{{int64(2500), false}}},
		{Data: [][]any{{payload, 1.75, int64(2500), false}}},
	}}}
	parentCtx, parent := provider.Tracer("parent-test").Start(context.Background(), "parent")
	database.parentSpanID = parent.SpanContext().SpanID()
	result, err := NewEshuSearchIndexStore(database).Search(parentCtx, EshuSearchIndexSearch{
		ScopeID: "repo-1", RepoID: "repo-1", Query: "payment runbook",
		Anchor: searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"}, Limit: 20,
	})
	parent.End()
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Document.ID != document.ID {
		t.Fatalf("Search() candidates = %#v, want document %q", result.Candidates, document.ID)
	}
	if !database.queryHadChildSpan || !database.rowsHadRecordingChildSpan || !database.closeHadRecordingChildSpan {
		t.Fatalf("query/rows/close child span recording = %t/%t/%t, want true/true/true",
			database.queryHadChildSpan, database.rowsHadRecordingChildSpan, database.closeHadRecordingChildSpan)
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended spans = %d, want child and parent", len(spans))
	}
	child := spans[0]
	if child.Name() != "query.semantic_search.persisted_index" {
		t.Fatalf("child span name = %q", child.Name())
	}
	if child.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("child parent = %s, want %s", child.Parent().SpanID(), parent.SpanContext().SpanID())
	}
	var names []string
	for _, event := range child.Events() {
		names = append(names, event.Name)
	}
	want := []string{"stats_complete", "query_returned", "first_row", "rows_complete"}
	if len(names) != len(want) {
		t.Fatalf("child events = %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("child events = %v, want %v", names, want)
		}
	}
	if got := traceEventInt(child.Events()[3].Attributes, "candidate_count"); got != 1 {
		t.Fatalf("rows_complete candidate_count = %d, want 1", got)
	}
	if len(child.Attributes()) != 0 {
		t.Fatalf("child attributes = %v, want no request identity", child.Attributes())
	}
}

func TestEshuSearchIndexStoreTracesDecodeFailureStage(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := searchIndexTracer
	searchIndexTracer = provider.Tracer("index-test")
	t.Cleanup(func() { searchIndexTracer = previousTracer })

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{
		{Data: [][]any{{int64(2500), false}}},
		{Data: [][]any{{[]byte("{"), 1.75, int64(2500), false}}},
	}}
	_, err := NewEshuSearchIndexStore(database).Search(context.Background(), EshuSearchIndexSearch{
		ScopeID: "repo-1", RepoID: "repo-1", Query: "payment",
		Anchor: searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"}, Limit: 20,
	})
	if err == nil || !strings.Contains(err.Error(), "decode persisted eshu search document") {
		t.Fatalf("Search() error = %v, want decode failure", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if len(spans[0].Events()) != 4 {
		t.Fatalf("error events = %v, want stats, query, first row, and stage error", spans[0].Events())
	}
	event := spans[0].Events()[3]
	if event.Name != "stage_error" || traceEventString(event.Attributes, "stage") != "decode" {
		t.Fatalf("error event = %q %v, want stage_error/decode", event.Name, event.Attributes)
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("span status = %v, want error", spans[0].Status().Code)
	}
}

func TestEshuSearchIndexStoreTracesDatabaseFailureStage(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		responses []fake.Rows
		stage     string
	}{
		{"stats", []fake.Rows{{FailWith: errors.New("stats unavailable")}}, "stats"},
		{"query", []fake.Rows{{Data: [][]any{{int64(1), false}}}, {FailWith: errors.New("query unavailable")}}, "query"},
		{"scan", []fake.Rows{{Data: [][]any{{int64(1), false}}}, {Data: [][]any{{[]byte("{}"), "wrong-score-type", int64(1), false}}}}, "scan"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer func() { _ = provider.Shutdown(context.Background()) }()
			previousTracer := searchIndexTracer
			searchIndexTracer = provider.Tracer("index-test")
			defer func() { searchIndexTracer = previousTracer }()
			database := &fake.ExecQueryer{QueryResponses: scenario.responses}
			_, err := NewEshuSearchIndexStore(database).Search(context.Background(), EshuSearchIndexSearch{
				ScopeID: "repo-1", RepoID: "repo-1", Query: "payment",
				Anchor: searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"}, Limit: 20,
			})
			if err == nil {
				t.Fatal("Search() error = nil, want failure")
			}
			spans := recorder.Ended()
			if len(spans) != 1 || spans[0].Status().Code != codes.Error {
				t.Fatalf("ended spans = %v, want one failed span", spans)
			}
			events := spans[0].Events()
			last := events[len(events)-1]
			if last.Name != "stage_error" || traceEventString(last.Attributes, "stage") != scenario.stage {
				t.Fatalf("last event = %q %v, want stage_error/%s", last.Name, last.Attributes, scenario.stage)
			}
		})
	}
}

func TestEshuSearchIndexStoreTracesZeroTermCompletion(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := searchIndexTracer
	searchIndexTracer = provider.Tracer("index-test")
	t.Cleanup(func() { searchIndexTracer = previousTracer })

	database := &fake.ExecQueryer{QueryResponses: []fake.Rows{{Data: [][]any{{int64(7), false}}}}}
	result, err := NewEshuSearchIndexStore(database).Search(context.Background(), EshuSearchIndexSearch{
		ScopeID: "repo-1", RepoID: "repo-1", Query: "...",
		Anchor: searchretrieval.Anchor{Kind: searchretrieval.ScopeKindRepo, ID: "repo-1"}, Limit: 20,
	})
	if err != nil || len(result.Candidates) != 0 || len(database.Queries) != 1 {
		t.Fatalf("Search() result/error/queries = %#v/%v/%d, want empty/nil/1", result, err, len(database.Queries))
	}
	spans := recorder.Ended()
	if len(spans) != 1 || len(spans[0].Events()) != 2 {
		t.Fatalf("ended spans = %v, want stats and completion events", spans)
	}
	events := spans[0].Events()
	if events[0].Name != "stats_complete" || events[1].Name != "rows_complete" ||
		traceEventInt(events[1].Attributes, "candidate_count") != 0 {
		t.Fatalf("zero-term events = %v, want stats then zero-candidate completion", events)
	}
}

type recordingSearchDatabase struct {
	*fake.ExecQueryer
	parentSpanID               trace.SpanID
	queryHadChildSpan          bool
	rowsHadRecordingChildSpan  bool
	closeHadRecordingChildSpan bool
}

func (database *recordingSearchDatabase) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	rows, err := database.ExecQueryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if strings.Contains(query, "FROM eshu_search_index_terms") {
		span := trace.SpanFromContext(ctx)
		database.queryHadChildSpan = span.IsRecording() && span.SpanContext().SpanID() != database.parentSpanID
		return &recordingSearchRows{Rows: rows, span: span, database: database}, nil
	}
	return rows, nil
}

type recordingSearchRows struct {
	db.Rows
	span     trace.Span
	database *recordingSearchDatabase
}

func (rows *recordingSearchRows) Next() bool {
	rows.database.rowsHadRecordingChildSpan = rows.span.IsRecording()
	return rows.Rows.Next()
}

func (rows *recordingSearchRows) Scan(dest ...any) error {
	rows.database.rowsHadRecordingChildSpan = rows.database.rowsHadRecordingChildSpan && rows.span.IsRecording()
	return rows.Rows.Scan(dest...)
}

func (rows *recordingSearchRows) Close() error {
	rows.database.closeHadRecordingChildSpan = rows.span.IsRecording()
	return rows.Rows.Close()
}

func traceEventInt(attributes []attribute.KeyValue, key string) int64 {
	for _, item := range attributes {
		if string(item.Key) == key {
			return item.Value.AsInt64()
		}
	}
	return -1
}

func traceEventString(attributes []attribute.KeyValue, key string) string {
	for _, item := range attributes {
		if string(item.Key) == key {
			return item.Value.AsString()
		}
	}
	return ""
}
