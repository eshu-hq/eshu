// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Source-derived half of the doc-lockstep guard (see doc_lockstep_test.go for
// the rationale and conventions, and doc_lockstep_switch_test.go for the
// trigger-class scanner). The pins in the hand-written files compare the code
// against a list, which catches a removal but not an addition: a new tagged
// struct or a new qualified call is simply absent from the list, so nothing
// looks at it. These scanners read the declarations out of the source instead,
// so the pinned set has to be a complete inventory rather than a sample.
//
// Each scanner returns findings and the count of what it examined; the caller
// fails on them. That split is what makes them falsifiable -- a scanner that
// silently found nothing is indistinguishable from a clean package, and these
// read source off disk, so a compiler-level overlay cannot break them for a
// negative test. Every negative test below runs the same scanner over a
// fixture directory.

// parseNonTestGoFiles parses every non-test .go file in dir. It is the shared
// front half of the scanners below.
//
// It reports build-constrained files separately instead of parsing them. A file
// the compiler never sees is not the package's behavior, and a scanner that
// reads one is reading a decoy: `//go:build ignore` on an aliases.go declaring a
// pristine triggerAllowed sorts ahead of preflight.go, so the old scanner read
// the decoy while `go build` used the widened real one. Nothing in this package
// is build-constrained today, and TestDocLockstepNoBuildConstrainedFiles keeps
// it that way -- skipping a file silently is the same blind spot in the other
// direction.
func parseNonTestGoFiles(dir string) (fset *token.FileSet, files map[string]*ast.File, constrained []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	fset = token.NewFileSet()
	files = map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		if hasBuildConstraint(parsed) {
			constrained = append(constrained, name)
			continue
		}
		files[name] = parsed
	}
	sort.Strings(constrained)
	return fset, files, constrained, nil
}

// hasBuildConstraint reports whether file carries a //go:build or // +build
// line ahead of its package clause -- the marker that decides whether the
// compiler reads the file at all.
func hasBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
			if strings.HasPrefix(text, "go:build") || strings.HasPrefix(text, "+build") {
				return true
			}
		}
	}
	return false
}

// TestDocLockstepNoBuildConstrainedFiles keeps every scanner in this package
// reading the same files the compiler does. parseNonTestGoFiles skips
// build-constrained files so a decoy cannot answer for the real one; this is
// the other half of that bargain, so a constrained file that genuinely belongs
// in the package cannot hide from the scanners either.
func TestDocLockstepNoBuildConstrainedFiles(t *testing.T) {
	t.Parallel()

	_, files, constrained, err := parseNonTestGoFiles(".")
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("parsed 0 non-test files; the assertion would be vacuous")
	}
	for _, name := range constrained {
		t.Errorf("%s carries a build constraint, so the compiler may not read it while the doc-lockstep scanners skip it; keep production files unconstrained or teach the scanners the constraint", name)
	}
}

// scanTaggedStructs reports the name of every struct type declared in dir's
// non-test files that carries at least one json-tagged field, plus how many
// files it read. Discovering the type set instead of listing it is what turns
// docTaggedStructs from a pair of maps that agree with each other into an
// assertion about the package.
func scanTaggedStructs(dir string) (files int, names []string, err error) {
	_, parsed, _, err := parseNonTestGoFiles(dir)
	if err != nil {
		return 0, nil, err
	}
	for _, file := range parsed {
		files++
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok || structType.Fields == nil {
					continue
				}
				for _, field := range structType.Fields.List {
					if field.Tag != nil && strings.Contains(field.Tag.Value, "json:") {
						names = append(names, typeSpec.Name.Name)
						break
					}
				}
			}
		}
	}
	sort.Strings(names)
	return files, names, nil
}

// TestDocLockstepTaggedStructsAreDiscovered closes the gap docJSONFieldNames
// and docTaggedStructs share: both are hand-written, so their length check
// only proves they agree with each other. Dropping a type from both, or adding
// a tagged struct to the package and pinning neither, leaves them consistent
// and unpinned. This asserts the pinned key set is the set the source
// actually declares.
func TestDocLockstepTaggedStructsAreDiscovered(t *testing.T) {
	t.Parallel()

	files, discovered, err := scanTaggedStructs(".")
	if err != nil {
		t.Fatalf("scan tagged structs: %v", err)
	}
	if files == 0 || len(discovered) == 0 {
		t.Fatalf("scanned %d files and found %d tagged structs; the assertion would be vacuous", files, len(discovered))
	}

	pinnedNames := make([]string, 0, len(docTaggedStructs()))
	for name := range docTaggedStructs() {
		pinnedNames = append(pinnedNames, name)
	}
	sort.Strings(pinnedNames)
	if strings.Join(discovered, ",") != strings.Join(pinnedNames, ",") {
		t.Errorf("source declares json-tagged structs %v, docTaggedStructs pins %v; pin the new type's wire names or drop the type", discovered, pinnedNames)
	}
}

// TestDocLockstepTaggedStructScannerReportsNewStruct proves the scanner names
// a tagged struct rather than reporting an empty set, and that it ignores an
// untagged one.
func TestDocLockstepTaggedStructScannerReportsNewStruct(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixture := "package fixture\n\n" +
		"type Advisory struct {\n\tScopeID string `json:\"scopeId\"`\n}\n\n" +
		"type Plain struct {\n\tName string\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ignored := "package fixture\n\ntype FromTest struct {\n\tName string `json:\"name\"`\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(ignored), 0o600); err != nil {
		t.Fatalf("write ignored fixture: %v", err)
	}

	files, names, err := scanTaggedStructs(dir)
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if files != 1 {
		t.Fatalf("scanned %d files, want 1 (the _test.go fixture must be skipped)", files)
	}
	if strings.Join(names, ",") != "Advisory" {
		t.Fatalf("names = %v, want exactly [Advisory]", names)
	}
}

// callFinding is one qualified call a non-test file makes on an imported
// package that the doc's allow-list does not cover.
type callFinding struct {
	File     string
	Package  string
	Selector string
}

// scanQualifiedCalls reports every selector each non-test file uses on an
// imported package identifier, restricted to the packages named in allowed,
// and names the ones outside allowed[pkg]. It reports the total selector uses
// it examined so a caller can refuse a vacuous pass.
func scanQualifiedCalls(dir string, allowed map[string]map[string]bool) (uses int, findings []callFinding, err error) {
	_, parsed, _, err := parseNonTestGoFiles(dir)
	if err != nil {
		return 0, nil, err
	}
	names := make([]string, 0, len(parsed))
	for name := range parsed {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		file := parsed[name]
		imported := map[string]bool{}
		for _, spec := range file.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return uses, findings, unquoteErr
			}
			ident := path[strings.LastIndex(path, "/")+1:]
			if spec.Name != nil {
				ident = spec.Name.Name
			}
			imported[ident] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || !imported[pkg.Name] {
				return true
			}
			permitted, watched := allowed[pkg.Name]
			if !watched {
				return true
			}
			uses++
			if !permitted[selector.Sel.Name] {
				findings = append(findings, callFinding{File: name, Package: pkg.Name, Selector: selector.Sel.Name})
			}
			return true
		})
	}
	return uses, findings, nil
}

// docAllowedQualifiedCalls is the strong form of two README.md claims that the
// import set alone cannot carry. "Nothing in this package calls fmt.Print* to
// a process stream" holds because the only fmt selectors are Fprintf and
// Sprintf, and "the production files read no environment variable and touch no
// file" holds because the only path/filepath selectors are the four pure string
// operations. Both packages are on the import allow-list, so Println, Glob,
// WalkDir, and EvalSymlinks would all pass TestDocLockstepNonTestImports.
func docAllowedQualifiedCalls() map[string]map[string]bool {
	return map[string]map[string]bool{
		"fmt":      {"Fprintf": true, "Sprintf": true},
		"filepath": {"IsAbs": true, "Rel": true, "Clean": true, "ToSlash": true},
	}
}

// TestDocLockstepProductionCallsStayPure pins README.md's "the production
// files read no environment variable and touch no file" claim, doc.go's
// "writes to no process stream", and AGENTS.md's anti-pattern entry against
// the calls the source actually makes. The scanner reads only the non-test
// files, which is the same line the README's claim now draws.
func TestDocLockstepProductionCallsStayPure(t *testing.T) {
	t.Parallel()

	allowed := docAllowedQualifiedCalls()
	uses, findings, err := scanQualifiedCalls(".", allowed)
	if err != nil {
		t.Fatalf("scan qualified calls: %v", err)
	}
	if uses == 0 {
		t.Fatal("examined 0 fmt or path/filepath calls; the assertion would be vacuous")
	}
	for _, finding := range findings {
		t.Errorf("%s calls %s.%s, which the package docs rule out; either drop the call or correct README.md, doc.go, and AGENTS.md", finding.File, finding.Package, finding.Selector)
	}
}

// TestDocLockstepCallScannerReportsImpureCalls is the negative half: it drives
// the same scanner over a fixture that prints to stdout and reaches the
// filesystem, and proves it names both.
func TestDocLockstepCallScannerReportsImpureCalls(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixture := "package fixture\n\nimport (\n\t\"fmt\"\n\t\"path/filepath\"\n)\n\n" +
		"func Run(p string) string {\n\tfmt.Println(\"debug\")\n\tmatches, _ := filepath.Glob(p)\n" +
		"\tif len(matches) > 0 {\n\t\tp = matches[0]\n\t}\n\treturn filepath.Clean(p)\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	uses, findings, err := scanQualifiedCalls(dir, docAllowedQualifiedCalls())
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	if uses != 3 {
		t.Fatalf("examined %d calls, want 3 (Println, Glob, Clean)", uses)
	}
	got := make([]string, 0, len(findings))
	for _, finding := range findings {
		got = append(got, finding.Package+"."+finding.Selector)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "filepath.Glob,fmt.Println" {
		t.Fatalf("findings = %v, want exactly [filepath.Glob fmt.Println]", got)
	}
}

// The trigger-class scanner used to live here. It reads the accepted set out
// of triggerAllowed's own switch, so it now asserts that function's structure
// rather than pattern-matching one way to write `return true`; that is a big
// enough piece to own its file. See doc_lockstep_switch_test.go.
