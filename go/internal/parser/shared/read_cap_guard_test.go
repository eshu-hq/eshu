// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// uncappedReadAllowlist names the parser-tree files that may read a whole file
// without ReadFileCapped, each with the reason (#8986 review P2-1).
var uncappedReadAllowlist = map[string]string{
	"hcl/include_chain.go":        "Terragrunt include walker: Lstat-checked 1 MiB cap of its own before the read",
	"scip/parser.go":              "SCIP index the indexer wrote, not repository content",
	"goldenaudit/golden_audit.go": "checked-in golden fixtures read by the audit tool",
	"shared/read_cap.go":          "the capped read itself",
}

// uncappedReadCalls maps an import path to its functions that read a whole
// file or stream and so bypass the per-file cap.
var uncappedReadCalls = map[string]map[string]bool{
	"os":        {"ReadFile": true},
	"io":        {"ReadAll": true},
	"io/ioutil": {"ReadFile": true, "ReadAll": true},
	"io/fs":     {"ReadFile": true},
}

// uncappedReadCallsIn returns each uncapped whole-file read in file as
// "path.Func". It resolves the file's own import names, so an aliased import
// (`osx "os"`) or a dot import (`. "os"`, then a bare ReadFile) is caught the
// same as the default name.
func uncappedReadCallsIn(file *ast.File) []string {
	byName := map[string]string{}
	dotImported := map[string]bool{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || uncappedReadCalls[importPath] == nil {
			continue
		}
		name := path.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		switch name {
		case "_":
		case ".":
			dotImported[importPath] = true
		default:
			byName[name] = importPath
		}
	}
	var found []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok {
				if importPath, ok := byName[pkg.Name]; ok && uncappedReadCalls[importPath][fun.Sel.Name] {
					found = append(found, importPath+"."+fun.Sel.Name)
				}
			}
		case *ast.Ident:
			for importPath := range dotImported {
				if uncappedReadCalls[importPath][fun.Name] {
					found = append(found, importPath+"."+fun.Name)
				}
			}
		}
		return true
	})
	return found
}

// TestUncappedReadCallsInResolvesImportNames (#8986 review P3-1) seeds each
// import form the guard must see through.
func TestUncappedReadCallsInResolvesImportNames(t *testing.T) {
	for _, cell := range []struct {
		name string
		src  string
		want string
	}{
		{name: "default name", src: `import "os"; func f() { os.ReadFile("x") }`, want: "os.ReadFile"},
		{name: "alias", src: `import osx "os"; func f() { osx.ReadFile("x") }`, want: "os.ReadFile"},
		{name: "dot import", src: `import . "os"; func f() { ReadFile("x") }`, want: "os.ReadFile"},
		{name: "io/fs default", src: `import "io/fs"; func f() { fs.ReadFile(nil, "x") }`, want: "io/fs.ReadFile"},
		{name: "ioutil alias", src: `import u "io/ioutil"; func f() { u.ReadAll(nil) }`, want: "io/ioutil.ReadAll"},
		{name: "io alias", src: `import stdio "io"; func f() { stdio.ReadAll(nil) }`, want: "io.ReadAll"},
		{name: "local identifier named os", src: `func f() { var os reader; os.ReadFile("x") }`, want: ""},
		{name: "capped read", src: `import "os"; func f() { os.Open("x") }`, want: ""},
	} {
		t.Run(cell.name, func(t *testing.T) {
			file, err := goparser.ParseFile(token.NewFileSet(), "seed.go", "package seed\n"+cell.src, goparser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse seed: %v", err)
			}
			got := strings.Join(uncappedReadCallsIn(file), ",")
			if got != cell.want {
				t.Fatalf("uncappedReadCallsIn = %q, want %q", got, cell.want)
			}
		})
	}
}

// TestParserTreeReadsRepositoryFilesThroughTheCap (#8986 review P2-1): every
// whole-file read of repository content under go/internal/parser goes through
// ReadFileCapped or ReadSource, so a file over the cap never reaches a parser
// through a side read (a C header, a sibling module, a tsconfig, a composer
// manifest). A new bypass fails here until it uses the capped read or joins the
// allowlist with a reason.
func TestParserTreeReadsRepositoryFilesThroughTheCap(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve parser root: %v", err)
	}
	var offenders []string
	scanned := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, ok := uncappedReadAllowlist[relative]; ok {
			return nil
		}
		file, err := goparser.ParseFile(token.NewFileSet(), path, nil, goparser.SkipObjectResolution)
		if err != nil {
			return err
		}
		scanned++
		for _, call := range uncappedReadCallsIn(file) {
			offenders = append(offenders, relative+": "+call)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk parser tree: %v", walkErr)
	}
	for relative := range uncappedReadAllowlist {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("allowlist entry %s names no file (%v); remove it", relative, err)
		}
	}
	if scanned < 100 {
		t.Fatalf("scanned %d parser source files, want the whole tree (>= 100)", scanned)
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("whole-file reads that bypass ReadFileCapped:\n  %s", strings.Join(offenders, "\n  "))
	}
}
