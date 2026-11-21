// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	multiVersionCVEID     = "CVE-2026-11113"
	multiVersionPackageID = "npm://registry.npmjs.org/brace-expansion"
	multiVersionRepoID    = "repository:r_11113"
	multiVersionAffected  = "1.1.11"
	multiVersionFixed     = "5.0.13"
)

// multiVersionLockfileRow builds one package-lock.json row of
// brace-expansion at version. Each row has its own fact id, as each
// installed copy is its own content entity.
func multiVersionLockfileRow(repositoryID, factID, version, scope string) facts.Envelope {
	row := packageManifestDependencyFactWithMetadata(
		repositoryID,
		"api",
		"package-lock.json",
		"brace-expansion",
		"npm",
		version,
		time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC),
		map[string]any{
			"section":          "package-lock",
			"lockfile":         true,
			"dependency_scope": scope,
		},
	)
	row.FactID = factID
	row.StableFactKey = "content_entity:" + factID
	return row
}

func multiVersionAdvisory() []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-11113", multiVersionCVEID, 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-11113",
			multiVersionCVEID,
			multiVersionPackageID,
			"npm",
			"brace-expansion",
			"2.0.2",
		),
	}
}

// multiVersionFindingKey is the part of a finding the winners table keys on
// (writer_identity.go) plus the row that represents it: none of it may depend
// on row order.
type multiVersionFindingKey struct {
	repositoryID    string
	version         string
	status          SupplyChainImpactStatus
	dependencyScope string
}

func multiVersionFindingKeys(findings []SupplyChainImpactFinding) []multiVersionFindingKey {
	keys := make([]multiVersionFindingKey, 0, len(findings))
	for _, finding := range findings {
		if finding.CVEID != multiVersionCVEID {
			continue
		}
		keys = append(keys, multiVersionFindingKey{
			repositoryID:    finding.RepositoryID,
			version:         finding.ObservedVersion,
			status:          finding.Status,
			dependencyScope: finding.DependencyScope,
		})
	}
	slices.SortFunc(keys, func(a, b multiVersionFindingKey) int {
		return cmp.Or(cmp.Compare(a.repositoryID, b.repositoryID), cmp.Compare(a.version, b.version))
	})
	return keys
}

// TestManifestFindingsClassifyEachInstalledVersion11113 pins issue #11113: a
// repository whose lockfile holds an affected and a fixed version of one
// package used to get one finding, judged on whichever row loaded first, so
// the fixed row could hide the vulnerable one. Each distinct installed
// version now classifies once, and the finding set is the same in both row
// orders.
func TestManifestFindingsClassifyEachInstalledVersion11113(t *testing.T) {
	t.Parallel()

	affectedRow := multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:affected", multiVersionAffected, "development")
	fixedRow := multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:fixed", multiVersionFixed, "runtime")

	want := []multiVersionFindingKey{
		{repositoryID: multiVersionRepoID, version: multiVersionAffected, status: SupplyChainImpactAffectedExact, dependencyScope: "development"},
		{repositoryID: multiVersionRepoID, version: multiVersionFixed, status: SupplyChainImpactNotAffectedKnownFixed, dependencyScope: "runtime"},
	}
	for _, tc := range []struct {
		name string
		rows []facts.Envelope
	}{
		{name: "fixed row first", rows: []facts.Envelope{fixedRow, affectedRow}},
		{name: "affected row first", rows: []facts.Envelope{affectedRow, fixedRow}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes := append(multiVersionAdvisory(), tc.rows...)
			got := multiVersionFindingKeys(BuildSupplyChainImpactFindings(envelopes))
			if !slices.Equal(got, want) {
				t.Fatalf("findings = %+v, want %+v: each installed version must classify once, whatever the row order", got, want)
			}
		})
	}
}

// multiVersionManifestRow builds one package.json row whose text is a range,
// so it proves no installed version.
func multiVersionManifestRow(factID, requested string) facts.Envelope {
	row := packageManifestDependencyFactWithMetadata(
		multiVersionRepoID,
		"api",
		"package.json",
		"brace-expansion",
		"npm",
		requested,
		time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC),
		map[string]any{"dependency_scope": "runtime"},
	)
	row.FactID = factID
	row.StableFactKey = "content_entity:" + factID
	return row
}

// runBothOrders builds findings for rows in the given order and reversed, and
// fails unless both orders give want.
func runBothOrders(t *testing.T, rows []facts.Envelope, want []multiVersionFindingKey) {
	t.Helper()
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	for _, order := range [][]facts.Envelope{rows, reversed} {
		got := multiVersionFindingKeys(BuildSupplyChainImpactFindings(append(multiVersionAdvisory(), order...)))
		if !slices.Equal(got, want) {
			t.Fatalf("findings = %+v, want %+v", got, want)
		}
	}
}

// TestManifestFindingsVersionlessRowsBesideVersionedRows11113: a range-only
// package.json row next to a lockfile row adds no blank-version finding; the
// repository is judged on the version it proves.
func TestManifestFindingsVersionlessRowsBesideVersionedRows11113(t *testing.T) {
	t.Parallel()
	runBothOrders(t, []facts.Envelope{
		multiVersionManifestRow("manifest-dep:11113:range", "^1.1.7"),
		multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:affected", multiVersionAffected, "development"),
	}, []multiVersionFindingKey{
		{repositoryID: multiVersionRepoID, version: multiVersionAffected, status: SupplyChainImpactAffectedExact, dependencyScope: "development"},
	})
}

// TestManifestFindingsVersionlessRepositoryKeepsOneFinding11113: a repository
// whose rows name no version keeps the pre-#11113 rule, one finding.
func TestManifestFindingsVersionlessRepositoryKeepsOneFinding11113(t *testing.T) {
	t.Parallel()
	findings := BuildSupplyChainImpactFindings(append(multiVersionAdvisory(),
		multiVersionManifestRow("manifest-dep:11113:range-a", "^1.1.7"),
		multiVersionManifestRow("manifest-dep:11113:range-b", "~2.0.0"),
	))
	got := multiVersionFindingKeys(findings)
	if len(got) != 1 || got[0].version != "" || got[0].status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("findings = %+v, want one possibly_affected finding with no observed version", got)
	}
}

// TestManifestFindingsSameVersionRowsPickOneRepresentative11113: two rows of
// one version yield one finding, and the row that represents it (here its
// dependency scope) does not depend on load order.
func TestManifestFindingsSameVersionRowsPickOneRepresentative11113(t *testing.T) {
	t.Parallel()
	runBothOrders(t, []facts.Envelope{
		multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:b-runtime", multiVersionAffected, "runtime"),
		multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:a-dev", multiVersionAffected, "development"),
	}, []multiVersionFindingKey{
		{repositoryID: multiVersionRepoID, version: multiVersionAffected, status: SupplyChainImpactAffectedExact, dependencyScope: "development"},
	})
}

// TestManifestFindingsTwoRepositoriesSplitIndependently11113: the split is per
// repository; a second repository holding one version keeps one finding.
func TestManifestFindingsTwoRepositoriesSplitIndependently11113(t *testing.T) {
	t.Parallel()
	const otherRepo = "repository:r_11113_other"
	runBothOrders(t, []facts.Envelope{
		multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:fixed", multiVersionFixed, "runtime"),
		multiVersionLockfileRow(otherRepo, "manifest-dep:11113:other", multiVersionFixed, "runtime"),
		multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:affected", multiVersionAffected, "development"),
	}, []multiVersionFindingKey{
		{repositoryID: multiVersionRepoID, version: multiVersionAffected, status: SupplyChainImpactAffectedExact, dependencyScope: "development"},
		{repositoryID: multiVersionRepoID, version: multiVersionFixed, status: SupplyChainImpactNotAffectedKnownFixed, dependencyScope: "runtime"},
		{repositoryID: otherRepo, version: multiVersionFixed, status: SupplyChainImpactNotAffectedKnownFixed, dependencyScope: "runtime"},
	})
}

// TestMultiVersionRepositoryGroupsSubSignal11113 pins the pass count: one
// repository classified at two versions counts once, a single-version one
// counts nothing.
func TestMultiVersionRepositoryGroupsSubSignal11113(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rows []facts.Envelope
		want int
	}{
		{name: "two versions", want: 1, rows: []facts.Envelope{
			multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:fixed", multiVersionFixed, "runtime"),
			multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:affected", multiVersionAffected, "development"),
		}},
		{name: "one version", want: 0, rows: []facts.Envelope{
			multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:fixed", multiVersionFixed, "runtime"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(append(multiVersionAdvisory(), tc.rows...))
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if stats.multiVersionRepositoryGroups != tc.want {
				t.Fatalf("multiVersionRepositoryGroups = %d, want %d", stats.multiVersionRepositoryGroups, tc.want)
			}
			subSignals := map[string]float64{}
			SupplyChainImpactHandler{}.recordBuildStats(t.Context(), stats, subSignals)
			got, present := subSignals[multiVersionRepositoryGroupsSubSignal]
			if !present {
				t.Fatalf("sub-signal %s missing: it must be written even when 0", multiVersionRepositoryGroupsSubSignal)
			}
			if got != float64(tc.want) {
				t.Fatalf("sub-signal %s = %v, want %d", multiVersionRepositoryGroupsSubSignal, got, tc.want)
			}
		})
	}
}

// BenchmarkManifestMultiVersionFindings11113 classifies one advisory against
// the QA shape of #11113: 252 repositories, 155 of them installing two to four
// versions of the package in lockfile rows, the rest one. It measures the
// classify cost the per-version split adds to one holder's pass.
func BenchmarkManifestMultiVersionFindings11113(b *testing.B) {
	pool := []string{"1.1.11", "1.1.12", "2.0.1", "2.0.2", "2.1.1", "5.0.13"}
	envelopes := multiVersionAdvisory()
	for r := 0; r < 252; r++ {
		k := 1
		if r < 155 {
			k = 2
			switch {
			case r%5 == 0:
				k = 4
			case r%3 == 0:
				k = 3
			}
		}
		repo := fmt.Sprintf("repository:r_bench_%03d", r)
		for j := 0; j < k; j++ {
			version := pool[(r+j*2)%len(pool)]
			envelopes = append(envelopes, multiVersionLockfileRow(repo, fmt.Sprintf("manifest-dep:bench:%03d:%s", r, version), version, "runtime"))
		}
	}
	b.ReportAllocs()
	var findings int
	for b.Loop() {
		findings = len(BuildSupplyChainImpactFindings(envelopes))
	}
	b.ReportMetric(float64(findings), "findings")
}
