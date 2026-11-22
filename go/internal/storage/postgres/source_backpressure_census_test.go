// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sourceSystemFamilyCensusAllowlist names the only coordinator sites that set
// a workflow work item's source_system from something other than a fixed
// collector-kind constant, and why each is still one source per family.
// Keys are slash paths under go/internal/coordinator plus the exact value
// expression.
var sourceSystemFamilyCensusAllowlist = map[string]string{
	"planner/component/extension/planner.go|sourceSystem": "component extension: source_system is the activation host claim of one " +
		"instance's config, so one (collector_kind, collector_instance_id) family serves one source",
	"activation_derived_scopes.go|config.Host.SourceSystem": "feeds the activation-derived planner (pinned above) and is gated by " +
		"checkActivationHost to equal the vulnerability_intelligence collector kind",
	"component_extension_service.go|config.Host.SourceSystem": "records the host-mismatch refusal error for checkActivationHost; " +
		"not a work item",
	"vulnerability/activation_derivation.go|sourceSystem": "activation-derived vulnerability work: the planner refuses a foreign " +
		"source system (TestActivationDerivedWorkRefusesForeignSourceSystem), pinning it to the instance's one host source",
}

// TestWorkflowFamilySourceCensusIsSingleSource is the #10882 fork-2 guard.
//
// CheckSourceBackpressure paces a claim only when EVERY pending source of the
// claiming family is slowed (familyPendingSources). That is correct and
// never over-throttles, but it under-paces a family that serves several
// sources: a healthy source in the family keeps the whole family claiming.
// The census (every site under internal/coordinator that sets a work item's
// source_system) shows each family is single-source by construction: either a
// fixed collector-kind constant or a per-instance config value pinned above.
// This test fails loudly the moment a new planner sets source_system from
// anything else, so a multi-source family cannot appear without someone
// deciding between the all-slowed rule and a per-item skip at claim selection.
func TestWorkflowFamilySourceCensusIsSingleSource(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "coordinator")
	fset := token.NewFileSet()
	var violations []string
	seenAllowed := map[string]bool{}
	constantSites := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		check := func(value ast.Expr, pos token.Pos) {
			text := exprText(fset, value)
			if isCollectorKindConstant(value) {
				constantSites++
				return
			}
			key := rel + "|" + text
			if _, ok := sourceSystemFamilyCensusAllowlist[key]; ok {
				seenAllowed[key] = true
				return
			}
			violations = append(violations, fset.Position(pos).String()+": source_system = "+text)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CompositeLit:
				if !isWorkItemType(n.Type) {
					return true
				}
				for _, elt := range n.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok && isIdent(kv.Key, "SourceSystem") {
						check(kv.Value, kv.Pos())
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "SourceSystem" && i < len(n.Rhs) {
						check(n.Rhs[i], n.Pos())
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk coordinator sources: %v", err)
	}
	if constantSites < 10 {
		t.Fatalf("census found only %d collector-kind constant sites; the walk or parser is broken", constantSites)
	}
	for key := range sourceSystemFamilyCensusAllowlist {
		if !seenAllowed[key] {
			t.Errorf("allowlist entry %q no longer matches a coordinator site: remove it", key)
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("a coordinator site sets source_system from something other than a collector-kind constant.\n"+
			"CheckSourceBackpressure paces a family only when EVERY pending source is slowed, so a multi-source\n"+
			"family would be under-paced (#10882). Decide per-item skip vs. keep the all-slowed rule, then record\n"+
			"the site in sourceSystemFamilyCensusAllowlist with the reason it stays single-source:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isWorkItemType(expr ast.Expr) bool {
	switch typed := expr.(type) {
	case *ast.SelectorExpr:
		return typed.Sel.Name == "WorkItem"
	case *ast.Ident:
		return typed.Name == "WorkItem"
	}
	return false
}

// isCollectorKindConstant matches string(scope.CollectorXxx): a fixed
// collector-kind constant, which makes every family of that kind one source.
func isCollectorKindConstant(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isIdent(call.Fun, "string") {
		return false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	return ok && isIdent(sel.X, "scope") && strings.HasPrefix(sel.Sel.Name, "Collector")
}

func exprText(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, expr); err != nil {
		return "<unprintable>"
	}
	return buf.String()
}
