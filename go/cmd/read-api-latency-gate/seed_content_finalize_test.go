// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestFinalizeContentSearchIndexesCallsFinalizer is the regression test for
// the #8556 wave failure: the gate bootstrap defers the trigram indexes
// (ESHU_DEFER_CONTENT_SEARCH_INDEXES=true) and the seed flow waited on
// readiness without ever building them, so the probe timed out on a fence
// nothing clears. Finalization must invoke the lifecycle finalizer exactly
// once with a working transaction beginner.
func TestFinalizeContentSearchIndexesCallsFinalizer(t *testing.T) {
	// No t.Parallel: the test stubs the package finalizer seam.

	recorder := stubContentSearchFinalizer(nil)
	defer recorder.restore()

	if err := finalizeContentSearchIndexes(context.Background(), "postgres://gate:invalid@localhost:1/gate"); err != nil {
		t.Fatalf("finalizeContentSearchIndexes() = %v, want nil when the finalizer succeeds", err)
	}
	if recorder.calls != 1 {
		t.Fatalf("finalizer calls = %d, want exactly 1", recorder.calls)
	}
	if recorder.beginner == nil {
		t.Fatal("finalizer beginner is nil, want the opened postgres handle")
	}
}

// TestFinalizeContentSearchIndexesWrapsFinalizerError pins the failure
// contract: a finalizer failure surfaces naming finalization, so the seed
// log points at the build instead of timing out in the readiness probe.
func TestFinalizeContentSearchIndexesWrapsFinalizerError(t *testing.T) {
	// No t.Parallel: the test stubs the package finalizer seam.

	sentinel := errors.New("trigram build exploded")
	recorder := stubContentSearchFinalizer(sentinel)
	defer recorder.restore()

	err := finalizeContentSearchIndexes(context.Background(), "postgres://gate:invalid@localhost:1/gate")
	if err == nil {
		t.Fatal("finalizeContentSearchIndexes() = nil, want the finalizer failure")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("finalizeContentSearchIndexes() = %v, want it to wrap the finalizer failure", err)
	}
	if !strings.Contains(err.Error(), "finalize content search indexes") {
		t.Fatalf("finalizeContentSearchIndexes() = %q, want it to name finalization", err.Error())
	}
}

type finalizeRecorder struct {
	calls    int
	beginner db.Beginner
	previous func(context.Context, db.Beginner) error
}

func (r *finalizeRecorder) restore() { ensureContentSearchIndexesForGate = r.previous }

func stubContentSearchFinalizer(err error) *finalizeRecorder {
	recorder := &finalizeRecorder{previous: ensureContentSearchIndexesForGate}
	ensureContentSearchIndexesForGate = func(_ context.Context, beginner db.Beginner) error {
		recorder.calls++
		recorder.beginner = beginner
		return err
	}
	return recorder
}
