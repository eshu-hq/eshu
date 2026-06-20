package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type fakeSearchDocExecCall struct {
	query string
	args  []any
}

type fakeSearchDocResult struct {
	affected int64
}

func (r fakeSearchDocResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeSearchDocResult) RowsAffected() (int64, error) { return r.affected, nil }

type fakeSearchDocExecer struct {
	execs          []fakeSearchDocExecCall
	retireAffected int64
	failOn         string
	affected       []fakeSearchDocAffected
}

type fakeSearchDocAffected struct {
	fragment string
	affected int64
}

func (f *fakeSearchDocExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, fakeSearchDocExecCall{query: query, args: args})
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return nil, errors.New("boom")
	}
	for _, match := range f.affected {
		if strings.Contains(query, match.fragment) {
			return fakeSearchDocResult{affected: match.affected}, nil
		}
	}
	if strings.Contains(query, "DELETE FROM fact_records") {
		return fakeSearchDocResult{affected: f.retireAffected}, nil
	}
	return fakeSearchDocResult{affected: 1}, nil
}

func sampleSearchDoc(id string) searchdocs.Document {
	return searchdocs.Document{
		ID:           id,
		RepoID:       "repo-1",
		SourceKind:   searchdocs.SourceKindCodeEntity,
		Title:        "Function Handle",
		GraphHandles: []searchdocs.GraphHandle{{Kind: "content_entity", ID: id}},
		TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

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
	// Two fact upserts plus fact retirement and persisted-index maintenance.
	if got := len(db.execs); got < 6 {
		t.Fatalf("exec calls = %d, want fact writes plus search-index maintenance", got)
	}
	insert := db.execs[0]
	if !strings.Contains(insert.query, "INSERT INTO fact_records") {
		t.Fatalf("first exec is not an insert: %q", insert.query)
	}
	if got, want := len(insert.args), 15; got != want {
		t.Fatalf("insert arg count = %d, want %d", got, want)
	}
	if got, want := insert.args[3], EshuSearchDocumentFactKind; got != want {
		t.Errorf("fact_kind = %v, want %v", got, want)
	}
	if got, want := insert.args[6], facts.SourceConfidenceInferred; got != want {
		t.Errorf("source_confidence = %v, want %v", got, want)
	}
	if got, want := insert.args[1], "scope-1"; got != want {
		t.Errorf("scope_id = %v, want %v", got, want)
	}
	if got, want := insert.args[13], false; got != want {
		t.Errorf("is_tombstone = %v, want false", got)
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
	payload, ok := factUpsert.args[14].([]byte)
	if !ok {
		t.Fatalf("payload arg = %T, want []byte", factUpsert.args[14])
	}
	var decoded struct {
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
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
			{fragment: "DELETE FROM eshu_search_index_terms\nWHERE scope_id = $1\n  AND generation_id = $2\n  AND document_id = $3", affected: 3},
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
	assertSpanStringAttribute(t, span, telemetry.MetricDimensionScopeID, "scope-1")
	assertSpanStringAttribute(t, span, telemetry.MetricDimensionGenerationID, "gen-1")
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
		telemetry.MetricDimensionDomain: string(DomainEshuSearchDocument),
		telemetry.MetricDimensionResult: "error",
	})
	_ = requireSpan(t, spanRecorder.Ended(), telemetry.SpanReducerEshuSearchIndexWrite)
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

func int64MetricValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if metricPointHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", name, wantAttrs)
	return 0
}

func assertHistogramPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Float64 histogram", name, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if metricPointHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %#v not found", name, wantAttrs)
}

func metricPointHasAttrs(attrs []attribute.KeyValue, want map[string]string) bool {
	for wantKey, wantValue := range want {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == wantKey && attr.Value.AsString() == wantValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func requireSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return nil
}

func assertSpanStringAttribute(t *testing.T, span sdktrace.ReadOnlySpan, key string, want string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			if got := attr.Value.AsString(); got != want {
				t.Fatalf("span attr %s = %q, want %q", key, got, want)
			}
			return
		}
	}
	t.Fatalf("span attr %s not found", key)
}
