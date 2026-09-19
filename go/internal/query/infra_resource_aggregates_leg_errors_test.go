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

// TestInfraAggregateTableLegFailureWinsOverCanceledGraphLeg covers the
// reverse: a table failure cancels the in-flight graph leg, and the table's
// own error is returned, not the graph leg's cancellation.
func TestInfraAggregateTableLegFailureWinsOverCanceledGraphLeg(t *testing.T) {
	t.Parallel()

	readModel := &dimensionFailingInfraReadModel{fakeInfraReadModel{ready: true}}
	store := NewGraphInfraResourceAggregateStore(blockingGraphQuery{}).WithReadModel(readModel)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{}); !errors.Is(err, errTableUnavailable) ||
		errors.Is(err, context.Canceled) {
		t.Errorf("CountInfraResources() error = %v, want the table error and not a cancellation", err)
	}
	if _, _, err := store.InfraResourceInventory(ctx, InfraResourceAggregateFilter{},
		InfraResourceInventoryByProvider, 10, 0); !errors.Is(err, errTableUnavailable) || errors.Is(err, context.Canceled) {
		t.Errorf("InfraResourceInventory() error = %v, want the table error and not a cancellation", err)
	}
}
