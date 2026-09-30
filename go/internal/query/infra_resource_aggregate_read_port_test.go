// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type rejectingInfraAggregateReader struct {
	calls int
	err   error
}

func (r *rejectingInfraAggregateReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	r.calls++
	return nil, r.err
}

func TestInfraAggregateGuardedReadPort(t *testing.T) {
	want := errors.New("reader is stale")
	reader := &rejectingInfraAggregateReader{err: want}
	graph := &stubInfraGraphQuery{}
	store := NewInfraResourceAggregateStoreWithReadStore(graph, reader, nil)
	_, err := store.CountInfraResources(t.Context(), InfraResourceAggregateFilter{Category: "k8s"})
	if !errors.Is(err, want) || reader.calls != 1 || len(graph.calls) != 0 {
		t.Fatalf("error %v, guarded calls %d, graph calls %d; want reader error and no graph fallback", err, reader.calls, len(graph.calls))
	}
}
