// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"reflect"
	"testing"
)

// TestPartitionReadLabelsDedupes pins that a repeated label resolves once:
// a repeated graph-only label would otherwise emit its CTEs twice (a
// duplicate-CTE-name SQL error), and a repeated entity label would UNION ALL
// its branch twice (silently doubled counts). Unknown labels still serve no
// branch.
func TestPartitionReadLabelsDedupes(t *testing.T) {
	entity, facts := partitionReadLabels([]string{
		"CloudResource", "TerraformResource", "CloudResource",
		"TerraformStateResource", "TerraformResource", "NoSuchLabel",
		"TerraformStateResource",
	})
	if !reflect.DeepEqual(entity, []string{"TerraformResource"}) {
		t.Fatalf("entity = %v, want [TerraformResource]", entity)
	}
	if !reflect.DeepEqual(facts, []string{"CloudResource", "TerraformStateResource"}) {
		t.Fatalf("facts = %v, want [CloudResource TerraformStateResource]", facts)
	}
}
