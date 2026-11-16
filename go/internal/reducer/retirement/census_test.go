// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"strings"
	"testing"
)

// TestCensusClassifiesVulnerabilityTargetRefreshProgress pins the #9879
// progress table's row: its scope ids are configured
// vulnerability-intelligence targets, so repository retirement keeps it
// like its vulnerability_source_states sibling.
func TestCensusClassifiesVulnerabilityTargetRefreshProgress(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("vulnerability_target_refresh_progress")
	if !ok {
		t.Fatal("vulnerability_target_refresh_progress has no census row; the live census fails on it")
	}
	if entry.Disposition != "kept" || len(entry.RemoveSteps) != 0 {
		t.Fatalf("disposition = %q steps %v, want kept with no remove steps", entry.Disposition, entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "scope_id" {
		t.Fatalf("key columns = %v, want [scope_id]", entry.KeyColumns)
	}
}

// TestCensusClassifiesWinnersDirtyTally pins the #10767 drain tally's row:
// nvd_scope_rows matches the live census's %scope% column filter but is a
// count of drained NVD pointer-flip rows, not a scope or repository
// identity, so the table is not_a_key (design 7766) with no purge step. The
// maintainer prunes its own buckets after three hours.
func TestCensusClassifiesWinnersDirtyTally(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("supply_chain_impact_winners_dirty_tally")
	if !ok {
		t.Fatal("supply_chain_impact_winners_dirty_tally has no census row; the live census fails on its nvd_scope_rows column")
	}
	if entry.Disposition != "not_a_key" || len(entry.RemoveSteps) != 0 {
		t.Fatalf("disposition = %q steps %v, want not_a_key with no remove steps", entry.Disposition, entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "nvd_scope_rows" {
		t.Fatalf("key columns = %v, want [nvd_scope_rows], the column the live filter matched", entry.KeyColumns)
	}
	if !strings.Contains(entry.Reason, "count") {
		t.Fatalf("reason %q must say nvd_scope_rows is a count, not a scope key", entry.Reason)
	}
}

// TestCensusClassifiesSupplyChainImpactScopePassState pins the #10389 pass
// state table's row (#10794): it is keyed by scope_id with no foreign key,
// and a surviving partial row would hold the stuck-partial degraded reason
// for a purged scope forever, so the purge deletes it at 2e.
func TestCensusClassifiesSupplyChainImpactScopePassState(t *testing.T) {
	t.Parallel()

	entry, ok := RetireTableDispositionFor("supply_chain_impact_scope_pass_state")
	if !ok {
		t.Fatal("supply_chain_impact_scope_pass_state has no census row; the live census fails on it")
	}
	if entry.Disposition != "2e" {
		t.Fatalf("disposition = %q, want 2e", entry.Disposition)
	}
	if len(entry.RemoveSteps) != 1 || entry.RemoveSteps[0] != "2e" {
		t.Fatalf("remove steps = %v, want [2e]", entry.RemoveSteps)
	}
	if len(entry.KeyColumns) != 1 || entry.KeyColumns[0] != "scope_id" {
		t.Fatalf("key columns = %v, want [scope_id]", entry.KeyColumns)
	}
	if entry.InvisibleToScan {
		t.Fatal("InvisibleToScan = true, want false: the schema scan sees scope_id")
	}
}

// TestRetireTableDispositionsAreWellFormed pins the census table's shape:
// every row names its table, carries a valid disposition code with only
// known purge steps, and cites its deleter or keep reason. The live test
// in the Postgres package proves coverage against the migrated schema;
// this one proves the rows themselves are coherent.
func TestRetireTableDispositionsAreWellFormed(t *testing.T) {
	t.Parallel()

	if len(RetireTableDispositions) == 0 {
		t.Fatal("census table is empty; the live comparison below would pass having checked nothing")
	}
	validDispositions := map[string]struct{}{
		"2b": {}, "2d": {}, "2e": {}, "3": {}, "3+count": {},
		"reaped": {}, "kept": {}, "not_a_key": {},
	}
	validSteps := map[string]struct{}{"2b": {}, "2d": {}, "2e": {}, "3": {}}
	for table, entry := range RetireTableDispositions {
		if entry.Table != table {
			t.Errorf("census row key %q carries table %q", table, entry.Table)
		}
		if _, ok := validDispositions[entry.Disposition]; !ok {
			t.Errorf("table %q has unknown disposition %q", table, entry.Disposition)
		}
		for _, step := range entry.RemoveSteps {
			if _, ok := validSteps[step]; !ok {
				t.Errorf("table %q lists unknown remove step %q", table, step)
			}
		}
		if strings.TrimSpace(entry.Reason) == "" {
			t.Errorf("table %q has no reason", table)
		}
		switch entry.Disposition {
		case "kept", "reaped", "not_a_key":
			if len(entry.RemoveSteps) != 0 {
				t.Errorf("table %q is %q but lists remove steps %v", table, entry.Disposition, entry.RemoveSteps)
			}
		default:
			if len(entry.RemoveSteps) == 0 {
				t.Errorf("table %q is %q but lists no remove step", table, entry.Disposition)
			}
		}
		if _, ok := RetireTableDispositionFor(table); !ok {
			t.Errorf("RetireTableDispositionFor(%q) misses a table the map holds", table)
		}
	}
}
