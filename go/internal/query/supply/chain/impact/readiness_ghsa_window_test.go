// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestReadinessQueryHasGHSARollingWindowLeg pins eshu-hq/eshu#10464: the
// readiness statement carries a GHSA rolling-window leg modelled on
// readinessNVDWindowScopesCTE, so a planned GHSA window is an expected
// collection row and its exact scope's snapshot proves coverage.
func TestReadinessQueryHasGHSARollingWindowLeg(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ghsa_rolling_window_raw AS (",
		"ghsa_rolling_window_scopes AS (",
		"ghsa_rolling_window_state_candidates AS (",
		"SELECT * FROM ghsa_rolling_window_state_candidates",
		"FROM ghsa_rolling_window_scopes AS rolling",
	} {
		if !strings.Contains(ListReadinessQuery, want) {
			t.Errorf("readiness statement missing GHSA rolling-window marker %q", want)
		}
	}
}

// TestReadinessGHSAWindowCastsAreGuarded is the GHSA twin of
// TestReadinessNVDWindowCastsAreGuarded: every cast of the planned GHSA
// window end the state-candidate leg carries as a text column must sit in
// the THEN arm of its CASE WHEN guard, so a malformed planned entry skips
// its row instead of aborting the read with a cast error. The scopes leg
// keeps the shared modified_start and modified_end guards from the NVD leg
// (a malformed entry never becomes an expected row); the end column it
// carries out is cast only under the candidate guard below.
func TestReadinessGHSAWindowCastsAreGuarded(t *testing.T) {
	t.Parallel()

	statement := ListReadinessQuery
	start := strings.Index(statement, "ghsa_rolling_window_raw AS (")
	end := strings.Index(statement, "nvd_rolling_window_raw AS (")
	if start < 0 || end <= start {
		t.Fatalf("readiness statement lost its GHSA rolling-window CTE boundaries (%d, %d)", start, end)
	}
	window := statement[start:end]
	for _, guard := range []string{readinessTargetModifiedStartValidSQL, readinessTargetModifiedEndValidSQL} {
		if !strings.Contains(window, guard) {
			t.Errorf("GHSA scopes leg lost its shared planned-bounds guard")
		}
	}
	window = strings.ReplaceAll(window, readinessGHSACandidateEndValidSQL, "GUARD(modified_end)")
	castPattern := regexp.MustCompile(`target\.modified_end\)::([a-z0-9]+)`)
	matches := castPattern.FindAllStringSubmatchIndex(window, -1)
	var casts []string
	for _, match := range matches {
		cast := window[match[2]:match[3]]
		casts = append(casts, cast)
		prefix := window[:match[0]]
		when := strings.LastIndex(prefix, "WHEN ")
		arm := strings.Join(strings.Fields(prefix[max(when, 0):]), " ")
		if when < 0 || !strings.HasPrefix(arm, "WHEN GUARD(modified_end) THEN ") {
			t.Errorf("window-end cast ::%s is not guarded by its CASE WHEN; nearest WHEN arm: %.120q",
				cast, prefix[max(when, 0):])
		}
	}
	sort.Strings(casts)
	if strings.Join(casts, ",") != "timestamptz" {
		t.Fatalf("GHSA rolling-window end casts = %v, want exactly one timestamptz cast", casts)
	}
}

// TestReadinessGHSAWindowCoveredThroughMatch pins the coverage rule: a
// planned GHSA window is covered only by an active snapshot on its exact
// scope whose covered_through equals the planned window end.
func TestReadinessGHSAWindowCoveredThroughMatch(t *testing.T) {
	t.Parallel()

	statement := ListReadinessQuery
	start := strings.Index(statement, "ghsa_rolling_window_raw AS (")
	end := strings.Index(statement, "nvd_rolling_window_raw AS (")
	if start < 0 || end <= start {
		t.Fatal("readiness statement lost its GHSA rolling-window CTE boundaries")
	}
	section := statement[start:end]
	for _, want := range []string{
		"ghsa_rolling_window_state_candidates AS (",
		"covered_through",
		"snapshot.payload->>'source' = 'ghsa'",
		"'ghsa'::text AS source",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("GHSA rolling-window leg missing coverage marker %q", want)
		}
	}
	if matched, err := regexp.MatchString(`(?s)covered_through.*modified_end|modified_end.*covered_through`, section); err != nil || !matched {
		t.Errorf("GHSA coverage match does not relate covered_through to the planned window end")
	}
}
