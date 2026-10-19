// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// This file holds the backfill package's own test doubles: the exec/query
// recorder the fake-driven engine tests stage responses on, the queued-rows
// reader behind those responses, and no-op evidence/phase stores for write
// paths whose durable assertions belong to the live seam tests. The root
// postgres package has similarly named doubles; the copies are deliberate —
// a subpackage's tests live in that subpackage and cannot share the root's
// unexported helpers.

// fakeExecCall records one ExecContext call.
type fakeExecCall struct {
	query string
	args  []any
}

// fakeQueryCall records one QueryContext call.
type fakeQueryCall struct {
	query string
	args  []any
}

// fakeExecQueryer replays staged query responses in FIFO order and records
// every exec and query. Tests that need scope-keyed deferred-fact answers set
// deferredFactsByScope instead of relying on FIFO order, which the per-scope
// fan-out does not guarantee.
type fakeExecQueryer struct {
	execs                []fakeExecCall
	queries              []fakeQueryCall
	queryResponses       []queueFakeRows
	deferredFactsByScope map[string][][]any
}

func (f *fakeExecQueryer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeExecCall{query: query, args: args})
	return fakeResult{}, nil
}

func (f *fakeExecQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	f.queries = append(f.queries, fakeQueryCall{query: query, args: args})

	// Route the per-scope deferred backfill fact query to a scope-keyed
	// response when configured, independent of FIFO order. args[2] is the
	// $3 scope_id parameter.
	if f.deferredFactsByScope != nil && query == ListDeferredScopedRelationshipFactRecordsQuery {
		var scopeID string
		if len(args) >= 3 {
			scopeID, _ = args[2].(string)
		}
		return &queueFakeRows{rows: f.deferredFactsByScope[scopeID]}, nil
	}

	// The partition memo gate's batched lookup always resolves to "no memo
	// rows" here, independent of FIFO order: fake-driven fixtures model a
	// fresh corpus with no prior memo state, so every candidate partition is
	// a memo MISS.
	if query == LookupDeferredBackfillPartitionMemosQuery {
		return &queueFakeRows{}, nil
	}

	// The memo write's ArgoCD-bearing check always resolves to "no
	// ArgoCD-bearing partitions" here: none of the fixture facts carries an
	// ArgoCD marker, so every memo candidate is non-bearing and gets a row.
	if query == ListArgoCDBearingPartitionsQuery {
		return &queueFakeRows{}, nil
	}

	if len(f.queryResponses) == 0 {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	rows := f.queryResponses[0]
	f.queryResponses = f.queryResponses[1:]
	if rows.err != nil {
		return nil, rows.err
	}
	return &rows, nil
}

// queueFakeRows replays staged rows.
type queueFakeRows struct {
	rows  [][]any
	err   error
	index int
}

func (r *queueFakeRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *queueFakeRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}

	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		case *time.Time:
			value, ok := row[i].(time.Time)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want time.Time", i, row[i])
			}
			*target = value
		case *int:
			value, ok := row[i].(int)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int", i, row[i])
			}
			*target = value
		case *int64:
			value, ok := row[i].(int64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want int64", i, row[i])
			}
			*target = value
		case *float64:
			value, ok := row[i].(float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want float64", i, row[i])
			}
			*target = value
		case *[]float64:
			value, ok := row[i].([]float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []float64", i, row[i])
			}
			*target = append((*target)[:0], value...)
		case *array.Float64Array:
			value, ok := row[i].([]float64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []float64", i, row[i])
			}
			*target = append((*target)[:0], value...)
		case *sql.NullString:
			value, ok := row[i].(sql.NullString)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullString", i, row[i])
			}
			*target = value
		case *sql.NullBool:
			value, ok := row[i].(sql.NullBool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullBool", i, row[i])
			}
			*target = value
		case *sql.NullInt64:
			value, ok := row[i].(sql.NullInt64)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullInt64", i, row[i])
			}
			*target = value
		case *sql.NullTime:
			value, ok := row[i].(sql.NullTime)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want sql.NullTime", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}

	r.index++
	return nil
}

func (r *queueFakeRows) Err() error { return nil }

func (r *queueFakeRows) Close() error { return nil }

type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }

func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

// recordingEvidenceStore accepts every evidence write and records the calls.
// Write paths that must prove durable rows belong to the live seam tests;
// fake-driven unit tests assert on the recorded calls instead.
type recordingEvidenceStore struct {
	mu    sync.Mutex
	calls []evidenceCall
}

type evidenceCall struct {
	generationID string
	facts        []relationships.EvidenceFact
}

func (r *recordingEvidenceStore) UpsertEvidenceFacts(_ context.Context, generationID string, facts []relationships.EvidenceFact) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, evidenceCall{generationID: generationID, facts: facts})
	return nil
}

func (r *recordingEvidenceStore) UpsertEvidenceFactsCounted(ctx context.Context, generationID string, evidence []relationships.EvidenceFact) (int64, error) {
	if err := r.UpsertEvidenceFacts(ctx, generationID, evidence); err != nil {
		return 0, err
	}
	return int64(len(evidence)), nil
}

// recordingPhaseStore accepts every phase publication and records the rows,
// so unit tests prove per-partition publication without a database.
type recordingPhaseStore struct {
	mu   sync.Mutex
	rows []gpphase.PhaseState
}

func (r *recordingPhaseStore) PublishGraphProjectionPhases(_ context.Context, rows []gpphase.PhaseState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, rows...)
	return nil
}

// publishedPartitions returns the distinct (acceptance-unit, source-run)
// pairs the store published, which equal the (scope, generation) partitions
// for cross-repo evidence phases.
func (r *recordingPhaseStore) publishedPartitions() map[[2]string]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[[2]string]struct{}, len(r.rows))
	for _, row := range r.rows {
		out[[2]string{row.Key.AcceptanceUnitID, row.Key.SourceRunID}] = struct{}{}
	}
	return out
}

// testStore builds a Store over database with recording evidence/phase
// constructors, so engine write paths run without durable collaborators.
func testStore(database db.ExecQueryer) (Store, *recordingEvidenceStore, *recordingPhaseStore) {
	evidence := &recordingEvidenceStore{}
	phases := &recordingPhaseStore{}
	store := NewStore(database)
	store.NewEvidenceStore = func(db.Transaction) EvidenceStore { return evidence }
	store.NewPhaseStore = func(db.Transaction) PhaseStore { return phases }
	return store, evidence, phases
}

// itoa formats a small test integer without importing strconv at every site.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// backfillDeferredSpanName is the span the deferred relationship backfill
// opens in BackfillAllRelationshipEvidence; the fan-out attributes and
// partition failure event must land on it. (Duplicated from the root seam
// tests: same package cannot share test helpers across the seam.)
const backfillDeferredSpanName = "relationship.backfill_deferred"

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

// recordingTracer wires a fresh SpanRecorder to a tracer so a test can read the
// spans BackfillAllRelationshipEvidence emits.
func recordingTracer() (*tracetest.SpanRecorder, trace.Tracer) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")
	return recorder, tracer
}
