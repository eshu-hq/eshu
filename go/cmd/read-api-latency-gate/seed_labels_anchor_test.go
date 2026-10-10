// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
)

// TestSeedLabelsAreAnchorLabels is the proof named by the anchor-census markers
// on the graph seeding statements (#7212). The seeding tool takes each label
// from the fixed infraLabels and iacEntityTypes lists, and writes an id on the
// nodes it creates, so every label in both lists must be a label the Neo4j
// entity-context anchor reaches.
func TestSeedLabelsAreAnchorLabels(t *testing.T) {
	labels := anchor.Labels()
	if len(infraLabels) == 0 || len(iacEntityTypes) == 0 {
		t.Fatal("seed label lists are empty; the proof is not reading the real lists")
	}
	for _, label := range infraLabels {
		if !labels[label] {
			t.Errorf("infra seed label %q is not an anchor label", label)
		}
	}
	for _, label := range iacEntityTypes {
		if !labels[label] {
			t.Errorf("IaC seed label %q is not an anchor label", label)
		}
	}
}
