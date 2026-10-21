// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// pinnedFakeCopies maps each shared test-double type to the files that must
// carry byte-equivalent definitions (#6694). Test fakes cannot be imported
// across the package split, so each side carries its own copy. Only copies
// that are identical today are pinned: pre-existing root and edge/writer
// variants that already differ are out of scope for this move.
var pinnedFakeCopies = map[string][]string{
	"recordingExecutor": {
		"writer_test.go",
		"cloud/aws/ec2/harness_test.go",
		"cloud/aws/iam/harness_test.go",
		"cloud/aws/image/harness_test.go",
		"cloud/aws/rds/harness_test.go",
		"cloud/aws/relationship/harness_test.go",
		"cloud/aws/s3/harness_test.go",
		"cloud/aws/security/group/harness_test.go",
		"cloud/azure/relationship/harness_test.go",
		"cloud/gcp/relationship/harness_test.go",
		"cloud/resource/harness_test.go",
	},
	"recordingGroupExecutor": {
		"cloud/aws/ec2/harness_test.go",
		"cloud/aws/security/group/harness_test.go",
		"cloud/gcp/relationship/harness_test.go",
		"cloud/resource/harness_test.go",
		"kubernetes/harness_test.go",
	},
	"backendRestartTerminalGroupExecutor": {
		"cloud/gcp/relationship/harness_test.go",
		"cloud/resource/harness_test.go",
	},
	"noopGroupExecutor": {
		"cloud/aws/ec2/harness_test.go",
		"cloud/aws/iam/harness_test.go",
		"cloud/aws/image/harness_test.go",
		"cloud/aws/relationship/harness_test.go",
		"cloud/aws/s3/harness_test.go",
		"cloud/aws/security/group/harness_test.go",
		"kubernetes/harness_test.go",
	},
}

// renderFakeType parses one harness file and renders the named type
// declaration plus its methods in canonical form, matching both pointer and
// value receivers so a value-receiver method is never silently skipped.
func renderFakeType(t *testing.T, path string, typeName string) string {
	t.Helper()

	// Parse without comments: the guard pins code identity, so the
	// per-side copy prose above each fake may differ harmlessly.
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	matchesReceiver := func(field *ast.Field) bool {
		switch recv := field.Type.(type) {
		case *ast.StarExpr:
			ident, ok := recv.X.(*ast.Ident)
			return ok && ident.Name == typeName
		case *ast.Ident:
			return recv.Name == typeName
		default:
			return false
		}
	}
	var rendered bytes.Buffer
	found := false
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok && typeSpec.Name.Name == typeName {
					if err := printer.Fprint(&rendered, fileSet, node); err != nil {
						t.Fatalf("render %s %s: %v", path, typeName, err)
					}
					found = true
				}
			}
		case *ast.FuncDecl:
			if node.Recv != nil {
				for _, field := range node.Recv.List {
					if matchesReceiver(field) {
						if err := printer.Fprint(&rendered, fileSet, node); err != nil {
							t.Fatalf("render %s method: %v", path, err)
						}
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s defines no %s", path, typeName)
	}
	// Leaf copies qualify the root-package types as sourcecypher.X while the
	// root copies reference them unqualified. Normalize the qualifier so the
	// comparison pins semantic identity, not spelling.
	return strings.ReplaceAll(rendered.String(), "sourcecypher.", "")
}

// renderBatchedRetractHelper parses one file and renders the named free
// function in canonical form with the name normalized to the root twin, so
// the comparison pins body identity, not the leaf-specific name.
func renderBatchedRetractHelper(t *testing.T, path, funcName, asName string) string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != funcName || function.Recv != nil {
			continue
		}
		function.Name.Name = asName
		var rendered bytes.Buffer
		if err := printer.Fprint(&rendered, fileSet, function); err != nil {
			t.Fatalf("render %s %s: %v", path, funcName, err)
		}
		// The leaf copy qualifies the root-package types as sourcecypher.X
		// while the root copy references them unqualified. Normalize the
		// qualifier so the comparison pins semantic identity, not spelling.
		return strings.ReplaceAll(rendered.String(), "sourcecypher.", "")
	}
	t.Fatalf("%s defines no function %s", path, funcName)
	return ""
}

// TestBatchedRetractHelperCopiesStayIdentical fails when the kubernetes leaf
// copy (buildBatchedNamespaceRetractStatements) drifts from its root twin
// (buildBatchedRetractStatements, canonical_node_writer_phases.go). The copy
// carries a leaf-specific name because the ifa direct-family scan keys bare
// function names; this test pins the bodies identical.
func TestBatchedRetractHelperCopiesStayIdentical(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)

	const twin = "buildBatchedRetractStatements"
	root := renderBatchedRetractHelper(t, filepath.Join(dir, "canonical_node_writer_phases.go"), twin, twin)
	leaf := renderBatchedRetractHelper(t, filepath.Join(dir, "kubernetes", "namespace.go"), "buildBatchedNamespaceRetractStatements", twin)
	if root != leaf {
		t.Errorf("kubernetes buildBatchedNamespaceRetractStatements drifts from root %s", twin)
	}
}

// TestPinnedFakeCopiesStayIdentical fails when any pinned fake copy drifts
// from the rest of its set.
func TestPinnedFakeCopiesStayIdentical(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)

	for _, typeName := range []string{
		"recordingExecutor",
		"recordingGroupExecutor",
		"backendRestartTerminalGroupExecutor",
		"noopGroupExecutor",
	} {
		files := pinnedFakeCopies[typeName]
		baseline := ""
		for _, relative := range files {
			rendered := renderFakeType(t, filepath.Join(dir, relative), typeName)
			if baseline == "" {
				baseline = rendered
				continue
			}
			if rendered != baseline {
				t.Errorf("%s in %s drifts from %s", typeName, relative, files[0])
			}
		}
	}
}
