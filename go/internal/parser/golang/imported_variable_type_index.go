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

// goImportedScopedBindingKind tags one of the imported-variable binding shapes
// the index replays for a call site. Using an explicit tag plus an inlined
// node value (rather than one heap-allocated closure per binding) keeps the
// binding self-contained so the stored slice never depends on a pointer into
// a stack frame that has since returned.
type goImportedScopedBindingKind uint8

const (
	// goImportedScopedBindingParam replays one parameter_declaration via
	// goRecordImportedParameterTypes.
	goImportedScopedBindingParam goImportedScopedBindingKind = iota + 1
	// goImportedScopedBindingVarSpec replays one var_spec via
	// goRecordImportedVarSpecTypes.
	goImportedScopedBindingVarSpec
	// goImportedScopedBindingAssignment replays one short_var_declaration or
	// assignment_statement via goRecordImportedAssignmentTypes.
	goImportedScopedBindingAssignment
)

// goImportedScopedBinding captures one imported-variable declaration inside a
// function scope. The startByte field orders bindings by source position so a
// query can stop scanning once it passes the call site, preserving the
// "definition must precede use" filter the prior per-call walk enforced.
// decl stores the declaration node by value so the binding remains valid
// independently of the walk that produced it.
type goImportedScopedBinding struct {
	startByte uint
	kind      goImportedScopedBindingKind
	decl      tree_sitter.Node
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
	for i := range bindings {
		if bindings[i].startByte > target {
			break
		}
		decl := bindings[i].decl
		switch bindings[i].kind {
		case goImportedScopedBindingParam:
			goRecordImportedParameterTypes(&decl, idx.source, idx.importAliases, result)
		case goImportedScopedBindingVarSpec:
			goRecordImportedVarSpecTypes(&decl, idx.source, idx.importAliases, result)
		case goImportedScopedBindingAssignment:
			// Pass the declaration's own startByte as maxStartByte so
			// goRecordImportedAssignmentTypes' internal guard always admits
			// this node. The "startByte > target" early-break above already
			// enforces the "must precede call" rule.
			goRecordImportedAssignmentTypes(&decl, idx.source, idx.importAliases, result, nil, bindings[i].startByte)
		}
	}
	return result
}

// bindingsForScope builds the per-scope binding list on first access and
// caches it. The walker stops at nested function_declaration / method_declaration
// / func_literal subtrees so an imported-variable binding declared inside an
// inner closure does not leak into the outer function's binding table.
func (idx *goImportedVariableTypeIndex) bindingsForScope(scope *tree_sitter.Node) []goImportedScopedBinding {
	if cached, ok := idx.scopeBindings[scope.Id()]; ok {
		return cached
	}
	bindings := make([]goImportedScopedBinding, 0, 8)
	walkScopeBindings(scope, func(child *tree_sitter.Node) {
		switch child.Kind() {
		case "parameter_declaration":
			bindings = append(bindings, goImportedScopedBinding{
				startByte: child.StartByte(),
				kind:      goImportedScopedBindingParam,
				decl:      *child,
			})
		case "var_spec":
			bindings = append(bindings, goImportedScopedBinding{
				startByte: child.StartByte(),
				kind:      goImportedScopedBindingVarSpec,
				decl:      *child,
			})
		case "short_var_declaration", "assignment_statement":
			bindings = append(bindings, goImportedScopedBinding{
				startByte: child.StartByte(),
				kind:      goImportedScopedBindingAssignment,
				decl:      *child,
			})
		}
	})
	idx.scopeBindings[scope.Id()] = bindings
	return bindings
}
