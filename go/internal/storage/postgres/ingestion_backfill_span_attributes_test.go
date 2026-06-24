package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// backfillDeferredSpanName is the span the deferred relationship backfill opens
// in BackfillAllRelationshipEvidence; the fan-out attributes and partition
// failure event must land on it.
const backfillDeferredSpanName = "relationship.backfill_deferred"

// errBoomPartition is the injected per-partition fact-load failure for the
// partition-failure span event test.
var errBoomPartition = errors.New("boom partition")

// findEndedSpan returns the single ended span with the given name, failing the
// test when it is absent or duplicated so an assertion never silently passes on
// a missing span.
func findEndedSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var match sdktrace.ReadOnlySpan
	for _, span := range ended {
		if span.Name() != name {
			continue
		}
		if match != nil {
			t.Fatalf("found more than one %q span", name)
		}
		match = span
	}
	if match == nil {
		t.Fatalf("no %q span recorded", name)
	}
	return match
}

// spanIntAttr reads a required int64 span attribute, failing when it is absent.
func spanIntAttr(t *testing.T, span sdktrace.ReadOnlySpan, key string) int64 {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsInt64()
		}
	}
	t.Fatalf("span %q missing attribute %q", span.Name(), key)
	return 0
}

// recordingTracer wires a fresh SpanRecorder to a tracer so a test can read the
// spans BackfillAllRelationshipEvidence emits.
func recordingTracer() (*tracetest.SpanRecorder, trace.Tracer) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	return recorder, tracer
}

// TestBackfillDeferredSpanRecordsFanOutAttributes is the #3710 observability gate
// for item 1: the relationship.backfill_deferred span must carry the fact-load
// fan-out shape (partition_count and worker_count) so an operator can read the
// partition cardinality and worker saturation off the trace without grepping
// logs. Two scope-generation partitions and a worker count of two must surface as
// partition_count=2 and worker_count=2.
func TestBackfillDeferredSpanRecordsFanOutAttributes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 24, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	scopeGenPartitions := [][]any{
		{"scope-infra", "gen-infra"},
		{"scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				contentFactRow(
					"fact-cross",
					"scope-infra",
					"gen-infra",
					"content",
					`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
				),
			},
		},
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`)},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`)},
				},
			},
			{rows: scopeGenPartitions},
			{rows: activeGens},
			{rows: activeGens},
		},
	}
	db := newBackfillTxDB(inner)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	store.maintenanceWorkers = 2

	recorder, tracer := recordingTracer()
	if err := store.BackfillAllRelationshipEvidence(context.Background(), tracer, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	span := findEndedSpan(t, recorder.Ended(), backfillDeferredSpanName)
	if got := spanIntAttr(t, span, "partition_count"); got != 2 {
		t.Fatalf("partition_count = %d, want 2", got)
	}
	if got := spanIntAttr(t, span, "worker_count"); got != 2 {
		t.Fatalf("worker_count = %d, want 2", got)
	}
}

// failingPartitionQueryer answers every per-scope fact query with a fixed error
// so the deferred fan-out reaches one partition load and fails it deterministically.
type failingPartitionQueryer struct{ err error }

func (q failingPartitionQueryer) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return nil, q.err
}

// TestBackfillDeferredSpanRecordsPartitionLoadFailure is the #3710 observability
// gate for the "which partition failed" signal: when a per-scope fact load
// errors, the relationship.backfill_deferred span must carry a
// partition_load_failed event naming the failing scope_id, so a 3 AM operator can
// see which partition aborted the pass from the trace, not only the returned
// error string.
func TestBackfillDeferredSpanRecordsPartitionLoadFailure(t *testing.T) {
	t.Parallel()

	params, ok := buildDeferredScopedFactQueryParams([]relationships.CatalogEntry{
		{RepoID: "repo-infra", Aliases: []string{"repo-infra", "infra-repo"}},
	})
	if !ok {
		t.Fatal("buildDeferredScopedFactQueryParams returned ok=false; test fixture has no usable anchor")
	}

	store := IngestionStore{maintenanceWorkers: 1}

	recorder, tracer := recordingTracer()
	ctx, span := tracer.Start(context.Background(), backfillDeferredSpanName)

	partitions := []scopeGenerationPartition{{ScopeID: "scope-x", GenerationID: "gen-x"}}
	_, err := store.loadDeferredScopedFactsAcrossPartitions(
		ctx, failingPartitionQueryer{err: errBoomPartition}, params, partitions, nil,
	)
	span.End()

	if err == nil {
		t.Fatal("loadDeferredScopedFactsAcrossPartitions() error = nil, want partition load failure")
	}

	ended := findEndedSpan(t, recorder.Ended(), backfillDeferredSpanName)
	var foundScope string
	for _, event := range ended.Events() {
		if event.Name != "partition_load_failed" {
			continue
		}
		for _, attr := range event.Attributes {
			if string(attr.Key) == "scope_id" {
				foundScope = attr.Value.AsString()
			}
		}
	}
	if foundScope != "scope-x" {
		t.Fatalf("partition_load_failed event scope_id = %q, want %q", foundScope, "scope-x")
	}
}
