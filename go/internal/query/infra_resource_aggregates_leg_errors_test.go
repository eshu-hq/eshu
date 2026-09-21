// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// blockingInfraReadModel is ready and blocks every table read until its
// context is canceled, the way an in-flight Postgres read does when the
// sibling graph leg fails first.
type blockingInfraReadModel struct{ fakeInfraReadModel }

func (b *blockingInfraReadModel) CountBuckets(ctx context.Context, _ inventory.Filter) ([]inventory.CountBucket, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *blockingInfraReadModel) DimensionBuckets(
	ctx context.Context, _ inventory.Filter, _ inventory.Dimension,
) (map[string]int64, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// errTableUnavailable is a table-leg failure that is not a cancellation.
var errTableUnavailable = errors.New("table unavailable")

// dimensionFailingInfraReadModel fails every table read with
// errTableUnavailable.
type dimensionFailingInfraReadModel struct{ fakeInfraReadModel }

func (f *dimensionFailingInfraReadModel) CountBuckets(context.Context, inventory.Filter) ([]inventory.CountBucket, error) {
	return nil, errTableUnavailable
}

func (f *dimensionFailingInfraReadModel) DimensionBuckets(
	context.Context, inventory.Filter, inventory.Dimension,
) (map[string]int64, error) {
	return nil, errTableUnavailable
}

// TestInfraAggregateGraphLegFailureWinsOverCanceledTableLeg pins the error
// precedence of the concurrent read-model read. When the graph leg fails
// with a bounded-read sentinel, it cancels the in-flight table leg, which
// then reports context.Canceled. The returned error must be the graph
// sentinel, so the handler maps it to 504 or 503, not the cancellation, which
// would surface as a generic 500.
func TestInfraAggregateGraphLegFailureWinsOverCanceledTableLeg(t *testing.T) {
	t.Parallel()

	for _, sentinel := range []error{ErrGraphReadDeadline, ErrGraphUnavailable} {
		readModel := &blockingInfraReadModel{fakeInfraReadModel{ready: true}}
		store := NewGraphInfraResourceAggregateStore(&stubInfraGraphQuery{err: sentinel}).WithReadModel(readModel)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		if _, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{}); !errors.Is(err, sentinel) {
			t.Errorf("CountInfraResources() error = %v, want errors.Is %v", err, sentinel)
		}
		if _, _, err := store.InfraResourceInventory(ctx, InfraResourceAggregateFilter{},
			InfraResourceInventoryByProvider, 10, 0); !errors.Is(err, sentinel) {
			t.Errorf("InfraResourceInventory() error = %v, want errors.Is %v", err, sentinel)
		}
		cancel()
	}
}

// TestInfraAggregateHandlersMapGraphLegFailureTo504And503 drives the same
// race through the HTTP handler: a graph deadline must answer 504 and an
// unavailable graph 503 on both routes, never 500.
func TestInfraAggregateHandlersMapGraphLegFailureTo504And503(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		sentinel error
		want     int
	}{{ErrGraphReadDeadline, http.StatusGatewayTimeout}, {ErrGraphUnavailable, http.StatusServiceUnavailable}} {
		for _, path := range []string{"/api/v0/infra/resources/count", "/api/v0/infra/resources/inventory"} {
			readModel := &blockingInfraReadModel{fakeInfraReadModel{ready: true}}
			store := NewGraphInfraResourceAggregateStore(&stubInfraGraphQuery{err: tc.sentinel}).WithReadModel(readModel)
			handler := &InfraHandler{Aggregates: store, Profile: ProfileProduction}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("%s with %v: status = %d, want %d; body = %s", path, tc.sentinel, w.Code, tc.want, w.Body.String())
			}
		}
	}
}

// TestInfraAggregateTableLegFailureFallsBackToGraph pins the P2-2
// availability contract: when the Postgres table leg fails, the read falls
// back to the full graph path (the pre-#6843 authority for every label) and
// reports source graph, instead of failing the route. A Postgres outage must
// not fail an answer the graph holds whole.
func TestInfraAggregateTableLegFailureFallsBackToGraph(t *testing.T) {
	t.Parallel()

	graph := &stubInfraGraphQuery{responses: map[string][]map[string]any{
		"RETURN bucket_count": {
			{"bucket_count": 2},
			{"bucket_count": 1},
		},
		"AS bucket, count(n)": {
			{"bucket": "aws", "bucket_count": 2},
			{"bucket": "gcp", "bucket_count": 1},
		},
	}}
	readModel := &dimensionFailingInfraReadModel{fakeInfraReadModel{ready: true}}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{})
	if err != nil {
		t.Fatalf("CountInfraResources() error = %v, want the graph fallback answer", err)
	}
	if count.Source != InfraResourceAggregateSourceGraph {
		t.Fatalf("CountInfraResources() source = %q, want graph fallback", count.Source)
	}
	if count.TotalResources != 3 {
		t.Fatalf("CountInfraResources() total = %d, want 3 from the graph fallback", count.TotalResources)
	}
	if count.ByProvider["aws"] != 2 || count.ByProvider["gcp"] != 1 {
		t.Fatalf("CountInfraResources() by_provider = %v, want map[aws:2 gcp:1] from the graph fallback", count.ByProvider)
	}

	rows, source, err := store.InfraResourceInventory(ctx, InfraResourceAggregateFilter{},
		InfraResourceInventoryByProvider, 10, 0)
	if err != nil {
		t.Fatalf("InfraResourceInventory() error = %v, want the graph fallback answer", err)
	}
	if source != InfraResourceAggregateSourceGraph {
		t.Fatalf("InfraResourceInventory() source = %q, want graph fallback", source)
	}
	if len(rows) != 2 || rows[0].Value != "aws" || rows[0].Count != 2 {
		t.Fatalf("InfraResourceInventory() rows = %+v, want [{aws 2} {gcp 1}] from the graph fallback", rows)
	}
}

// TestInfraAggregateTableFailureWithBrokenGraphReportsTableError pins that
// the fallback does not retry a proven-broken backend: when the table leg
// fails and the concurrent graph leg already failed genuinely, the table
// error is returned immediately with no second graph call, instead of a slow
// doomed fallback.
func TestInfraAggregateTableFailureWithBrokenGraphReportsTableError(t *testing.T) {
	t.Parallel()

	readModel := &dimensionFailingInfraReadModel{fakeInfraReadModel{ready: true}}
	graph := &stubInfraGraphQuery{err: ErrGraphUnavailable}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{}); !errors.Is(err, errTableUnavailable) {
		t.Errorf("CountInfraResources() error = %v, want the table error without a fallback retry", err)
	}
	if _, _, err := store.InfraResourceInventory(ctx, InfraResourceAggregateFilter{},
		InfraResourceInventoryByProvider, 10, 0); !errors.Is(err, errTableUnavailable) {
		t.Errorf("InfraResourceInventory() error = %v, want the table error without a fallback retry", err)
	}
	// One graph call per route (the canceled concurrent mixed-writer leg);
	// the fallback must not have run.
	if len(graph.calls) != 2 {
		t.Errorf("graph calls = %d, want 2 (no fallback retry against a broken backend)", len(graph.calls))
	}
}

// TestInfraAggregateTableLegFailureFallsBackBeforeCancelWins covers the
// reverse race under the P2-2 contract: a table failure cancels the in-flight
// graph leg, then the fallback re-reads the graph sequentially. Against a hung
// graph the fallback fails with the caller's own deadline: never the table
// error (the graph was given its chance) and never a bare cancellation.
func TestInfraAggregateTableLegFailureFallsBackBeforeCancelWins(t *testing.T) {
	t.Parallel()

	readModel := &dimensionFailingInfraReadModel{fakeInfraReadModel{ready: true}}
	store := NewGraphInfraResourceAggregateStore(blockingGraphQuery{}).WithReadModel(readModel)
	// Each route gets a fresh deadline: the count fallback consumes its whole
	// budget blocking on the hung graph, and the inventory fallback needs its
	// own live budget to prove the same contract.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{}); !errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errTableUnavailable) || errors.Is(err, context.Canceled) {
		t.Errorf("CountInfraResources() error = %v, want the fallback deadline without the table error or a cancellation", err)
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := store.InfraResourceInventory(ctx, InfraResourceAggregateFilter{},
		InfraResourceInventoryByProvider, 10, 0); !errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errTableUnavailable) || errors.Is(err, context.Canceled) {
		t.Errorf("InfraResourceInventory() error = %v, want the fallback deadline without the table error or a cancellation", err)
	}
}
