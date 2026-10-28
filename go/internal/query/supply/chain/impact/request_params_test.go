// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func impactParamRequest(params string) (*httptest.ResponseRecorder, *http.Request) {
	return httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x?"+params, nil)
}

// TestRequestedProfile proves the profile parameter defaults to
// precise, accepts the closed vocabulary, and rejects the rest.
func TestRequestedProfile(t *testing.T) {
	t.Parallel()

	w, r := impactParamRequest("")
	if profile, ok := RequestedProfile(w, r); !ok || profile != ProfilePrecise {
		t.Errorf("default profile = %q, %v; want precise, true", profile, ok)
	}
	w, r = impactParamRequest("profile=comprehensive")
	if profile, ok := RequestedProfile(w, r); !ok || profile != ProfileComprehensive {
		t.Errorf("profile = %q, %v; want comprehensive, true", profile, ok)
	}
	w, r = impactParamRequest("profile=broad")
	if _, ok := RequestedProfile(w, r); ok || w.Code != http.StatusBadRequest {
		t.Errorf("bogus profile must 400, got ok=%v code=%d", ok, w.Code)
	}
	if FilterProfile(ProfilePrecise) != ProfilePrecise || FilterProfile(ProfileComprehensive) != "" {
		t.Error("profile filter mapping wrong")
	}
}

// TestRequiredFindingLimit proves the findings page size is required
// and bounded.
func TestRequiredFindingLimit(t *testing.T) {
	t.Parallel()

	w, r := impactParamRequest("limit=25")
	if limit, ok := RequiredFindingLimit(w, r); !ok || limit != 25 {
		t.Errorf("limit = %d, %v; want 25, true", limit, ok)
	}
	for _, params := range []string{"", "limit=0", "limit=201", "limit=many"} {
		w, r := impactParamRequest(params)
		if _, ok := RequiredFindingLimit(w, r); ok || w.Code != http.StatusBadRequest {
			t.Errorf("params %q must 400, got ok=%v code=%d", params, ok, w.Code)
		}
	}
}

// TestParseIncludeSuppressed proves the flag defaults false, parses
// both spellings, and rejects anything else.
func TestParseIncludeSuppressed(t *testing.T) {
	t.Parallel()

	w, r := impactParamRequest("")
	if got, ok := ParseIncludeSuppressed(w, r); !ok || got {
		t.Errorf("default = %v, %v; want false, true", got, ok)
	}
	w, r = impactParamRequest("include_suppressed=true")
	if got, ok := ParseIncludeSuppressed(w, r); !ok || !got {
		t.Errorf("explicit true = %v, %v", got, ok)
	}
	w, r = impactParamRequest("include_suppressed=maybe")
	if _, ok := ParseIncludeSuppressed(w, r); ok || w.Code != http.StatusBadRequest {
		t.Errorf("bogus flag must 400, got ok=%v code=%d", ok, w.Code)
	}
	if !IsSupportedSuppressionState("active") || IsSupportedSuppressionState("napping") {
		t.Error("suppression-state vocabulary wrong")
	}
}

// TestScannerFilterSets proves each route admits its own filter names,
// the rejection gate 400s foreign filters, and severity parsing stays
// in its closed vocabulary.
func TestScannerFilterSets(t *testing.T) {
	t.Parallel()

	if _, ok := FindingsScannerFilters()["severity"]; !ok {
		t.Error("findings filters must admit severity")
	}
	if _, ok := ExplanationScannerFilters()["finding_id"]; !ok {
		t.Error("explain filters must admit finding_id")
	}
	if _, ok := SecurityAlertScannerFilters()["provider"]; !ok {
		t.Error("security-alert filters must admit provider")
	}

	w, r := impactParamRequest("severity=high&language=go")
	if RejectUnsupportedVulnerabilityScannerFilters(w, r, ExplanationScannerFilters()) {
		t.Error("language is foreign to the explain route and must be rejected")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("rejection status = %d, want 400", w.Code)
	}
	w, r = impactParamRequest("severity=high")
	if !RejectUnsupportedVulnerabilityScannerFilters(w, r, FindingsScannerFilters()) {
		t.Error("admitted filters must pass the gate")
	}

	w, r = impactParamRequest("severity=CRITICAL")
	if severity, ok := ParseScannerSeverity(w, r); !ok || severity != "critical" {
		t.Errorf("severity = %q, %v; want critical, true", severity, ok)
	}
	w, r = impactParamRequest("severity=urgent")
	if _, ok := ParseScannerSeverity(w, r); ok || w.Code != http.StatusBadRequest {
		t.Errorf("bogus severity must 400, got ok=%v code=%d", ok, w.Code)
	}

	_, r = impactParamRequest("a=&b=second")
	if got := FirstNonEmptyQueryParam(r, "a", "b"); got != "second" {
		t.Errorf("first non-empty param = %q, want second", got)
	}
}

// TestPriorityFilter proves the priority parser validates buckets,
// floors, and sorts together.
func TestPriorityFilter(t *testing.T) {
	t.Parallel()

	_, r := impactParamRequest("priority_bucket=high&min_priority_score=50&sort=priority")
	bucket, floor, sort, err := PriorityFilter(r)
	if err != nil {
		t.Fatalf("PriorityFilter failed: %v", err)
	}
	if bucket != "high" || floor != 50 || sort != "priority_score_desc" {
		t.Errorf("priority parse = (%q, %d, %q)", bucket, floor, sort)
	}
	for _, params := range []string{
		"priority_bucket=urgent",
		"min_priority_score=101",
		"min_priority_score=high",
		"sort=random",
	} {
		_, r := impactParamRequest(params)
		if _, _, _, err := PriorityFilter(r); err == nil {
			t.Errorf("params %q must fail validation", params)
		}
	}
	if !ValidPriorityBucket("informational") || ValidPriorityBucket("") {
		t.Error("priority-bucket vocabulary wrong")
	}
	if NormalizeSort("") != "finding_id" || NormalizeSort("priority_score_asc") != "priority_score_asc" {
		t.Error("sort normalization wrong")
	}
}

// TestNormalizeReadinessCollections proves evidence families filter
// to the closed set, snapshots deduplicate and sort, and string
// helpers compact and dedupe.
func TestNormalizeReadinessCollections(t *testing.T) {
	t.Parallel()

	families := normalizeEvidenceSources([]EvidenceFamily{
		{Family: EvidenceFamilySBOMComponent},
		{Family: "unknown-family"},
		{Family: "  "},
		{Family: EvidenceFamilyVulnerabilityAdvisory},
	})
	if len(families) != 2 || families[0].Family != EvidenceFamilySBOMComponent {
		t.Errorf("families = %+v, want the two known kinds sorted", families)
	}
	if normalizeEvidenceSources(nil) == nil {
		t.Error("empty families must yield an empty slice, not nil")
	}

	snapshots := normalizeSourceSnapshots([]SourceSnapshot{
		{Source: "npm", Ecosystem: "npm", SnapshotDigest: "d2"},
		{Source: " npm ", Ecosystem: "npm", SnapshotDigest: "d1"},
		{Source: "npm", Ecosystem: "npm", SnapshotDigest: "d1"},
		{Source: "  "},
	})
	if len(snapshots) != 2 || snapshots[0].SnapshotDigest != "d1" {
		t.Errorf("snapshots = %+v, want 2 deduplicated and sorted", snapshots)
	}

	if got := uniqueSortedReadinessStrings([]string{"b", "a", " ", "b"}); len(got) != 2 || got[0] != "a" {
		t.Errorf("unique sorted = %v", got)
	}
	if !ReadinessMissingContains([]string{"a"}, "a") || ReadinessMissingContains(nil, "a") {
		t.Error("ReadinessMissingContains wrong")
	}
}

// TestImpactStringHelpers proves the family-local string helpers
// compact blanks, take the first value, dedupe appends, and read
// string maps defensively.
func TestImpactStringHelpers(t *testing.T) {
	t.Parallel()

	if got := compactStrings([]string{" a ", "", "b"}); len(got) != 2 || got[0] != "a" {
		t.Errorf("compacted = %v", got)
	}
	if firstNonEmptyString("", "  ", "x") != "x" || firstNonEmptyString("", " ") != "" {
		t.Error("firstNonEmptyString wrong")
	}
	values := []string{"a"}
	appendUniqueString(&values, "a")
	appendUniqueString(&values, " ")
	appendUniqueString(&values, "b")
	if len(values) != 2 {
		t.Errorf("unique append = %v", values)
	}
	mapped := stringMapVal(map[string]any{"env": map[string]any{"prod": "deploy_event", "blank": " "}}, "env")
	if len(mapped) != 1 || mapped["prod"] != "deploy_event" {
		t.Errorf("string map = %v", mapped)
	}
	if stringMapVal(map[string]any{"env": "flat"}, "env") != nil {
		t.Error("non-map values must read nil")
	}
}
