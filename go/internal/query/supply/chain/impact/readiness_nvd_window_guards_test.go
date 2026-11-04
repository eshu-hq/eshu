// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"sort"
	"strings"
	"testing"
)

// TestReadinessNVDWindowCastsAreGuarded is the #9971 twin of
// TestReadinessDescriptorConfigCastsAreGuarded for the rolling-window
// section: every cast of a planned window entry field must sit in the THEN
// arm of its own field's CASE WHEN guard, so a malformed requested_scope_set
// entry skips its row instead of aborting the read with a cast error.
func TestReadinessNVDWindowCastsAreGuarded(t *testing.T) {
	statement := ListReadinessQuery
	start := strings.Index(statement, "nvd_rolling_window_raw AS (")
	end := strings.Index(statement, "target_vulnerability_source_overflow AS (")
	if start < 0 || end <= start {
		t.Fatalf("readiness statement lost its rolling-window CTE boundaries (%d, %d)", start, end)
	}
	window := statement[start:end]
	for field, guard := range readinessWindowGuards {
		window = strings.ReplaceAll(window, guard, "GUARD("+field+")")
	}
	matches := readinessConfigCastPattern.FindAllStringSubmatchIndex(window, -1)
	var fields []string
	for _, match := range matches {
		field := window[match[2]:match[3]]
		cast := window[match[4]:match[5]]
		fields = append(fields, field+cast)
		if _, ok := readinessWindowGuards[field]; !ok {
			t.Errorf("window cast %s of target field %q has no registered guard", cast, field)
			continue
		}
		prefix := window[:match[0]]
		when := strings.LastIndex(prefix, "WHEN ")
		arm := strings.Join(strings.Fields(prefix[max(when, 0):]), " ")
		if when < 0 || !strings.HasPrefix(arm, "WHEN GUARD("+field+") THEN ") {
			t.Errorf("window cast %s of %q is not guarded by its CASE WHEN; nearest WHEN arm: %.120q",
				cast, field, prefix[max(when, 0):])
		}
	}
	sort.Strings(fields)
	want := []string{"modified_end::timestamptz", "modified_start::timestamptz", "results_per_page::numeric"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("rolling-window casts = %v, want %v", fields, want)
	}
}
