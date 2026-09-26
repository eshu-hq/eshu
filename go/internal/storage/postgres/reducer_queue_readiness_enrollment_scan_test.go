// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// reducerImportPath is the import path of the reducer package tree. A
// FailureClass() anywhere in go/internal that returns a constant from this tree
// is a reducer-queue class, whichever package the error type lives in.
const reducerImportPath = "github.com/eshu-hq/eshu/go/internal/reducer"

// reducerTreeConstants maps each reducer-tree import path to its package-level
// string constants, read from the files the enrollment scan already parsed. It
// is what lets a selector such as reducer.SharedEdgeTargetNotReadyFailureClass,
// returned from a type outside the reducer tree, resolve to its class string.
func reducerTreeConstants(reducerDir string, parsedFiles map[string]*ast.File) map[string]map[string]string {
	consts := map[string]map[string]string{}
	for filePath, file := range parsedFiles {
		rel, err := filepath.Rel(reducerDir, filepath.Dir(filePath))
		if err != nil {
			continue
		}
		importPath := reducerImportPath
		if rel != "." {
			importPath = path.Join(reducerImportPath, filepath.ToSlash(rel))
		}
		for _, decl := range file.Decls {
			gen, isGen := decl.(*ast.GenDecl)
			if !isGen || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				lit, isLit := value.Values[0].(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if consts[importPath] == nil {
					consts[importPath] = map[string]string{}
				}
				consts[importPath][value.Names[0].Name] = unquoted
			}
		}
	}
	return consts
}

// parseReducerClassReferencesOutsideTree parses every non-test Go file under
// internalDir that the root/immediate-subpackage scan did not already parse and
// that could return a reducer-tree failure class: it must mention both
// FailureClass() and the reducer import path. The byte prefilter keeps the walk
// over thousands of files cheap; a file it skips cannot satisfy the selector
// rule below, so it is not a false negative.
//
// This is the #7268 widening. The shared-edge writer's targetMissingError lives
// in internal/storage/cypher/edge/writer and returns
// reducer.SharedEdgeTargetNotReadyFailureClass; the reducer-only scan could not
// see it, so the class could have been left unenrolled with this guard green.
func parseReducerClassReferencesOutsideTree(
	t *testing.T,
	fset *token.FileSet,
	internalDir string,
	alreadyParsed map[string]*ast.File,
) map[string]*ast.File {
	t.Helper()

	found := map[string]*ast.File{}
	err := filepath.WalkDir(internalDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if _, done := alreadyParsed[filePath]; done {
			return nil
		}
		src, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return readErr
		}
		if !bytes.Contains(src, []byte("FailureClass()")) || !bytes.Contains(src, []byte(`"`+reducerImportPath)) {
			return nil
		}
		parsed, parseErr := parser.ParseFile(fset, filePath, src, 0)
		if parseErr != nil {
			return parseErr
		}
		found[filePath] = parsed
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", internalDir, err)
	}
	return found
}

// reducerSelectorClass resolves a FailureClass body of the form
// `return pkg.Name`, where pkg is an import of the reducer tree, to the
// constant's string. It returns (value, "", true) when resolved, ("",
// "pkg.Name", true) when pkg is a reducer-tree import but the constant is not
// a readable string literal — reported as unreadable rather than dropped — and
// ("", "", false) when the body is not a reducer-tree selector at all.
func reducerSelectorClass(
	file *ast.File,
	fn *ast.FuncDecl,
	consts map[string]map[string]string,
) (string, string, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", "", false
	}
	ret, isReturn := fn.Body.List[0].(*ast.ReturnStmt)
	if !isReturn || len(ret.Results) != 1 {
		return "", "", false
	}
	sel, isSel := ret.Results[0].(*ast.SelectorExpr)
	if !isSel {
		return "", "", false
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	importPath := importPathForName(file, pkg.Name)
	if importPath != reducerImportPath && !strings.HasPrefix(importPath, reducerImportPath+"/") {
		return "", "", false
	}
	if value, ok := consts[importPath][sel.Sel.Name]; ok {
		return value, "", true
	}
	return "", pkg.Name + "." + sel.Sel.Name, true
}

// importPathForName returns the import path file binds to name, honouring an
// explicit alias and otherwise the path's last element.
func importPathForName(file *ast.File, name string) string {
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		bound := path.Base(importPath)
		if spec.Name != nil {
			bound = spec.Name.Name
		}
		if bound == name {
			return importPath
		}
	}
	return ""
}
