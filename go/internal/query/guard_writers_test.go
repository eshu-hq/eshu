// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"strings"
)

// queryParam is one named parameter of a function and its argument index.
type queryParam struct {
	name  string
	index int
}

// queryForwardableParams returns fn's named parameters that fn never assigns,
// with their argument indexes (an unnamed parameter still takes an index).
func queryForwardableParams(fn *ast.FuncDecl) []queryParam {
	var params []queryParam
	index := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			index++
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" && !queryAssigns(fn.Body, name.Name) {
				params = append(params, queryParam{name: name.Name, index: index})
			}
			index++
		}
	}
	return params
}

// queryTaint returns the identifiers in body that carry name's value: name
// itself, and every local assigned from an expression that mentions a carrier
// (`body := map[string]any{"detail": message}`, `body["detail"] = message`).
func queryTaint(body *ast.BlockStmt, name string) map[string]bool {
	taint := map[string]bool{name: true}
	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(node ast.Node) bool {
			var lhs, rhs []ast.Expr
			switch n := node.(type) {
			case *ast.AssignStmt:
				lhs, rhs = n.Lhs, n.Rhs
			case *ast.ValueSpec:
				for _, ident := range n.Names {
					lhs = append(lhs, ident)
				}
				rhs = n.Values
			default:
				return true
			}
			if !queryMentionsAny(rhs, taint) {
				return true
			}
			for _, target := range lhs {
				if base := queryBaseIdent(target); base != "" && base != "_" && !taint[base] {
					taint[base] = true
					changed = true
				}
			}
			return true
		})
	}
	return taint
}

// queryBaseIdent returns the variable an assignment target writes: x for x,
// x[k], and x.f.
func queryBaseIdent(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return queryBaseIdent(e.X)
	case *ast.SelectorExpr:
		return queryBaseIdent(e.X)
	case *ast.StarExpr:
		return queryBaseIdent(e.X)
	default:
		return ""
	}
}

// queryMentionsAny reports whether any of exprs mentions an identifier in names.
func queryMentionsAny(exprs []ast.Expr, names map[string]bool) bool {
	found := false
	for _, expr := range exprs {
		ast.Inspect(expr, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && names[ident.Name] {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// deriveSpec returns fn's writer spec under the writers known so far, and
// whether fn is a writer at all. fn forwards a status when a never-assigned
// parameter is passed unchanged as a writer's status. It forwards a message
// when a never-assigned parameter, directly or through locals, reaches a
// writer's message argument or a raw body write (queryBodyWriteArgs); a raw
// body write sits behind fn's forwarded status, else behind the literal fn
// passes to WriteHeader, else behind the implicit 200. fn is fixed when it
// passes a disallowed literal status (queryAllowedDirectStatus) to a writer,
// or calls a fixed writer.
func (tree *queryGuardTree) deriveSpec(f *queryGuardFile, fn *ast.FuncDecl) (queryWriterSpec, bool) {
	params := queryForwardableParams(fn)
	taints := make([]map[string]bool, len(params))
	for i, param := range params {
		taints[i] = queryTaint(fn.Body, param.name)
	}
	spec := queryWriterSpec{status: -1}
	var bodyMessages uint64
	headerMode := queryMessageClient
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for i, param := range params {
			if queryMentionsAny(queryBodyWriteArgs(f, call), taints[i]) && param.index < 64 {
				bodyMessages |= 1 << param.index
			}
		}
		callee, name, ok := tree.resolve(f, call)
		if !ok {
			return true
		}
		chain := name
		if callee.chain != "" {
			chain = name + " -> " + callee.chain
		}
		statusParam, statusArg := queryForwardedStatus(call, callee, params)
		if statusParam >= 0 && spec.status < 0 {
			spec.status, spec.chain = statusParam, chain
		}
		if statusArg != nil && statusParam < 0 && callee.messages == 0 && !queryIsNonFailureStatus(statusArg) {
			headerMode = queryMessageServer
		}
		switch {
		case callee.fixed != "" && spec.fixed == "":
			fixedChain := name
			if callee.fixedChain != "" {
				fixedChain = name + " -> " + callee.fixedChain
			}
			spec.fixed, spec.fixedChain = callee.fixed, fixedChain
		case statusArg != nil && spec.fixed == "" && !queryIsZeroSentinel(callee, statusArg):
			if value, ok := queryStatusLiteral(statusArg); ok && !queryAllowedDirectStatus(value) {
				spec.fixed, spec.fixedChain = queryExprString(statusArg), chain
			}
		}
		for i, param := range params {
			if param.index >= 64 || !queryMentionsAny(queryMessageArgs(call.Args, callee.messages), taints[i]) {
				continue
			}
			spec.messages |= 1 << param.index
			spec.messageMode = max(spec.messageMode, queryForwardedMode(callee, statusArg, statusParam, spec.status))
		}
		return true
	})
	if bodyMessages != 0 {
		spec.messages |= bodyMessages
		switch {
		case spec.status >= 0:
			spec.messageMode = max(spec.messageMode, queryMessageStatusArg)
		default:
			spec.messageMode = max(spec.messageMode, headerMode)
		}
	}
	if spec.status < 0 && spec.messages == 0 && spec.fixed == "" {
		return queryWriterSpec{}, false
	}
	return spec, true
}

// queryForwardedStatus returns call's status argument for callee, and the
// argument index of the never-assigned parameter it is, or -1.
func queryForwardedStatus(call *ast.CallExpr, callee queryWriterSpec, params []queryParam) (int, ast.Expr) {
	if callee.status < 0 || callee.status >= len(call.Args) {
		return -1, nil
	}
	statusArg := call.Args[callee.status]
	if ident, ok := statusArg.(*ast.Ident); ok {
		for _, param := range params {
			if param.name == ident.Name {
				return param.index, statusArg
			}
		}
	}
	return -1, statusArg
}

// queryMessageArgs returns the arguments whose bit is set in messages.
func queryMessageArgs(args []ast.Expr, messages uint64) []ast.Expr {
	var out []ast.Expr
	for i, arg := range args {
		if i < 64 && messages&(1<<i) != 0 {
			out = append(out, arg)
		}
	}
	return out
}

// queryBodyWriteArgs returns the arguments a raw body write puts in the
// response: x.Encode(v) and x.Write(b) under any receiver, io.WriteString's
// string, and the operands of fmt.Fprint, fmt.Fprintf, and fmt.Fprintln.
func queryBodyWriteArgs(f *queryGuardFile, call *ast.CallExpr) []ast.Expr {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) == 0 {
		return nil
	}
	if pkg, ok := sel.X.(*ast.Ident); ok {
		if _, imported := f.imports[pkg.Name]; imported {
			switch {
			case pkg.Name == "io" && sel.Sel.Name == "WriteString",
				pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Fprint"):
				return call.Args[1:]
			}
			return nil
		}
	}
	if (sel.Sel.Name == "Encode" || sel.Sel.Name == "Write") && len(call.Args) == 1 {
		return call.Args
	}
	return nil
}

// queryForwardedMode is the message mode a writer inherits from one call that
// passes its message on: the callee's own mode, with a status-argument mode
// resolved against the status the call passes.
func queryForwardedMode(callee queryWriterSpec, statusArg ast.Expr, statusParam, ownStatus int) queryMessageMode {
	if callee.messageMode != queryMessageStatusArg {
		return callee.messageMode
	}
	if statusParam >= 0 && statusParam == ownStatus {
		return queryMessageStatusArg
	}
	if statusArg != nil && queryIsNonFailureStatus(statusArg) {
		return queryMessageClient
	}
	return queryMessageServer
}

// queryIsNonFailureStatus reports whether expr is a literal status in
// [100, 499]: a body behind it is not a server failure.
func queryIsNonFailureStatus(expr ast.Expr) bool {
	value, ok := queryStatusLiteral(expr)
	return ok && value >= 100 && value <= 499
}

// queryIsParam reports whether name is a parameter of a function type.
func queryIsParam(typ *ast.FuncType, name string) bool {
	if typ == nil || typ.Params == nil {
		return false
	}
	for _, field := range typ.Params.List {
		for _, param := range field.Names {
			if param.Name == name {
				return true
			}
		}
	}
	return false
}

// queryAssigns reports whether any assignment or increment in body, nested
// function literals included, names name on its left-hand side.
func queryAssigns(body *ast.BlockStmt, name string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == name {
					found = true
				}
			}
		case *ast.IncDecStmt:
			if ident, ok := n.X.(*ast.Ident); ok && ident.Name == name {
				found = true
			}
		case *ast.ValueSpec:
			for _, ident := range n.Names {
				if ident.Name == name {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// queryStatusLiteral returns the value of an int literal or a net/http Status
// constant.
func queryStatusLiteral(expr ast.Expr) (int, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return queryIntLiteral(e)
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok || pkg.Name != "http" {
			return 0, false
		}
		value, ok := queryHTTPStatusValues[e.Sel.Name]
		return value, ok
	default:
		return 0, false
	}
}

// queryExprString renders a status operand for a failure message.
func queryExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Value
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return queryExprString(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return queryExprString(e.Fun) + "(...)"
	default:
		return "<expr>"
	}
}
