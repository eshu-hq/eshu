// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"testing"
)

// TestGroupOutlierCohortsSources proves each definition produces its cohort:
// one interface keyed by entity id, one router mount grouping two endpoint
// paths, one package grouping two files, each naming its source.
func TestGroupOutlierCohortsSources(t *testing.T) {
	t.Parallel()

	seeds := []OutlierCohortSeed{
		{Source: CohortInterface, IfaceID: "iface-a", IfaceName: "Handler", MemberID: "m-1"},
		{Source: CohortInterface, IfaceID: "iface-a", IfaceName: "Handler", MemberID: "m-2"},
		{Source: CohortInterface, IfaceID: "iface-a", IfaceName: "Handler", MemberID: "m-3"},
		{Source: CohortRouter, EndpointPath: "/widgets", MemberID: "h-1"},
		{Source: CohortRouter, EndpointPath: "/widgets/123", MemberID: "h-2"},
		{Source: CohortRouter, EndpointPath: "/widgets/123/items", MemberID: "h-3"},
		{Source: CohortPackage, FilePath: "pkg/store/a.go", MemberID: "p-1"},
		{Source: CohortPackage, FilePath: "pkg/store/b.go", MemberID: "p-2"},
		{Source: CohortPackage, FilePath: "pkg/store/c.go", MemberID: "p-3"},
	}
	cohorts, suppressions := GroupOutlierCohorts(seeds, DefaultOutlierParams())
	if len(suppressions) != 0 {
		t.Errorf("suppressions = %v, want none", suppressions)
	}
	if len(cohorts) != 3 {
		t.Fatalf("cohorts = %d, want 3", len(cohorts))
	}
	// Trust order: interface, router, package.
	if cohorts[0].Source != CohortInterface || cohorts[0].Key != "iface-a" {
		t.Errorf("first cohort = %q/%q, want interface/iface-a", cohorts[0].Source, cohorts[0].Key)
	}
	if cohorts[0].Label != "Handler" {
		t.Errorf("interface label = %q, want Handler", cohorts[0].Label)
	}
	if cohorts[1].Source != CohortRouter || cohorts[1].Key != "widgets" {
		t.Errorf("second cohort = %q/%q, want router/widgets", cohorts[1].Source, cohorts[1].Key)
	}
	if cohorts[2].Source != CohortPackage || cohorts[2].Key != "pkg/store" {
		t.Errorf("third cohort = %q/%q, want package/pkg/store", cohorts[2].Source, cohorts[2].Key)
	}
}

// TestGroupOutlierCohortsRouterLabels pins the mount label format: a
// first-segment mount renders "/<mount> routes", and the root path renders
// "/ routes" instead of the doubled "// routes".
func TestGroupOutlierCohortsRouterLabels(t *testing.T) {
	t.Parallel()

	seeds := []OutlierCohortSeed{
		{Source: CohortRouter, EndpointPath: "/widgets", MemberID: "h-1"},
		{Source: CohortRouter, EndpointPath: "/widgets/123", MemberID: "h-2"},
		{Source: CohortRouter, EndpointPath: "/widgets/123/items", MemberID: "h-3"},
		{Source: CohortRouter, EndpointPath: "/", MemberID: "r-1"},
		{Source: CohortRouter, EndpointPath: "/", MemberID: "r-2"},
		{Source: CohortRouter, EndpointPath: "/", MemberID: "r-3"},
	}
	cohorts, _ := GroupOutlierCohorts(seeds, DefaultOutlierParams())
	if len(cohorts) != 2 {
		t.Fatalf("cohorts = %d, want 2", len(cohorts))
	}
	// Key collation: "/" sorts before "widgets".
	if cohorts[0].Key != "/" || cohorts[0].Label != "/ routes" {
		t.Errorf("root cohort = %q/%q, want key / with label \"/ routes\"", cohorts[0].Key, cohorts[0].Label)
	}
	if cohorts[1].Key != "widgets" || cohorts[1].Label != "/widgets routes" {
		t.Errorf("mount cohort = %q/%q, want key widgets with label \"/widgets routes\"",
			cohorts[1].Key, cohorts[1].Label)
	}
}

// TestGroupOutlierCohortsBelowMinimum proves small cohorts drop with one
// counted below_min_cohort each, never silently.
func TestGroupOutlierCohortsBelowMinimum(t *testing.T) {
	t.Parallel()

	seeds := []OutlierCohortSeed{
		{Source: CohortPackage, FilePath: "pkg/a.go", MemberID: "p-1"},
		{Source: CohortPackage, FilePath: "pkg/a.go", MemberID: "p-2"},
		{Source: CohortPackage, FilePath: "other/b.go", MemberID: "q-1"},
	}
	cohorts, suppressions := GroupOutlierCohorts(seeds, DefaultOutlierParams())
	if len(cohorts) != 0 {
		t.Errorf("cohorts = %d, want 0", len(cohorts))
	}
	if suppressions[RuleBelowMinCohort] != 2 {
		t.Errorf("below_min_cohort = %v, want 2", suppressions)
	}
}

// TestGroupOutlierCohortsTruncatesAndReports proves cohorts above the cap
// analyze their first members in entity-id order and report the cutoff.
func TestGroupOutlierCohortsTruncatesAndReports(t *testing.T) {
	t.Parallel()

	seeds := make([]OutlierCohortSeed, 0, 7)
	for _, member := range []string{"m-1", "m-2", "m-3", "m-4", "m-5", "m-6", "m-7"} {
		seeds = append(seeds, OutlierCohortSeed{Source: CohortPackage, FilePath: "pkg/a.go", MemberID: member})
	}
	params := DefaultOutlierParams()
	params.MaxCohortSize = 5
	cohorts, suppressions := GroupOutlierCohorts(seeds, params)
	if len(cohorts) != 1 {
		t.Fatalf("cohorts = %d, want 1", len(cohorts))
	}
	cohort := cohorts[0]
	if len(cohort.Members) != 5 || cohort.TotalMembers != 7 {
		t.Errorf("members/total = %d/%d, want 5/7", len(cohort.Members), cohort.TotalMembers)
	}
	if cohort.Members[4] != "m-5" {
		t.Errorf("truncation kept %v, want first five in id order", cohort.Members)
	}
	if suppressions[RuleCohortTruncated] != 2 {
		t.Errorf("cohort_truncated = %v, want 2", suppressions)
	}
}

// TestGroupOutlierCohortsDedupesMembers proves duplicate membership rows
// collapse: one member counts once toward size and share.
func TestGroupOutlierCohortsDedupesMembers(t *testing.T) {
	t.Parallel()

	seeds := []OutlierCohortSeed{
		{Source: CohortRouter, EndpointPath: "/a", MemberID: "h-1"},
		{Source: CohortRouter, EndpointPath: "/a", MemberID: "h-1"},
		{Source: CohortRouter, EndpointPath: "/a/x", MemberID: "h-2"},
		{Source: CohortRouter, EndpointPath: "/a/y", MemberID: "h-3"},
	}
	cohorts, _ := GroupOutlierCohorts(seeds, DefaultOutlierParams())
	if len(cohorts) != 1 || len(cohorts[0].Members) != 3 {
		t.Errorf("cohorts = %+v, want one cohort of 3", cohorts)
	}
}

// TestOutlierFingerprintRoundTrip proves the investigate address survives
// the round trip and foreign fingerprints are refused.
func TestOutlierFingerprintRoundTrip(t *testing.T) {
	t.Parallel()

	fingerprint := OutlierFingerprint(CohortRouter, "widgets", "guard")
	source, key, callee, ok := ParseOutlierFingerprint(fingerprint)
	if !ok || source != CohortRouter || key != "widgets" || callee != "guard" {
		t.Errorf("round trip = %q/%q/%q/%v, want router/widgets/guard/true", source, key, callee, ok)
	}
	for _, foreign := range []string{"", "fp-wrap", "router", "router\x00\x00guard", "bogus\x00widgets\x00guard", "router\x00widgets"} {
		if _, _, _, ok := ParseOutlierFingerprint(foreign); ok {
			t.Errorf("foreign fingerprint %q parsed, want refusal", foreign)
		}
	}
}
