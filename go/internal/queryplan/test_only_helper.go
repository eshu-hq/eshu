// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// rejectTestOnlyHelperImport fails when a non-test file under the query source
// tree reaches the test-double package.
//
// This is a boundary rule, not an inventory one: the callsite walk covers
// querytestutil like every other directory, so nothing here is about keeping a
// subtree out of the gate. Production code must not depend on test doubles at
// all. A fake answers from funcs a test installs, so a production caller
// reaching one gets whatever the zero value returns -- no rows -- which is a
// silent wrong answer rather than a failure.
//
// The two legal exits are in the error text: move the helper to
// internal/query/querycontract if production genuinely needs it, or keep the
// import in a _test.go file, which this check does not read.
func rejectTestOnlyHelperImport(path string) error {
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
// The match is on the trailing path element, which can only over-report: a
// production file importing some unrelated package with the same final element
// fails the inventory rather than passing it.
func testOnlyHelperImport(file *ast.File) string {
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		if importPath == testOnlyHelperPackage || strings.HasSuffix(importPath, "/"+testOnlyHelperPackage) {
			return importPath
		}
	}
	return ""
}
