// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The production store must provide the corpus-fence gauge observer contract.
var _ telemetry.CorpusFenceObserver = (*QueueObserverStore)(nil)

func TestQueueObserverStoreCorpusFenceCounts(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		{int64(3), "deployable_unit_correlation", int64(782), float64(1866240)},
		{int64(3), "workload_materialization", int64(67), float64(417)},
	}}}}
	observer := NewQueueObserverStore(queryer)
	observer.Now = func() time.Time { return time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC) }

	holders, deferred, oldest, err := observer.CorpusFenceCounts(context.Background())
	if err != nil {
		t.Fatalf("CorpusFenceCounts() error = %v", err)
	}
	if holders != 3 {
		t.Fatalf("holders = %d, want 3", holders)
	}
	if deferred["deployable_unit_correlation"] != 782 || deferred["workload_materialization"] != 67 {
		t.Fatalf("deferred = %+v, want deployable 782 and workload 67", deferred)
	}
	if oldest["deployable_unit_correlation"] != 1866240 {
		t.Fatalf("oldest deployable = %v, want 1866240", oldest["deployable_unit_correlation"])
	}
	// One statement per snapshot refresh: no per-domain or per-item query.
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want exactly 1: %v", len(queryer.queries), queryer.queries)
	}
}

func TestQueueObserverStoreCorpusFenceCountsOpenFence(t *testing.T) {
	t.Parallel()

	// The LEFT JOIN filler row of an open fence with no deferred consumers.
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{
		{int64(0), "", int64(0), float64(0)},
	}}}}
	observer := NewQueueObserverStore(queryer)

	holders, deferred, oldest, err := observer.CorpusFenceCounts(context.Background())
	if err != nil {
		t.Fatalf("CorpusFenceCounts() error = %v", err)
	}
	if holders != 0 || len(deferred) != 0 || len(oldest) != 0 {
		t.Fatalf("open fence = holders %d, deferred %+v, oldest %+v; want all empty", holders, deferred, oldest)
	}
}

// TestCorpusFenceGaugesQueryShape pins the bounded-cost contract: the holder
// count reuses the fence's own predicate (it can never disagree with the
// gate), and the deferred aggregate is the selective per-row active-work
// probe over the three fence consumers' resolution-not-ready classes.
func TestCorpusFenceGaugesQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		incompleteScopeRelationshipGenerationsPredicate,
		"active_fact_work_items AS (",
		"stage = 'reducer'",
		"status = 'retrying'",
		reducer.WorkloadMaterializationResolutionNotReadyFailureClass,
		reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		servicecatalog.ServiceCatalogCorrelationResolutionNotReadyFailureClass,
		string(reducer.DomainWorkloadMaterialization),
		string(reducer.DomainDeployableUnitCorrelation),
		string(reducer.DomainServiceCatalogCorrelation),
		"MIN(created_at)",
	} {
		if !strings.Contains(corpusFenceGaugesQuery, want) {
			t.Fatalf("corpusFenceGaugesQuery missing %q:\n%s", want, corpusFenceGaugesQuery)
		}
	}
}

func TestQueueObserverStoreCorpusFenceCountsQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{err: errors.New("boom")}}}
	observer := NewQueueObserverStore(queryer)

	if _, _, _, err := observer.CorpusFenceCounts(context.Background()); err == nil {
		t.Fatal("CorpusFenceCounts() error = nil, want the query failure")
	}
}
