// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph_test

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestInfraSearchKeyLabelsMatchQueryTaxonomy keeps the schema's search-key
// label set equal to the label set the infra search handler reads
// (querycontract.AllInfraLabels). A label the handler searches without an
// index, or an index on a label it never searches, fails here.
func TestInfraSearchKeyLabelsMatchQueryTaxonomy(t *testing.T) {
	t.Parallel()

	got := graph.InfraSearchKeyLabels()
	want := append([]string(nil), querycontract.AllInfraLabels...)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("graph.InfraSearchKeyLabels() = %v, want querycontract.AllInfraLabels %v", got, want)
	}
}
