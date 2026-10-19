// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

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
