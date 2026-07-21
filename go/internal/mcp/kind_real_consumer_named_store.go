// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// registryFactKinds returns the set of every wire fact-kind string in the
// generated registry (facts.FactKindRegistry()). namedConstStoreKinds uses
// this as its admission filter so a scanned const's value must be a real,
// registered kind before it counts as evidence of consumption.
func registryFactKinds() map[string]bool {
	entries := facts.FactKindRegistry()
	valid := make(map[string]bool, len(entries))
	for _, e := range entries {
		valid[e.Kind] = true
	}
	return valid
}

// namedConstStoreKinds scans every non-test .go file directly under dir for
// a top-level string const declaration in a file that also references
// "fact_kind"/"FactKind" somewhere (evidence the file is fact_kind-scoped,
// not an unrelated helper), and returns the set of every such const's
// literal value that is ALSO a real registry fact-kind wire string
// (validKinds, from facts.FactKindRegistry()). The registry membership
// check is what keeps this signal precise: without it, ANY top-level string
// const in a matching file — regardless of what the constant is actually
// for — would be admitted as a "consumer" for whatever fact kind its value
// happened to spell, including an unrelated const that coincidentally
// shares a string with a real (but genuinely unconsumed) kind. Requiring
// the value to already be a real, registered kind bounds the false-positive
// surface to "this specific registry kind string appears as some const's
// value in a fact_kind-scoped file" — still a heuristic (see
// realConsumerNamedStoreDir's doc comment for the motivating true positive),
// but one that cannot invent evidence for a kind that was never emitted.
// See realConsumerNamedStoreDir's doc comment for the motivating case.
func namedConstStoreKinds(dir string, validKinds map[string]bool) (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", dir, err)
	}

	kinds := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		contents, err := readFileForGate(path)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: read %s: %w", path, err)
		}
		if !strings.Contains(contents, "fact_kind") && !strings.Contains(contents, "FactKind") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i := range vspec.Names {
					if i >= len(vspec.Values) {
						continue
					}
					lit, ok := vspec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					unquoted, err := unquoteGoString(lit.Value)
					if err != nil || !validKinds[unquoted] {
						continue
					}
					kinds[unquoted] = true
				}
			}
		}
	}
	return kinds, nil
}
