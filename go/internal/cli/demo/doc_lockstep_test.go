// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

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
)

// packageSourceFiles parses every non-test .go file in the package directory.
// A parse failure is fatal rather than skipped: a test that silently reads
// nothing would report a drifted doc as clean, which is the failure mode both
// tests in this file exist to prevent.
func packageSourceFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		files = append(files, file)
	}
	if len(files) < 5 {
		t.Fatalf("parsed only %d non-test .go files; expected the package's sources to be present", len(files))
	}
	return fset, files
}

// exportedSurface collects what this package actually exports: package-level
// declarations, and the exported method names hanging off them. Methods are
// returned as both `Type.Method` and bare `Method` because README.md cites
// them both ways (`Runtime.Up`, but `BenchmarkVerdict` "with `Criterion`").
func exportedSurface(files []*ast.File) map[string]bool {
	surface := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv == nil {
					surface[d.Name.Name] = true
					continue
				}
				surface[d.Name.Name] = true
				if recv := receiverTypeName(d.Recv); recv != "" {
					surface[recv+"."+d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							surface[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, id := range s.Names {
							if id.IsExported() {
								surface[id.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return surface
}

// receiverTypeName reduces a method receiver to its bare type name, dropping
// any pointer star, so `func (r *Runtime) Up` reports `Runtime`.
func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	expr := recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

var backtickedRE = regexp.MustCompile("`([^`]+)`")

// readmeExportedSurfaceClaims returns every backticked name inside README.md's
// machine-checked markers. Names carrying a `/` (import paths) or a
// lower-cased qualifier (`firstrun.EnvelopeError`) are another package's and
// are skipped; what remains is what this package is claiming for itself.
func readmeExportedSurfaceClaims(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	body := string(raw)

	const beginMarker = "<!-- exported-surface:begin -->"
	const endMarker = "<!-- exported-surface:end -->"
	begin := strings.Index(body, beginMarker)
	end := strings.Index(body, endMarker)
	if begin < 0 || end < 0 || end <= begin {
		t.Fatalf("README.md must delimit its exported-surface list with %q and %q; the list is unchecked without them",
			beginMarker, endMarker)
	}
	section := body[begin+len(beginMarker) : end]

	claims := make([]string, 0, 40)
	for _, match := range backtickedRE.FindAllStringSubmatch(section, -1) {
		name := strings.TrimSpace(match[1])
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, " ") {
			continue
		}
		// Another package's symbol, e.g. firstrun.EnvelopeError. This
		// package's own methods are cited as Type.Method, which starts upper.
		if qualifier, _, isQualified := strings.Cut(name, "."); isQualified && !ast.IsExported(qualifier) {
			continue
		}
		if !ast.IsExported(strings.TrimPrefix(name, ".")) {
			continue
		}
		claims = append(claims, name)
	}
	sort.Strings(claims)
	return claims
}

// TestREADMEExportedSurfaceIsReal pins README.md's "Exported surface" list to
// the package's real exported API.
//
// The list drifted in exactly the way an unchecked list does: it advertised
// `RequiredPhases` after the phase list was renamed back to the unexported
// `requiredPhases`, and it claimed `CriterionName`/`CriterionStatus`, which
// belong to firstrunbench and were never this package's to export. Neither was
// caught by scripts/verify-package-docs.sh, which checks that README.md,
// doc.go, and AGENTS.md EXIST, not that what they say is true.
func TestREADMEExportedSurfaceIsReal(t *testing.T) {
	t.Parallel()

	_, files := packageSourceFiles(t)
	surface := exportedSurface(files)
	claims := readmeExportedSurfaceClaims(t)

	if len(claims) < 20 {
		t.Fatalf("only %d exported-surface claims parsed out of README.md; the marker section is not being read", len(claims))
	}

	for _, name := range claims {
		if !surface[name] {
			t.Errorf("README.md's exported-surface list claims %q, which package demo does not export; "+
				"either export it or move it out of the marked list (an unexported or another package's symbol belongs in the prose below the end marker)",
				name)
		}
	}
}

// TestSideEffectSurfaceMatchesDocGo pins the two totality claims doc.go makes
// about this package's side effects:
//
//   - "Nine docker invocations, all through the ExecFunc seam"
//   - "Two file reads and no writes: os.Stat ... and os.ReadFile in
//     LoadManifest" (plus the os.Environ disclosure in the paragraph below it)
//
// AGENTS.md tells contributors to add a new docker call to doc.go's
// enumeration by hand and says "that list is meant to be exhaustive; a call
// added without updating it makes the doc wrong rather than incomplete". That
// instruction had nothing enforcing it. Both claims are asserted as counts and
// SET EQUALITY rather than deny-lists, so a call nobody thought to ban fails
// them too — which is how a totality claim actually decays.
func TestSideEffectSurfaceMatchesDocGo(t *testing.T) {
	t.Parallel()

	// The exact numbers doc.go states. Changing either means changing doc.go's
	// enumeration in the same commit.
	const wantExecCalls = 9
	wantOSSelectors := []string{"Environ", "ReadFile", "Stat"}

	fset, files := packageSourceFiles(t)
	execCalls := 0
	gotOS := map[string]bool{}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				// r.exec(ctx, env, "docker", ...) — the single seam every
				// subprocess in this package goes through.
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "exec" {
					execCalls++
					if !callPassesDockerAsArgv0(node) {
						t.Errorf("%s: exec call does not pass \"docker\" as argv[0]; doc.go states every invocation does",
							fset.Position(node.Pos()))
					}
				}
			case *ast.SelectorExpr:
				if pkg, ok := node.X.(*ast.Ident); ok && pkg.Name == "os" {
					gotOS[node.Sel.Name] = true
				}
			}
			return true
		})
	}

	if execCalls != wantExecCalls {
		t.Errorf("docker invocations through the exec seam = %d, want %d; doc.go enumerates them exhaustively, so a call added or removed without updating that list makes the doc wrong",
			execCalls, wantExecCalls)
	}

	got := make([]string, 0, len(gotOS))
	for name := range gotOS {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(wantOSSelectors, ",") {
		t.Errorf("os selectors used = %v, want exactly %v; doc.go claims os.Stat and os.ReadFile are the only file access and names os.Environ as the one other os call, so any difference means the code and the claim have diverged",
			got, wantOSSelectors)
	}
}

// callPassesDockerAsArgv0 reports whether an exec-seam call passes the literal
// "docker" as its command name. The seam's signature is
// exec(ctx, env, name, args...), so argv[0] is the third argument.
func callPassesDockerAsArgv0(call *ast.CallExpr) bool {
	const nameArgIndex = 2
	if len(call.Args) <= nameArgIndex {
		return false
	}
	lit, ok := call.Args[nameArgIndex].(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `"docker"`
}

// TestDirectImportsMatchTheDependenciesSection is the standing guard behind
// README.md's Dependencies claim, which names this package's direct imports
// exactly.
//
// It is a SET EQUALITY, not a deny-list: a dependency nobody thought to ban
// fails it too, which is how a totality claim actually decays. It caught real
// drift — when internal/cli/firstrun was extracted, EnvelopeError moved there
// and this package gained a second Eshu import while the README still named
// only firstrunbench.
func TestDirectImportsMatchTheDependenciesSection(t *testing.T) {
	t.Parallel()

	// The exact set README.md's Dependencies section names. Widen this only by
	// widening that section in the same change.
	want := []string{
		"github.com/eshu-hq/eshu/go/internal/cli/firstrun",
		"github.com/eshu-hq/eshu/go/internal/cli/firstrunbench",
		"gopkg.in/yaml.v3",
	}

	_, files := packageSourceFiles(t)
	got := map[string]bool{}
	for _, file := range files {
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			// A standard-library path's first segment carries no dot; every
			// module path's does (github.com/..., gopkg.in/...).
			if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
				got[path] = true
			}
		}
	}

	names := make([]string, 0, len(got))
	for path := range got {
		names = append(names, path)
	}
	sort.Strings(names)

	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("non-stdlib direct imports = %v, want exactly %v; README.md's Dependencies section names the set, so any difference means the code and the claim have diverged",
			names, want)
	}
}
