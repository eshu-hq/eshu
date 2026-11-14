// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// repositoryManifestFreshnessRow is the freshness lookup row for repo-1's
// active generation.
func repositoryManifestFreshnessRow() []any {
	return []any{"generation-1", "active", sql.NullTime{Time: time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC), Valid: true}}
}

// TestListActiveRepositoryManifestDependencyFactsFailsOnAMissingBase pins the
// #10660 serving read on a stub queryer: the scope is delta-active and the
// base lookup finds no full generation at its commit. The read fails with the
// non-retryable ManifestDeltaBaseRefusedError before returning rows, so query
// serving never presents a delta-only page as the repository's complete
// manifest.
func TestListActiveRepositoryManifestDependencyFactsFailsOnAMissingBase(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestDependencyFakeRow(0)}}, // active page
		crossplaneRefusalTarget,                       // delta-target lookup
		{},                                            // delta-base page
		{rows: [][]any{repositoryManifestFreshnessRow()}}, // freshness lookup
		{}, // base lookup: no base
	}}
	loaded, freshness, refused, err := NewFactStore(db).WithInstruments(instruments).ListActiveRepositoryManifestDependencyFacts(
		context.Background(), "repo-1", nil, "", 201,
	)
	var refusal *ManifestDeltaBaseRefusedError
	if !errors.As(err, &refusal) {
		t.Fatalf("read error = %v, want *ManifestDeltaBaseRefusedError", err)
	}
	if !refused {
		t.Fatal("refused = false, want true for a missing delta base")
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded rows = %d, want 0: the refusing read returns no fallback rows", len(loaded))
	}
	// The freshness lookup rides the same snapshot, so it is populated even
	// when the overlay refuses the base; the handler answers 503 on refused
	// and never serves the freshness beside partial rows.
	if !freshness.ScopeFound || freshness.ActiveGenerationID != "generation-1" ||
		freshness.GenerationStatus != "active" {
		t.Fatalf("freshness = %+v, want the stubbed active generation", freshness)
	}
	if refusal.ScopeID != "repository:repo-1" || refusal.GenerationID != "generation-1" ||
		refusal.Outcome != deltaOverlayBaseMissing {
		t.Fatalf("refusal = %+v, want repo-1 generation-1 union_base_missing", refusal)
	}
	// The refusing read runs one statement more than the sibling pair-probe
	// refusal: the freshness lookup rides the same snapshot.
	if got := len(db.queries); got != 5 {
		t.Fatalf("statements = %d, want 5: active, targets, base, freshness, resolve", got)
	}
	if got := deltaUnionOutcomeCount(t, reader, deltaOverlayBaseMissing); got != 1 {
		t.Fatalf("union_base_missing = %d, want 1 counted refusal", got)
	}
}

// TestListActiveRepositoryManifestDependencyFactsRejectsEmptyScopeAndLimit
// pins the fail-closed edges: an empty scope returns no rows without a
// statement, and a non-positive fetch bound fails loudly instead of
// widening into an unbounded read.
func TestListActiveRepositoryManifestDependencyFactsRejectsEmptyScopeAndLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFactStore(db)
	loaded, freshness, refused, err := store.ListActiveRepositoryManifestDependencyFacts(context.Background(), "", nil, "", 201)
	if err != nil {
		t.Fatalf("empty scope error = %v, want nil", err)
	}
	if refused || len(loaded) != 0 || freshness.ScopeFound {
		t.Fatalf("empty scope = (%d rows, %+v, refused=%v), want empty", len(loaded), freshness, refused)
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("empty scope statements = %d, want 0", got)
	}

	if _, _, _, err := store.ListActiveRepositoryManifestDependencyFacts(context.Background(), "repo-1", nil, "", 0); err == nil {
		t.Fatal("fetch limit 0 error = nil, want the required-limit error")
	}

	if _, _, _, err := NewFactStore(nil).ListActiveRepositoryManifestDependencyFacts(context.Background(), "repo-1", nil, "", 201); err == nil {
		t.Fatal("nil database error = nil, want the required-database error")
	}
}
