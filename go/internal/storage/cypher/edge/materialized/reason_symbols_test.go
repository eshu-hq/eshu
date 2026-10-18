// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

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

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/inheritance"
)

// goIdentifierInReason matches the Go identifiers the registries cite inside
// their human-readable reason strings — the template or retract constant that
// writes or reaps each edge type.
var goIdentifierInReason = regexp.MustCompile(`\b(batchCanonical\w+|canonical\w+Cypher|retract\w+Cypher)\b`)

var inheritanceCitation = regexp.MustCompile(`\binheritance\.([A-Za-z_]\w*)\b`)

var parenthesizedInheritanceCitation = regexp.MustCompile(`\(\s*inheritance[^)]*\)`)

var qualifiedInheritanceCitation = regexp.MustCompile(`\(\s*inheritance\.([A-Za-z_]\w*)\s*\)`)

var cypherRelationshipType = regexp.MustCompile(`-\[\w+:([A-Z][A-Z0-9_]*)\]->`)

// inheritanceStringConstants parses string constants declared by the owning
// inheritance package. It intentionally does not include test files or symbols
// from neighboring packages.
func inheritanceStringConstants(t *testing.T, dir string, packageName string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read inheritance package dir: %v", err)
	}
	fset := token.NewFileSet()
	declared := map[string]struct{}{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if file.Name.Name != packageName {
			t.Fatalf("parse %s: package is %q, want %q", name, file.Name.Name, packageName)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || !isStringConstant(value) {
					continue
				}
				for _, ident := range value.Names {
					declared[ident.Name] = struct{}{}
				}
			}
		}
	}
	return declared
}

func isStringConstant(value *ast.ValueSpec) bool {
	if ident, ok := value.Type.(*ast.Ident); ok && ident.Name == "string" {
		return true
	}
	if len(value.Values) != len(value.Names) {
		return false
	}
	for _, expr := range value.Values {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return false
		}
	}
	return len(value.Values) > 0
}

func validateInheritanceCitations(registry map[string]string, declared map[string]struct{}) []string {
	var missing []string
	for edgeType, reason := range registry {
		citations := inheritanceCitation.FindAllStringSubmatch(reason, -1)
		forms := parenthesizedInheritanceCitation.FindAllString(reason, -1)
		qualified := qualifiedInheritanceCitation.FindAllStringSubmatch(reason, -1)
		if len(forms) != 1 || len(citations) != 1 || len(qualified) != 1 || citations[0][1] != qualified[0][1] {
			missing = append(missing, edgeType+" must contain exactly one parenthesized inheritance.<StringConstant> citation")
			continue
		}
		if _, ok := declared[qualified[0][1]]; !ok {
			missing = append(missing, edgeType+" cites missing inheritance."+qualified[0][1])
		}
	}
	return missing
}

func validateInheritanceTemplateEdges(registry map[string]string, templateEdges map[string]string) []string {
	var mismatches []string
	for edgeType, reason := range registry {
		citations := qualifiedInheritanceCitation.FindAllStringSubmatch(reason, -1)
		if len(citations) != 1 {
			continue
		}
		symbol := citations[0][1]
		if actual, ok := templateEdges[symbol]; !ok {
			mismatches = append(mismatches, edgeType+" cites inheritance."+symbol+" without a parsed relationship type")
		} else if actual != edgeType {
			mismatches = append(mismatches, edgeType+" cites inheritance."+symbol+", which writes "+actual)
		}
	}
	return mismatches
}

func inheritanceTemplateEdges(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read inheritance package dir: %v", err)
	}
	fset := token.NewFileSet()
	edges := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || !isStringConstant(value) || len(value.Names) != len(value.Values) {
					continue
				}
				for index, name := range value.Names {
					literal, ok := value.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					query, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", name.Name, err)
					}
					matches := cypherRelationshipType.FindAllStringSubmatch(query, -1)
					if len(matches) == 1 {
						edges[name.Name] = matches[0][1]
					}
				}
			}
		}
	}
	return edges
}

func TestValidateInheritanceCitationsRejectsBrokenReasons(t *testing.T) {
	t.Parallel()

	wrongPackage := t.TempDir()
	if err := os.WriteFile(filepath.Join(wrongPackage, "other.go"), []byte("package other\nconst BatchUpsertCypher = `MATCH (n)`\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := t.TempDir()
	if err := os.WriteFile(filepath.Join(owner, "other.go"), []byte("package inheritance\nconst DifferentCypher = `MATCH (n)`\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	declared := inheritanceStringConstants(t, owner, "inheritance")
	wrongPackageSymbols := inheritanceStringConstants(t, wrongPackage, "other")
	if _, ok := wrongPackageSymbols["BatchUpsertCypher"]; !ok {
		t.Fatal("wrong-package fixture does not declare the citation symbol")
	}
	if _, ok := declared["BatchUpsertCypher"]; ok {
		t.Fatal("owner-package fixture unexpectedly declares the wrong-package symbol")
	}

	cases := []struct {
		name   string
		reason string
	}{
		{name: "nonexistent template", reason: "class/type inheritance (inheritance.DoesNotExist)"},
		{name: "missing citation", reason: "class/type inheritance"},
		{name: "malformed citation", reason: "class/type inheritance (inheritance)"},
		{name: "same symbol only in another package", reason: "class/type inheritance (inheritance.BatchUpsertCypher)"},
		{name: "extra malformed citation", reason: "class/type inheritance (inheritance.DifferentCypher) (inheritance)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateInheritanceCitations(map[string]string{"INHERITS": tc.reason}, declared); len(got) == 0 {
				t.Fatalf("validateInheritanceCitations(%q) unexpectedly succeeded", tc.reason)
			}
		})
	}
}

// packageIdentifiers returns every identifier declared in this package's
// non-test sources.
func packageIdentifiers(t *testing.T) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	declared := map[string]struct{}{}
	decl := regexp.MustCompile(`(?m)^(?:func|const|var|type)\s+\(?\s*(\w+)|^\s*(\w+)\s*=\s*` + "`")
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range decl.FindAllStringSubmatch(string(raw), -1) {
			for _, g := range m[1:] {
				if g != "" {
					declared[g] = struct{}{}
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no identifiers parsed from the package; this test would pass vacuously")
	}
	return declared
}

// TestRegistryReasonsCiteRealSymbols catches registry reasons that name a
// function or constant which does not exist.
//
// Every materialized-edge registry maps an edge type to a reason naming what
// writes it, and those names are the first thing a maintainer greps for when a
// family's coverage looks wrong. Three separate reasons in this package have
// already cited invented symbols — two template constants whose real names were
// different, and a dispatch function that had been renamed — because prose is
// not compiled and nothing checked it.
//
// The regex deliberately matches only the repo's own naming conventions for
// these artifacts. A reason mentioning something outside those shapes is not
// checked here; the point is to pin the citations that are load-bearing for
// navigation, not to police English.
func TestRegistryReasonsCiteRealSymbols(t *testing.T) {
	t.Parallel()

	declared := packageIdentifiers(t)

	registries := map[string]map[string]string{
		"code_calls":      sourcecypher.CodeCallMaterializedEdgeTypes(),
		"repo_dependency": RepoDependencyMaterializedEdgeTypes(),
	}
	for _, family := range SingleTypeMaterializedEdgeFamilyNames() {
		reg, ok := SingleTypeMaterializedEdgeTypes(family)
		if !ok {
			t.Fatalf("family %q vanished between listing and lookup", family)
		}
		registries[family] = reg
	}

	var missing []string
	for family, registry := range registries {
		for edgeType, reason := range registry {
			for _, m := range goIdentifierInReason.FindAllString(reason, -1) {
				if _, ok := declared[m]; !ok {
					missing = append(missing, family+"/"+edgeType+" cites "+m)
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("registry reasons cite symbols this package does not declare:\n  %s\nA maintainer grepping for one of these finds nothing and cannot verify the family's derivation.",
			strings.Join(missing, "\n  "))
	}
}

// writerFileCitation matches the multi-type DIRECT table's reason convention:
// the writer symbol plus the cypher-package file that declares it, relative
// to the cypher package root, e.g.
// "(SecurityGroupReachabilityWriter in cloud/aws/security/group/security_reachability_edge_writer.go)".
var writerFileCitation = regexp.MustCompile(`\(\s*([A-Za-z_]\w*)\s+in\s+([a-z0-9_/]+\.go)\s*\)`)

// declaredTopLevelNames returns every top-level declared name in one
// non-test .go file: funcs, types, consts and vars, including entries inside
// a const/var block (where the exported registry-holder aliases live).
func declaredTopLevelNames(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	declared := map[string]struct{}{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			declared[d.Name.Name] = struct{}{}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range s.Names {
						declared[name.Name] = struct{}{}
					}
				case *ast.TypeSpec:
					declared[s.Name.Name] = struct{}{}
				}
			}
		}
	}
	return declared
}

// TestMultiTypeDirectReasonsCiteOwningWriter ranges the multi-type DIRECT
// table's reasons the same way TestRegistryReasonsCiteRealSymbols ranges the
// single-type ones: every reason must cite its owning writer as "(<Symbol> in
// <file>)", the file must exist in the parent cypher package, and the symbol
// must be declared there. A reason citing a renamed writer or a moved file
// fails here instead of sending the next maintainer grepping for nothing.
func TestMultiTypeDirectReasonsCiteOwningWriter(t *testing.T) {
	t.Parallel()

	var missing []string
	for _, family := range MultiTypeDirectMaterializedEdgeFamilyNames() {
		reg, ok := MultiTypeDirectMaterializedEdgeTypes(family)
		if !ok {
			t.Fatalf("family %q vanished between listing and lookup", family)
		}
		for edgeType, reason := range reg {
			citations := writerFileCitation.FindAllStringSubmatch(reason, -1)
			if len(citations) == 0 {
				missing = append(missing, family+"/"+edgeType+" cites no (Writer in file.go) owner")
				continue
			}
			for _, m := range citations {
				symbol, file := m[1], filepath.Join("..", "..", m[2])
				if _, err := os.Stat(filepath.Clean(file)); err != nil {
					missing = append(missing, family+"/"+edgeType+" cites missing file "+m[2])
					continue
				}
				if _, ok := declaredTopLevelNames(t, file)[symbol]; !ok {
					missing = append(missing, family+"/"+edgeType+" cites undeclared symbol "+symbol+" in "+m[2])
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("multi-type DIRECT registry reasons cite missing writers:\n  %s", strings.Join(missing, "\n  "))
	}
}

func TestInheritanceRegistryReasonsCiteOwningPackage(t *testing.T) {
	t.Parallel()

	registry := inheritance.MaterializedEdgeTypes()
	if len(registry) != 4 {
		t.Fatalf("inheritance registry has %d entries, want all four relationship types", len(registry))
	}
	dir := filepath.Join("..", "..", "canonical", "inheritance")
	declared := inheritanceStringConstants(t, dir, "inheritance")
	if missing := validateInheritanceCitations(registry, declared); len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("inheritance registry reasons cite missing or malformed owning-package symbols:\n  %s", strings.Join(missing, "\n  "))
	}
	if mismatches := validateInheritanceTemplateEdges(registry, inheritanceTemplateEdges(t, dir)); len(mismatches) > 0 {
		t.Fatalf("inheritance registry reasons cite templates for different edge types:\n  %s", strings.Join(mismatches, "\n  "))
	}
}

func TestInheritanceCitationGuardRejectsWrongExistingTemplate(t *testing.T) {
	t.Parallel()

	registry := inheritance.MaterializedEdgeTypes()
	registry["INHERITS"] = "class/type inheritance (inheritance.BatchOverrideUpsertCypher)"
	templateEdges := inheritanceTemplateEdges(t, filepath.Join("..", "..", "canonical", "inheritance"))
	if got := validateInheritanceTemplateEdges(registry, templateEdges); len(got) == 0 {
		t.Fatal("wrong existing template citation passed; want edge/template mismatch")
	}
}
