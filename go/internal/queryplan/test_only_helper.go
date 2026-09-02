// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// validateTestOnlyHelperExclusion proves the one directory the callsite walk
// skips is still the test-only helper package the skip assumes it is.
//
// The skip exists because a graph fake has Run and RunSingle methods and its
// RunSingle answers by calling Run, which is indistinguishable from a
// production graph read to a syntactic walk. That assumption used to be carried
// by prose alone, which made the exclusion fail-open: a production read landing
// in the excluded package would leave the inventory green while omitting the
// callsite. This makes the exclusion pay for itself. The rules it enforces:
//
//   - The package holds at least one non-test Go file, so a stale exclusion
//     over an empty or deleted directory is a failure rather than a silent pass.
//   - Its non-test files import the standard library only. A real graph read
//     needs a driver, a session, or a handler family, and none of those are
//     reachable from here.
//   - The only Run or RunSingle calls are a fake's own Run and RunSingle
//     methods delegating to their receiver. A call on a parameter, a field, or
//     a package-level value is a production-shaped read and fails.
//
// helperDir that does not exist excludes nothing, so there is nothing to prove.
// That case is settled by a stat before the walk rather than by matching
// fs.ErrNotExist on the walk's error: matching would also swallow a file that
// vanished mid-walk, turning a real failure into the silent pass this check
// exists to remove.
func validateTestOnlyHelperExclusion(helperDir string) error {
	if _, err := os.Stat(helperDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat test-only helper package %s: %w", helperDir, err)
	}
	var violations []string
	sources := 0
	err := filepath.WalkDir(helperDir, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.IsDir() {
			return nil
		}
		name := dirEntry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		sources++
		display, relErr := filepath.Rel(helperDir, path)
		if relErr != nil {
			return fmt.Errorf("resolve helper source path %s: %w", path, relErr)
		}
		fileViolations, err := inspectTestOnlyHelperFile(path, filepath.ToSlash(filepath.Join(testOnlyHelperPackage, display)))
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk test-only helper package: %w", err)
	}
	if sources == 0 {
		violations = append(violations, fmt.Sprintf(
			"%s: excluded from the query callsite inventory but holds no non-test Go file; drop the exclusion",
			testOnlyHelperPackage,
		))
	}
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	return errors.New(strings.Join(violations, "; "))
}

// inspectTestOnlyHelperFile reports every way one non-test file in the excluded
// helper package fails to look like a test double.
func inspectTestOnlyHelperFile(path, display string) ([]string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse test-only helper source %s: %w", path, err)
	}
	violations := testOnlyHelperImportViolations(file, display)
	return append(violations, testOnlyHelperCallViolations(file, display)...), nil
}

// testOnlyHelperImportViolations rejects any dependency outside the standard
// library. Nothing a fake needs lives outside it, and a graph read does.
func testOnlyHelperImportViolations(file *ast.File, display string) []string {
	var violations []string
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil || isStandardLibraryImport(importPath) {
			continue
		}
		violations = append(violations, fmt.Sprintf(
			"%s: imports %s; the excluded helper package may import the standard library only",
			display,
			importPath,
		))
	}
	return violations
}

// testOnlyHelperCallViolations rejects every Run or RunSingle call except the
// one shape the exclusion was granted for: a fake's Run or RunSingle method
// delegating to its own receiver.
func testOnlyHelperCallViolations(file *ast.File, display string) []string {
	var violations []string
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			if countQueryCalls(declaration) > 0 {
				violations = append(violations, fmt.Sprintf(
					"%s: package-level declaration calls Run or RunSingle; the excluded helper package must not read a graph",
					display,
				))
			}
			continue
		}
		if function.Body == nil {
			continue
		}
		receiver := selfDelegatingReceiver(function)
		symbol := functionSymbol(function)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Run" && selector.Sel.Name != "RunSingle") {
				return true
			}
			base, isIdent := selector.X.(*ast.Ident)
			if isIdent && receiver != "" && base.Name == receiver {
				return true
			}
			violations = append(violations, fmt.Sprintf(
				"%s: %s calls %s on a value other than its own receiver; the excluded helper package must not read a graph",
				display,
				symbol,
				selector.Sel.Name,
			))
			return true
		})
	}
	return violations
}

// selfDelegatingReceiver returns the receiver name of a fake's own Run or
// RunSingle method, and the empty string for any other function. Only those two
// methods may reach a Run or RunSingle call, and only on themselves.
func selfDelegatingReceiver(function *ast.FuncDecl) string {
	if function.Name.Name != "Run" && function.Name.Name != "RunSingle" {
		return ""
	}
	if function.Recv == nil || len(function.Recv.List) == 0 || len(function.Recv.List[0].Names) == 0 {
		return ""
	}
	name := function.Recv.List[0].Names[0].Name
	if name == "_" {
		return ""
	}
	return name
}

// rejectTestOnlyHelperImport fails when a non-test file under the query source
// tree reaches the excluded helper package.
//
// An exclusion is only sound while the excluded subtree is unreachable from
// production code. This is the direction the walk itself cannot see: it stops
// descending at the helper directory, so an import pointing back into it would
// otherwise be invisible to the inventory.
func rejectTestOnlyHelperImport(path string) error {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse query source imports %s: %w", path, err)
	}
	if importPath := testOnlyHelperImport(file); importPath != "" {
		return fmt.Errorf(
			"%s imports the test-only helper package %s; production code must not reach a subtree the callsite inventory excludes",
			path,
			importPath,
		)
	}
	return nil
}

// testOnlyHelperImport returns the import path by which file reaches the
// excluded helper package, or the empty string when it does not.
//
// The exclusion is only honest while production code cannot reach the package.
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

// isStandardLibraryImport reports whether importPath names a standard library
// package. Only a module path carries a dot in its first element.
func isStandardLibraryImport(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
