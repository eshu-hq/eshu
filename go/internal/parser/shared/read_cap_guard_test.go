// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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

// uncappedReadCalls are the whole-file reads that bypass the per-file cap.
var uncappedReadCalls = map[string]bool{
	"os.ReadFile":     true,
	"io.ReadAll":      true,
	"ioutil.ReadFile": true,
	"ioutil.ReadAll":  true,
	"fs.ReadFile":     true,
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
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if ok && uncappedReadCalls[pkg.Name+"."+selector.Sel.Name] {
				offenders = append(offenders, relative+": "+pkg.Name+"."+selector.Sel.Name)
			}
			return true
		})
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
