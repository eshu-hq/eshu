// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// parseCollectRootsFixture parses one Go source file in memory and runs
// CollectRoots over it exactly as deadcode.Evidence does in production:
// parent lookup built once per file, empty index maps, fresh output maps.
func parseCollectRootsFixture(
	t *testing.T,
	source string,
	importAliases map[string][]string,
	importedParamMethods shared.GoImportedInterfaceParamMethods,
) (functionRootKinds, interfaceRootKinds, structRootKinds map[string][]string) {
	t.Helper()
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}
	src := []byte(source)
	tree := parser.Parse(src, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree, want non-nil")
	}
	defer tree.Close()
	root := tree.RootNode()
	lookup := symbols.BuildParentLookup(root)
	functionRootKinds = make(map[string][]string)
	interfaceRootKinds = make(map[string][]string)
	structRootKinds = make(map[string][]string)
	CollectRoots(
		root,
		src,
		importAliases,
		importedParamMethods,
		nil,
		nil,
		functionRootKinds,
		interfaceRootKinds,
		structRootKinds,
		lookup,
	)
	return functionRootKinds, interfaceRootKinds, structRootKinds
}

func parseCollectRoots(t *testing.T, source string) (map[string][]string, map[string][]string, map[string][]string) {
	t.Helper()
	return parseCollectRootsFixture(t, source, nil, nil)
}

func requireRootKind(t *testing.T, kinds map[string][]string, key, wantKind string) {
	t.Helper()
	got, ok := kinds[key]
	if !ok {
		t.Fatalf("root kinds for %q missing, want entry containing %q (map=%v)", key, wantKind, kinds)
	}
	if !slices.Contains(got, wantKind) {
		t.Fatalf("root kinds for %q = %v, want entry containing %q", key, got, wantKind)
	}
}

func requireNoRootKinds(t *testing.T, kinds map[string][]string) {
	t.Helper()
	for key, got := range kinds {
		if len(got) != 0 {
			t.Fatalf("root kinds for %q = %v, want no entries", key, got)
		}
	}
	if len(kinds) != 0 {
		t.Fatalf("root kind map = %v, want empty", kinds)
	}
}

func TestCollectRootsNilRootKeepsMapsEmpty(t *testing.T) {
	functionRootKinds := make(map[string][]string)
	interfaceRootKinds := make(map[string][]string)
	structRootKinds := make(map[string][]string)
	CollectRoots(nil, []byte("package test"), nil, nil, nil, nil, functionRootKinds, interfaceRootKinds, structRootKinds, nil)
	requireNoRootKinds(t, functionRootKinds)
	requireNoRootKinds(t, interfaceRootKinds)
	requireNoRootKinds(t, structRootKinds)
}

func TestCollectRootsEmptyFileMarksNothing(t *testing.T) {
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, "package test\n")
	requireNoRootKinds(t, functionRootKinds)
	requireNoRootKinds(t, interfaceRootKinds)
	requireNoRootKinds(t, structRootKinds)
}

// interfaceReturnFixture exercises the return-type satisfaction flow: a
// constructor returning a local interface while building a concrete struct
// marks the interface reference, the implementation method, and the
// implementation type in one pass.
const interfaceReturnFixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

func early() {
	var i = impl{}
	i.Process()
}

func makeProcessor() Processor {
	return impl{}
}
`

func TestCollectRootsInterfaceReturnMarksImplementation(t *testing.T) {
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, interfaceReturnFixture)
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, functionRootKinds, "impl.process", "go.direct_method_call")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
	requireRootKind(t, structRootKinds, "impl", "go.type_reference")
}

func TestCollectRootsGenericConstraintMarksMethods(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

func useGeneric[T Processor](t T) {
	t.Process()
}
`
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
	requireRootKind(t, functionRootKinds, "impl.process", "go.generic_constraint_method")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}

func TestCollectRootsNonInterfaceConstraintMarksNothing(t *testing.T) {
	const fixture = `package test

func identity[T any](v T) T {
	return v
}
`
	functionRootKinds, interfaceRootKinds, _ := parseCollectRoots(t, fixture)
	requireNoRootKinds(t, interfaceRootKinds)
	if _, ok := functionRootKinds["identity"]; ok {
		t.Fatalf("functionRootKinds has unexpected entry for identity: %v", functionRootKinds)
	}
}

func TestCollectRootsFunctionValueReference(t *testing.T) {
	const fixture = `package test

func worker() {}

var handler = worker
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_value_reference")
}

func TestCollectRootsMethodValueReference(t *testing.T) {
	const fixture = `package test

type Service struct{}

func (s Service) Do() {}

func setup() {
	var svc Service
	var callback = svc.Do
	_ = callback
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "service.do", "go.method_value_reference")
}

func TestCollectRootsDirectMethodCall(t *testing.T) {
	const fixture = `package test

type Service struct{}

func (s Service) Serve() {}

func setup() {
	var svc Service
	svc.Serve()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "service.serve", "go.direct_method_call")
}

func TestCollectRootsReturnStatementFunctionValue(t *testing.T) {
	const fixture = `package test

func worker() {}

func getter() func() {
	return worker
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_value_reference")
}

func TestCollectRootsAssignmentFunctionValue(t *testing.T) {
	const fixture = `package test

func worker() {}

func setup() {
	var current func()
	current = worker
	_ = current
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_value_reference")
}

func TestCollectRootsShortVarFunctionValue(t *testing.T) {
	const fixture = `package test

func worker() {}

func setup() {
	current := worker
	_ = current
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_value_reference")
}

func TestCollectRootsVarSpecInterfaceType(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

var active Processor = impl{}
`
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}
