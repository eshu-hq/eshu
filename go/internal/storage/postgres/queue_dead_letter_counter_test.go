// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	storagecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// #7386: a dead-letter is the terminal outcome of a work item, and until now no
// counter said how many there were or why. eshu_dp_queue_dead_letters_total
// counts them at the two Fail paths, by queue and a bounded failure_class.

const deadLetterMetric = "eshu_dp_queue_dead_letters_total"

// rowsResult is a Postgres result reporting a fixed rows-affected count.
type rowsResult int64

func (rowsResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

// wireClassError is a non-retryable cause that names its own failure class.
type wireClassError struct{ class string }

func (e wireClassError) Error() string        { return "boom" }
func (e wireClassError) FailureClass() string { return e.class }

func newDeadLetterInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// deadLetterPoints returns the counter's data points keyed "queue/failure_class".
func deadLetterPoints(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	points := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != deadLetterMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", deadLetterMetric, m.Data)
			}
			for _, p := range sum.DataPoints {
				queue, _ := p.Attributes.Value(attribute.Key("queue"))
				class, _ := p.Attributes.Value(attribute.Key(telemetry.MetricDimensionFailureClass))
				points[queue.AsString()+"/"+class.AsString()] += p.Value
			}
		}
	}
	return points
}

func requirePoints(t *testing.T, got, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s points = %v, want %v", deadLetterMetric, got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s points = %v, want %v", deadLetterMetric, got, want)
		}
	}
}

func projectorFailWork() projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-1"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-1"},
		AttemptCount: 1,
	}
}

func newDeadLetterProjectorQueue(db *recordingExecQueryer, inst *telemetry.Instruments) ProjectorQueue {
	queue := NewProjectorQueue(db, "projector-1", time.Minute)
	queue.Instruments = inst
	queue.MaxAttempts = 3
	return queue
}

func TestProjectorFailRecordsDeadLetterByStoredClass(t *testing.T) {
	t.Parallel()
	inst, reader := newDeadLetterInstruments(t)
	db := &recordingExecQueryer{}
	queue := newDeadLetterProjectorQueue(db, inst)

	if err := queue.Fail(context.Background(), projectorFailWork(), errors.New("bad input")); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	stored, _ := db.execs[0].args[1].(string)
	if stored == "" {
		t.Fatal("dead-letter stored no failure class")
	}
	requirePoints(t, deadLetterPoints(t, reader), map[string]int64{"projector/" + stored: 1})
}

func TestProjectorFailRetryWithAttemptsLeftRecordsNoDeadLetter(t *testing.T) {
	t.Parallel()
	inst, reader := newDeadLetterInstruments(t)
	queue := newDeadLetterProjectorQueue(&recordingExecQueryer{}, inst)

	if err := queue.Fail(context.Background(), projectorFailWork(), &retryableTestError{message: "transient"}); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	requirePoints(t, deadLetterPoints(t, reader), map[string]int64{})
}

func TestProjectorFailRejectedClaimRecordsNoDeadLetter(t *testing.T) {
	t.Parallel()
	inst, reader := newDeadLetterInstruments(t)
	queue := newDeadLetterProjectorQueue(&recordingExecQueryer{result: rowsResult(0)}, inst)

	err := queue.Fail(context.Background(), projectorFailWork(), errors.New("bad input"))
	if !errors.Is(err, ErrProjectorClaimRejected) {
		t.Fatalf("Fail() error = %v, want ErrProjectorClaimRejected", err)
	}
	requirePoints(t, deadLetterPoints(t, reader), map[string]int64{})
}

func TestProjectorFailSelfClassifyingCauseLabelsWithItsStoredClass(t *testing.T) {
	t.Parallel()
	inst, reader := newDeadLetterInstruments(t)
	db := &recordingExecQueryer{}
	queue := newDeadLetterProjectorQueue(db, inst)
	work := projectorFailWork()
	work.AttemptCount = queue.MaxAttempts // attempts exhausted: a retryable cause dead-letters

	cause := storagecypher.GraphWriteTimeoutError{Operation: "write", Timeout: time.Second}
	if err := queue.Fail(context.Background(), work, cause); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if got := db.execs[0].args[1]; got != "graph_write_timeout" {
		t.Fatalf("stored failure class = %v, want graph_write_timeout", got)
	}
	requirePoints(t, deadLetterPoints(t, reader), map[string]int64{"projector/graph_write_timeout": 1})
}

func TestProjectorFailLabelsAnUnboundedClassAsOther(t *testing.T) {
	t.Parallel()
	for name, class := range map[string]string{
		"punctuation": "Weird/Class:1",
		"uppercase":   "Projection_Bug",
		"empty":       "",
		"too long":    strings.Repeat("a", 65),
	} {
		inst, reader := newDeadLetterInstruments(t)
		db := &recordingExecQueryer{}
		queue := newDeadLetterProjectorQueue(db, inst)
		cause := wireClassError{class: class}
		if err := queue.Fail(context.Background(), projectorFailWork(), cause); err != nil {
			t.Fatalf("%s: Fail() error = %v", name, err)
		}
		stored, _ := db.execs[0].args[1].(string)
		if name == "empty" {
			// An empty class falls back to the triage class, which is a bounded
			// constant: the label is the stored class, not "other".
			requirePoints(t, deadLetterPoints(t, reader), map[string]int64{"projector/" + stored: 1})
			continue
		}
		if stored != cause.class {
			t.Fatalf("%s: stored failure class = %q, want the raw %q (only the label is bounded)", name, stored, cause.class)
		}
		requirePoints(t, deadLetterPoints(t, reader), map[string]int64{"projector/other": 1})
	}
}

func TestReducerFailIntentRecordsDeadLetterWithQueueReducer(t *testing.T) {
	t.Parallel()
	inst, reader := newDeadLetterInstruments(t)
	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		database: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute,
		MaxAttempts: 3, Now: func() time.Time { return time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) },
		Instruments: inst,
	}

	if err := queue.Fail(context.Background(), reducer.Intent{IntentID: "intent-1", AttemptCount: 1}, errors.New("bad input")); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	stored, _ := db.execs[0].args[1].(string)
	requirePoints(t, deadLetterPoints(t, reader), map[string]int64{"reducer/" + stored: 1})
}

func TestReducerFailIntentRejectedClaimRecordsNoDeadLetter(t *testing.T) {
	t.Parallel()
	inst, reader := newDeadLetterInstruments(t)
	db := &fakeExecQueryer{execResults: []sql.Result{rowsResult(0)}}
	queue := ReducerQueue{
		database: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute,
		MaxAttempts: 3, Now: func() time.Time { return time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) },
		Instruments: inst,
	}

	err := queue.Fail(context.Background(), reducer.Intent{IntentID: "intent-1", AttemptCount: 1}, errors.New("bad input"))
	if !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("Fail() error = %v, want ErrReducerClaimRejected", err)
	}
	requirePoints(t, deadLetterPoints(t, reader), map[string]int64{})
}

// BenchmarkRecordQueueDeadLetter measures what the counter adds to a Fail path
// that dead-letters: one bounded-label check and one counter Add, as the two
// call sites do it.
func BenchmarkRecordQueueDeadLetter(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("bench"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		inst.QueueDeadLetters.Add(ctx, 1, metric.WithAttributes(
			attribute.String("queue", "projector"),
			telemetry.AttrFailureClass(queuestore.BoundedFailureClassLabel("graph_write_timeout")),
		))
	}
}
