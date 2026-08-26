// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// table, so the set form made the lookup's fail-closed branch unreachable:
	// every call site sat behind `isEdge`, which already meant "the lookup would
	// succeed". A contract that cannot fire is not a contract, and this guard
	// exists to keep exactly that shape out of the ledger.
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

	ledger := loadMaterializedEdgeLedgerSurfaces(t, filepath.Join(repoRoot, "specs"))
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
		// ledger[family], not a hardcoded file name: the surfaces come from
		// BOTH halves, and naming the shared manifest for a row that lives in
		// the direct one sends the operator to a file the row is not in.
		t.Errorf("specs/%s names family %q, which neither reducer.MaterializedEdgeFamilies() nor reducer.DirectMaterializedEdgeFamilies() enumerates; remove the stale row", ledger[family], family)
	}
}

// loadMaterializedEdgeLedgerSurfaces reads BOTH halves of the committed claims
// ledger as TEXT and returns every family named by a `surface:` key, from the
// coverage: and waivers: sections alike.
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
// The value is the file the surface was found in, so a failure about a row can
// name the half that actually carries it. A family named in both halves keeps
// the first, which is the shared manifest; the coverage gate rejects a waiver
// declared twice across the halves, so the ambiguity is caught there rather
// than being resolved here.
func loadMaterializedEdgeLedgerSurfaces(t *testing.T, specsDir string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, name := range []string{MaterializedEdgeManifestFileName, MaterializedEdgeDirectManifestFileName} {
		path := filepath.Join(specsDir, name)
		raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative path built from a package constant.
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		found := 0
		for _, m := range manifestSurfaceKey.FindAllStringSubmatch(string(raw), -1) {
			if _, dup := out[m[1]]; !dup {
				out[m[1]] = name
			}
			found++
		}
		if found == 0 {
			t.Fatalf("no %q surface keys parsed from %s; the ledger format changed and this check went vacuous", MaterializedEdgeSurfacePrefix, path)
		}
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

// TestEachLedgerHalfHoldsOnlyItsOwnFamilies pins WHICH file a family's row lives
// in, not merely that some file holds one.
//
// loadMaterializedEdgeLedgerSurfaces folds both manifests into one
// family -> first-file map, and every other check here reads the union. That is
// the right shape for asking "is this family covered anywhere", and the wrong
// shape for the split itself: a family whose rows sit in the wrong half
// satisfies every union check in this file. The two-file split is what keeps
// each half readable and under the 500-line cap, and #6181 treats it as
// load-bearing — so it needs an assertion rather than a convention.
//
// It is NOT the only thing that reds on a misplacement, and claiming so would
// be the same overreach this change removes one file over. Measured, by moving
// rows in a throwaway tree: the coverage/waiver reconciliation in
// materialized_edges.go reds too, in both directions, because the moved row
// becomes a dangling waiver or a lost coverage row against the family set its
// caller passes. What it does NOT do is say the row is in the wrong HALF — it
// reports "stale waiver" and sends the maintainer to the wrong question. This
// check names the actual mistake.
//
// Limit, also measured: the map is family -> FIRST file, so this is a
// family-level assertion, not a row-level one. Moving SOME of a family's rows
// while leaving others in its correct half does not red here — the family still
// resolves to the half it belongs to. Moving all of them does. The reconciliation
// above is what covers the partial case, loudly if not precisely.
//
// The direction that matters is misplacement, not absence: absence is already
// caught by the coverage gate, which requires every (surface, proof_gate) pair
// to be covered or waived.
func TestEachLedgerHalfHoldsOnlyItsOwnFamilies(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootDir(t)
	ledger := loadMaterializedEdgeLedgerSurfaces(t, filepath.Join(repoRoot, "specs"))
	if len(ledger) == 0 {
		t.Fatal("ledger parsed to zero surfaces; this check would assert nothing")
	}

	shared := setOf(reducer.MaterializedEdgeFamilies())
	direct := setOf(reducer.DirectMaterializedEdgeFamilies())
	if len(shared) == 0 || len(direct) == 0 {
		t.Fatal("one of the two family enumerations is empty; every row would resolve to the other half for the wrong reason")
	}

	checked := 0
	for family, file := range ledger {
		_, isShared := shared[family]
		_, isDirect := direct[family]
		switch file {
		case MaterializedEdgeManifestFileName:
			if !isShared {
				t.Errorf("%s carries a row for %q, which %s does not enumerate. If it is a direct-materialization family its row belongs in %s -- a row in the wrong half satisfies every union check in this file, so nothing here names the misplacement",
					file, family, "reducer.MaterializedEdgeFamilies()", MaterializedEdgeDirectManifestFileName)
			}
		case MaterializedEdgeDirectManifestFileName:
			if !isDirect {
				t.Errorf("%s carries a row for %q, which %s does not enumerate. If it reaches the graph through the shared-projection intent path its row belongs in %s",
					file, family, "reducer.DirectMaterializedEdgeFamilies()", MaterializedEdgeManifestFileName)
			}
		default:
			t.Fatalf("ledger surface %q came from unexpected file %q", family, file)
		}
		checked++
	}
	t.Logf("checked %d ledger row(s) against the half that owns them", checked)
}
