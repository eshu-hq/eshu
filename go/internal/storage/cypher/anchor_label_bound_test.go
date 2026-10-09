// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
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

	// The single-row writer takes its label from the row's EntityType, which
	// buildSemanticEntityRowMap filters with its own inline list, not with
	// semanticEntityPlans. Offer it a valid row for every candidate type and
	// require that every type it accepts is an anchor label, so a type added to
	// the filter alone cannot reach the writer.
	candidates := map[string]bool{"Directory": true, "File": true, "Parameter": true, "Unconstrained": true}
	for _, plan := range plans {
		candidates[plan.label] = true
	}
	for _, label := range canonical.EntityTypeLabelMap() {
		candidates[label] = true
	}
	var accepted []string
	for candidate := range candidates {
		row := semantic.EntityRow{
			RepoID: "repo", EntityID: "entity", EntityName: "name", FilePath: "/f", RelativePath: "f",
			EntityType: candidate, StartLine: 1, EndLine: 1,
		}
		if _, ok := buildSemanticEntityRowMap(row); !ok {
			continue
		}
		accepted = append(accepted, candidate)
		if !labels[candidate] {
			t.Errorf("buildSemanticEntityRowMap accepts entity type %q, which is not an anchor label", candidate)
		}
	}
	if len(accepted) < len(plans) {
		t.Fatalf("buildSemanticEntityRowMap accepted %d of %d plan types; the candidate rows are not valid", len(accepted), len(plans))
	}
}
