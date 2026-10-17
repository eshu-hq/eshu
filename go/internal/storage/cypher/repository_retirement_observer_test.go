// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestCanonicalNodeWriterNotifiesRetiredRepositoryObserver proves the #8316
// retirement half of push invalidation end to end at the writer: a committed
// retirement reports its retired Repository ids to the injected observer.
// Retried reports of the same retirement reach the observer once,
// deduplicated.
func TestCanonicalNodeWriterNotifiesRetiredRepositoryObserver(t *testing.T) {
	t.Parallel()

	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{
		cleanupCounters:     WriteCounters{NodesDeleted: 1},
		cleanupDropped:      []DroppedEdge{},
		cleanupRetiredIDs:   []string{"repository:r_old"},
		reportsPerStatement: 2,
	}}
	var observed [][]string
	writer := NewCanonicalNodeWriter(exec, 500, h.instruments).WithRetiredRepositoryObserver(
		func(_ context.Context, repoIDs []string) error {
			observed = append(observed, append([]string(nil), repoIDs...))
			return nil
		})
	if err := writer.Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(observed) != 1 {
		t.Fatalf("observer calls = %d, want 1 deduplicated call", len(observed))
	}
	if !reflect.DeepEqual(observed[0], []string{"repository:r_old"}) {
		t.Fatalf("observed retired ids = %#v, want [repository:r_old]", observed[0])
	}
}

// TestCanonicalNodeWriterObserverErrorNeverFailsTheWrite proves the observer
// is best-effort: the graph delete already committed, so an invalidation
// failure is logged and the write still succeeds. The gap reconciliation
// re-derives the loss.
func TestCanonicalNodeWriterObserverErrorNeverFailsTheWrite(t *testing.T) {
	t.Parallel()

	h := newRetirementHarness(t)
	exec := &retirementGroupExecutor{retirementReportingExecutor{
		cleanupCounters:   WriteCounters{NodesDeleted: 1},
		cleanupDropped:    []DroppedEdge{},
		cleanupRetiredIDs: []string{"repository:r_old"},
	}}
	called := false
	writer := NewCanonicalNodeWriter(exec, 500, h.instruments).WithRetiredRepositoryObserver(
		func(context.Context, []string) error {
			called = true
			return errors.New("postgres unavailable")
		})
	if err := writer.Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v, want nil despite the observer error", err)
	}
	if !called {
		t.Fatal("observer was not called")
	}
}

// TestUnionRetiredRepositoryIDsDedupesAcrossEntries proves the repair set is
// the deduplicated union across retried attempts: repeat reports collapse,
// blank ids drop out, and first-seen order is stable.
func TestUnionRetiredRepositoryIDsDedupesAcrossEntries(t *testing.T) {
	t.Parallel()

	entries := []WriteCountEntry{
		{RetiredIDs: []string{"repo-old-1", ""}},
		{RetiredIDs: []string{"repo-old-2", "repo-old-1"}},
	}
	if got := unionRetiredRepositoryIDs(entries); !reflect.DeepEqual(got, []string{"repo-old-1", "repo-old-2"}) {
		t.Fatalf("unionRetiredRepositoryIDs() = %#v, want [repo-old-1 repo-old-2]", got)
	}
	if got := unionRetiredRepositoryIDs(nil); got != nil {
		t.Fatalf("unionRetiredRepositoryIDs(nil) = %#v, want nil", got)
	}
}

// TestCanonicalNodeWriterSkipsObserverInSteadyState proves no invalidation
// work leaves the writer when the retirement matched nothing.
func TestCanonicalNodeWriterSkipsObserverInSteadyState(t *testing.T) {
	t.Parallel()

	h := newRetirementHarness(t)
	exec := &retirementReportingExecutor{}
	called := false
	writer := NewCanonicalNodeWriter(exec, 500, h.instruments).WithRetiredRepositoryObserver(
		func(context.Context, []string) error {
			called = true
			return nil
		})
	if err := writer.Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if called {
		t.Fatal("observer called for a steady-state retirement that deleted nothing")
	}
}
