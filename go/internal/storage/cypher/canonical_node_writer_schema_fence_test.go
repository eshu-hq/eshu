// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// These cover the write-path half of the #6102 stale-writer fence. The schema
// compatibility gate runs once, at process startup, so it decides only whether
// a writer may START. A writer already past that gate keeps writing across a
// schema application recorded underneath it -- which during a Helm upgrade is
// every pod of the previous generation, because the schema-bootstrap Job is a
// pre-upgrade hook. WithSchemaWriteFence is what makes the decision reach a
// write that is already in flight.

// TestCanonicalNodeWriterStopsAtARefusingSchemaFence proves the refusal lands
// before any statement executes. Checking only that Write returns an error
// would pass even if the whole materialization had already been written.
func TestCanonicalNodeWriterStopsAtARefusingSchemaFence(t *testing.T) {
	t.Parallel()

	refusal := errors.New("graph schema incompatible for backend nornicdb: runtime expects fingerprint aaa")
	exec := &mockExecutor{}
	fenceCalls := 0
	writer := NewCanonicalNodeWriter(exec, 500, nil).
		WithSchemaWriteFence(func(context.Context) error {
			fenceCalls++
			return refusal
		})

	err := writer.Write(context.Background(), phaseOrderMaterialization())
	if err == nil {
		t.Fatal("Write() error = nil, want the schema refusal")
	}
	if !errors.Is(err, refusal) {
		t.Fatalf("Write() error = %v, want it to wrap the fence refusal", err)
	}
	if fenceCalls != 1 {
		t.Fatalf("fence consulted %d times, want exactly 1 per write", fenceCalls)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("executed %d statements after the fence refused; a refused writer must write nothing:\n%v",
			len(exec.calls), exec.calls)
	}
	// Terminal here would dead-letter a backlog the operator still wants
	// projected by the pod that replaces this one.
	if !projector.IsRetryable(err) {
		t.Fatalf("Write() error = %v is terminal, want retryable so the work stays queued", err)
	}
}

// TestCanonicalNodeWriterWritesWhenTheSchemaFenceAdmits is the other direction:
// an admitting fence must not cost the write anything.
func TestCanonicalNodeWriterWritesWhenTheSchemaFenceAdmits(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	fenceCalls := 0
	writer := NewCanonicalNodeWriter(exec, 500, nil).
		WithSchemaWriteFence(func(context.Context) error {
			fenceCalls++
			return nil
		})

	if err := writer.Write(context.Background(), phaseOrderMaterialization()); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if fenceCalls != 1 {
		t.Fatalf("fence consulted %d times, want exactly 1 per write", fenceCalls)
	}
	if len(exec.calls) == 0 {
		t.Fatal("executed no statements; an admitted write must still write")
	}
}

// TestCanonicalNodeWriterWithoutASchemaFenceStillWrites pins the default. Every
// test in this package, and every writer built without the option, must behave
// exactly as it did before the fence existed.
func TestCanonicalNodeWriterWithoutASchemaFenceStillWrites(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	if err := writer.Write(context.Background(), phaseOrderMaterialization()); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if len(exec.calls) == 0 {
		t.Fatal("executed no statements without a fence configured")
	}
}
