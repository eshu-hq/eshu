// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"go/ast"
	"sort"
)

// caseRegion pairs one switch/select branch (*ast.CaseClause or
// *ast.CommClause) with the decode bindings recorded from assignments
// directly inside that branch, excluding nested branches' own assignments
// (each nested branch forms its own region when the outer walk reaches it).
type caseRegion struct {
	node    ast.Node
	boundTo map[string]string
}

// outerDecodeBindings finds every local identifier bound to a decoded typed
// struct OUTSIDE any switch/select branch: either a direct decodeFuncs()
// call-result assignment in straight-line code (recordDecodeBindings), or a
// function parameter whose type is one of structToFunc's qualified struct
// names (the cross-function helper-parameter case). It returns identifier
// name -> decode func name. Assignments inside a branch belong to that
// branch's own region (see caseDecodeBindings), so a branch-local `:=`
// shadowing an outer name does not rebind the outer code's reads.
func outerDecodeBindings(fn *ast.FuncDecl, decodeFuncs map[string]struct{}, forwarders RootForwarders, qualifiers DecodeQualifiers, structToFunc map[string]string) map[string]string {
	boundTo := map[string]string{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.CaseClause, *ast.CommClause:
			return false
		}
		if assign, ok := n.(*ast.AssignStmt); ok {
			recordDecodeBindings(assign, decodeFuncs, forwarders, qualifiers, boundTo)
		}
		return true
	})
	recordParameterBindings(fn, structToFunc, boundTo)
	return boundTo
}

// caseDecodeBindings returns, in source order, one caseRegion per
// switch/select branch of fn that binds at least one identifier to a decoded
// typed struct. A branch-local binding shadows the function-wide one for
// reads inside that branch (see recordScopedFieldReads): without this, a
// handler that dispatches on fact kind and reuses one identifier across
// sibling branches — `policy, err := factschema.DecodeAWSIAMTrustPolicy`
// in one case and `policy, err := factschema.DecodeAWSIAMPermissionBoundary`
// in the next, the loader trust-chain anchor's exact shape (#6392) —
// collapses every branch's reads onto the last binding, and the struct join
// in BuildManifest then drops the reads whose fields the wrong struct lacks.
// Branches nest: a read resolves against the nearest enclosing region's
// bindings chained over every enclosing region's bindings over the outer ones.
// A nested branch that binds nothing itself has no region of its own, so the
// enclosing region's walk descends into it and its reads resolve against the
// enclosing overlay — they never vanish (that silent drop was a false-green
// hole in the first version of this scoping, fixed in #6392 review). A nested
// branch that binds again gets its own region whose overlay chains the
// enclosing ones, so a read of an identifier bound only mid-chain still
// resolves (same hole class one level deeper, fixed with it).
func caseDecodeBindings(fn *ast.FuncDecl, decodeFuncs map[string]struct{}, forwarders RootForwarders, qualifiers DecodeQualifiers) []caseRegion {
	var regions []caseRegion
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.CaseClause, *ast.CommClause:
			boundTo := map[string]string{}
			ast.Inspect(n, func(inner ast.Node) bool {
				if inner != n {
					switch inner.(type) {
					case *ast.CaseClause, *ast.CommClause:
						return false
					}
				}
				if assign, ok := inner.(*ast.AssignStmt); ok {
					recordDecodeBindings(assign, decodeFuncs, forwarders, qualifiers, boundTo)
				}
				return true
			})
			if len(boundTo) > 0 {
				regions = append(regions, caseRegion{node: n, boundTo: boundTo})
			}
		}
		return true
	})
	return regions
}

// recordScopedFieldReads walks body and records a FieldUsage in two shapes:
//
//  1. `ident.Field` where ident is a key of the applicable binding map (a
//     seam-bound value from a decode call or a seam-typed parameter) —
//     attributed to that map's decode func for ident.
//  2. `wrapper.<seamField>.<StructField>` where wrapper is a key of
//     wrapperBound (a value of a wrapper struct type) and <seamField> is a
//     field of that wrapper whose type is a seam struct — the read of
//     <StructField> is attributed to the decode func that seam field came
//     from. This follows the one wrapper-mediated hop the migrated
//     IAM/secrets_iam handlers use (statement.permission.Actions,
//     principal.decoded.AccountID); deeper nesting (`a.b.c.d`) is not followed.
//
// The applicable binding map is region-scoped: reads outside any switch/select
// branch resolve against outer, while reads inside a branch resolve against
// the nearest enclosing region's bindings chained over every enclosing
// region's bindings over outer, so a branch-local binding shadows the
// function-wide one exactly where Go scoping says it does, a nested branch
// that binds nothing still resolves its reads against the enclosing overlay,
// and a nested region still sees identifiers bound only mid-chain. Each read
// is recorded exactly once: walks prune subtrees that carry their own region
// and descend into branches without one. Wrapper bindings stay function-wide:
// a wrapper identifier rebound across sibling branches keeps the last binding
// (same limitation class caseDecodeBindings closes for decode calls; no
// production instance today).
//
// A read that matches no declared field of the attributed struct is dropped
// later by BuildManifest (it joins against the struct's declared fields), so a
// wrapper read of a non-schema field never becomes a false violation.
func recordScopedFieldReads(body *ast.BlockStmt, fileName string, outer map[string]string, regions []caseRegion, wrapperBound map[string]string, wrappers map[string]map[string]string, usage map[string][]FieldUsage) {
	// regionNodes marks every branch that carries its own region. A walk
	// prunes those subtrees (their reads belong to their own region's walk)
	// but descends into branches without one, so every read in the function
	// is recorded exactly once, against the nearest enclosing region's
	// bindings — a read in a nested branch that binds nothing itself resolves
	// against the enclosing region's overlay, never vanishes.
	regionNodes := make(map[ast.Node]struct{}, len(regions))
	for _, r := range regions {
		regionNodes[r.node] = struct{}{}
	}
	// regionOverlay chains one region's bindings over every enclosing
	// region's bindings over outer (outermost first, so the nearest binding
	// shadows the wider ones exactly where Go scoping says it does).
	// Enclosure is subtree containment; an enclosing region always starts
	// earlier in the file, so sorting by position orders outermost first
	// deterministically.
	regionOverlay := func(r caseRegion) map[string]string {
		type chained struct {
			pos int
			set map[string]string
		}
		var chain []chained
		for _, other := range regions {
			if other.node != r.node && nodeContains(other.node, r.node) {
				chain = append(chain, chained{pos: int(other.node.Pos()), set: other.boundTo})
			}
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].pos < chain[j].pos })
		overlay := make(map[string]string, len(outer)+len(r.boundTo))
		for k, v := range outer {
			overlay[k] = v
		}
		for _, link := range chain {
			for k, v := range link.set {
				overlay[k] = v
			}
		}
		for k, v := range r.boundTo {
			overlay[k] = v
		}
		return overlay
	}
	pruneNestedRegions := func(self, n ast.Node) bool {
		if n == self {
			return false
		}
		switch n.(type) {
		case *ast.CaseClause, *ast.CommClause:
			_, ok := regionNodes[n]
			return ok
		}
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if pruneNestedRegions(nil, n) {
			return false
		}
		recordRead(n, fileName, outer, wrapperBound, wrappers, usage)
		return true
	})
	for _, r := range regions {
		overlay := regionOverlay(r)
		ast.Inspect(r.node, func(n ast.Node) bool {
			if pruneNestedRegions(r.node, n) {
				return false
			}
			recordRead(n, fileName, overlay, wrapperBound, wrappers, usage)
			return true
		})
	}
}

// nodeContains reports whether target lies anywhere inside root's subtree
// (root itself counts). Used to chain a nested region's overlay over every
// enclosing region's bindings.
func nodeContains(root, target ast.Node) bool {
	if root == nil || target == nil {
		return false
	}
	if root == target {
		return true
	}
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found {
			return false
		}
		if n == target {
			found = true
			return false
		}
		return true
	})
	return found
}

// recordRead attributes one AST node when it is a field read off a seam-bound
// identifier in either of the two shapes recordScopedFieldReads documents;
// any other node is a no-op.
func recordRead(n ast.Node, fileName string, boundTo, wrapperBound map[string]string, wrappers map[string]map[string]string, usage map[string][]FieldUsage) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if ident, isIdent := sel.X.(*ast.Ident); isIdent {
		if funcName, isBound := boundTo[ident.Name]; isBound {
			usage[funcName] = append(usage[funcName], FieldUsage{File: fileName, GoFieldName: sel.Sel.Name})
		}
		return
	}
	inner, isSel := sel.X.(*ast.SelectorExpr)
	if !isSel {
		return
	}
	base, isIdent := inner.X.(*ast.Ident)
	if !isIdent {
		return
	}
	wrapperType, isWrapperBound := wrapperBound[base.Name]
	if !isWrapperBound {
		return
	}
	funcName, isSeamField := wrappers[wrapperType][inner.Sel.Name]
	if !isSeamField {
		return
	}
	usage[funcName] = append(usage[funcName], FieldUsage{File: fileName, GoFieldName: sel.Sel.Name})
}
