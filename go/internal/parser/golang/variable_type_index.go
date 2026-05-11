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

// goScopedBinding is one variable declaration inside a function scope. The
// startByte field orders bindings by source position so a query can stop
// scanning once it passes the call site.
type goScopedBinding struct {
	startByte uint
	apply     func(target map[string]string)
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
	for _, binding := range bindings {
		if binding.startByte > target {
			break
		}
		binding.apply(result)
	}
	return result
}

// bindingsForScope builds the per-scope binding list on first access and
// caches it. Each entry captures the declaration node's startByte plus a
// closure that mutates the running variableTypes map the same way the old
// per-call walk did.
func (idx *goVariableTypeIndex) bindingsForScope(scope *tree_sitter.Node) []goScopedBinding {
	if cached, ok := idx.scopeBindings[scope.Id()]; ok {
		return cached
	}
	bindings := make([]goScopedBinding, 0, 8)
	walkNamed(scope, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "function_declaration", "method_declaration", "func_literal":
			if child.StartByte() != scope.StartByte() {
				return
			}
			decl := child
			bindings = append(bindings, goScopedBinding{
				startByte: child.StartByte(),
				apply: func(target map[string]string) {
					goRecordLocalParameterTypes(decl, idx.source, idx.structTypes, target)
				},
			})
		case "var_spec":
			decl := child
			bindings = append(bindings, goScopedBinding{
				startByte: child.StartByte(),
				apply: func(target map[string]string) {
					goRecordLocalVarSpecTypes(decl, idx.source, idx.structTypes, idx.constructorReturns, target)
				},
			})
		case "short_var_declaration", "assignment_statement":
			decl := child
			bindings = append(bindings, goScopedBinding{
				startByte: child.StartByte(),
				apply: func(target map[string]string) {
					goRecordLocalAssignmentTypes(decl, idx.source, idx.structTypes, idx.constructorReturns, target)
				},
			})
		}
	})
	idx.scopeBindings[scope.Id()] = bindings
	return bindings
}
