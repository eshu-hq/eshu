// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestHeldRetractVariantsCoverEveryProductionDelete is the census that keeps
// F1 fixed: every production DELETE/DETACH-DELETE Cypher const in the #6228
// families' writer files must be held by the registry (RetractCypher or
// RetractVariants) or carry an explicit exclusion naming the unregistered
// sibling it belongs to. The security_group_sg_rule ByUIDs variant lived
// unheld while the guard stayed green because nothing enumerated production
// variants against held ones; this test is that enumeration.
//
// It scans top-level const/var string literals only: every production
// retract template in this tree is a named const, so a DELETE literal
// anywhere else is itself a finding for a reviewer, not a census input.
func TestHeldRetractVariantsCoverEveryProductionDelete(t *testing.T) {
	t.Parallel()

	files := []string{
		filepath.Join("..", "writer", "code_taint_evidence.go"),
		filepath.Join("..", "writer", "code_interproc_evidence.go"),
		filepath.Join("..", "writer", "incident_routing_evidence.go"),
		filepath.Join("..", "writer", "observability_coverage.go"),
		filepath.Join("..", "..", "cloud", "aws", "security", "group", "security_reachability_edge_writer.go"),
		filepath.Join("..", "..", "cloud", "aws", "security", "group", "security_reachability_edge_writer_ledger.go"),
	}
	found := deleteRetractConsts(t, files)
	held := heldRetractVariantStrings()
	if violations := checkHeldRetractConsts(found, held, unheldRetractConstExclusions); len(violations) > 0 {
		t.Errorf("production retract consts not held by the registry:\n  %s", strings.Join(violations, "\n  "))
	}
}

// unheldRetractConstExclusions names production retract consts that exist but
// are deliberately not held. It is empty today: the last entry covered the
// security_group_rule_endpoint TO retracts, and registering that family holds
// both variants, so the exclusion died with its reason. A new exclusion needs
// the same shape: the sibling family and its waiver. A stale exclusion (the
// const no longer exists, e.g. absorbed into a registered family) fails here
// so the exclusion dies with the reason for it.
var unheldRetractConstExclusions = map[string]string{}

// heldRetractVariantStrings returns the set of every retract template the two
// registry tables hold, keyed by exact template text.
func heldRetractVariantStrings() map[string]struct{} {
	held := map[string]struct{}{}
	for _, entry := range singleTypeMaterializedEdgeFamilies {
		for _, variant := range allHeldRetracts(entry) {
			held[variant] = struct{}{}
		}
	}
	for _, entry := range multiTypeDirectMaterializedEdgeFamilies {
		for _, variant := range allHeldRetracts(entry) {
			held[variant] = struct{}{}
		}
	}
	return held
}

// deleteRetractConsts returns the top-level const/var string literals
// containing a DELETE in each file, keyed by const name.
func deleteRetractConsts(t *testing.T, files []string) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, file := range files {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range value.Names {
					for _, expr := range value.Values {
						lit, ok := expr.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						text, err := strconv.Unquote(lit.Value)
						if err != nil {
							text = lit.Value
						}
						if strings.Contains(text, "DELETE") {
							found[name.Name] = text
						}
					}
				}
			}
		}
	}
	return found
}

// checkHeldRetractConsts returns a violation per production retract const
// that is neither held by the registry nor explicitly excluded, plus one per
// stale exclusion whose const no longer exists. Exclusions arrive as a
// parameter (not a global) so the seeded pair can probe the seam with
// synthetic maps without touching the package-level table.
func checkHeldRetractConsts(found map[string]string, held map[string]struct{}, exclusions map[string]string) []string {
	var violations []string
	for name := range found {
		if _, ok := held[found[name]]; ok {
			continue
		}
		if _, ok := exclusions[name]; ok {
			continue
		}
		violations = append(violations, name+": production retract const is not held by any registry family; hold it (RetractCypher/RetractVariants) or exclude it with its sibling family and waiver")
	}
	for name, reason := range exclusions {
		if _, ok := found[name]; !ok {
			violations = append(violations, name+": exclusion is stale (const no longer exists); remove it ("+reason+")")
		}
	}
	sort.Strings(violations)
	return violations
}

// TestHeldRetractConstsCheckSeededPair proves the census can fail: a clean
// const set passes, a planted unheld DELETE const is reported by name, and a
// stale exclusion is reported as its own violation. If this pair ever stops
// distinguishing, the real-tree census above is a comment, not a guard.
func TestHeldRetractConstsCheckSeededPair(t *testing.T) {
	t.Parallel()

	const heldCypher = "MATCH (n)-[rel:HELD]->(m) WHERE rel.scope_id IN $scope_ids DELETE rel"
	const unheldCypher = "MATCH (n)-[rel]->(m) WHERE rel.scope_id IN $scope_ids DELETE rel"

	clean := map[string]string{"retractHeldCypher": heldCypher}
	none := map[string]string{}
	if violations := checkHeldRetractConsts(clean, map[string]struct{}{heldCypher: {}}, none); len(violations) != 0 {
		t.Fatalf("clean const set reported violations, want none: %v", violations)
	}

	planted := map[string]string{"retractHeldCypher": heldCypher, "retractSneakyByUIDsCypher": unheldCypher}
	violations := checkHeldRetractConsts(planted, map[string]struct{}{heldCypher: {}}, none)
	if len(violations) == 0 {
		t.Fatal("planted unheld retract const reported no violation; the census cannot detect the F1 failure it exists to catch")
	}
	matched := false
	for _, v := range violations {
		if strings.Contains(v, "retractSneakyByUIDsCypher") {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("violation does not name the planted const: %v", violations)
	}

	excluded := map[string]string{"retractHeldCypher": heldCypher}
	exclusions := map[string]string{"retractSeededGoneCypher": "seeded stale exclusion"}
	if violations := checkHeldRetractConsts(excluded, map[string]struct{}{heldCypher: {}}, exclusions); len(violations) == 0 {
		t.Fatal("stale exclusion reported no violation; dead exclusions would accumulate silently")
	}
}
