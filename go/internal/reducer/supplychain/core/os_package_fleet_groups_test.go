// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// digestlessOSRow binds one vendor installed row to its own scope and fact ID
// with no sibling scanner analysis, the shape #9984 is about: nothing resolves
// the row to an image digest, so it lands in the digest-less base.
func digestlessOSRow(row installedOSRow, scope string) facts.Envelope {
	envelope := row.envelope()
	envelope.FactID = "os-" + scope
	envelope.ScopeID = "image://fleet/" + scope
	envelope.GenerationID = "gen-" + scope
	return envelope
}

// taggedOSVDebian11Facts converts the live debian curl record kept to one
// entry relabelled Debian:11, so the fact's ranges name only that release line.
func taggedOSVDebian11Facts(t testing.TB) []facts.Envelope {
	t.Helper()
	record := osvFixtureRecord(t, "debian-cve-2023-38039.json")
	entry := record.Affected[0]
	entry.Package.Ecosystem = "Debian:11"
	record.Affected = append(record.Affected[:0:0], entry)
	return convertedOSVRecord(t, record, false)
}

// vendorDebian11Facts is the vendor-sourced fact published for debian-11 whose
// exact-version list names 1:1.0-3.
func vendorDebian11Facts() []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5),
		withDistroLine(vendorLineScenarios[0].affectedEnvelope(""), "debian-11"),
	}
}

// vendorAlpine318Facts is the vendor-sourced fact published for alpine 3.18.
func vendorAlpine318Facts() []facts.Envelope {
	scenario := vendorLineScenarios[2]
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5),
		scenario.affectedEnvelope(scenario.purl("alpine-3.18")),
	}
}

// derivativeDistroRow is a debian-vendor row whose purl names a distro the
// host-line table has no row for.
func derivativeDistroRow(distro string) installedOSRow {
	row := debianCurl("5", "1:1.0-3")
	row.distro = distro
	return row
}

// fleetPermutations returns every ordering of rows.
func fleetPermutations(rows []facts.Envelope) [][]facts.Envelope {
	if len(rows) <= 1 {
		return [][]facts.Envelope{slices.Clone(rows)}
	}
	var out [][]facts.Envelope
	for at := range rows {
		rest := slices.Delete(slices.Clone(rows), at, at+1)
		for _, tail := range fleetPermutations(rest) {
			out = append(out, append([]facts.Envelope{rows[at]}, tail...))
		}
	}
	return out
}

// fleetFindingsJSON runs the production build and returns the findings with the
// bytes the byte-identical acceptance compares.
func fleetFindingsJSON(t *testing.T, advisory, rows []facts.Envelope) (string, []SupplyChainImpactFinding) {
	t.Helper()
	findings := BuildSupplyChainImpactFindings(append(slices.Clone(advisory), rows...))
	encoded, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	return string(encoded), findings
}

// assertFleetOrderIndependent builds every ordering of rows, fails on the first
// one whose findings differ by a byte, and returns the findings they agree on.
func assertFleetOrderIndependent(t *testing.T, advisory, rows []facts.Envelope) []SupplyChainImpactFinding {
	t.Helper()
	var (
		want     string
		findings []SupplyChainImpactFinding
	)
	for at, order := range fleetPermutations(rows) {
		got, built := fleetFindingsJSON(t, advisory, order)
		if at == 0 {
			want, findings = got, built
			continue
		}
		if got != want {
			t.Fatalf("row order changed the findings:\nfirst order: %s\nordering %d: %s", want, at, got)
		}
	}
	return findings
}

// findingOnLine returns the one finding whose installed purl names distro line
// and version, and fails when there is none or more than one.
func findingOnLine(t *testing.T, findings []SupplyChainImpactFinding, line, version string) SupplyChainImpactFinding {
	t.Helper()
	var matched []SupplyChainImpactFinding
	for _, finding := range findings {
		if strings.Contains(finding.PURL, "distro="+line) && finding.ObservedVersion == version {
			matched = append(matched, finding)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("findings on %s at %s = %d, want 1: %#v", line, version, len(matched), findings)
	}
	return matched[0]
}

// TestDigestlessFleetRowsOnTwoReleaseLinesYieldOneFindingPerLine is the #9984
// reproduction: two installed rows with no sibling analysis, on debian 11 and
// debian 12 at one version, and one advisory fact for the debian-11 line. Each
// host gets its own finding and its own verdict in both row orders, for a
// vendor-sourced fact and for a release-tagged OSV fact.
func TestDigestlessFleetRowsOnTwoReleaseLinesYieldOneFindingPerLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		advisory func(t *testing.T) []facts.Envelope
		version  string
	}{
		{
			name:     "vendor fact with distro=debian-11",
			advisory: func(*testing.T) []facts.Envelope { return vendorDebian11Facts() },
			version:  "1:1.0-3",
		},
		{
			name:     "tagged OSV fact for Debian:11",
			advisory: func(t *testing.T) []facts.Envelope { return taggedOSVDebian11Facts(t) },
			version:  "7.74.0-1.3+deb11u7",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := []facts.Envelope{
				digestlessOSRow(debianCurl("11", tc.version), "d11"),
				digestlessOSRow(debianCurl("12", tc.version), "d12"),
			}
			findings := assertFleetOrderIndependent(t, tc.advisory(t), rows)
			if len(findings) != 2 {
				t.Fatalf("findings = %d, want one per release line: %#v", len(findings), findings)
			}
			listed := findingOnLine(t, findings, "debian-11", tc.version)
			if listed.Status != SupplyChainImpactAffectedExact {
				t.Fatalf("debian 11 status = %s/%s, want affected_exact", listed.Status, listed.MatchReason)
			}
			foreign := findingOnLine(t, findings, "debian-12", tc.version)
			assertVendorLineVerdict(t, foreign, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
			if supplyChainImpactFindingID(listed) == supplyChainImpactFindingID(foreign) {
				t.Fatal("finding IDs collide: the writer would keep one row for two release lines")
			}
		})
	}
}

// TestDigestlessFleetGroupsOnePerLineAndVersion runs every ordering of a fleet
// that exercises the grouping key: each row either shares both (release line,
// installed version) with another or differs in one of them.
func TestDigestlessFleetGroupsOnePerLineAndVersion(t *testing.T) {
	t.Parallel()
	arm := debianCurl("12", "1:1.0-3")
	arm.arch = "arm64"
	alpineRow := func(release string) installedOSRow { return alpineCurl(release, "8.15.0-r4") }
	cases := []struct {
		name     string
		advisory []facts.Envelope
		rows     []facts.Envelope
		want     int
	}{
		{
			name:     "three lines",
			advisory: vendorDebian11Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(debianCurl("10", "1:1.0-3"), "d10"),
				digestlessOSRow(debianCurl("11", "1:1.0-3"), "d11"),
				digestlessOSRow(debianCurl("12", "1:1.0-3"), "d12"),
			},
			want: 3,
		},
		{
			name:     "one line two versions",
			advisory: vendorDebian11Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(debianCurl("11", "1:1.0-3"), "d11-a"),
				digestlessOSRow(debianCurl("11", "1:1.0-4"), "d11-b"),
				digestlessOSRow(debianCurl("11", "1:1.0-3"), "d11-c"),
			},
			want: 2,
		},
		{
			name:     "line spellings and architectures share a group",
			advisory: vendorDebian11Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(debianCurl("12", "1:1.0-3"), "d12-plain"),
				digestlessOSRow(debianCurl("12 (bookworm)", "1:1.0-3"), "d12-named"),
				digestlessOSRow(debianCurl("12.5", "1:1.0-3"), "d12-minor"),
				digestlessOSRow(arm, "d12-arm"),
			},
			want: 1,
		},
		{
			name:     "alpine patch releases share their major.minor line",
			advisory: vendorAlpine318Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(alpineRow("3.18.4"), "a318-4"),
				digestlessOSRow(alpineRow("3.18.5"), "a318-5"),
				digestlessOSRow(alpineRow("3.19.1"), "a319-1"),
			},
			want: 2,
		},
		{
			// edge is not numeric, so the host-line table cannot read it. An
			// unreadable line stays apart from every other distro_version
			// rather than folding into a line the table cannot prove.
			name:     "unreadable lines group by their own distro_version",
			advisory: vendorAlpine318Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(alpineRow("edge"), "a-edge"),
				digestlessOSRow(alpineRow("3.18.4"), "a318"),
				digestlessOSRow(alpineRow("rolling"), "a-rolling"),
				digestlessOSRow(alpineRow("edge"), "a-edge-2"),
			},
			want: 3,
		},
		{
			name:     "surrounding space does not split a group",
			advisory: vendorAlpine318Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(alpineRow("edge"), "a-edge"),
				digestlessOSRow(alpineRow(" edge "), "a-edge-spaced"),
				digestlessOSRow(alpineCurl("edge", " 8.15.0-r4 "), "a-version-spaced"),
			},
			want: 1,
		},
		{
			// Two distros no table row reads, one vendor source and one version
			// string: nothing says they are one release line.
			name:     "unreadable distros stay apart at one distro_version",
			advisory: vendorDebian11Facts(),
			rows: []facts.Envelope{
				digestlessOSRow(derivativeDistroRow("raspbian"), "raspbian"),
				digestlessOSRow(derivativeDistroRow("devuan"), "devuan"),
			},
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if findings := assertFleetOrderIndependent(t, tc.advisory, tc.rows); len(findings) != tc.want {
				t.Fatalf("findings = %d, want %d: %#v", len(findings), tc.want, findings)
			}
		})
	}
}

// TestDigestlessFleetBesideDigestRows pins the mix: each digest keeps its own
// finding and the digest-less rows split by line beside them, in every order.
func TestDigestlessFleetBesideDigestRows(t *testing.T) {
	t.Parallel()
	digestA := fmt.Sprintf("sha256:%064x", 0xa1)
	digestB := fmt.Sprintf("sha256:%064x", 0xb2)
	withA := fleetOSRow(debianCurl("11", "1:1.0-3"), "image://fleet/with-a", "gen-with-a", digestA)
	withB := fleetOSRow(debianCurl("12", "1:1.0-3"), "image://fleet/with-b", "gen-with-b", digestB)
	rows := []facts.Envelope{
		withA[0],
		withB[0],
		digestlessOSRow(debianCurl("11", "1:1.0-3"), "bare-11"),
		digestlessOSRow(debianCurl("12", "1:1.0-3"), "bare-12"),
	}
	findings := assertFleetOrderIndependent(t, append(vendorDebian11Facts(), withA[1], withB[1]), rows)
	if len(findings) != 4 {
		t.Fatalf("findings = %d, want 2 digest + 2 digest-less: %#v", len(findings), findings)
	}
	bare := map[SupplyChainImpactStatus]int{}
	for _, finding := range findings {
		switch finding.SubjectDigest {
		case digestA:
			assertVendorLineVerdict(t, finding, SupplyChainImpactAffectedExact, supplyChainVersionReasonDPKGExactAffected)
		case digestB:
			assertVendorLineVerdict(t, finding, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
		case "":
			bare[finding.Status]++
		default:
			t.Fatalf("unexpected digest %q", finding.SubjectDigest)
		}
	}
	if bare[SupplyChainImpactAffectedExact] != 1 || bare[SupplyChainImpactPossiblyAffected] != 1 {
		t.Fatalf("digest-less verdicts = %v, want one exact and one possibly_affected", bare)
	}
}

// TestDigestlessFleetDuplicateRowsDoNotAddFindings pins duplicates: a fact
// loaded twice and a source-joined row reachable under two keys stay one row.
func TestDigestlessFleetDuplicateRowsDoNotAddFindings(t *testing.T) {
	t.Parallel()
	row := digestlessOSRow(debianCurl("11", "1:1.0-3"), "d11")
	other := digestlessOSRow(debianCurl("12", "1:1.0-3"), "d12")
	findings := assertFleetOrderIndependent(t, vendorDebian11Facts(), []facts.Envelope{row, row, other, row})
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want one per line: %#v", len(findings), findings)
	}

	scenario := vendorLineScenarios[1]
	advisory := []facts.Envelope{
		vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5),
		scenario.affectedEnvelope(scenario.purl(scenario.matchLine)),
	}
	joined := assertFleetOrderIndependent(t, advisory, []facts.Envelope{
		digestlessOSRow(scenario.row("12"), "src-12"),
		digestlessOSRow(scenario.row("11"), "src-11"),
	})
	if len(joined) != 2 {
		t.Fatalf("source-joined findings = %d, want one per line: %#v", len(joined), joined)
	}
	if got := findingOnLine(t, joined, "debian-12", scenario.version); got.OSPackageMatchDerivation != osPackageMatchDerivationSource {
		t.Fatalf("derivation = %q, want the source join to survive the split", got.OSPackageMatchDerivation)
	}
}

// TestDigestlessFleetBuildIsIdempotent pins that the partition leaves its input
// alone: a second build over the same envelopes gives the same bytes.
func TestDigestlessFleetBuildIsIdempotent(t *testing.T) {
	t.Parallel()
	rows := []facts.Envelope{
		digestlessOSRow(debianCurl("12", "1:1.0-3"), "z-host"),
		digestlessOSRow(debianCurl("11", "1:1.0-3"), "m-host"),
		digestlessOSRow(debianCurl("12", "1:1.0-3"), "a-host"),
	}
	envelopes := append(vendorDebian11Facts(), rows...)
	first, err := json.Marshal(BuildSupplyChainImpactFindings(envelopes))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.Marshal(BuildSupplyChainImpactFindings(envelopes))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("second build differs:\n%s\n%s", first, second)
	}
}

// TestDigestlessFleetFansOutOverConsumingRepositories pins the composition the
// split keeps: each group still runs the per-repository manifest fanout (#9725),
// so two lines and two consuming repositories give four findings.
func TestDigestlessFleetFansOutOverConsumingRepositories(t *testing.T) {
	t.Parallel()
	packageID := vendorLineScenarios[0].packageID
	advisory := append(vendorDebian11Facts(),
		packageConsumptionCorrelationEnvelope("consumption-a", "repository:a", packageID, "Dockerfile"),
		packageConsumptionCorrelationEnvelope("consumption-b", "repository:b", packageID, "Dockerfile"),
	)
	findings := assertFleetOrderIndependent(t, advisory, []facts.Envelope{
		digestlessOSRow(debianCurl("11", "1:1.0-3"), "d11"),
		digestlessOSRow(debianCurl("12", "1:1.0-3"), "d12"),
	})
	if len(findings) != 4 {
		t.Fatalf("findings = %d, want 2 lines x 2 repositories: %#v", len(findings), findings)
	}
}
