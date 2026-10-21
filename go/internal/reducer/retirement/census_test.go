// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"strings"
	"testing"
)

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
