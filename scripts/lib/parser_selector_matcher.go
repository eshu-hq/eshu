// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ignore

// parser_selector_matcher applies Go regexp semantics to documented test
// selectors without compiling or executing the parser test suite.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--inventory" {
		if err := printTestInventory(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: parser-selector-matcher REGEXP TEST_NAME... | --inventory DIR")
		os.Exit(2)
	}

	matcher, err := regexp.Compile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, name := range os.Args[2:] {
		if matcher.MatchString(name) {
			fmt.Println(name)
		}
	}
}

func printTestInventory(dir string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return fmt.Errorf("list Rust test sources: %w", err)
	}
	names := make(map[string]struct{})
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			continue
		}
		// Keep build-constrained tests in the inventory deliberately. A
		// documented command must target the child package on every platform,
		// not only on the platform running this verifier.
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		if file.Name.Name != "rust_test" {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !isGoTestName(fn.Name.Name) {
				continue
			}
			if fn.Name.Name == "TestMain" && isGoTestFunc(fn, "M") {
				continue
			}
			if !isGoTestFunc(fn, "T") {
				return fmt.Errorf("%s declares %s with a non-test signature", path, fn.Name.Name)
			}
			names[fn.Name.Name] = struct{}{}
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no external Rust tests found in %s", dir)
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		fmt.Println(name)
	}
	return nil
}

func isGoTestName(name string) bool {
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	if len(name) == len("Test") {
		return true
	}
	next, _ := utf8.DecodeRuneInString(name[len("Test"):])
	return !unicode.IsLower(next)
}

func isGoTestFunc(fn *ast.FuncDecl, arg string) bool {
	if fn.Type.TypeParams.NumFields() > 0 ||
		fn.Type.Results != nil && len(fn.Type.Results.List) > 0 ||
		fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	param := fn.Type.Params.List[0]
	if len(param.Names) > 1 {
		return false
	}
	star, ok := param.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	if ident, ok := star.X.(*ast.Ident); ok && ident.Name == arg {
		return true
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == arg
}
