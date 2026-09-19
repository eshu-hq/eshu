// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// TestInfraAggregateGraphOnlyCategorySkipsReadModelReadiness pins that a
// category whose labels are all graph-only (category=cloud resolves to
// CloudResource alone) never consults the read model's readiness check. The
// complete answer lives in the graph, so a Postgres or marker-table failure
// must not fail it.
func TestInfraAggregateGraphOnlyCategorySkipsReadModelReadiness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	filter := InfraResourceAggregateFilter{Category: "cloud"}

	countModel := &fakeInfraReadModel{readyErr: errors.New("pg down")}
	countGraph := &stubInfraGraphQuery{}
	count, err := NewGraphInfraResourceAggregateStore(countGraph).WithReadModel(countModel).
		CountInfraResources(ctx, filter)
	if err != nil {
		t.Fatalf("cloud CountInfraResources() error = %v, want the graph answer despite the read model failure", err)
	}
	if count.Source != InfraResourceAggregateSourceGraph {
		t.Fatalf("cloud count source = %q, want graph", count.Source)
	}
	if countModel.readyCalls != 0 {
		t.Fatalf("cloud count checked read model readiness %d times, want 0", countModel.readyCalls)
	}
	if len(countGraph.calls) == 0 {
		t.Fatal("cloud count did not read the graph")
	}

	inventoryModel := &fakeInfraReadModel{readyErr: errors.New("pg down")}
	inventoryGraph := &stubInfraGraphQuery{}
	_, source, err := NewGraphInfraResourceAggregateStore(inventoryGraph).WithReadModel(inventoryModel).
		InfraResourceInventory(ctx, filter, InfraResourceInventoryByProvider, 10, 0)
	if err != nil {
		t.Fatalf("cloud InfraResourceInventory() error = %v, want the graph answer despite the read model failure", err)
	}
	if source != InfraResourceAggregateSourceGraph {
		t.Fatalf("cloud inventory source = %q, want graph", source)
	}
	if inventoryModel.readyCalls != 0 {
		t.Fatalf("cloud inventory checked read model readiness %d times, want 0", inventoryModel.readyCalls)
	}
	if len(inventoryGraph.calls) == 0 {
		t.Fatal("cloud inventory did not read the graph")
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
