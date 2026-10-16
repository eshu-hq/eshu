// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The writers that set searched fields keep their old constructors for tests
// and fixtures; those emit the Neo4j-keyed shape. Production code must pass
// the backend dialect through the WithDialect constructors, so a binary that
// forgets cannot silently write the wrong shape. Only the NornicDB writer seam
// may switch a canonical writer to the NornicDB shape after construction.
var (
	dialectFreeConstructors = map[string]bool{
		"NewCanonicalNodeWriter":     true,
		"NewCloudResourceNodeWriter": true,
		"NewEC2InstanceNodeWriter":   true,
	}
	writerPackageDir    = "internal/storage/cypher"
	nornicDBSeamDir     = "internal/storage/nornicdb"
	nornicDBShapeSwitch = "WithoutInfraSearchKey"
)

// constructorGuardFindings walks every non-test Go file under root and
// reports calls to a dialect-free constructor outside the writer package, and
// calls to WithoutInfraSearchKey outside the NornicDB seam. It reads the AST,
// so a name in a comment or string never counts.
func constructorGuardFindings(t *testing.T, root string) (findings []string, scanned int) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "testdata", "vendor", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		dir := filepath.ToSlash(filepath.Dir(rel))
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			where := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), fset.Position(call.Pos()).Line)
			switch {
			case dialectFreeConstructors[name] && dir != writerPackageDir:
				findings = append(findings, where+": "+name+" emits the default keyed shape; production code must use "+name+"WithDialect with the backend dialect")
			case name == nornicDBShapeSwitch && dir != nornicDBSeamDir:
				findings = append(findings, where+": "+name+" is reserved for the NornicDB writer seam ("+nornicDBSeamDir+")")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(findings)
	return findings, scanned
}

// TestProductionWiringUsesDialectConstructors fails when a non-test file
// outside the writer package builds a writer without naming its dialect.
func TestProductionWiringUsesDialectConstructors(t *testing.T) {
	t.Parallel()

	findings, scanned := constructorGuardFindings(t, filepath.Join("..", "..", ".."))
	if scanned < 1000 {
		t.Fatalf("guard scanned %d files, want the whole go/ module", scanned)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestConstructorGuardFlagsSeededViolations proves the guard can fail on a
// planted call and stays clean on the WithDialect form, the writer package
// itself, the NornicDB seam, and a name that appears only in a comment.
func TestConstructorGuardFlagsSeededViolations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cmd/bad/main.go", "package main\n\nfunc main() { _ = sourcecypher.NewCanonicalNodeWriter(nil, 0, nil) }\n")
	write("cmd/badcloud/main.go", "package main\n\nfunc main() { _ = sourcecypher.NewCloudResourceNodeWriter(nil, 0) }\n")
	write("cmd/badswitch/main.go", "package main\n\nfunc main() { w.WithoutInfraSearchKey() }\n")
	write("cmd/good/main.go", "package main\n\n// NewCanonicalNodeWriter( is only named here.\nfunc main() { _ = sourcecypher.NewCanonicalNodeWriterWithDialect(nil, 0, nil, d) }\n")
	write("internal/storage/cypher/own.go", "package cypher\n\nfunc own() { _ = NewEC2InstanceNodeWriter(nil, 0) }\n")
	write("internal/storage/nornicdb/seam.go", "package nornicdb\n\nfunc seam() { w.WithoutInfraSearchKey() }\n")
	write("cmd/bad/main_test.go", "package main\n\nfunc x() { _ = sourcecypher.NewCanonicalNodeWriter(nil, 0, nil) }\n")

	findings, _ := constructorGuardFindings(t, root)
	joined := strings.Join(findings, "\n")
	for _, want := range []string{"cmd/bad/main.go:3", "cmd/badcloud/main.go:3", "cmd/badswitch/main.go:3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("seeded violation %s not found; findings = %v", want, findings)
		}
	}
	for _, clean := range []string{"cmd/good/", "internal/storage/cypher/own.go", "internal/storage/nornicdb/seam.go", "main_test.go"} {
		if strings.Contains(joined, clean) {
			t.Errorf("%s flagged, want clean; findings = %v", clean, findings)
		}
	}
}
