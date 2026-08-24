// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

	declaredEdge := setOf(reducer.DirectMaterializedEdgeWritePorts())
	declaredNode := setOf(reducer.DirectMaterializedEdgeNodeOnlyWritePorts())
	sharedPort := reducer.SharedProjectionEdgeWritePort()

	merging := 0
	seen := map[string]struct{}{}
	for _, row := range classified {
		seen[row.Port] = struct{}{}
		_, isEdge := declaredEdge[row.Port]
		_, isNode := declaredNode[row.Port]

		if isEdge && isNode {
			t.Errorf("%s is declared both as a direct edge family and as node-only (%s); it cannot be both", row.Port, row.Impl)
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
			t.Errorf("%s is declared a direct materialized-edge family but reaches no relationship MERGE in %s; either the writer stopped materializing its family, or the declaration is stale", row.Port, row.Impl)
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
