// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// manifestSurfaceKey pulls the family out of a `surface: "materialized_edges:x"`
// line in the committed ledger.
var manifestSurfaceKey = regexp.MustCompile(`surface:\s*"` + regexp.QuoteMeta(MaterializedEdgeSurfacePrefix) + `([^"]+)"`)

// directMaterializedEdgeFamilyTableFile is where directMaterializedEdgeFamilyByPort
// is declared, repo-relative.
//
// A constant, and checked to exist below, because the failures here are the one
// line a 3-AM operator gets and they shipped naming
// go/internal/reducer/direct_materialized_edge_families.go — a file that has
// never existed. A message that sends the reader to nothing is worse than a
// vague one: it reads as precise.
const directMaterializedEdgeFamilyTableFile = "go/internal/reducer/materialized_edge_families.go"

// TestDirectMaterializedEdgePortsMatchTheExecutedCypher is the drift guard for
// reducer.DirectMaterializedEdgeFamilies() (#6181).
//
// # What it guards, and why the obvious guard is not enough
//
// The direct half of the reducer's materialized-edge surface has no runtime
// registry to derive from: each family is written by its own port straight to a
// go/internal/storage/cypher writer, with nothing binding them together. So the
// enumeration is a table, and a table needs a guard that fails when reality
// moves without it.
//
// The guard this replaces derived the family set by matching port names against
// `^Write(.+)Edges$` and asserted, in its own doc comment, that the shape was
// structural. It is a convention. Six production ports break it — a port named
// for nodes that MERGEs TARGETS_ENVIRONMENT, two named for evidence that MERGE
// TAINT_FLOWS_TO and HAS_TAINT_EVIDENCE, one named for entities that MERGEs
// CONTAINS — and a name-derived guard reports green with all six invisible.
// That is the nominally-covered false green the whole exhaustiveness effort
// exists to prevent, so this guard reads the Cypher instead.
//
// # What makes it bite rather than pass vacuously
//
// The check that matters is stated over the Cypher, not over the tables: EVERY
// port that reaches a relationship MERGE must be a declared direct family (or
// the one shared-projection port). Checking only the ports already declared
// would pass on the commit that adds a new one, which is the whole failure being
// fixed. Stated this way, a new port that merges a relationship fails on the
// commit that adds it no matter what it is named — the property the old guard
// lacked.
//
// Retract, read, and sweep ports are not exempted by name. They pass because
// the scan finds no relationship MERGE behind them: a retract template MATCHes
// an existing relationship and DELETEs it. If one ever starts merging, it is
// caught like anything else.
//
// Both directions are checked. A declared family whose port stops merging fails
// too, so the tables cannot keep claiming a family the code no longer writes.
func TestDirectMaterializedEdgePortsMatchTheExecutedCypher(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	if _, err := os.Stat(filepath.Join(repoRoot, directMaterializedEdgeFamilyTableFile)); err != nil {
		t.Fatalf("stat %s: %v; every failure below tells the operator to edit that file, so it has to be there", directMaterializedEdgeFamilyTableFile, err)
	}

	ports := scanReducerInterfacePorts(t, filepath.Join(repoRoot, "go", "internal", "reducer"))
	src := parseCypherPackage(t, filepath.Join(repoRoot, "go", "internal", "storage", "cypher"))
	classified := classifyCypherPorts(src, ports)
	if len(classified) == 0 {
		t.Fatal("no reducer interface port resolved to a cypher implementation; the scan went vacuous and would pass no matter how many families are blind")
	}

	// isEdge is derived from DirectMaterializedEdgeFamilyForPort rather than from
	// a set built out of DirectMaterializedEdgeWritePorts(). Both read the same
	// table, so under the set form every call site IN THIS GUARD sat behind
	// `isEdge`, which already meant "the lookup would succeed" -- so the guard
	// could never reach the lookup's fail-closed branch, and the contract it
	// most needs held was one this guard was not holding.
	//
	// Not unreachable everywhere, and an earlier wording here said it was:
	// TestDirectEdgeFamilyResolutionFailsClosedOnAnUnclassifiedPort below is a
	// pre-existing call site that does not sit behind `isEdge` and does reach
	// that branch. The claim was wrong in the direction that flattered this
	// change, so it is corrected rather than softened. What survives is
	// narrower and still worth the derivation.
	declaredNode := setOf(reducer.DirectMaterializedEdgeNodeOnlyWritePorts())
	sharedPort := reducer.SharedProjectionEdgeWritePort()

	merging := 0
	seen := map[string]struct{}{}
	for _, row := range classified {
		seen[row.Port] = struct{}{}
		edgeFamily, isEdge := reducer.DirectMaterializedEdgeFamilyForPort(row.Port)
		if isEdge && strings.TrimSpace(edgeFamily) == "" {
			t.Errorf("%s is declared a direct materialized-edge port but maps to a blank family in %s; a blank family names no ledger row and would report as covered by nothing",
				row.Port, directMaterializedEdgeFamilyTableFile)
		}
		_, isNode := declaredNode[row.Port]

		if isEdge && isNode {
			t.Errorf("%s is declared both as a direct edge family (%q) and as node-only (%s); it cannot be both", row.Port, edgeFamily, row.Impl)
		}
		if row.WritesEdges {
			merging++
		}

		switch {
		case row.Port == sharedPort:
			// The shared-projection port carries its family as a runtime
			// `domain` argument, and those values are MaterializedEdgeFamilies().
			if isEdge || isNode {
				t.Errorf("%s is the shared-projection port and must be in neither direct table", row.Port)
			}
		// Node-only first. A port declared node-only has isEdge == false, so
		// the undeclared case below subsumes it: ordered the other way this
		// branch never fires and the operator gets the generic message for a
		// port they can see is already declared. An unreachable guard reads
		// like coverage and is not — the argument this package's own
		// waiver-issue guard makes about a branch it deliberately omits.
		case isNode && row.WritesEdges:
			t.Errorf("%s is declared node-only but MERGEs a relationship in %s: %q. It materializes an edge family the ledger cannot see — move it from directMaterializedEdgeNodeOnlyPorts to directMaterializedEdgeFamilyByPort (%s)",
				row.Port, row.Impl, row.Evidence, directMaterializedEdgeFamilyTableFile)
		case row.WritesEdges && !isEdge:
			t.Errorf("reducer graph-write port %s (%s) MERGEs a relationship — %q — but is not a declared direct materialized-edge family. The Ifá ledger cannot see the family it writes: declare it in directMaterializedEdgeFamilyByPort (%s) and give it a ledger row in specs/%s.",
				row.Port, row.Impl, row.Evidence, directMaterializedEdgeFamilyTableFile, MaterializedEdgeDirectManifestFileName)
		case isEdge && !row.WritesEdges:
			// The family, not just the port. The ledger is keyed by family and
			// this guard reports by port, so an operator handed only the port
			// has to open the table to learn which specs/ row is now claiming a
			// family nothing writes. directEdgeFamilyOrBug fails closed instead
			// of printing a blank family that names no row.
			family := edgeFamily
			t.Errorf("%s is declared to write direct materialized-edge family %q but reaches no relationship MERGE in %s; either the writer stopped materializing that family, or the declaration is stale -- reconcile %s with the ledger row for %q in specs/%s",
				row.Port, family, row.Impl, directMaterializedEdgeFamilyTableFile, family, MaterializedEdgeDirectManifestFileName)
		}
	}

	// A scan that suddenly finds no relationship MERGE anywhere would let every
	// check above pass by finding nothing to check. The package demonstrably
	// merges relationships, so zero means the scan broke, not that the writers
	// stopped writing.
	if merging == 0 {
		t.Fatal("no reducer port was found to reach a relationship MERGE; the Cypher scan broke and every check above passed vacuously")
	}

	for _, port := range reducer.DirectMaterializedEdgeWritePorts() {
		if _, ok := seen[port]; !ok {
			t.Errorf("direct edge family declared for port %s, which is no longer a reducer interface method implemented in go/internal/storage/cypher; remove the stale entry", port)
		}
	}
	for _, port := range reducer.DirectMaterializedEdgeNodeOnlyWritePorts() {
		if _, ok := seen[port]; !ok {
			t.Errorf("node-only classification declared for port %s, which is no longer a reducer interface method implemented in go/internal/storage/cypher; remove the stale entry", port)
		}
	}
	if _, ok := seen[sharedPort]; !ok {
		t.Errorf("shared-projection port %s no longer resolves; SharedProjectionEdgeWritePort is stale", sharedPort)
	}
}

// directEdgeFamilyOrBug resolves a port already known to be DECLARED as a
// direct edge-write port to the family it writes, and reports a bug instead of
// a blank family when the two accessors disagree.
//
// reducer.DirectMaterializedEdgeWritePorts() and
// reducer.DirectMaterializedEdgeFamilyForPort() read the same table, so a port
// in the first and absent from the second cannot happen while that table is
// intact. That is precisely why the second return is checked rather than
// dropped: the only way to reach it is a registration bug, and
// DirectMaterializedEdgeFamilyForPort's contract says a caller MUST fail closed
// on it. Dropping it would print a failure about family "" and send an operator
// looking for a ledger row nobody ever wrote.
func directEdgeFamilyOrBug(port string) (string, error) {
	family, ok := reducer.DirectMaterializedEdgeFamilyForPort(port)
	if !ok {
		return "", fmt.Errorf("port %s is enumerated by DirectMaterializedEdgeWritePorts() but DirectMaterializedEdgeFamilyForPort reports no family for it; both read the same table in %s, so this is a registration bug there, not a family that happens to be absent", port, directMaterializedEdgeFamilyTableFile)
	}
	if strings.TrimSpace(family) == "" {
		return "", fmt.Errorf("port %s is declared a direct edge-write port in %s with a blank family; a blank family key names no row in specs/%s", port, directMaterializedEdgeFamilyTableFile, MaterializedEdgeDirectManifestFileName)
	}
	return family, nil
}

// TestDirectMaterializationFamiliesAreEnumerated proves the Ifá
// materialized-edge exhaustiveness gate can SEE every direct-materialization
// edge family, and fails naming each one it cannot (#6181).
//
// The gate's whole purpose is to make one defect class unreachable: an edge
// family silently regressing to zero rows. It delivers that for every family in
// specs/ifa-materialized-edge-coverage.v1.yaml. For a family absent from the
// ledger it delivers nothing at all — and absence is worse than a waiver, which
// is the distinction this test exists to hold. A waiver is a tracked, named
// exemption: RunMaterializedEdgeCoverage still emits a finding for it, still
// prints its issue, and an operator reading the report sees the gap. A family
// nobody enumerated produces no row, no finding, and no line of output. The
// gate is not lenient about it, it is BLIND to it, and blindness appears in no
// waiver count a reviewer would read.
//
// This is a distinct claim from the drift guard above. That one holds the
// enumeration to the code; this one holds the LEDGER to the enumeration. A
// family can be correctly enumerated and still have no row, which is exactly
// the state #6181 found.
func TestDirectMaterializationFamiliesAreEnumerated(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	ledger := ledgerSurfaceHalves(loadMaterializedEdgeLedgerRows(t, filepath.Join(repoRoot, "specs")))
	direct := reducer.DirectMaterializedEdgeFamilies()
	if len(direct) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeFamilies() returned zero families; the enumeration itself is broken")
	}

	var blind []string
	for _, family := range direct {
		if _, ok := ledger[family]; !ok {
			blind = append(blind, family)
		}
	}
	if len(blind) > 0 {
		t.Errorf("%d direct-materialization edge famil(ies) are absent from both specs/%s and specs/%s — not waived, UNENUMERATED, so the exhaustiveness gate cannot know they exist and reports green without them (#6181):",
			len(blind), MaterializedEdgeManifestFileName, MaterializedEdgeDirectManifestFileName)
		for _, family := range blind {
			t.Errorf("  %s", family)
		}
		t.Errorf("each needs a row in specs/%s, the direct half — a coverage row, or an explicit waiver naming a tracked issue, but PRESENT either way", MaterializedEdgeDirectManifestFileName)
	}

	// The reverse direction: a ledger row naming nothing the code enumerates is
	// a claim about a family that does not exist, and it would quietly inflate
	// the covered/waived counts a reviewer reads.
	known := setOf(append(reducer.MaterializedEdgeFamilies(), direct...))
	var orphaned []string
	for family := range ledger {
		if _, ok := known[family]; !ok {
			orphaned = append(orphaned, family)
		}
	}
	sort.Strings(orphaned)
	for _, family := range orphaned {
		// The halves the family was actually found in, not a hardcoded file
		// name: the surfaces come from BOTH files, and naming the shared
		// manifest for a row that lives in the direct one sends the operator to
		// a file the row is not in. Both are named when both carry one.
		t.Errorf("specs/%s names family %q, which neither reducer.MaterializedEdgeFamilies() nor reducer.DirectMaterializedEdgeFamilies() enumerates; remove the stale row", strings.Join(ledger[family], " and specs/"), family)
	}
}

// ledgerHalf pairs one file of the split ledger with the enumeration that owns
// the families whose rows belong in it.
//
// One table, read by BOTH the loader below and the half check at the end of
// this file, because two lists of the same thing drift apart quietly. Adding a
// third half to the loader alone would produce rows that no check examined --
// present, parsed, and asserted against nothing, which is the same shape of
// blindness this whole file exists to prevent, one level up. Wired here, a new
// half is read and owned in the same edit or not at all.
//
// It also replaces a `default:` case in the half check. Being precise about
// that branch, because being loose about "this cannot fire" is the error under
// review: it was unreachable for the code as written -- the file names came
// only from the loader's own two constants -- and reachable for exactly one
// future edit, someone adding a third file to the loader and not to the check,
// where it fired a t.Fatalf telling them to come back and finish the job.
//
// So it was neither dead code nor a real contract, and neither keeping nor
// deleting it was right. Keeping it left a branch no run can exercise;
// deleting it left the half-edit silently unchecked. The table removes the
// choice: there is one list, so a third half is read and owned in one edit,
// and there is no second place to forget.
type ledgerHalf struct {
	// File is the manifest's base name under specs/.
	File string
	// Owner enumerates the families whose rows belong in File.
	Owner func() []string
	// OwnerName is the source expression for Owner, so a failure names what a
	// maintainer has to go and read.
	OwnerName string
	// Misplaced is what to tell a maintainer holding a row this half does not
	// own: where it belongs, and why nothing else says so.
	Misplaced string
}

// materializedEdgeLedgerHalves returns the two halves of the claims ledger.
func materializedEdgeLedgerHalves() []ledgerHalf {
	return []ledgerHalf{
		{
			File:      MaterializedEdgeManifestFileName,
			Owner:     reducer.MaterializedEdgeFamilies,
			OwnerName: "reducer.MaterializedEdgeFamilies()",
			Misplaced: "If it is a direct-materialization family that row belongs in " + MaterializedEdgeDirectManifestFileName +
				" -- a row in the wrong half satisfies every union check in this file, and the family having its other rows in the right half hides the misplacement from anything asserting per family",
		},
		{
			File:      MaterializedEdgeDirectManifestFileName,
			Owner:     reducer.DirectMaterializedEdgeFamilies,
			OwnerName: "reducer.DirectMaterializedEdgeFamilies()",
			Misplaced: "If it reaches the graph through the shared-projection intent path that row belongs in " + MaterializedEdgeManifestFileName,
		},
	}
}

// ledgerSurfaceRow is one `surface:` key found in one half of the committed
// claims ledger: the family it names, and the file carrying it.
type ledgerSurfaceRow struct {
	// Family is the key with the "materialized_edges:" prefix stripped.
	Family string
	// File is the base name of the manifest the row was read from.
	File string
}

// loadMaterializedEdgeLedgerRows reads BOTH halves of the committed claims
// ledger as TEXT and returns one row per `surface:` key, from the coverage: and
// waivers: sections alike, in file order and then in the order each file
// declares them.
//
// Text rather than the typed loaders (replaycoverage.LoadManifest /
// LoadMaterializedEdgeWaivers) on purpose: those validate rows against the
// enumerated family set and would reject or drop a surface naming a family the
// enumeration does not return. That is exactly the row this test needs to be
// able to see — a ledger entry for a family the enumeration is blind to would
// be invisible to a typed read, and the orphaned-row check would then pass by
// construction.
//
// Both files, because reading one is the very failure being tested for: a
// family whose ledger half is never opened looks identical to a family nobody
// enumerated.
//
// EVERY (family, file) pairing, not the first. An earlier version returned
// family -> FIRST file, and the shared manifest is read first, so a family with
// rows in both halves resolved to "shared" and its rows in the direct half were
// never examined. Measured by moving rows in a throwaway tree: moving one of
// shell_exec's two shared-half coverage rows into the direct manifest left
// TestEachLedgerHalfHoldsOnlyItsOwnFamilies green, while moving one of
// built_from's two direct-half waiver rows the other way red it. The guard was
// direction-dependent, and the direction it missed is the silent one.
func loadMaterializedEdgeLedgerRows(t *testing.T, specsDir string) []ledgerSurfaceRow {
	t.Helper()

	var out []ledgerSurfaceRow
	for _, half := range materializedEdgeLedgerHalves() {
		path := filepath.Join(specsDir, half.File)
		raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative path built from a package constant.
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found := 0
		for _, m := range manifestSurfaceKey.FindAllStringSubmatch(string(raw), -1) {
			out = append(out, ledgerSurfaceRow{Family: m[1], File: half.File})
			found++
		}
		if found == 0 {
			t.Fatalf("no %q surface keys parsed from %s; the ledger format changed and this check went vacuous", MaterializedEdgeSurfacePrefix, path)
		}
	}
	return out
}

// ledgerSurfaceHalves folds rows into family -> every half carrying a row for
// it, sorted, for the union questions ("is this family claimed anywhere") that
// do not care which file the claim sits in.
//
// Every half rather than one, so a failure about a family named in both can say
// both instead of sending the reader to whichever file happened to be read
// first.
func ledgerSurfaceHalves(rows []ledgerSurfaceRow) map[string][]string {
	out := map[string][]string{}
	for _, row := range rows {
		if !slices.Contains(out[row.Family], row.File) {
			out[row.Family] = append(out[row.Family], row.File)
		}
	}
	for _, files := range out {
		sort.Strings(files)
	}
	return out
}

// setOf renders a slice as a lookup set.
func setOf(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

// TestDirectEdgeFamilyResolutionFailsClosedOnAnUnclassifiedPort holds
// reducer.DirectMaterializedEdgeFamilyForPort to the contract its doc comment
// states: the second return is false for an unclassified port, and a caller
// MUST fail closed on it rather than treat the family as absent (#6181).
//
// The distinction is not academic. The drift guard above reports by PORT and
// the ledger is keyed by FAMILY, so the guard resolves one to the other before
// telling an operator which row to touch. A caller that dropped the second
// return would print a failure naming family "" and send that operator hunting
// a ledger row that was never written — a wrong pointer, which reads as precise
// and is worse than no pointer at all.
func TestDirectEdgeFamilyResolutionFailsClosedOnAnUnclassifiedPort(t *testing.T) {
	t.Parallel()

	declared := reducer.DirectMaterializedEdgeWritePorts()
	if len(declared) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeWritePorts() returned zero ports; every check below would pass vacuously")
	}
	for _, port := range declared {
		family, err := directEdgeFamilyOrBug(port)
		if err != nil {
			t.Errorf("directEdgeFamilyOrBug(%s): %v", port, err)
			continue
		}
		if family == "" {
			t.Errorf("directEdgeFamilyOrBug(%s) resolved a blank family with no error", port)
		}
	}

	// The ports that are deliberately NOT direct edge families. Each must
	// resolve false: a node-only port answering with a family would mean the
	// two tables overlap, and the shared-projection port answering with one
	// would mean its runtime `domain` families are being double-counted as a
	// fixed direct family.
	for _, port := range append(reducer.DirectMaterializedEdgeNodeOnlyWritePorts(), reducer.SharedProjectionEdgeWritePort()) {
		if family, ok := reducer.DirectMaterializedEdgeFamilyForPort(port); ok {
			t.Errorf("DirectMaterializedEdgeFamilyForPort(%s) = (%q, true); that port is classified node-only or shared-projection and must not resolve to a direct family", port, family)
		}
	}

	const unclassified = "WriteReviewProbeNotARealPortEdges"
	if family, ok := reducer.DirectMaterializedEdgeFamilyForPort(unclassified); ok || family != "" {
		t.Errorf("DirectMaterializedEdgeFamilyForPort(%s) = (%q, %t), want (\"\", false)", unclassified, family, ok)
	}
	if _, err := directEdgeFamilyOrBug(unclassified); err == nil {
		t.Errorf("directEdgeFamilyOrBug(%s) returned no error; an unrecognised port is a registration bug, never a valid steady state, and swallowing it is what turns a missing declaration into a silent blank family", unclassified)
	}
}

// TestEachLedgerHalfHoldsOnlyItsOwnFamilies pins WHICH file each ledger ROW
// lives in, not merely that some file holds one for its family.
//
// Every other check here reads the union of the two halves. That is the right
// shape for asking "is this family claimed anywhere", and the wrong shape for
// the split itself: a row in the wrong half satisfies every union check in this
// file. The two-file split is what keeps each half readable and under the
// 500-line cap, and #6181 treats it as load-bearing — so it needs an assertion
// rather than a convention.
//
// Per (family, half), not per family, because a family-level assertion is
// direction-dependent. Measured by moving rows in a throwaway tree against the
// version that resolved a family to the FIRST file carrying it: moving one of
// built_from's two direct-half waiver rows into the shared manifest red it,
// and moving one of shell_exec's two shared-half coverage rows into the direct
// manifest did NOT — the family still resolved to the shared half it belongs
// to, so the misplaced row was never examined. Both red now.
//
// It is NOT the only thing that reds on a misplacement, and claiming so would
// be the same overreach this change removes one file over. Measured the same
// way: move ALL of a family's rows and the coverage/waiver reconciliation reds
// too, in both directions, because the moved rows become a dangling waiver or a
// lost coverage row against the family set its caller passes. What it does NOT
// do is say the row is in the wrong HALF — it reports "stale waiver" or
// "uncovered", which sends the maintainer to the wrong question. On a PARTIAL
// move it is quieter still: the only other red was the fixture that reconciles
// the SHARED half alone, and everything reading the merged
// LoadMaterializedEdgeLedger stayed green, because the merge puts the row back
// where the reconciliation expects it. This check names the actual mistake, and
// on the partial case it is the only check that names anything.
//
// The direction that matters is misplacement, not absence: absence is already
// caught by the coverage gate, which requires every (surface, proof_gate) pair
// to be covered or waived.
func TestEachLedgerHalfHoldsOnlyItsOwnFamilies(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	rows := loadMaterializedEdgeLedgerRows(t, filepath.Join(repoRoot, "specs"))
	if len(rows) == 0 {
		t.Fatal("ledger parsed to zero rows; this check would assert nothing")
	}

	// Disjoint enumerations are the PRECONDITION of everything below, not a
	// nicety. A family in both would be owned by whichever half its row happens
	// to sit in, so a misplaced row would satisfy the owner test on either
	// side and this check would assert nothing for it -- vacuous in exactly the
	// way a guard that cannot fail is. materialized_edges_waiver_issue_test.go
	// records the same property as an observation its resolution order leans
	// on; here it is the thing being leaned on, so it fails fast instead.
	sharedFamilies := setOf(reducer.MaterializedEdgeFamilies())
	var both []string
	for _, family := range reducer.DirectMaterializedEdgeFamilies() {
		if _, clash := sharedFamilies[family]; clash {
			both = append(both, family)
		}
	}
	if len(both) > 0 {
		sort.Strings(both)
		t.Fatalf("%d famil(ies) are enumerated as BOTH shared-projection and direct-materialization: %v. Every check below goes vacuous for them -- a row in either half satisfies the half that owns it -- so this stops before asserting anything",
			len(both), both)
	}

	// Every row is examined; the report is deduplicated to one line per
	// (family, half). A family with a dozen misplaced rows in one file is one
	// mistake, and repeating the line a dozen times buries the other findings.
	reported := make(map[ledgerSurfaceRow]struct{}, len(rows))
	for _, half := range materializedEdgeLedgerHalves() {
		owned := setOf(half.Owner())
		if len(owned) == 0 {
			t.Fatalf("%s enumerates zero families; every row in %s would report as misplaced for the wrong reason", half.OwnerName, half.File)
		}
		for _, row := range rows {
			if row.File != half.File {
				continue
			}
			if _, dup := reported[row]; dup {
				continue
			}
			reported[row] = struct{}{}
			if _, ok := owned[row.Family]; !ok {
				t.Errorf("%s carries a row for %q, which %s does not enumerate. %s", half.File, row.Family, half.OwnerName, half.Misplaced)
			}
		}
	}
	t.Logf("checked %d ledger row(s) across %d (family, half) pairing(s)", len(rows), len(reported))
}
