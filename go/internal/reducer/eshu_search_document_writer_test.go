// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWriteEshuSearchDocumentsUpsertsAndRetires(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeSearchDocExecer{retireAffected: 2}
	writer := PostgresEshuSearchDocumentWriter{DB: db, Now: func() time.Time { return now }}

	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{sampleSearchDoc("searchdoc:content_entity:e-1"), sampleSearchDoc("searchdoc:content_entity:e-2")},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if result.CanonicalWrites != 2 {
		t.Errorf("canonical writes = %d, want 2", result.CanonicalWrites)
	}
	if result.Retired != 2 {
		t.Errorf("retired = %d, want 2", result.Retired)
	}
	// Bulk fact insert + fact retirement + persisted-index maintenance.
	if got := len(db.execs); got < 6 {
		t.Fatalf("exec calls = %d, want fact writes plus search-index maintenance", got)
	}
	insert := db.execs[0]
	if !strings.Contains(insert.query, "INSERT INTO fact_records") {
		t.Fatalf("first exec is not an insert: %q", insert.query)
	}
	// Bulk insert uses 15 parallel slice args (one slice per column).
	if got, want := len(insert.args), 15; got != want {
		t.Fatalf("insert arg count = %d, want %d", got, want)
	}
	// Each arg is now a slice; verify the first element of each slice.
	factKinds, ok := insert.args[3].([]string)
	if !ok || len(factKinds) == 0 {
		t.Fatalf("fact_kind arg = %T, want []string with 2 elements", insert.args[3])
	}
	if got, want := factKinds[0], EshuSearchDocumentFactKind; got != want {
		t.Errorf("fact_kind[0] = %v, want %v", got, want)
	}
	sourceConfs, ok := insert.args[6].([]string)
	if !ok || len(sourceConfs) == 0 {
		t.Fatalf("source_confidence arg = %T, want []string", insert.args[6])
	}
	if got, want := sourceConfs[0], string(facts.SourceConfidenceInferred); got != want {
		t.Errorf("source_confidence[0] = %v, want %v", got, want)
	}
	scopeIDs, ok := insert.args[1].([]string)
	if !ok || len(scopeIDs) == 0 {
		t.Fatalf("scope_id arg = %T, want []string", insert.args[1])
	}
	if got, want := scopeIDs[0], "scope-1"; got != want {
		t.Errorf("scope_id[0] = %v, want %v", got, want)
	}
	tombstones, ok := insert.args[13].([]bool)
	if !ok || len(tombstones) == 0 {
		t.Fatalf("is_tombstone arg = %T, want []bool", insert.args[13])
	}
	if tombstones[0] {
		t.Errorf("is_tombstone[0] = true, want false")
	}
	var retire fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM fact_records") {
			retire = exec
			break
		}
	}
	if !strings.Contains(retire.query, "DELETE FROM fact_records") {
		t.Fatalf("missing fact retirement delete: %#v", db.execs)
	}
	ids, ok := retire.args[3].([]string)
	if !ok || len(ids) != 2 {
		t.Fatalf("retire written-id arg = %v, want 2 ids", retire.args[3])
	}
}

func TestWriteEshuSearchDocumentsMaintainsPersistedSearchIndex(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	var sawDocumentUpsert, sawTermRefresh, sawStatsUpsert bool
	for _, exec := range db.execs {
		sawDocumentUpsert = sawDocumentUpsert || strings.Contains(exec.query, "INSERT INTO eshu_search_index_documents")
		sawTermRefresh = sawTermRefresh || strings.Contains(exec.query, "INSERT INTO eshu_search_index_terms")
		sawStatsUpsert = sawStatsUpsert || strings.Contains(exec.query, "INSERT INTO eshu_search_index_stats")
	}
	if !sawDocumentUpsert {
		t.Fatal("missing persisted search-index document upsert")
	}
	if !sawTermRefresh {
		t.Fatal("missing persisted search-index term refresh")
	}
	if !sawStatsUpsert {
		t.Fatal("missing persisted search-index stats upsert")
	}
}

func TestWriteEshuSearchDocumentsPayloadIncludesContentHash(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}
	doc := sampleSearchDoc("searchdoc:content_entity:e-1")

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents:    []searchdocs.Document{doc},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	var factUpsert fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO fact_records") {
			factUpsert = exec
			break
		}
	}
	if factUpsert.query == "" {
		t.Fatal("missing fact upsert")
	}
	// Bulk insert: args[14] is now []string of JSON payloads (one per doc).
	payloads, ok := factUpsert.args[14].([]string)
	if !ok || len(payloads) == 0 {
		t.Fatalf("payload arg = %T, want []string", factUpsert.args[14])
	}
	var decoded struct {
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal([]byte(payloads[0]), &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got, want := decoded.ContentHash, searchhybrid.DocumentContentHash(doc); got != want {
		t.Fatalf("content_hash = %q, want %q", got, want)
	}
}

func TestWriteEshuSearchDocumentsRecordsSearchIndexTelemetry(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	db := &fakeSearchDocExecer{
		affected: []fakeSearchDocAffected{
			{fragment: "DELETE FROM eshu_search_index_terms\nWHERE scope_id = $1\n  AND generation_id = $2\n  AND document_id <> ALL($3::text[])", affected: 4},
			{fragment: "DELETE FROM eshu_search_index_documents\nWHERE scope_id = $1\n  AND generation_id = $2\n  AND document_id <> ALL($3::text[])", affected: 2},
			// Bulk refresh: single ANY-array delete replacing the old per-doc delete.
			{fragment: "document_id   = ANY($3::text[])", affected: 3},
		},
	}
	writer := PostgresEshuSearchDocumentWriter{
		DB:          db,
		Instruments: instruments,
		Tracer:      tracerProvider.Tracer("test"),
	}

	_, err = writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := int64MetricValue(t, rm, "eshu_dp_search_index_mutations_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainEshuSearchDocument),
		telemetry.MetricDimensionKind:      "document",
		telemetry.MetricDimensionOperation: "upsert",
		telemetry.MetricDimensionResult:    "success",
	}); got != 1 {
		t.Fatalf("document upsert metric = %d, want 1", got)
	}
	if got := int64MetricValue(t, rm, "eshu_dp_search_index_mutations_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainEshuSearchDocument),
		telemetry.MetricDimensionKind:      "document",
		telemetry.MetricDimensionOperation: "retire",
		telemetry.MetricDimensionResult:    "success",
	}); got != 2 {
		t.Fatalf("document retire metric = %d, want 2", got)
	}
	if got := int64MetricValue(t, rm, "eshu_dp_search_index_mutations_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainEshuSearchDocument),
		telemetry.MetricDimensionKind:      "term",
		telemetry.MetricDimensionOperation: "retire",
		telemetry.MetricDimensionResult:    "success",
	}); got != 7 {
		t.Fatalf("term retire metric = %d, want 7", got)
	}
	if got := int64MetricValue(t, rm, "eshu_dp_search_index_mutations_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainEshuSearchDocument),
		telemetry.MetricDimensionKind:      "term",
		telemetry.MetricDimensionOperation: "upsert",
		telemetry.MetricDimensionResult:    "success",
	}); got <= 0 {
		t.Fatalf("term upsert metric = %d, want positive", got)
	}
	assertHistogramPoint(t, rm, "eshu_dp_search_index_write_duration_seconds", map[string]string{
		telemetry.MetricDimensionDomain: string(DomainEshuSearchDocument),
		telemetry.MetricDimensionResult: "success",
	})

	span := requireSpan(t, spanRecorder.Ended(), telemetry.SpanReducerEshuSearchIndexWrite)
	assertSpanStringAttribute(t, span, telemetry.MetricDimensionDomain, string(DomainEshuSearchDocument))
	assertSpanStringAttribute(t, span, telemetry.LogKeyScopeID, "scope-1")
	assertSpanStringAttribute(t, span, telemetry.LogKeyGenerationID, "gen-1")
}

func TestWriteEshuSearchDocumentsRecordsSearchIndexErrors(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	writer := PostgresEshuSearchDocumentWriter{
		DB:          &fakeSearchDocExecer{failOn: "INSERT INTO eshu_search_index_terms"},
		Instruments: instruments,
		Tracer:      tracerProvider.Tracer("test"),
	}

	_, err = writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err == nil {
		t.Fatal("WriteEshuSearchDocuments error = nil, want term write error")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := int64MetricValue(t, rm, "eshu_dp_search_index_errors_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainEshuSearchDocument),
		telemetry.MetricDimensionOperation: "term_upsert",
	}); got != 1 {
		t.Fatalf("search index error metric = %d, want 1", got)
	}
	assertHistogramPoint(t, rm, "eshu_dp_search_index_write_duration_seconds", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainEshuSearchDocument),
		telemetry.MetricDimensionOperation: "term_upsert",
		telemetry.MetricDimensionResult:    "error",
	})
	_ = requireSpan(t, spanRecorder.Ended(), telemetry.SpanReducerEshuSearchIndexWrite)
}

func TestWriteEshuSearchDocumentsReportsSubphaseTimings(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{delay: time.Millisecond}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	tests := []struct {
		name     string
		duration time.Duration
	}{
		{name: "fact upsert", duration: result.Timings.FactUpsertDuration},
		{name: "document upsert", duration: result.Timings.IndexDocumentUpsertDuration},
		{name: "term refresh", duration: result.Timings.IndexTermRefreshDuration},
		{name: "term upsert", duration: result.Timings.IndexTermUpsertDuration},
		{name: "fact retire", duration: result.Timings.FactRetireDuration},
		{name: "term retire", duration: result.Timings.IndexTermRetireDuration},
		{name: "document retire", duration: result.Timings.IndexDocumentRetireDuration},
		{name: "stats upsert", duration: result.Timings.IndexStatsUpsertDuration},
	}
	for _, tt := range tests {
		if tt.duration <= 0 {
			t.Errorf("%s duration = %v, want positive subphase timing", tt.name, tt.duration)
		}
	}
}

func TestWriteEshuSearchDocumentsEmptySetRetiresAll(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{retireAffected: 5}
	writer := PostgresEshuSearchDocumentWriter{DB: db}
	result, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Errorf("canonical writes = %d, want 0", result.CanonicalWrites)
	}
	if result.Retired != 5 {
		t.Errorf("retired = %d, want 5", result.Retired)
	}
	if got := len(db.execs); got < 4 {
		t.Fatalf("exec calls = %d, want fact retirement plus empty-index maintenance", got)
	}
	var retire fakeSearchDocExecCall
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "DELETE FROM fact_records") {
			retire = exec
			break
		}
	}
	if !strings.Contains(retire.query, "DELETE FROM fact_records") {
		t.Fatalf("missing fact retirement delete: %#v", db.execs)
	}
	ids, ok := retire.args[3].([]string)
	if !ok || len(ids) != 0 {
		t.Fatalf("retire id arg = %v, want empty slice", retire.args[3])
	}
}

func TestWriteEshuSearchDocumentsDeterministicFactID(t *testing.T) {
	t.Parallel()

	first := eshuSearchDocumentFactID("scope-1", "gen-1", "searchdoc:content_entity:e-1")
	second := eshuSearchDocumentFactID("scope-1", "gen-1", "searchdoc:content_entity:e-1")
	if first != second {
		t.Fatalf("fact id not deterministic: %q vs %q", first, second)
	}
	if other := eshuSearchDocumentFactID("scope-1", "gen-2", "searchdoc:content_entity:e-1"); other == first {
		t.Fatal("fact id must differ across generations")
	}
}

func TestWriteEshuSearchDocumentsRequiresDatabaseAndScope(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresEshuSearchDocumentWriter{}).WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{ScopeID: "s", GenerationID: "g"}); err == nil {
		t.Fatal("expected error for nil database")
	}
	db := &fakeSearchDocExecer{}
	if _, err := (PostgresEshuSearchDocumentWriter{DB: db}).WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{GenerationID: "g"}); err == nil {
		t.Fatal("expected error for missing scope")
	}
}

// TestWriteEshuSearchDocumentsBatchedWritesBoundedExecCount is the TDD regression
// test for the cross-scope starvation bug (issue #3430). Writing N documents must
// issue O(1) bulk statements — not O(N) per-document round-trips — so whale
// repositories do not monopolise all reducer workers indefinitely.
//
// The bound is: fact_batch_insert(1) + fact_retire(1) + retire_terms(1) +
// retire_docs(1) + doc_upsert(1) + term_refresh_delete(1) + term_upsert(1) +
// stats(1) = 8 total ExecContext calls regardless of document count.
// We allow a small constant slack for future additions but assert the count is
// strictly less than N/2 for N=500, which would be violated by any per-doc loop.
func TestWriteEshuSearchDocumentsBatchedWritesBoundedExecCount(t *testing.T) {
	t.Parallel()

	const docCount = 500
	docs := make([]searchdocs.Document, docCount)
	for i := range docs {
		docs[i] = sampleSearchDoc(fmt.Sprintf("searchdoc:content_entity:e-%d", i))
	}

	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-batch",
		ScopeID:      "scope-batch",
		GenerationID: "gen-batch",
		SourceSystem: "content_entities",
		Documents:    docs,
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	got := len(db.execs)
	// Per-document loop would produce ≥ 3*N calls (doc upsert + term delete +
	// term upsert) plus N fact inserts = ≥ 4*N = 2000. A batched writer issues
	// O(1) statements; assert strictly less than N/2 = 250 calls.
	const maxAllowed = docCount / 2
	if got >= maxAllowed {
		t.Fatalf("ExecContext calls = %d for %d documents, want < %d (batched writes required, got O(N) per-doc loop)", got, docCount, maxAllowed)
	}
}

func TestWriteEshuSearchDocumentsTermInsertAvoidsConflictUpdate(t *testing.T) {
	t.Parallel()

	db := &fakeSearchDocExecer{}
	writer := PostgresEshuSearchDocumentWriter{DB: db}

	_, err := writer.WriteEshuSearchDocuments(context.Background(), EshuSearchDocumentWrite{
		IntentID:     "intent-term-insert",
		ScopeID:      "scope-term-insert",
		GenerationID: "gen-term-insert",
		SourceSystem: "content_entities",
		Documents: []searchdocs.Document{
			sampleSearchDoc("searchdoc:content_entity:e-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteEshuSearchDocuments error = %v", err)
	}

	for _, exec := range db.execs {
		if !strings.Contains(exec.query, "INSERT INTO eshu_search_index_terms") {
			continue
		}
		if strings.Contains(exec.query, "ON CONFLICT") {
			t.Fatalf("term insert query still uses conflict-update path after page refresh:\n%s", exec.query)
		}
		return
	}
	t.Fatalf("missing eshu_search_index_terms insert in execs: %#v", db.execs)
}
