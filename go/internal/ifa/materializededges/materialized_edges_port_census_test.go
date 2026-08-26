// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// portClassificationProseFile is the file whose paragraph about the drift
// guard's one-directional totality carries the counts pinned here,
// repo-relative.
const portClassificationProseFile = "go/internal/reducer/materialized_edge_families.go"

// retractShapedPortPrefixes are the name shapes that make a port's presence in
// neither classification table self-explaining.
//
// A prefix rather than a curated list, because these are the ports the prose
// waves at in bulk ("all but one are retract, sweep, execute or read ports")
// and a list would have to be edited for every new retract port — which is the
// kind of bookkeeping that gets skipped, leaving the check either stale or
// deleted. What is NOT waved at in bulk is pinned by name below.
var retractShapedPortPrefixes = []string{"Retract", "Sweep", "Execute"}

// neitherTablePortsWithoutARetractShapedName pins, by name, every port in
// neither classification table whose name does not carry Retract, Sweep or
// Execute — mapped to the reason it is in neither table.
//
// This is what makes "all but one are retract, sweep, execute or read ports"
// checkable rather than decorative. The bulk claim is a prefix test; the
// residue is exactly these three, and a fourth appearing means the sentence has
// become false in a way no count would show. A port silently joining this
// residue is precisely the drift the prose would otherwise absorb.
var neitherTablePortsWithoutARetractShapedName = map[string]string{
	reducer.SharedProjectionEdgeWritePort(): "the shared-projection port: it writes edges, but its family travels as a runtime domain argument, so it is exempted by its own branch rather than by writing none",
	"HasCanonicalCodeTargets":               "a read port: it answers a question about the graph and writes nothing",
	"FailureClass":                          "not a graph-write port at all: it is declared on reducerClassifiedFailure in service_heartbeat.go, an error-taxonomy interface, and reaches the scan only because the scan harvests every method on every reducer interface and matches by bare name",
}

// portClassificationCensus is the count of reducer graph-write ports the Cypher
// scan classifies, split by which classification table claims them.
type portClassificationCensus struct {
	// Classified is every reducer interface port the scan resolves to a cypher
	// implementation.
	Classified int
	// Neither is the ports in neither directMaterializedEdgeFamilyByPort nor
	// directMaterializedEdgeNodeOnlyPorts, INCLUDING the shared-projection port.
	Neither int
	// Undeclared is Neither without the shared-projection port: the ports the
	// prose says would fail the build if the guard were total in both
	// directions.
	Undeclared int
	// UndeclaredWithoutARetractShapedName is the residue the prose pins by name.
	UndeclaredWithoutARetractShapedName []string
}

// portClassificationClaim is one prose count, the pattern that finds it, and
// how many times the file is expected to state it.
//
// Occurrences is pinned rather than checked as "at least one" for the reason
// materializedEdgeCountClaimFiles already records one file over: the "43"
// appears twice in this paragraph, and rephrasing one past the pattern leaves
// the other matching and the claim still looking covered.
type portClassificationClaim struct {
	// Label names the count in a failure message.
	Label string
	// Pattern captures the claimed number in group 1.
	Pattern *regexp.Regexp
	// Occurrences is how many times the prose states this count.
	Occurrences int
	// Want reads the derived value the prose must match.
	Want func(portClassificationCensus) int
}

// portClassificationClaims are the counts the drift-guard paragraph asserts.
//
// Deliberately NOT folded into familyCountClaim, which the sibling
// allProjectionDomains gate uses. That regex's capture groups encode one
// distinction — whole set versus waived subset — over counts derived from
// reducer.MaterializedEdgeFamilies() and the committed ledger. These come from
// the port scan instead. One pattern spanning both derivations would have to
// decide which source a bare number belongs to, and it would get that wrong the
// first time a family count and a port count sat in the same file.
var portClassificationClaims = []portClassificationClaim{
	{
		Label:       "ports the Cypher scan classifies",
		Pattern:     regexp.MustCompile(`scan classifies (\d+) ports`),
		Occurrences: 1,
		Want:        func(c portClassificationCensus) int { return c.Classified },
	},
	{
		Label:       "classified ports in neither classification table",
		Pattern:     regexp.MustCompile(`(\d+) are in neither table`),
		Occurrences: 1,
		Want:        func(c portClassificationCensus) int { return c.Neither },
	},
	{
		Label:       "undeclared ports the guard deliberately lets through",
		Pattern:     regexp.MustCompile(`(\d+) undeclared ports`),
		Occurrences: 2,
		Want:        func(c portClassificationCensus) int { return c.Undeclared },
	},
}

// TestPortClassificationCensusMatchesTheProse holds the drift-guard paragraph
// in materialized_edge_families.go to the scan it describes (#6181).
//
// The paragraph explains why the guard is one-directional: a port in neither
// table that writes no edge falls through silently, and that is tolerable
// because of what those ports ARE. The counts are the evidence for that
// argument: how many ports the scan classifies, how many land in neither table,
// how many of those are undeclared, and that all but one of them is a retract,
// sweep, execute or read port. A reader spends those numbers as proof that the
// silent direction is bounded, so they are not restated here — a second copy of
// a count is a second thing to drift.
//
// Nothing derived them. The sibling allProjectionDomains gate's regex matches
// only "N allProjectionDomains" phrasing, so these numbers sat outside every
// check in the repo and would have drifted the first time a port was added to
// either table — while the paragraph below them concedes that a new node-only
// port "lands with nothing red". A number that drifts silently is worse than no
// number, because the reader cannot tell it has gone stale.
//
// So they are derived here from the same scan the drift guard runs, and the
// identity of the single exception is pinned too: a count alone would stay
// green if FailureClass left the residue and some other port took its place.
func TestPortClassificationCensusMatchesTheProse(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	census := takePortClassificationCensus(t, repoRoot)

	// Pin the residue before the counts. A count failure after a residue change
	// reads as "the number moved"; naming the port that joined or left says what
	// actually happened.
	wantResidue := make([]string, 0, len(neitherTablePortsWithoutARetractShapedName))
	for port := range neitherTablePortsWithoutARetractShapedName {
		wantResidue = append(wantResidue, port)
	}
	sort.Strings(wantResidue)
	if strings.Join(census.UndeclaredWithoutARetractShapedName, ",") != strings.Join(wantResidue, ",") {
		t.Errorf("ports in neither classification table without a retract/sweep/execute name are %v, want %v.\n  The prose in %s says all but one of them are retract, sweep, execute or read ports, with FailureClass the exception.\n  A port that joined this set is in neither table for a NEW reason: declare it in one of the two tables, or add it to neitherTablePortsWithoutARetractShapedName with the reason and correct the prose.",
			census.UndeclaredWithoutARetractShapedName, wantResidue, portClassificationProseFile)
	}

	path := filepath.Join(repoRoot, portClassificationProseFile)
	raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative path from a package constant.
	if err != nil {
		t.Fatalf("read %s: %v", portClassificationProseFile, err)
	}
	lines := strings.Split(string(raw), "\n")

	for _, claim := range portClassificationClaims {
		want := claim.Want(census)
		found := 0
		for i, line := range lines {
			for _, m := range claim.Pattern.FindAllStringSubmatch(line, -1) {
				claimed, convErr := strconv.Atoi(m[1])
				if convErr != nil {
					continue
				}
				found++
				if claimed != want {
					t.Errorf("%s:%d claims %d %s, but the scan counts %d\n  line: %s\n  fix the prose, not this test — the count comes from the same scan the drift guard runs",
						portClassificationProseFile, i+1, claimed, claim.Label, want, strings.TrimSpace(line))
				}
			}
		}
		if found != claim.Occurrences {
			t.Errorf("%s states the %s count %d time(s) matching %q, want exactly %d; a claim was added or reworded past this gate, and a claim this gate cannot see is a claim that goes stale silently",
				portClassificationProseFile, claim.Label, found, claim.Pattern.String(), claim.Occurrences)
		}
	}

	t.Logf("classified=%d neither=%d undeclared=%d residue=%v",
		census.Classified, census.Neither, census.Undeclared, census.UndeclaredWithoutARetractShapedName)
}

// takePortClassificationCensus runs the drift guard's own scan and counts the
// ports each way.
//
// The same scan, not a re-implementation: numbers derived a second way would
// pin the prose to something other than what the guard actually sees, and the
// paragraph is describing the guard.
func takePortClassificationCensus(t *testing.T, repoRoot string) portClassificationCensus {
	t.Helper()

	ports := scanReducerInterfacePorts(t, filepath.Join(repoRoot, "go", "internal", "reducer"))
	src := parseCypherPackage(t, filepath.Join(repoRoot, "go", "internal", "storage", "cypher"))
	classified := classifyCypherPorts(src, ports)
	if len(classified) == 0 {
		t.Fatal("no reducer interface port resolved to a cypher implementation; the scan went vacuous and every count below would be zero")
	}

	nodeOnly := setOf(reducer.DirectMaterializedEdgeNodeOnlyWritePorts())
	sharedPort := reducer.SharedProjectionEdgeWritePort()

	census := portClassificationCensus{Classified: len(classified)}
	for _, row := range classified {
		if _, isEdge := reducer.DirectMaterializedEdgeFamilyForPort(row.Port); isEdge {
			continue
		}
		if _, isNode := nodeOnly[row.Port]; isNode {
			continue
		}
		census.Neither++
		if row.Port != sharedPort {
			census.Undeclared++
		}
		if !hasRetractShapedName(row.Port) {
			census.UndeclaredWithoutARetractShapedName = append(census.UndeclaredWithoutARetractShapedName, row.Port)
		}
	}
	sort.Strings(census.UndeclaredWithoutARetractShapedName)

	if census.Neither == 0 {
		t.Fatal("every classified port is in one of the two tables; the paragraph this gate holds describes a set that no longer exists, so the counts below assert nothing")
	}
	return census
}

// hasRetractShapedName reports whether a port name carries one of the shapes
// the prose waves at in bulk.
func hasRetractShapedName(port string) bool {
	for _, prefix := range retractShapedPortPrefixes {
		if strings.HasPrefix(port, prefix) {
			return true
		}
	}
	return false
}
