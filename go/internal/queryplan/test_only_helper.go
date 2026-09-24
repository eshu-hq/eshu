// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// rejectTestOnlyHelperImport fails when a non-test file under the query source
// tree reaches the test-double package.
//
// This is a boundary rule, not an inventory one: the callsite walk covers
// testutil like every other directory, so nothing here is about keeping a
// subtree out of the gate. Production code must not depend on test doubles at
// all. A fake answers from funcs a test installs, so a production caller
// reaching one gets whatever the zero value returns -- no rows -- which is a
// silent wrong answer rather than a failure.
//
// The two legal exits are in the error text: move the helper to
// internal/query/querycontract if production genuinely needs it, or keep the
// import in a _test.go file, which this check does not read. A file inside the
// helper tree itself (queryDir/testutil and its content and graph leaves)
// is exempt: a double built on another double is test code reaching test code.
func rejectTestOnlyHelperImport(queryDir, path string) error {
	if insideTestOnlyHelperTree(queryDir, path) {
		return nil
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse query source imports %s: %w", path, err)
	}
	if importPath := testOnlyHelperImport(file); importPath != "" {
		return fmt.Errorf(
			"%s imports the test-only helper package %s; production code must not depend on test doubles -- move the helper to internal/query/querycontract if production needs it, or keep the import in a _test.go file",
			path,
			importPath,
		)
	}
	return nil
}

// testOnlyHelperImport returns the import path by which file reaches the
// test-double package, or the empty string when it does not.
//
// The match is on a whole path element, not only the last one: #6642 nested
// the content and graph doubles under the helper package, and an import of
// testutil/graph is the same defect as an import of testutil. An
// unrelated package with a testutil element anywhere in its path can only
// over-report, failing the inventory rather than passing it. An element that
// merely starts with the name (testutilities) does not match.
func testOnlyHelperImport(file *ast.File) string {
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		for _, element := range strings.Split(importPath, "/") {
			if element == testOnlyHelperPackage {
				return importPath
			}
		}
	}
	return ""
}

// insideTestOnlyHelperTree reports whether path sits in queryDir's helper
// package or one of its nested leaves.
func insideTestOnlyHelperTree(queryDir, path string) bool {
	relative, err := filepath.Rel(queryDir, path)
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	return first == testOnlyHelperPackage
}
