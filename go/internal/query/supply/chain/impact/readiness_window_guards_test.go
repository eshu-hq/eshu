// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// These guards pin, from the shipped readiness statement, that a garbage NVD
// window or page size in collector configuration classifies as
// source_targets_malformed instead of aborting the read with a Postgres cast
// error. The disposable-Postgres proof is
// TestSupplyChainImpactReadinessConflictingDuplicateScopeUnavailableLive in
// go/internal/query; these run in the default test pass.

// readinessConfigCastPattern finds a cast whose operand reads a configured
// target field through -> or ->>, capturing the field and the cast type.
var readinessConfigCastPattern = regexp.MustCompile(`target->>?'([a-z_]+)'\)(::[a-z0-9]+)`)

var readinessWindowGuards = map[string]string{
	"modified_start":   readinessTargetModifiedStartValidSQL,
	"modified_end":     readinessTargetModifiedEndValidSQL,
	"results_per_page": readinessTargetResultsPerPageValidSQL,
}

func readinessTargetsSections(t *testing.T) (invalid, configured string) {
	t.Helper()
	statement := ListReadinessQuery
	start := strings.Index(statement, "invalid_vulnerability_source_targets AS (")
	split := strings.Index(statement, "configured_vulnerability_source_targets AS (")
	end := strings.Index(statement, "target_vulnerability_source_scopes AS (")
	if start < 0 || split <= start || end <= split {
		t.Fatalf("readiness statement lost its target CTE boundaries (%d, %d, %d)", start, split, end)
	}
	return statement[start:split], statement[split:end]
}

// TestReadinessDescriptorConfigCastsAreGuarded fails when any cast of a
// configured target field in the descriptor is not the THEN arm of a CASE
// WHEN on that field's guard, so a garbage value would raise a cast error.
func TestReadinessDescriptorConfigCastsAreGuarded(t *testing.T) {
	_, configured := readinessTargetsSections(t)
	// A guard may cast inside its own CASE; mask each guard with a marker so
	// only casts outside a guard are inspected, then require each to sit in
	// the THEN arm of a WHEN on its own field's marker.
	for field, guard := range readinessWindowGuards {
		configured = strings.ReplaceAll(configured, guard, "GUARD("+field+")")
	}
	matches := readinessConfigCastPattern.FindAllStringSubmatchIndex(configured, -1)
	var fields []string
	for _, match := range matches {
		field := configured[match[2]:match[3]]
		cast := configured[match[4]:match[5]]
		fields = append(fields, field+cast)
		if _, ok := readinessWindowGuards[field]; !ok {
			t.Errorf("cast %s of target field %q has no registered guard", cast, field)
			continue
		}
		prefix := configured[:match[0]]
		when := strings.LastIndex(prefix, "WHEN ")
		arm := strings.Join(strings.Fields(prefix[max(when, 0):]), " ")
		if when < 0 || !strings.HasPrefix(arm, "WHEN GUARD("+field+") THEN ") {
			t.Errorf("cast %s of %q is not guarded by its CASE WHEN; nearest WHEN arm: %.120q",
				cast, field, prefix[max(when, 0):])
		}
	}
	sort.Strings(fields)
	want := []string{"modified_end::timestamptz", "modified_start::timestamptz", "results_per_page::numeric"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Fatalf("configured-target casts = %v, want %v", fields, want)
	}
}

// TestReadinessInvalidTargetsRejectsEachWindowGuardFailure fails when the
// invalid CTE stops classifying a present value that fails its cast guard.
func TestReadinessInvalidTargetsRejectsEachWindowGuardFailure(t *testing.T) {
	invalid, _ := readinessTargetsSections(t)
	for field, guard := range readinessWindowGuards {
		if !strings.Contains(invalid, "AND NOT "+guard+")") && !strings.Contains(invalid, "AND NOT ("+guard+"))") {
			t.Errorf("invalid_vulnerability_source_targets does not reject a %s that fails its guard", field)
		}
	}
}

// TestReadinessRFC3339PatternMatchesCollectorParse pins the timestamp shape
// against the collector's time.Parse(time.RFC3339). Calendar validity (month
// 13, February 30) is left to pg_input_is_valid, so those rows only need Go
// to refuse them.
func TestReadinessRFC3339PatternMatchesCollectorParse(t *testing.T) {
	pattern := regexp.MustCompile(readinessRFC3339Pattern)
	for _, tc := range []struct {
		value     string
		wantShape bool
		goAccepts bool
	}{
		{"2026-05-01T00:00:00Z", true, true},
		{"2026-05-01T23:59:59.123456Z", true, true},
		{"2026-05-01T00:00:00.5+02:00", true, true},
		{"2026-05-01T00:00:00-05:30", true, true},
		{"2026-13-01T00:00:00Z", true, false},
		{"2026-02-30T00:00:00Z", true, false},
		{"garbage", false, false},
		{"2026-05-01", false, false},
		{"2026-05-01T00:00:00", false, false},
		{"2026-05-01 00:00:00Z", false, false},
		{"2026-05-01t00:00:00Z", false, false},
		{"2026-05-01T00:00:00z", false, false},
		{"2026-05-01T24:00:00Z", false, false},
		{"2026-05-01T23:59:60Z", false, false},
		{"1714521600", false, false},
	} {
		if got := pattern.MatchString(tc.value); got != tc.wantShape {
			t.Errorf("pattern match %q = %t, want %t", tc.value, got, tc.wantShape)
		}
		if _, err := time.Parse(time.RFC3339, tc.value); (err == nil) != tc.goAccepts {
			t.Errorf("time.Parse(RFC3339, %q) accepted = %t, want %t", tc.value, err == nil, tc.goAccepts)
		}
	}
}
