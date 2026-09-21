// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// TestInfraAggregateGraphOnlyCategorySkipsReadModelReadiness pins that a
// category whose labels are all fact-served (category=cloud resolves to
// CloudResource alone) never consults the read model's readiness check. Fact
// truth needs no backfill marker: it is current from normal pipeline
// operation, so a marker-table failure must not fail the read. (A Postgres
// table outage falls back to the graph path reporting source graph: the
// fallback is explicit in the truth envelope, never silent.)
func TestInfraAggregateGraphOnlyCategorySkipsReadModelReadiness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	filter := InfraResourceAggregateFilter{Category: "cloud"}

	countModel := &fakeInfraReadModel{readyErr: errors.New("pg down")}
	countGraph := &stubInfraGraphQuery{}
	count, err := NewGraphInfraResourceAggregateStore(countGraph).WithReadModel(countModel).
		CountInfraResources(ctx, filter)
	if err != nil {
		t.Fatalf("cloud CountInfraResources() error = %v, want the facts answer despite the readiness failure", err)
	}
	if count.Source != InfraResourceAggregateSourceReadModel {
		t.Fatalf("cloud count source = %q, want read_model", count.Source)
	}
	if countModel.readyCalls != 0 {
		t.Fatalf("cloud count checked read model readiness %d times, want 0", countModel.readyCalls)
	}
	if len(countGraph.calls) != 0 {
		t.Fatalf("cloud count read the graph %d times, want 0", len(countGraph.calls))
	}

	inventoryModel := &fakeInfraReadModel{readyErr: errors.New("pg down")}
	inventoryGraph := &stubInfraGraphQuery{}
	_, source, err := NewGraphInfraResourceAggregateStore(inventoryGraph).WithReadModel(inventoryModel).
		InfraResourceInventory(ctx, filter, InfraResourceInventoryByProvider, 10, 0)
	if err != nil {
		t.Fatalf("cloud InfraResourceInventory() error = %v, want the facts answer despite the readiness failure", err)
	}
	if source != InfraResourceAggregateSourceReadModel {
		t.Fatalf("cloud inventory source = %q, want read_model", source)
	}
	if inventoryModel.readyCalls != 0 {
		t.Fatalf("cloud inventory checked read model readiness %d times, want 0", inventoryModel.readyCalls)
	}
	if len(inventoryGraph.calls) != 0 {
		t.Fatalf("cloud inventory read the graph %d times, want 0", len(inventoryGraph.calls))
	}
}

// TestInfraAggregateTableCategoryStillChecksReadiness keeps the readiness
// check on every category that resolves at least one read-model label, so the
// graph-only bypass cannot widen into skipping the marker.
func TestInfraAggregateTableCategoryStillChecksReadiness(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{readyErr: errors.New("pg down")}
	graph := &stubInfraGraphQuery{}
	_, err := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel).
		CountInfraResources(context.Background(), InfraResourceAggregateFilter{Category: "k8s"})
	if err == nil {
		t.Fatal("k8s CountInfraResources() error = nil, want the readiness failure surfaced")
	}
	if readModel.readyCalls != 1 {
		t.Fatalf("k8s readiness checks = %d, want 1", readModel.readyCalls)
	}
}
