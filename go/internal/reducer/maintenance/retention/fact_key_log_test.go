// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import "testing"

// factKeyCounts is a #7420 key sidecar pre-count, keyed like RowsPruned.
func factKeyCounts(relationship, manifest, registry int64) map[string]int64 {
	return map[string]int64{
		"relationship_reference_candidate_keys": relationship,
		"package_manifest_consumption_keys":     manifest,
		"package_registry_identity_keys":        registry,
	}
}

// warnRecords returns the WARN records of one cycle log.
func warnRecords(records []map[string]any) []map[string]any {
	var warns []map[string]any
	for _, record := range records {
		if record["level"] == "WARN" {
			warns = append(warns, record)
		}
	}
	return warns
}

// TestGenerationRetentionWarnsWhenFactKeyDeleteDiffersFromCount is the #7420
// review carry-over: when the whole-generation key sidecar delete removes a
// different number of rows than the batch's pre-count (keys written or
// orphaned between the two), the cycle logs one WARN naming both figures. It
// is a WARN, not a failure: the cycle still completes.
func TestGenerationRetentionWarnsWhenFactKeyDeleteDiffersFromCount(t *testing.T) {
	records := runOnceWithLog(t, Result{
		GenerationsPruned:  1,
		RowsPruned:         map[string]int64{"fact_records": 10},
		FactKeyRowsPruned:  factKeyCounts(5, 3, 1),
		FactKeyRowsCounted: factKeyCounts(5, 2, 1),
	})
	warns := warnRecords(records)
	if len(warns) != 1 {
		t.Fatalf("got %d WARN records, want 1: %v", len(warns), records)
	}
	if warns[0]["msg"] != "generation retention fact key delete differs from its pre-count" {
		t.Errorf("WARN msg = %v", warns[0]["msg"])
	}
	pruned, ok := warns[0]["fact_key_rows_pruned"].(map[string]any)
	if !ok {
		t.Fatalf("WARN record lacks fact_key_rows_pruned: %v", warns[0])
	}
	if n, _ := pruned["package_manifest_consumption_keys"].(float64); n != 3 {
		t.Errorf("fact_key_rows_pruned[package_manifest_consumption_keys] = %v, want 3", pruned["package_manifest_consumption_keys"])
	}
	counted, ok := warns[0]["fact_key_rows_counted"].(map[string]any)
	if !ok {
		t.Fatalf("WARN record lacks fact_key_rows_counted: %v", warns[0])
	}
	if n, _ := counted["package_manifest_consumption_keys"].(float64); n != 2 {
		t.Errorf("fact_key_rows_counted[package_manifest_consumption_keys] = %v, want 2", counted["package_manifest_consumption_keys"])
	}
}

// TestGenerationRetentionNoFactKeyWarnWhenCountsAgree pins the quiet path:
// matching counts (RowsPruned may add chunk page keys on top), and a cycle
// with no whole-generation key delete at all, log no WARN.
func TestGenerationRetentionNoFactKeyWarnWhenCountsAgree(t *testing.T) {
	for name, result := range map[string]Result{
		"agree": {
			GenerationsPruned: 1, RowsPruned: factKeyCounts(9, 2, 1),
			FactKeyRowsPruned: factKeyCounts(5, 2, 1), FactKeyRowsCounted: factKeyCounts(5, 2, 1),
		},
		"no-delete": {GenerationsPruned: 1, RowsPruned: map[string]int64{"fact_records": 4}},
	} {
		if warns := warnRecords(runOnceWithLog(t, result)); len(warns) != 0 {
			t.Errorf("%s: got WARN records %v, want none", name, warns)
		}
	}
}
