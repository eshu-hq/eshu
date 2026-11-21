// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestGroupConsumptionRowsByRepository10972 pins the single-pass grouping
// behind issue #10972 fix A: one walk over a package's consumption rows
// partitioned by repository, preserving index order within each repository
// so firstConsumption keeps its within-repository preference. Blank
// repository rows are excluded exactly as consumptionRepositoriesForPackage
// excludes them, keyed on the raw repository ID (no trimming fix).
func TestGroupConsumptionRowsByRepository10972(t *testing.T) {
	t.Parallel()

	const pkgID = "pkg:npm/lodash"
	rows := []supplychainmodel.PackageConsumption{
		{FactID: "b1", PackageID: pkgID, RepositoryID: "repository:b"},
		{FactID: "blank-empty", PackageID: pkgID, RepositoryID: ""},
		{FactID: "a1", PackageID: pkgID, RepositoryID: "repository:a"},
		{FactID: "b2", PackageID: pkgID, RepositoryID: "repository:b"},
		{FactID: "blank-spaces", PackageID: pkgID, RepositoryID: "   "},
		{FactID: "a2", PackageID: pkgID, RepositoryID: "repository:a"},
		{FactID: "b1-dup", PackageID: pkgID, RepositoryID: "repository:b"},
		{FactID: "raw-spaced", PackageID: pkgID, RepositoryID: " repository:c"},
	}

	grouped := groupConsumptionRowsByRepository(rows)

	if len(grouped) != 3 {
		t.Fatalf("len(grouped) = %d, want 3 (blank rows excluded)", len(grouped))
	}
	factIDs := func(repo string) []string {
		var out []string
		for _, row := range grouped[repo] {
			out = append(out, row.FactID)
		}
		return out
	}
	if got, want := factIDs("repository:a"), []string{"a1", "a2"}; !slices.Equal(got, want) {
		t.Fatalf("repository:a rows = %v, want %v (index order)", got, want)
	}
	if got, want := factIDs("repository:b"), []string{"b1", "b2", "b1-dup"}; !slices.Equal(got, want) {
		t.Fatalf("repository:b rows = %v, want %v (index order, duplicates kept)", got, want)
	}
	if got, want := factIDs(" repository:c"), []string{"raw-spaced"}; !slices.Equal(got, want) {
		t.Fatalf("raw-spaced repo rows = %v, want %v (keyed on raw ID)", got, want)
	}
}

// TestAppendManifestFindingsPerRepositoryIdentity10972 pins the finding-level
// contract of appendSupplyChainImpactManifestFindings across the #10972 fix:
// one finding per distinct non-blank repository in sorted order, the
// within-repository firstConsumption preference intact, deterministic across
// calls, with the single-repo and zero-row shapes preserved.
func TestAppendManifestFindingsPerRepositoryIdentity10972(t *testing.T) {
	t.Parallel()

	const pkgID = "pkg:npm/lodash"
	pkg := supplychainmodel.AffectedPackage{
		FactID:    "affected-10972",
		CVEID:     "CVE-2026-0001",
		PackageID: pkgID,
		Ecosystem: "npm",
		Name:      "lodash",
	}
	cves := supplyChainCVEGroup{cveID: "CVE-2026-0001"}

	build := func(rows []supplychainmodel.PackageConsumption) []SupplyChainImpactFinding {
		index := supplyChainImpactIndex{
			consumption: map[string][]supplychainmodel.PackageConsumption{pkgID: rows},
		}
		return appendSupplyChainImpactManifestFindings(nil, cves, []supplychainmodel.AffectedPackage{pkg}, index, false)
	}
	repositories := func(findings []SupplyChainImpactFinding) []string {
		var out []string
		for _, finding := range findings {
			out = append(out, finding.RepositoryID)
		}
		return out
	}

	rows := []supplychainmodel.PackageConsumption{
		{FactID: "consume-b-plain", PackageID: pkgID, RepositoryID: "repository:b"},
		{FactID: "consume-blank", PackageID: pkgID, RepositoryID: ""},
		{FactID: "consume-a", PackageID: pkgID, RepositoryID: "repository:a"},
		{FactID: "consume-b-lock", PackageID: pkgID, RepositoryID: "repository:b", InstalledVersion: "4.17.21", Lockfile: true},
		{FactID: "consume-b-dup", PackageID: pkgID, RepositoryID: "repository:b"},
	}

	first := build(rows)
	if got, want := repositories(first), []string{"repository:a", "repository:b"}; !slices.Equal(got, want) {
		t.Fatalf("finding repositories = %v, want %v (sorted, blank excluded)", got, want)
	}
	// The lockfile row wins within its repository even though the plain row
	// arrived first: firstConsumption preference survives the grouping.
	if !slices.Contains(first[1].EvidenceFactIDs, "consume-b-lock") {
		t.Fatalf("repository:b evidence = %v, want consume-b-lock (lockfile preference)", first[1].EvidenceFactIDs)
	}
	second := build(rows)
	if got, want := repositories(second), repositories(first); !slices.Equal(got, want) {
		t.Fatalf("second call repositories = %v, want %v (deterministic order)", got, want)
	}
	for i := range first {
		if !slices.Equal(first[i].EvidenceFactIDs, second[i].EvidenceFactIDs) {
			t.Fatalf("finding %d evidence differs across calls: %v vs %v", i, first[i].EvidenceFactIDs, second[i].EvidenceFactIDs)
		}
	}

	single := build([]supplychainmodel.PackageConsumption{
		{FactID: "consume-only", PackageID: pkgID, RepositoryID: "repository:solo"},
	})
	if got := repositories(single); !slices.Equal(got, []string{"repository:solo"}) {
		t.Fatalf("single-repo repositories = %v, want [repository:solo]", got)
	}

	zero := build(nil)
	if len(zero) > 1 {
		t.Fatalf("zero-row findings = %d, want at most the single unscoped fallback", len(zero))
	}
	for _, finding := range zero {
		if finding.RepositoryID != "" {
			t.Fatalf("zero-row finding carries repository %q, want the unscoped fallback", finding.RepositoryID)
		}
	}
}
