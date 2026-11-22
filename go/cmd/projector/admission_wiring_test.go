// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reduceradmission"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// admissionCountingDB is an exec-capable database handle that answers every
// query with no rows and counts them, so a test can prove which gates read
// the queue.
type admissionCountingDB struct {
	mu      sync.Mutex
	queries int
}

type admissionEmptyRows struct{}

func (admissionEmptyRows) Next() bool        { return false }
func (admissionEmptyRows) Scan(...any) error { return nil }
func (admissionEmptyRows) Err() error        { return nil }
func (admissionEmptyRows) Close() error      { return nil }

func (d *admissionCountingDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queries++
	return admissionEmptyRows{}, nil
}

func (d *admissionCountingDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return admissionResult{}, nil
}

func (d *admissionCountingDB) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.queries
}

type admissionResult struct{}

func (admissionResult) LastInsertId() (int64, error) { return 0, nil }
func (admissionResult) RowsAffected() (int64, error) { return 1, nil }

type admissionRecordingWriter struct{ calls int }

func (w *admissionRecordingWriter) Enqueue(_ context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error) {
	w.calls++
	return runtime.IntentResult{Count: len(intents)}, nil
}

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// TestProjectorAdmissionWriterIsOffByDefault pins #10882 F2: the standalone
// projector never had the global admission gate, and enabling the per-source
// leg must not turn it on. An empty env returns the queue writer unwrapped.
func TestProjectorAdmissionWriterIsOffByDefault(t *testing.T) {
	t.Parallel()

	inner := &admissionRecordingWriter{}
	database := &admissionCountingDB{}
	writer, err := projectorAdmissionIntentWriter(database, inner, envFrom(nil), nil, nil)
	if err != nil {
		t.Fatalf("projectorAdmissionIntentWriter() error = %v, want nil", err)
	}
	if writer != runtime.ReducerIntentWriter(inner) {
		t.Fatalf("empty env wrapped the writer (%T): the projector global gate must default off", writer)
	}
}

// TestProjectorAdmissionWriterSourceLegDoesNotEnableGlobalGate proves the
// per-source leg alone wraps the writer but runs zero queue-depth reads per
// enqueue: the global gate (QueueDepths, graph-write-timeout depth) stays off
// unless the operator sets a mark for the projector process.
func TestProjectorAdmissionWriterSourceLegDoesNotEnableGlobalGate(t *testing.T) {
	t.Parallel()

	inner := &admissionRecordingWriter{}
	database := &admissionCountingDB{}
	env := envFrom(map[string]string{
		"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE":     "1h",
		"ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE":    "15m",
		"ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR":    "25",
		"ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY":    "2s",
		"ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT": "30m",
	})
	writer, err := projectorAdmissionIntentWriter(database, inner, env, nil, nil)
	if err != nil {
		t.Fatalf("projectorAdmissionIntentWriter() error = %v, want nil", err)
	}
	if writer == runtime.ReducerIntentWriter(inner) {
		t.Fatal("per-source leg configured but the writer is unwrapped")
	}
	// Unattributed intents skip the per-source consult, so any query here
	// would be the global gate reading queue depth.
	if _, err := writer.Enqueue(context.Background(), []runtime.ReducerIntent{{EntityKey: "e1"}}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner enqueue calls = %d, want 1", inner.calls)
	}
	if got := database.count(); got != 0 {
		t.Fatalf("queue reads per enqueue = %d, want 0: the per-source leg must not enable the global gate", got)
	}
}

// TestProjectorAdmissionWriterExplicitMarkEnablesGlobalGate proves an
// operator who sets a mark for the projector process gets the global gate.
func TestProjectorAdmissionWriterExplicitMarkEnablesGlobalGate(t *testing.T) {
	t.Parallel()

	inner := &admissionRecordingWriter{}
	database := &admissionCountingDB{}
	env := envFrom(map[string]string{"ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK": "100"})
	writer, err := projectorAdmissionIntentWriter(database, inner, env, nil, nil)
	if err != nil {
		t.Fatalf("projectorAdmissionIntentWriter() error = %v, want nil", err)
	}
	if writer == runtime.ReducerIntentWriter(inner) {
		t.Fatal("explicit high-water mark but the writer is unwrapped")
	}
	if _, err := writer.Enqueue(context.Background(), []runtime.ReducerIntent{{EntityKey: "e1"}}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if got := database.count(); got == 0 {
		t.Fatal("queue reads per enqueue = 0, want the global gate to read queue depth")
	}
}

// TestProjectorSourcePacerIsOffUnlessConfigured pins #10882 F8's wiring: the
// projector service takes a pre-write source consult only when the per-source
// leg is configured, and a disabled leg is an untyped nil (a typed nil
// pointer would pass the service's nil check and be consulted).
func TestProjectorSourcePacerIsOffUnlessConfigured(t *testing.T) {
	t.Parallel()

	database := &admissionCountingDB{}
	off, err := reduceradmission.NewSourcePacer(database, envFrom(nil), nil, nil)
	if err != nil || off != nil {
		t.Fatalf("NewSourcePacer(empty env) = (%v, %v), want (nil, nil)", off, err)
	}
	on, err := reduceradmission.NewSourcePacer(database, envFrom(map[string]string{
		"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE":     "1h",
		"ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE":    "15m",
		"ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR":    "25",
		"ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY":    "2s",
		"ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT": "30m",
	}), nil, nil)
	if err != nil || on == nil {
		t.Fatalf("NewSourcePacer(configured) = (%v, %v), want a pacer", on, err)
	}
}
