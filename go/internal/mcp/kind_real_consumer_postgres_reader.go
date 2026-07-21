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
)

// postgresPayloadReaderKinds scans every non-test .go file directly under
// dir (expected: go/internal/storage/postgres) for a function whose body
// BOTH (a) compares a fact-kind value against a `facts.<Kind>` selector
// (`== facts.<Kind>` / `facts.<Kind> ==` / `!= facts.<Kind>` /
// `facts.<Kind> !=`) AND (b) reads a payload field in that same function
// body — a `json.Unmarshal` call, or an index expression on an identifier or
// selector whose name contains "payload" or "decoded"
// (`decoded["identity_type"]`, `envelope.Payload["field"]`). Only kinds from
// functions satisfying BOTH conditions are returned.
//
// This is round 2 of the #5474 review's storage/postgres blind spot: files
// like cloud_identity_policy_evidence.go and cloud_resource_change_evidence.go
// compare `factKind != facts.AzureIdentityObservationFactKind` /
// `facts.AzureResourceChangeFactKind`, then json.Unmarshal the raw payload
// bytes and read specific fields (identity_type, role_class,
// principal_fingerprint, operation, client_type, change_type, ...) — genuine
// consumers with no typed decode seam and no locally-declared const (they
// reference facts.<Kind>FactKind directly), so neither
// realConsumerDecodeSeamDirs nor namedConstStoreKinds sees them.
//
// The payload-read requirement is deliberate and narrow, mirroring
// factsDispatchedKinds' reducer-vs-projector split: a bare kind comparison
// with no payload read is exactly the graph-projection-readiness bookkeeping
// pattern (go/internal/projector/runtime_phase.go's
// `case facts.TerraformStateSnapshotFactKind, facts.TerraformStateWarningFactKind:`)
// that must NOT count as consumption. Requiring an actual field extraction
// in the same function keeps this signal from re-admitting that false-green
// class; a kind compared-but-never-read (e.g. purely for existence/counting)
// stays correctly unconsumed.
func postgresPayloadReaderKinds(dir string, factsConstValues map[string]string) (map[string]bool, error) {
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
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			comparedKinds := factsComparedKindsInBody(fn.Body, factsConstValues)
			if len(comparedKinds) == 0 {
				continue
			}
			if !hasPayloadFieldRead(fn.Body) {
				continue
			}
			for wire := range comparedKinds {
				kinds[wire] = true
			}
		}
	}
	return kinds, nil
}

// factsComparedKindsInBody returns every wire fact-kind string body compares
// a value against via `== facts.<Kind>` / `facts.<Kind> ==` /
// `!= facts.<Kind>` / `facts.<Kind> !=`.
func factsComparedKindsInBody(body *ast.BlockStmt, factsConstValues map[string]string) map[string]bool {
	found := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) {
			return true
		}
		if wire, ok := factsSelectorWireKind(bin.X, factsConstValues); ok {
			found[wire] = true
		}
		if wire, ok := factsSelectorWireKind(bin.Y, factsConstValues); ok {
			found[wire] = true
		}
		return true
	})
	return found
}

// hasPayloadFieldRead reports whether body contains a json.Unmarshal call or
// an index expression keyed off an identifier/selector whose name contains
// "payload" or "decoded" (case-insensitive) — the two shapes
// go/internal/storage/postgres's raw-JSON readers use to extract fields from
// a fact's payload without a typed decode seam.
func hasPayloadFieldRead(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch node := n.(type) {
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Unmarshal" {
				found = true
				return false
			}
		case *ast.IndexExpr:
			if payloadLikeName(node.X) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// payloadLikeName reports whether expr is an *ast.Ident or *ast.SelectorExpr
// whose name contains "payload" or "decoded" (case-insensitive).
func payloadLikeName(expr ast.Expr) bool {
	var name string
	switch e := expr.(type) {
	case *ast.Ident:
		name = e.Name
	case *ast.SelectorExpr:
		name = e.Sel.Name
	default:
		return false
	}
	lower := strings.ToLower(name)
	return strings.Contains(lower, "payload") || strings.Contains(lower, "decoded")
}
