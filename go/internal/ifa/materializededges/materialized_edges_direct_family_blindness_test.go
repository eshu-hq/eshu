// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// directEdgeWritePortName matches a reducer edge-write PORT: an interface
// method the reducer declares to hand a batch of materialized edge rows to a
// go/internal/storage/cypher writer.
//
// The `(.+)` between Write and Edges is what separates a DIRECT port from the
// shared-projection one. go/internal/reducer/shared_projection_worker.go
// declares the shared path as the bare `WriteEdges(ctx, domain string, ...)` —
// no family in the name, because the family travels as a runtime `domain`
// argument, and its 14 possible values are exactly allProjectionDomains and
// therefore exactly reducer.MaterializedEdgeFamilies(). Every OTHER port bakes
// its family into the method name because it writes one family and only that
// family. Requiring a non-empty middle group is therefore not a spelling trick:
// it is the structural difference between "enumerated by the gate" and "not".
var directEdgeWritePortName = regexp.MustCompile(`^Write(.+)Edges$`)

// camelBoundary splits a Go exported identifier at the points a snake_case
// rendering needs an underscore: a lower-or-digit followed by an upper, or an
// acronym run followed by a new word (IAMCanPerform -> IAM|Can|Perform).
var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])|([A-Z]+)([A-Z][a-z])`)

// manifestSurfaceKey pulls the family out of a `surface: "materialized_edges:x"`
// line in the committed ledger.
var manifestSurfaceKey = regexp.MustCompile(`surface:\s*"` + regexp.QuoteMeta(MaterializedEdgeSurfacePrefix) + `([^"]+)"`)

// directEdgeWritePort is one reducer-declared direct edge-write port: a graph
// edge family the reducer can write without passing through the shared
// projection intent path.
type directEdgeWritePort struct {
	// File is the repo-relative reducer source file declaring the port.
	File string
	// Method is the interface method name, e.g. "WriteIAMCanPerformEdges".
	Method string
	// Family is the snake_case family token derived from Method, e.g.
	// "iam_can_perform". See TestDirectMaterializationFamiliesAreEnumerated's
	// doc comment for why this derived name does not weaken the assertion.
	Family string
}

// TestDirectMaterializationFamiliesAreEnumerated proves whether the Ifá
// materialized-edge exhaustiveness gate can SEE the direct-materialization edge
// families, and fails naming every family it cannot (#6181).
//
// The gate's whole purpose is to make one defect class unreachable: an edge
// family silently regressing to zero rows. It delivers that for every family in
// specs/ifa-materialized-edge-coverage.v1.yaml. For a family absent from the
// ledger it delivers nothing at all — and absence is worse than a waiver, which
// is the distinction this test exists to hold. A waiver is a tracked, named
// exemption: RunMaterializedEdgeCoverage still emits a finding for it, still
// prints its issue, and an operator reading the report sees the gap. A family
// nobody enumerated produces no row, no finding, and no line of output. The gate
// is not lenient about it, it is BLIND to it, and blindness appears in no waiver
// count a reviewer would read.
//
// # Why this is worth more than the count pin
//
// TestMaterializedEdgeFamilyCountClaimsMatchTheCode pins the prose count
// against reducer.MaterializedEdgeFamilies(). That catches a family being
// REMOVED from the enumeration. It cannot catch a family that was never added,
// because both sides of the comparison derive from the same enumeration — a
// sixth direct family landing tomorrow moves neither. This test derives the
// left-hand side from the reducer's SOURCE instead, so a new direct edge-write
// port fails it on the commit that adds the port.
//
// # Why the derived family name does not weaken the failure
//
// Family is derived mechanically from the port's method name, and the eventual
// ledger key for these families is a design decision that is not this test's to
// make. That would matter if the ledger held these families under some OTHER
// name — the test would then report a false gap. It does not, and the companion
// assertion below proves it rather than assuming it: the ledger's complete
// surface-key set is compared against reducer.MaterializedEdgeFamilies(), so
// "the ledger contains no direct family under ANY name" is established
// independently of what this test chose to call them.
func TestDirectMaterializationFamiliesAreEnumerated(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	ports := scanDirectEdgeWritePorts(t, filepath.Join(repoRoot, "go", "internal", "reducer"))
	if len(ports) == 0 {
		t.Fatalf("scanned go/internal/reducer and found no %q ports; the scan went vacuous and would pass no matter how many families are blind", directEdgeWritePortName.String())
	}

	ledger := loadMaterializedEdgeLedgerSurfaces(t, filepath.Join(repoRoot, "specs", MaterializedEdgeManifestFileName))

	// Companion assertion, and the load-bearing one: the ledger holds EXACTLY
	// the shared-projection families. Any direct family present under a name
	// this test did not guess would show up here as an unexpected surface key.
	enumerated := reducer.MaterializedEdgeFamilies()
	if diff := symmetricDiff(ledger, enumerated); len(diff) != 0 {
		t.Errorf("the coverage ledger's surface keys and reducer.MaterializedEdgeFamilies() disagree on %v; the per-port report below derives its family names mechanically and assumes this set is exactly the shared-projection families", diff)
	}

	var blind []directEdgeWritePort
	for _, p := range ports {
		if _, ok := ledger[p.Family]; !ok {
			blind = append(blind, p)
		}
	}
	if len(blind) == 0 {
		return
	}

	t.Errorf("%d direct-materialization edge famil(ies) are absent from specs/%s entirely — not waived, UNENUMERATED, so the exhaustiveness gate cannot know they exist and reports green without them (#6181):", len(blind), MaterializedEdgeManifestFileName)
	for _, p := range blind {
		t.Errorf("  %-34s %s (%s)", p.Family, p.Method, p.File)
	}
	t.Errorf("each needs a row in specs/%s — a coverage row, or an explicit waiver naming a tracked issue, but PRESENT either way", MaterializedEdgeManifestFileName)
}

// scanDirectEdgeWritePorts AST-parses every non-test .go file in dir and
// returns the direct edge-write ports it declares, sorted by family.
//
// AST rather than a text scan for the reason
// TestPropertyKeyedRelationshipMergesMatchKnownAllowList already records for
// its own scan of go/internal/storage/cypher: a doc comment quoting an example
// port signature is never part of the expression tree, so it cannot be mistaken
// for a declaration. A regex over raw source would need exclusion logic to tell
// the two apart, and that logic is where such a scan goes quietly wrong.
//
// Only interface methods count. A concrete method or a local helper that
// happens to match the name is not a port the reducer depends on, and counting
// one would report a family the reducer cannot actually reach.
func scanDirectEdgeWritePorts(t *testing.T, dir string) []directEdgeWritePort {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	var out []directEdgeWritePort
	seen := map[string]struct{}{}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		rel := filepath.Join("go", "internal", "reducer", name)

		ast.Inspect(file, func(n ast.Node) bool {
			iface, ok := n.(*ast.InterfaceType)
			if !ok || iface.Methods == nil {
				return true
			}
			for _, field := range iface.Methods.List {
				if _, isFunc := field.Type.(*ast.FuncType); !isFunc {
					continue
				}
				for _, ident := range field.Names {
					m := directEdgeWritePortName.FindStringSubmatch(ident.Name)
					if m == nil {
						continue
					}
					family := snakeCase(m[1])
					if _, dup := seen[family]; dup {
						continue
					}
					seen[family] = struct{}{}
					out = append(out, directEdgeWritePort{File: rel, Method: ident.Name, Family: family})
				}
			}
			return true
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Family < out[j].Family })
	return out
}

// loadMaterializedEdgeLedgerSurfaces reads the committed coverage manifest as
// TEXT and returns every family named by a `surface:` key, from the coverage:
// and waivers: sections alike.
//
// Text rather than the typed loaders (replaycoverage.LoadManifest /
// LoadMaterializedEdgeWaivers) on purpose: those validate rows against the
// enumerated family set and would reject or drop a surface naming a family
// reducer.MaterializedEdgeFamilies() does not return. That is exactly the row
// this test needs to be able to see — a ledger entry for a family the
// enumeration is blind to would be invisible to a typed read, and the companion
// assertion would then pass by construction.
func loadMaterializedEdgeLedgerSurfaces(t *testing.T, path string) map[string]struct{} {
	t.Helper()

	raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative path built from a package constant.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := map[string]struct{}{}
	for _, m := range manifestSurfaceKey.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatalf("no %q surface keys parsed from %s; the ledger format changed and this check went vacuous", MaterializedEdgeSurfacePrefix, path)
	}
	return out
}

// symmetricDiff returns the families present in exactly one of a set and a
// slice, sorted, so a mismatch report names what actually differs rather than
// dumping both sides.
func symmetricDiff(set map[string]struct{}, list []string) []string {
	other := make(map[string]struct{}, len(list))
	for _, s := range list {
		other[s] = struct{}{}
	}
	var diff []string
	for k := range set {
		if _, ok := other[k]; !ok {
			diff = append(diff, "only-in-ledger:"+k)
		}
	}
	for k := range other {
		if _, ok := set[k]; !ok {
			diff = append(diff, "only-in-enumeration:"+k)
		}
	}
	sort.Strings(diff)
	return diff
}

// snakeCase renders an exported Go identifier as the lower_snake_case token the
// materialized_edges:<family> surface keys use, keeping acronym runs intact
// (IAMCanPerform -> iam_can_perform, S3LogsTo -> s3_logs_to).
func snakeCase(s string) string {
	out := camelBoundary.ReplaceAllString(s, "${1}${3}_${2}${4}")
	return strings.ToLower(out)
}
