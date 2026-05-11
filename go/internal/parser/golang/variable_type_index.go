package golang

import (
	"maps"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goVariableTypeIndex amortizes scoped variable-type lookups across one parse.
//
// Before the index, goKnownLocalVariableTypesForNode rebuilt the file's
// package-level variable types from scratch and walked the enclosing function
// scope on every call. goCollectSemanticDeadCodeRoots invoked it once per
// var_spec, short_var_declaration, assignment_statement, composite_literal,
// return_statement, and call_expression — i.e. effectively once per
// expression node in a Go file. The combined cost was O(call_sites * file_size)
// in tree-sitter cgo calls per file, which dominated parse wall time on
// repo-scale dogfood inputs (see #161 follow-up).
//
// The index computes the file's package-level variable types once, then lazily
// builds a sorted slice of scoped declarations per function scope. Per-call
// lookups are O(package_vars + scope_bindings_before_call) in pure Go map and
// slice work — no further cgo.
type goVariableTypeIndex struct {
	packageVars        map[string]string
	scopeBindings      map[uintptr][]goScopedBinding
	source             []byte
	structTypes        map[string]struct{}
	constructorReturns map[string]string
	lookup             *goParentLookup
}

// goScopedBindingKind tags one of the variable-binding shapes the index
// replays for a call site. Using an explicit tag plus an inlined node value
// avoids one heap-allocated closure per binding (the prior shape) and keeps
// the binding self-contained, so the stored slice never depends on a pointer
// into a stack frame that has since returned.
type goScopedBindingKind uint8

const (
	// goScopedBindingFuncParams replays the scope function's parameter list
	// via goRecordLocalParameterTypes. Visited once when the scope node
	// itself is the function_declaration / method_declaration / func_literal.
	goScopedBindingFuncParams goScopedBindingKind = iota + 1
	// goScopedBindingVarSpec replays one var_spec via goRecordLocalVarSpecTypes.
	goScopedBindingVarSpec
	// goScopedBindingAssignment replays one short_var_declaration or
	// assignment_statement via goRecordLocalAssignmentTypes.
	goScopedBindingAssignment
)

// goScopedBinding is one variable declaration inside a function scope. The
// startByte field orders bindings by source position so a query can stop
// scanning once it passes the call site. decl stores the declaration node by
// value: callers replay it via a stable pointer (&decl) that is independent
// of the walk that produced it.
type goScopedBinding struct {
	startByte uint
	kind      goScopedBindingKind
	decl      tree_sitter.Node
}

// goBuildVariableTypeIndex computes the package-level variable types eagerly
// and prepares lazy storage for per-scope bindings. structTypes and
// constructorReturns are retained because per-scope bindings need them at
// expand time, but they must not be mutated after this call.
func goBuildVariableTypeIndex(
	root *tree_sitter.Node,
	source []byte,
	structTypes map[string]struct{},
	constructorReturns map[string]string,
	lookup *goParentLookup,
) *goVariableTypeIndex {
	idx := &goVariableTypeIndex{
		scopeBindings:      make(map[uintptr][]goScopedBinding),
		source:             source,
		structTypes:        structTypes,
		constructorReturns: constructorReturns,
		lookup:             lookup,
	}
	idx.packageVars = goKnownLocalPackageVariableTypes(root, source, structTypes, constructorReturns, lookup)
	return idx
}

// ForNode returns the variable-type map visible to node, equivalent to what
// goKnownLocalVariableTypesForNode returned before the index existed. Callers
// receive a fresh map and may mutate it.
func (idx *goVariableTypeIndex) ForNode(root *tree_sitter.Node, node *tree_sitter.Node) map[string]string {
	if idx == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(idx.packageVars))
	maps.Copy(result, idx.packageVars)
	scope := goEnclosingFunctionScope(node, idx.lookup)
	if scope == nil {
		return result
	}
	bindings := idx.bindingsForScope(scope)
	target := node.StartByte()
	for i := range bindings {
		if bindings[i].startByte > target {
			break
		}
		decl := bindings[i].decl
		switch bindings[i].kind {
		case goScopedBindingFuncParams:
			goRecordLocalParameterTypes(&decl, idx.source, idx.structTypes, result)
		case goScopedBindingVarSpec:
			goRecordLocalVarSpecTypes(&decl, idx.source, idx.structTypes, idx.constructorReturns, result)
		case goScopedBindingAssignment:
			goRecordLocalAssignmentTypes(&decl, idx.source, idx.structTypes, idx.constructorReturns, result)
		}
	}
	return result
}

// bindingsForScope builds the per-scope binding list on first access and
// caches it. The walker stops at nested function_declaration / method_declaration
// / func_literal subtrees so a `var x = ...` inside an inner closure does not
// leak into the outer function's binding table (Go lexical scoping).
func (idx *goVariableTypeIndex) bindingsForScope(scope *tree_sitter.Node) []goScopedBinding {
	if cached, ok := idx.scopeBindings[scope.Id()]; ok {
		return cached
	}
	bindings := make([]goScopedBinding, 0, 8)
	walkScopeBindings(scope, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			if child.StartByte() != scope.StartByte() {
				return
			}
			bindings = append(bindings, goScopedBinding{
				startByte: child.StartByte(),
				kind:      goScopedBindingFuncParams,
				decl:      *child,
			})
		case "var_spec":
			bindings = append(bindings, goScopedBinding{
				startByte: child.StartByte(),
				kind:      goScopedBindingVarSpec,
				decl:      *child,
			})
		case "short_var_declaration", "assignment_statement":
			bindings = append(bindings, goScopedBinding{
				startByte: child.StartByte(),
				kind:      goScopedBindingAssignment,
				decl:      *child,
			})
		}
	})
	idx.scopeBindings[scope.Id()] = bindings
	return bindings
}
