// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

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

// mergeRelTypeFromWriteCypher returns the single relationship type a write
// template MERGEs, read out of the held const itself — the same
// const-reference approach as the registry guard in edge/materialized
// (which holds IdentityCypher rather than copied text). The type comes from
// the template, never from a literal copied into the test: a writer-template
// type change flows into the caller instead of leaving a stale string behind.
// Unlike that guard — identityPropertiesFromCypher + identityMergePropertyOpen
// in edge/materialized/families_test.go, which extracts the relationship-MERGE
// property map with an uppercase-only, brace-requiring regex that captures no
// type — this helper derives the relationship type with a regex that requires
// the `-->` arrow and captures the type. The two share only the held-const
// discipline, not the extraction itself.
func mergeRelTypeFromWriteCypher(t *testing.T, family, cypher string) string {
	t.Helper()
	matches := regexp.MustCompile(`-\[\s*\w*\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\s*\]->`).FindAllStringSubmatch(cypher, -1)
	if len(matches) != 1 {
		t.Fatalf("family %q: write template MERGEs %d relationship types, want exactly 1 for a single-type derivation:\n%s", family, len(matches), cypher)
	}
	return matches[0][1]
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
	// The security-group family moved to the cloud/aws/security/group leaf
	// with its writer (#6694); its derived-types-are-canonical pin lives in
	// the leaf's registry_contract_test.go, following the same discipline.
	// Derived from the held write template via mergeRelTypeFromWriteCypher,
	// never copied: if the writer template ever changes type, this follows it
	// and the canonical-membership check below turns red on a stale registry
	// instead of asserting membership of a stale string.
	derived[mergeRelTypeFromWriteCypher(t, "code_taint_evidence", CodeTaintEvidenceUpsertCypher)] = struct{}{}
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
var evidenceNodeCreatorPattern = regexp.MustCompile(`(?:MERGE|CREATE)\s*\(\s*[A-Za-z_][A-Za-z0-9_]*\s*:\s*(CodeTaintEvidence|IncidentRoutingEvidence|SecretsIAMServiceAccount|SecretsIAMVaultAuthRole|SecretsIAMVaultPolicy|SecretsIAMSecretMetadataPath)\b`)

// evidenceNodeSoleCreators allowlists the single production file that may
// create nodes of each private evidence label. A second creator must turn the
// build red here: DETACH DELETE retracts are only safe while no other
// writer's edges can hang off these nodes.
var evidenceNodeSoleCreators = map[string]string{
	"CodeTaintEvidence":            "code_taint_evidence.go",
	"IncidentRoutingEvidence":      "incident_routing_evidence.go",
	"SecretsIAMServiceAccount":     "secrets_graph_writer.go",
	"SecretsIAMVaultAuthRole":      "secrets_graph_writer.go",
	"SecretsIAMVaultPolicy":        "secrets_graph_writer.go",
	"SecretsIAMSecretMetadataPath": "secrets_graph_writer.go",
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

// evidenceEdgeAttachPattern finds a relationship MERGE or CREATE anchored at
// a node pattern, either arrow direction, inside a Cypher string literal. A
// literal that references a private evidence label and MERGEs or CREATEs a
// relationship attaches an edge to that label's nodes, whether or not it
// creates them: it covers the MATCH-existing-then-attach-edge writer the
// node-creator scan cannot see. Both verbs matter because DETACH DELETE
// cascades CREATE-attached edges the same way, and both arrow directions
// matter because a left-arrow attacher (`MERGE (ev)<-[rel:USES]-(f)`) hangs a
// foreign edge off the node just as a right-arrow one does.
var evidenceEdgeAttachPattern = regexp.MustCompile(`(?:MERGE|CREATE)\s*\([^)]*\)\s*(?:-\s*\[|<-\s*\[)`)

// evidenceLabelAnchorPattern matches a Cypher label reference (`:Label`) for
// either private evidence label. The colon keeps prose mentions such as
// "CodeTaintEvidenceWriter" in a description string from anchoring the scan;
// only a real node-pattern reference counts.
var evidenceLabelAnchorPattern = regexp.MustCompile(`:\s*(CodeTaintEvidence|IncidentRoutingEvidence|SecretsIAMServiceAccount|SecretsIAMVaultAuthRole|SecretsIAMVaultPolicy|SecretsIAMSecretMetadataPath)\b`)

// scanEvidenceEdgeAttachers walks root for non-test .go files and returns the
// files attaching edges to each evidence label, keyed by label with sorted,
// root-relative slash paths. A file counts when one string literal both
// references the label as a node pattern and MERGEs or CREATEs a
// relationship. Test
// files are excluded like in scanEvidenceNodeCreators: they seed foreign
// edges through fakes and DSN-gated backends, never through production
// writers.
func scanEvidenceEdgeAttachers(t *testing.T, root string) map[string][]string {
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
			if !evidenceEdgeAttachPattern.MatchString(text) {
				return true
			}
			for _, m := range evidenceLabelAnchorPattern.FindAllStringSubmatch(text, -1) {
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

// checkEvidenceEdgeAttachers returns a violation per evidence label whose
// edge-attachers are not exactly its allowlisted owner file. The owner MERGEs
// its own edge in the same write template that creates the nodes, so it is
// the one expected attacher; a second file MERGEing an edge onto these nodes
// means DETACH DELETE would cascade-delete foreign edges with them, and a
// missing owner means the writer stopped attaching its own edge. Files compare
// by basename like the creator check.
func checkEvidenceEdgeAttachers(found map[string][]string) []string {
	var violations []string
	for label, wantFile := range evidenceNodeSoleCreators {
		got := found[label]
		bases := make([]string, 0, len(got))
		for _, f := range got {
			bases = append(bases, filepath.Base(f))
		}
		if len(got) != 1 || bases[0] != wantFile {
			violations = append(violations, label+": edge-attachers = "+strings.Join(got, ",")+", want exactly "+wantFile)
		}
	}
	sort.Strings(violations)
	return violations
}

// TestEvidenceNodeSoleCreatorsCensus is the kind-A safety case in executable
// form: exactly one production file creates each private evidence label, and
// exactly that owner file MERGEs relationships onto those nodes. A second
// creator (or a writer that stops creating its nodes) fails here before any
// fixture or live gate runs, as does a second writer that MATCHes an existing
// evidence node and MERGEs its own edge onto it — a pattern the node-creation
// scan cannot see, yet DETACH DELETE cascades those foreign edges with the
// node.
func TestEvidenceNodeSoleCreatorsCensus(t *testing.T) {
	t.Parallel()

	found := scanEvidenceNodeCreators(t, "../../..")
	if violations := checkEvidenceNodeSoleCreators(found); len(violations) > 0 {
		t.Errorf("evidence-node sole-creator census failed:\n  %s", strings.Join(violations, "\n  "))
	}
	attached := scanEvidenceEdgeAttachers(t, "../../..")
	if violations := checkEvidenceEdgeAttachers(attached); len(violations) > 0 {
		t.Errorf("evidence-edge attacher census failed:\n  %s", strings.Join(violations, "\n  "))
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
	const secretsWriterBody = "package seeded\nconst seededSecretsWriteCypher = `UNWIND $rows AS row\nMERGE (sa:SecretsIAMServiceAccount {uid: row.uid})\nMERGE (vr:SecretsIAMVaultAuthRole {uid: row.uid})\nMERGE (pol:SecretsIAMVaultPolicy {uid: row.uid})\nMERGE (path:SecretsIAMSecretMetadataPath {uid: row.uid})`\n"
	const secondCreatorBody = "package seeded\nconst sneakyWriteCypher = `UNWIND $rows AS row\nMERGE (ev:CodeTaintEvidence {uid: row.uid})`\n"

	clean := t.TempDir()
	writeSeededGoFile(t, clean, "code_taint_evidence.go", taintWriterBody)
	writeSeededGoFile(t, clean, "incident_routing_evidence.go", routingWriterBody)
	writeSeededGoFile(t, clean, "secrets_graph_writer.go", secretsWriterBody)
	if violations := checkEvidenceNodeSoleCreators(scanEvidenceNodeCreators(t, clean)); len(violations) != 0 {
		t.Fatalf("clean tree reported violations, want none: %v", violations)
	}

	planted := t.TempDir()
	writeSeededGoFile(t, planted, "code_taint_evidence.go", taintWriterBody)
	writeSeededGoFile(t, planted, "incident_routing_evidence.go", routingWriterBody)
	writeSeededGoFile(t, planted, "secrets_graph_writer.go", secretsWriterBody)
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

// TestEvidenceEdgeAttachersCensusSeededPair proves the edge-attachment census
// can fail: a clean tree whose owners MERGE their own edges passes the check,
// and planting a second writer that MATCHes an existing evidence node and
// attaches its own edge is detected and reported as a violation. Each
// attacher shape the pattern claims — right-arrow MERGE, left-arrow MERGE,
// and CREATE — gets its own planted tree, so a verb or direction the pattern
// stops seeing turns exactly one subtest red. If this pair ever stops
// distinguishing clean from planted, the attacher census above is a comment,
// not a guard.
func TestEvidenceEdgeAttachersCensusSeededPair(t *testing.T) {
	t.Parallel()

	const taintWriterBody = "package seeded\nconst seededWriteCypher = `UNWIND $rows AS row\nMATCH (f:Function {uid: row.function_uid})\nMERGE (ev:CodeTaintEvidence {uid: row.uid})\nMERGE (f)-[rel:HAS_TAINT_EVIDENCE]->(ev)`\n"
	const routingWriterBody = "package seeded\nconst seededRoutingWriteCypher = `UNWIND $rows AS row\nMERGE (incident:IncidentRoutingEvidence {uid: row.incident_uid})\nMERGE (routing:IncidentRoutingEvidence {uid: row.uid})\nMERGE (incident)-[rel:HAS_INTENDED_ROUTING]->(routing)`\n"
	const secretsWriterBody = "package seeded\nconst seededSecretsWriteCypher = `UNWIND $rows AS row\nMERGE (sa:SecretsIAMServiceAccount {uid: row.uid})\nMERGE (vr:SecretsIAMVaultAuthRole {uid: row.uid})\nMERGE (pol:SecretsIAMVaultPolicy {uid: row.uid})\nMERGE (path:SecretsIAMSecretMetadataPath {uid: row.uid})\nMERGE (sa)-[rel:SECRETS_IAM_AUTHENTICATES_TO_VAULT_ROLE]->(vr)`\n"

	clean := t.TempDir()
	writeSeededGoFile(t, clean, "code_taint_evidence.go", taintWriterBody)
	writeSeededGoFile(t, clean, "incident_routing_evidence.go", routingWriterBody)
	writeSeededGoFile(t, clean, "secrets_graph_writer.go", secretsWriterBody)
	if violations := checkEvidenceEdgeAttachers(scanEvidenceEdgeAttachers(t, clean)); len(violations) != 0 {
		t.Fatalf("clean tree reported violations, want none: %v", violations)
	}

	planted := []struct {
		name string
		file string
		body string
	}{
		{
			name: "right-arrow MERGE attacher",
			file: "edge_attacher.go",
			body: "package seeded\nconst sneakyEdgeCypher = `MATCH (ev:CodeTaintEvidence {uid: row.uid})\nMERGE (f)-[rel:USES]->(ev)`\n",
		},
		{
			name: "left-arrow MERGE attacher",
			file: "left_arrow_attacher.go",
			body: "package seeded\nconst sneakyLeftEdgeCypher = `MATCH (ev:CodeTaintEvidence {uid: row.uid})\nMERGE (ev)<-[rel:USES]-(f)`\n",
		},
		{
			name: "CREATE-verb attacher",
			file: "create_attacher.go",
			body: "package seeded\nconst sneakyCreateEdgeCypher = `MATCH (ev:CodeTaintEvidence {uid: row.uid})\nCREATE (f)-[rel:USES]->(ev)`\n",
		},
	}
	for _, p := range planted {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeSeededGoFile(t, dir, "code_taint_evidence.go", taintWriterBody)
			writeSeededGoFile(t, dir, "incident_routing_evidence.go", routingWriterBody)
			writeSeededGoFile(t, dir, "secrets_graph_writer.go", secretsWriterBody)
			writeSeededGoFile(t, dir, p.file, p.body)
			violations := checkEvidenceEdgeAttachers(scanEvidenceEdgeAttachers(t, dir))
			if len(violations) == 0 {
				t.Fatalf("planted %q reported no violation; the census cannot detect that attacher shape", p.name)
			}
			matched := false
			for _, v := range violations {
				if strings.Contains(v, "CodeTaintEvidence") && strings.Contains(v, p.file) {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("violation does not name the planted %q: %v", p.file, violations)
			}
		})
	}
}
