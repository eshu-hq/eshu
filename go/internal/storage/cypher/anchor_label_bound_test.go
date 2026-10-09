// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// TestEntityUpsertTemplateLabelsAreAnchorLabels is the proof named by the
// anchor-census markers on the canonical entity upsert templates (#7212). The
// templates take their label at write time from the projector's entity label
// map, and the row builder drops Module and Parameter before the generic entity
// phase (they have dedicated phases with their own keys, see
// projector/canonical/builder.go). Every other label in that closed map must be
// a label the Neo4j entity-context anchor reaches, so an id that arrives through
// the row props can never land on a node the anchor cannot find.
func TestEntityUpsertTemplateLabelsAreAnchorLabels(t *testing.T) {
	t.Parallel()

	labels := anchor.Labels()
	var outside []string
	for entityType, label := range canonical.EntityTypeLabelMap() {
		if label == "Parameter" || labels[label] {
			continue
		}
		outside = append(outside, entityType+" -> "+label)
	}
	if len(outside) > 0 {
		sort.Strings(outside)
		t.Fatalf("canonical entity labels the anchor cannot reach (give each a uid or id uniqueness constraint, or keep it out of the generic entity phase): %v", outside)
	}
	if len(canonical.EntityTypeLabelMap()) < 50 {
		t.Fatalf("entity label map has %d entries; the proof is not reading the real map", len(canonical.EntityTypeLabelMap()))
	}
}

// TestSemanticEntityUpsertLabelsAreAnchorLabels is the proof named by the
// anchor-census markers on the semantic entity upsert statements: the labels come
// from the closed semanticEntityPlans list, and each must be an anchor label.
func TestSemanticEntityUpsertLabelsAreAnchorLabels(t *testing.T) {
	t.Parallel()

	labels := anchor.Labels()
	plans := semanticEntityPlans()
	if len(plans) == 0 {
		t.Fatal("semanticEntityPlans is empty; the proof is not reading the real list")
	}
	var outside []string
	for _, plan := range plans {
		if !labels[plan.label] {
			outside = append(outside, plan.label)
		}
	}
	if len(outside) > 0 {
		sort.Strings(outside)
		t.Fatalf("semantic entity labels the anchor cannot reach: %v", outside)
	}
}
