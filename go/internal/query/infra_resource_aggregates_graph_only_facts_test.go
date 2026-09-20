// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestInfraAggregateGraphOnlyLabelsServedFromFacts pins issue #6843: the
// graph-only labels (CloudResource, TerraformStateResource) are served from
// the Postgres fact read model, so a hybrid read (here category=terraform,
// which also resolves the mixed-writer labels) runs the graph pass only for
// the indexed mixed-writer seeks. The fact rows carry the same bucket signals
// the graph branches grouped by (source_system/provider, environment
// unknown), so counts equal the graph's without a whole-label scan.
func TestInfraAggregateGraphOnlyLabelsServedFromFacts(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{ready: true, count: []inventory.CountBucket{
		{Label: "TerraformResource", Provider: "aws", Environment: "prod", Count: 6},
		{Label: "TerraformStateResource", Provider: "aws", Environment: "unknown", Count: 5},
	}}
	graph := &stubInfraGraphQuery{responses: map[string][]map[string]any{
		"MATCH (n:TerraformModule)": {
			{"label": "TerraformModule", "provider_bucket": "unknown", "environment_bucket": "unknown", "bucket_count": int64(4)},
		},
	}}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)

	got, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{Category: "terraform"})
	if err != nil {
		t.Fatalf("CountInfraResources() error = %v", err)
	}
	want := InfraResourceAggregateCount{
		TotalResources: 15,
		ByProvider:     map[string]int{"aws": 11, "unknown": 4},
		ByEnvironment:  map[string]int{"prod": 6, "unknown": 9},
		ByLabel:        map[string]int{"TerraformResource": 6, "TerraformStateResource": 5, "TerraformModule": 4},
		Source:         InfraResourceAggregateSourceHybrid,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("count:\n got %+v\nwant %+v", got, want)
	}
	if len(graph.calls) != 1 {
		t.Fatalf("graph calls = %d, want 1 (mixed-writer seeks only)", len(graph.calls))
	}
	cypher := graph.calls[0].Cypher
	for _, label := range inventory.GraphOnlyLabels {
		if strings.Contains(cypher, "MATCH (n:"+label+")") {
			t.Fatalf("graph pass must not read fact-served label %s whole:\n%s", label, cypher)
		}
	}
	if len(readModel.countFilters) != 1 {
		t.Fatalf("read model count calls = %d, want 1", len(readModel.countFilters))
	}
	found := false
	for _, got := range readModel.countFilters[0].Labels {
		if got == "TerraformStateResource" {
			found = true
		}
	}
	if !found {
		t.Fatalf("read model filter labels = %v, want TerraformStateResource included", readModel.countFilters[0].Labels)
	}
}

// TestInfraAggregateCloudCategoryServedFromFacts pins the cloud-category leg
// of #6843: category=cloud reads only CloudResource, which the fact read
// model holds whole, so the graph pass disappears there too.
func TestInfraAggregateCloudCategoryServedFromFacts(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{ready: true, count: []inventory.CountBucket{
		{Label: "CloudResource", Provider: "aws", Environment: "unknown", Count: 4},
	}}
	graph := &stubInfraGraphQuery{}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)

	got, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{Category: "cloud"})
	if err != nil {
		t.Fatalf("cloud CountInfraResources() error = %v", err)
	}
	if got.Source != InfraResourceAggregateSourceReadModel {
		t.Fatalf("cloud source = %q, want read_model (CloudResource served from facts)", got.Source)
	}
	if got.TotalResources != 4 || got.ByLabel["CloudResource"] != 4 {
		t.Fatalf("cloud count = %+v, want 4 CloudResource", got)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("cloud category read the graph %d times, want 0", len(graph.calls))
	}
}
