package golang

import (
	"maps"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goImportedVariableTypeIndex amortizes scoped imported-variable-type lookups
// across one parse of the package-interface prescan path.
//
// Before the index, goKnownImportedVariableTypesForCall called
// goKnownImportedVariableTypes (a full-tree walk) per call_expression and then
// additionally walked the enclosing function scope per call. On dense Go
// inputs such as Terraform's internal/terraform tree, that pattern made the
// snapshot stage's PreScanGoPackageSemanticRoots phase saturate CPU without
// emitting any facts (#161 follow-up; the user-visible symptom was the
// ingester producing zero fact_records after 80+ CPU minutes even though
// per-file Parse completed in under a second).
//
// The index walks the file once to classify each binding as either
// package-scope (no enclosing function) or scope-local (enclosing function
// id), caches the per-scope list lazily on first access, and answers each
// call_expression query by copying the package-scope map and replaying the
// scope's bindings up to the call's start byte — pure Go work without further
// cgo tree-sitter calls beyond the one-time classification walk.
type goImportedVariableTypeIndex struct {
	packageVars   map[string]string
	scopeBindings map[uintptr][]goImportedScopedBinding
	source        []byte
	importAliases map[string][]string
	lookup        *goParentLookup
}

// goImportedScopedBinding captures one imported-variable declaration inside a
// function scope. The startByte field orders bindings by source position so a
// query can stop scanning once it passes the call site, preserving the
// "definition must precede use" filter the prior per-call walk enforced.
type goImportedScopedBinding struct {
	startByte uint
	apply     func(target map[string]string)
}

// goBuildImportedVariableTypeIndex returns an index built around the file's
// package-scope imported-variable types. Per-scope binding lists are computed
// lazily on first ForCall access for the scope; scopes never queried pay
// nothing beyond the one-time package-scope walk performed here.
func goBuildImportedVariableTypeIndex(
	root *tree_sitter.Node,
	source []byte,
	importAliases map[string][]string,
	lookup *goParentLookup,
) *goImportedVariableTypeIndex {
	idx := &goImportedVariableTypeIndex{
		scopeBindings: make(map[uintptr][]goImportedScopedBinding),
		source:        source,
		importAliases: importAliases,
		lookup:        lookup,
	}
	idx.packageVars = goKnownImportedVariableTypes(root, source, importAliases, lookup)
	return idx
}

// ForCall returns the imported-variable-type map visible at a call expression
// — equivalent to goKnownImportedVariableTypesForCall before the index. The
// returned map is freshly allocated and may be mutated by the caller without
// poisoning the cache.
func (idx *goImportedVariableTypeIndex) ForCall(call *tree_sitter.Node) map[string]string {
	if idx == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(idx.packageVars))
	maps.Copy(result, idx.packageVars)
	scope := goEnclosingFunctionScope(call, idx.lookup)
	if scope == nil {
		return result
	}
	bindings := idx.bindingsForScope(scope)
	target := call.StartByte()
	for _, binding := range bindings {
		if binding.startByte > target {
			break
		}
		binding.apply(result)
	}
	return result
}

// bindingsForScope builds the per-scope binding list on first access and
// caches it. Each entry pairs the declaration node's startByte with a closure
// that records its imported-type binding into the running variableTypes map
// using the same helpers the prior per-call walk invoked, so ForCall remains
// behaviorally identical to goKnownImportedVariableTypesForCall.
func (idx *goImportedVariableTypeIndex) bindingsForScope(scope *tree_sitter.Node) []goImportedScopedBinding {
	if cached, ok := idx.scopeBindings[scope.Id()]; ok {
		return cached
	}
	bindings := make([]goImportedScopedBinding, 0, 8)
	walkNamed(scope, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "parameter_declaration":
			decl := child
			bindings = append(bindings, goImportedScopedBinding{
				startByte: child.StartByte(),
				apply: func(target map[string]string) {
					goRecordImportedParameterTypes(decl, idx.source, idx.importAliases, target)
				},
			})
		case "var_spec":
			decl := child
			bindings = append(bindings, goImportedScopedBinding{
				startByte: child.StartByte(),
				apply: func(target map[string]string) {
					goRecordImportedVarSpecTypes(decl, idx.source, idx.importAliases, target)
				},
			})
		case "short_var_declaration", "assignment_statement":
			decl := child
			declStart := child.StartByte()
			bindings = append(bindings, goImportedScopedBinding{
				startByte: declStart,
				apply: func(target map[string]string) {
					// Pass the declaration's own startByte as maxStartByte so
					// goRecordImportedAssignmentTypes's internal guard always
					// admits this node. The outer "binding.startByte > target"
					// loop already enforces the "must precede call" rule.
					goRecordImportedAssignmentTypes(decl, idx.source, idx.importAliases, target, nil, declStart)
				},
			})
		}
	})
	idx.scopeBindings[scope.Id()] = bindings
	return bindings
}
