// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// TestObservabilityCoverageDerivedTypesRoundTripValidator proves every type
// ObservabilityCoverageRelationshipTypes derives is exactly a type the write
// path would emit: stripping the label prefix must yield a vocabulary member,
// and feeding that member back through canonicalObservabilityCoverageCypherType
// (the validator the write path screens each row with) must return the same
// type. A change to the token rule that the derivation does not follow turns
// this red instead of silently narrowing what the registry asserts.
func TestObservabilityCoverageDerivedTypesRoundTripValidator(t *testing.T) {
	t.Parallel()

	derived := ObservabilityCoverageRelationshipTypes()
	if len(derived) == 0 {
		t.Fatal("ObservabilityCoverageRelationshipTypes() is empty; this test would pass vacuously")
	}
	for relType := range derived {
		t.Run(relType, func(t *testing.T) {
			t.Parallel()
			signal, ok := strings.CutPrefix(relType, observabilityCoverageEdgeLabel+"_")
			if !ok {
				t.Fatalf("derived type %q does not carry the %q prefix", relType, observabilityCoverageEdgeLabel+"_")
			}
			got, err := canonicalObservabilityCoverageCypherType(map[string]any{"coverage_signal": signal})
			if err != nil {
				t.Fatalf("derived type %q round-trips to signal %q the write path rejects: %v", relType, signal, err)
			}
			if got != relType {
				t.Fatalf("validator returns %q for signal %q, want the derived type %q", got, signal, relType)
			}
		})
	}
}

// TestContractFamilyDerivedTypesAreCanonicalEdgeTypes proves the closed static
// types behind the kind-A and kind-C derivations name edges the graph can
// carry: a typo'd or invented type that is internally consistent between a
// registry and its retract would make the live gate assert an always-empty
// population. The AWS_COVERS_<signal> types are deliberately absent here:
// signal-suffixed types are vocabulary-closed, not registry-listed, and their
// pin is the validator round-trip above, not canonical membership.
func TestContractFamilyDerivedTypesAreCanonicalEdgeTypes(t *testing.T) {
	t.Parallel()

	canonical := map[string]struct{}{}
	for _, e := range edgetype.All() {
		canonical[string(e)] = struct{}{}
	}
	if len(canonical) == 0 {
		t.Fatal("edgetype.All() is empty; this test would pass vacuously")
	}
	derived := map[string]struct{}{}
	for _, relType := range IncidentRoutingEvidenceRelationshipTypes() {
		derived[relType] = struct{}{}
	}
	for relType := range SecurityGroupSGRuleRelationshipTypes() {
		derived[relType] = struct{}{}
	}
	if _, ok := derived["HAS_TAINT_EVIDENCE"]; !ok {
		derived["HAS_TAINT_EVIDENCE"] = struct{}{}
	}
	for relType := range derived {
		if _, ok := canonical[relType]; !ok {
			t.Errorf("derived type %q is not a canonical edgetype; the live gate would assert an always-empty population for it", relType)
		}
	}
}

// evidenceNodeCreatorPattern finds a node-creating MERGE or CREATE of a
// private evidence label inside a Cypher string literal, capturing the label.
// AST-based, not a raw-text scan: only *ast.BasicLit STRING nodes are
// inspected, so a doc comment or a test-helper regex that quotes example
// Cypher can never be mistaken for a production template.
var evidenceNodeCreatorPattern = regexp.MustCompile(`(?:MERGE|CREATE)\s*\(\s*[A-Za-z_][A-Za-z0-9_]*\s*:\s*(CodeTaintEvidence|IncidentRoutingEvidence)\b`)

// evidenceNodeSoleCreators allowlists the single production file that may
// create nodes of each private evidence label. A second creator must turn the
// build red here: DETACH DELETE retracts are only safe while no other
// writer's edges can hang off these nodes.
var evidenceNodeSoleCreators = map[string]string{
	"CodeTaintEvidence":       "code_taint_evidence_writer.go",
	"IncidentRoutingEvidence": "incident_routing_evidence_writer.go",
}

// scanEvidenceNodeCreators walks root for non-test .go files and returns the
// files creating each evidence label, keyed by label with sorted,
// root-relative slash paths. Test files are excluded: they seed through fakes
// and DSN-gated backends, never through production writers.
func scanEvidenceNodeCreators(t *testing.T, root string) map[string][]string {
	t.Helper()
	found := map[string]map[string]struct{}{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				text = lit.Value
			}
			for _, m := range evidenceNodeCreatorPattern.FindAllStringSubmatch(text, -1) {
				if found[m[1]] == nil {
					found[m[1]] = map[string]struct{}{}
				}
				found[m[1]][rel] = struct{}{}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	out := map[string][]string{}
	for label, files := range found {
		for f := range files {
			out[label] = append(out[label], f)
		}
		sort.Strings(out[label])
	}
	return out
}

// checkEvidenceNodeSoleCreators returns a violation per evidence label whose
// creators are not exactly its allowlisted file: a missing file means the
// writer stopped creating its own nodes, an extra file means a second creator
// exists and the DETACH DELETE safety case is void. Files compare by basename
// so the census survives a package move; a same-named file in a second
// directory still reports (two creators), it just names both paths.
func checkEvidenceNodeSoleCreators(found map[string][]string) []string {
	var violations []string
	for label, wantFile := range evidenceNodeSoleCreators {
		got := found[label]
		bases := make([]string, 0, len(got))
		for _, f := range got {
			bases = append(bases, filepath.Base(f))
		}
		if len(got) != 1 || bases[0] != wantFile {
			violations = append(violations, label+": creators = "+strings.Join(got, ",")+", want exactly "+wantFile)
		}
	}
	sort.Strings(violations)
	return violations
}

// TestEvidenceNodeSoleCreatorsCensus is the kind-A safety case in executable
// form: exactly one production file creates each private evidence label. A
// second creator (or a writer that stops creating its nodes) fails here before
// any fixture or live gate runs.
func TestEvidenceNodeSoleCreatorsCensus(t *testing.T) {
	t.Parallel()

	found := scanEvidenceNodeCreators(t, "..")
	if violations := checkEvidenceNodeSoleCreators(found); len(violations) > 0 {
		t.Errorf("evidence-node sole-creator census failed:\n  %s", strings.Join(violations, "\n  "))
	}
}

// writeSeededGoFile writes one parseable non-test .go file holding content
// into dir, for the census sensitivity pair below.
func writeSeededGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestEvidenceNodeSoleCreatorsCensusSeededPair proves the census can fail: a
// clean tree passes the check, and planting a second creator file is detected
// and reported as a violation. If this pair ever stops distinguishing the
// two, the real-tree census above is a comment, not a guard.
func TestEvidenceNodeSoleCreatorsCensusSeededPair(t *testing.T) {
	t.Parallel()

	const taintWriterBody = "package seeded\nconst seededWriteCypher = `UNWIND $rows AS row\nMERGE (ev:CodeTaintEvidence {uid: row.uid})`\n"
	const routingWriterBody = "package seeded\nconst seededRoutingWriteCypher = `UNWIND $rows AS row\nMERGE (routing:IncidentRoutingEvidence {uid: row.uid})`\n"
	const secondCreatorBody = "package seeded\nconst sneakyWriteCypher = `UNWIND $rows AS row\nMERGE (ev:CodeTaintEvidence {uid: row.uid})`\n"

	clean := t.TempDir()
	writeSeededGoFile(t, clean, "code_taint_evidence_writer.go", taintWriterBody)
	writeSeededGoFile(t, clean, "incident_routing_evidence_writer.go", routingWriterBody)
	if violations := checkEvidenceNodeSoleCreators(scanEvidenceNodeCreators(t, clean)); len(violations) != 0 {
		t.Fatalf("clean tree reported violations, want none: %v", violations)
	}

	planted := t.TempDir()
	writeSeededGoFile(t, planted, "code_taint_evidence_writer.go", taintWriterBody)
	writeSeededGoFile(t, planted, "incident_routing_evidence_writer.go", routingWriterBody)
	writeSeededGoFile(t, planted, "second_creator.go", secondCreatorBody)
	violations := checkEvidenceNodeSoleCreators(scanEvidenceNodeCreators(t, planted))
	if len(violations) == 0 {
		t.Fatal("planted second creator reported no violation; the census cannot detect the failure it exists to catch")
	}
	matched := false
	for _, v := range violations {
		if strings.Contains(v, "CodeTaintEvidence") && strings.Contains(v, "second_creator.go") {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("violation does not name the planted creator: %v", violations)
	}
}
