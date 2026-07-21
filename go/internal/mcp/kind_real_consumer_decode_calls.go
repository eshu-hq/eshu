// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// factschemaDecodeGlob is the sdk/go/factschema glob covering every file
// declaring an exported Decode<Kind> function.
const factschemaDecodeGlob = "sdk/go/factschema/decode*.go"

// factschemaExportedDecodeFuncKinds parses factschemaDecodeGlob and returns
// a map from each exported Decode<Kind> function name (e.g.
// "DecodeAWSIAMPermissionPolicy") to the wire fact-kind string it decodes,
// resolved through constValues (the identifier table factKindConstantValues
// builds from the same package's FactKind* constants).
//
// This is the package-internal sibling of payloadusage's decode-seam
// derivation. payloadusage.ParseDecodeSeams only recognizes local
// decode<kind> WRAPPER functions in caller packages (go/internal/reducer and
// friends) that reference factschema.FactKind* through a package-qualified
// selector; it cannot see factschema's OWN exported Decode<Kind> functions,
// because inside package factschema the FactKind* reference is a bare
// identifier, never a qualified selector. Some consumers call
// factschema.Decode<Kind> directly instead of defining a local decode<kind>
// wrapper — go/internal/storage/postgres/secrets_iam_trust_chain_anchor_decode.go
// calling factschema.DecodeAWSIAMPermissionPolicy is the concrete case that
// motivated this table — and that call site is a real consumer
// directFactschemaDecodeCalls needs this table to detect.
func factschemaExportedDecodeFuncKinds(repoRoot string, constValues map[string]string) (map[string]string, error) {
	glob := filepath.Join(repoRoot, factschemaDecodeGlob)
	matches, err := filepath.Glob(glob)
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", glob, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("kind_real_consumer: no files matched %s", glob)
	}
	sort.Strings(matches)

	funcKinds := map[string]string{}
	fset := token.NewFileSet()
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Decode") {
				continue
			}
			ident, ok := bareFactKindIdent(fn.Body)
			if !ok {
				continue
			}
			wire, ok := constValues[ident]
			if !ok {
				// Not every "Decode*" function decodes a registered fact
				// kind (e.g. a shared helper); skip rather than fail, since
				// this table is best-effort supplementary evidence, not a
				// fail-closed enumeration like factKindConstantValues.
				continue
			}
			funcKinds[fn.Name.Name] = wire
		}
	}
	return funcKinds, nil
}

// bareFactKindIdent finds the first bare (unqualified) FactKind* identifier
// referenced in body — the in-package equivalent of payloadusage's
// decodeFuncFactKindConst, which requires a "factschema."-qualified selector
// and so cannot see factschema's own internal references to its own
// constants.
func bareFactKindIdent(body *ast.BlockStmt) (string, bool) {
	var found string
	ast.Inspect(body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if strings.HasPrefix(ident.Name, "FactKind") {
			found = ident.Name
			return false
		}
		return true
	})
	return found, found != ""
}

// directFactschemaDecodeCalls scans every non-test .go file under each dir in
// dirs for call expressions of the form factschema.Decode<Kind>(...) and
// returns the set of wire fact-kind strings called, resolved through
// funcKinds (from factschemaExportedDecodeFuncKinds). This catches a
// consumer that calls factschema's exported Decode<Kind> directly instead of
// defining a local decode<kind> wrapper — the shape payloadusage's own
// decode-seam scan expects and the only shape realConsumerDecodeSeamDirs'
// payloadusage.ParseDecodeSeamsGlob call recognizes.
func directFactschemaDecodeCalls(dirs []string, funcKinds map[string]string) (map[string]bool, error) {
	called := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", dir, err)
		}
		for _, path := range matches {
			if isGoTestFile(path) {
				continue
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "factschema" {
					return true
				}
				if wire, ok := funcKinds[sel.Sel.Name]; ok {
					called[wire] = true
				}
				return true
			})
		}
	}
	return called, nil
}
