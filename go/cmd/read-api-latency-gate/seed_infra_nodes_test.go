// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestInfraNodeRowsAreLowCardinalityAndPrecomputed guards a real gate bug
// (issue #6797): the bulk seed put `CASE i % 3 WHEN 0 THEN 'aws' ...` inside a
// CREATE property map, and NornicDB stores that expression as literal text with
// the loop variable substituted, so every node got a UNIQUE provider and
// environment string (count(DISTINCT n.provider) = 150,000). The infra
// aggregate then grouped 150,000 buckets instead of 3 and 2. The values are
// now computed in Go and sent as parameters.
func TestInfraNodeRowsAreLowCardinalityAndPrecomputed(t *testing.T) {
	rows := infraNodeRows("K8sResource", idRange{First: 0, Last: 299})

	if len(rows) != 300 {
		t.Fatalf("len(rows) = %d, want 300 (inclusive range)", len(rows))
	}
	providers := map[string]bool{}
	environments := map[string]bool{}
	for _, r := range rows {
		providers[r["provider"].(string)] = true
		environments[r["environment"].(string)] = true
	}
	if len(providers) != 3 {
		t.Errorf("distinct providers = %d (%v), want 3", len(providers), providers)
	}
	if len(environments) != 2 {
		t.Errorf("distinct environments = %d (%v), want 2", len(environments), environments)
	}
	for v := range providers {
		if v != "aws" && v != "gcp" && v != "azure" {
			t.Errorf("provider %q is not a real provider name", v)
		}
	}
}

func TestInfraNodeRowsUseTheSharedSeedIdentity(t *testing.T) {
	rows := infraNodeRows("HelmChart", idRange{First: 10, Last: 12})

	want := []string{"HelmChart-seed-10", "HelmChart-seed-11", "HelmChart-seed-12"}
	for i, id := range want {
		if rows[i]["id"] != id {
			t.Errorf("rows[%d].id = %v, want %s: the graph node id must equal the content_entities entity_id for parity", i, rows[i]["id"], id)
		}
		if rows[i]["source_system"] != "HelmChart" {
			t.Errorf("rows[%d].source_system = %v, want HelmChart", i, rows[i]["source_system"])
		}
	}
}
