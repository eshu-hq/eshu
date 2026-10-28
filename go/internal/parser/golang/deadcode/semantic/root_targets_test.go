// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/golang/symbols"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func TestCollectRootsImportedReturnMarksExportedMethods(t *testing.T) {
	const fixture = `package test

type impl struct{}

func (i *impl) Process() {}

func (i *impl) Reset() {}

func build() external.Widget {
	return impl{}
}
`
	functionRootKinds, _, structRootKinds := parseCollectRoots(t, fixture)
	// The imported result target cannot name methods, so every exported
	// method on the returned concrete type counts as an implementation.
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, functionRootKinds, "impl.reset", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}

func TestCollectRootsImportedParamType(t *testing.T) {
	const fixture = `package test

type impl struct{}

func (i *impl) Process() {}

func sink(value external.Widget) {
}

func setup() {
	built := impl{}
	sink(built)
}
`
	functionRootKinds, _, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}

func TestCollectRootsUnresolvableCallArgumentMarksNothing(t *testing.T) {
	const fixture = `package test

func sink(value external.Widget) {
}

func setup() {
	sink(42)
}
`
	functionRootKinds, _, structRootKinds := parseCollectRootsFixture(
		t, fixture, nil, shared.GoImportedInterfaceParamMethods{"sink": {0: {}}},
	)
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty for an unresolvable argument", functionRootKinds)
	}
	if len(structRootKinds) != 0 {
		t.Fatalf("structRootKinds = %v, want empty for an unresolvable argument", structRootKinds)
	}
}

func TestCollectRootsImmediatelyInvokedFuncLiteral(t *testing.T) {
	const fixture = `package test

func worker() {}

func setup() {
	func() {
		worker()
	}()
}
`
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, fixture)
	// A bare callee is not a function value: this package marks value
	// references (assignments, arguments, returns), while call targets
	// resolve in the call-gather layer. The invocation names no value,
	// so no callback, argument, or value flow fires.
	requireNoRootKinds(t, functionRootKinds)
	requireNoRootKinds(t, interfaceRootKinds)
	requireNoRootKinds(t, structRootKinds)
}

func TestCollectRootsUndeclaredReceiverMarksNothing(t *testing.T) {
	const fixture = `package test

func setup() {
	helper.Go()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty for an undeclared receiver", functionRootKinds)
	}
}

func TestCollectRootsSiblingMethodCallOnReceiver(t *testing.T) {
	const fixture = `package test

type Service struct{}

func (s Service) Serve() {}

func (s Service) Boot() {
	s.Serve()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "service.serve", "go.direct_method_call")
}

func TestCollectRootsFieldChainOnMethodReceiver(t *testing.T) {
	const fixture = `package test

type Inner struct{}

func (i Inner) Go() {}

type Outer struct {
	Child Inner
}

func (o Outer) Find() {
	o.Child.Go()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "inner.go", "go.direct_method_call")
}

func TestCollectRootsFmtUnresolvableArgument(t *testing.T) {
	const fixture = `package test

import "fmt"

func render() string {
	return fmt.Sprintf("%v", missing)
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	for _, kinds := range functionRootKinds {
		for _, kind := range kinds {
			if kind == "go.fmt_stringer_method" {
				t.Fatalf("functionRootKinds contains unexpected fmt Stringer root: %v", functionRootKinds)
			}
		}
	}
}

func TestCollectRootsFmtArithmeticArgument(t *testing.T) {
	const fixture = `package test

import "fmt"

func render() string {
	return fmt.Sprintf("%v", 1+2)
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty for a non-receiver fmt argument", functionRootKinds)
	}
}

func TestCollectRootsFmtSelectorCallArgument(t *testing.T) {
	const fixture = `package test

import "fmt"

func render() string {
	return fmt.Sprintf("%v", factory.Build("seed"))
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	for _, kinds := range functionRootKinds {
		for _, kind := range kinds {
			if kind == "go.fmt_stringer_method" {
				t.Fatalf("functionRootKinds contains unexpected fmt Stringer root: %v", functionRootKinds)
			}
		}
	}
}

func TestCollectRootsFmtPointerDerefArgument(t *testing.T) {
	const fixture = `package test

import "fmt"

type Token struct{}

func (t Token) String() string {
	return "token"
}

func render(current *Token) string {
	return fmt.Sprintf("%v", *current)
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	// The variable index strips the pointer, so a dereferenced value
	// still resolves to its element type for Stringer marking.
	requireRootKind(t, functionRootKinds, "token.string", "go.fmt_stringer_method")
}

func TestCollectRootsUnknownMethodValueMarksNothing(t *testing.T) {
	const fixture = `package test

var callback = unknown.Do
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty for an unresolvable method value", functionRootKinds)
	}
}

func TestCollectRootsNonInterfaceKeyedField(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type Holder struct {
	Worker Processor
	Count  int
}

func build() Holder {
	return Holder{Count: 3}
}
`
	functionRootKinds, interfaceRootKinds, _ := parseCollectRoots(t, fixture)
	// The field declaration still references the interface, but a
	// literal that only sets a plain field contributes no
	// implementation evidence.
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty without concrete evidence", functionRootKinds)
	}
}

func TestCollectRootsSliceOfInterfaceLiteral(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

var workers = []Processor{impl{}}
`
	_, _, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, structRootKinds, "impl", "go.type_reference")
}

func TestCollectRootsGenericConstraintSkipsUnrelatedMethods(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

type Other struct{}

func (o Other) Extra() {}

func useGeneric[T Processor](t T) {
	t.Process()
}
`
	functionRootKinds, interfaceRootKinds, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "impl.process", "go.generic_constraint_method")
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
	if _, ok := functionRootKinds["other.extra"]; ok {
		t.Fatalf("functionRootKinds has unexpected entry for other.extra: %v", functionRootKinds)
	}
}

func TestCollectRootsUnrelatedGenericConstraintMarksNothing(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

func pick[T any](value T) T {
	return value
}
`
	_, interfaceRootKinds, _ := parseCollectRoots(t, fixture)
	requireNoRootKinds(t, interfaceRootKinds)
}

func TestGoReceiverTypeFromConversionText(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "Empty", base: "", want: ""},
		{name: "PlainIdentifier", base: "svc", want: ""},
		{name: "LeadingParen", base: "(svc)", want: ""},
		{name: "Conversion", base: "Service(\"seed\")", want: "service"},
		{name: "QualifiedConversion", base: "pkg.Token(\"seed\")", want: "token"},
		{name: "NoParenSuffix", base: "svc.Value", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goReceiverTypeFromConversionText(tt.base); got != tt.want {
				t.Fatalf("goReceiverTypeFromConversionText(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestGoCollectFunctionCallbackArgumentNilNode(t *testing.T) {
	// A nil callback argument must be a no-op, not a panic: the caller
	// guards the common cases, this pins the guard itself.
	goCollectFunctionCallbackArgument(nil, []byte("package test"), nil, nil, nil, make(map[string][]string))
}

func TestGoKeyedElementFieldAndValue(t *testing.T) {
	// The helper takes only non-nil nodes in production (every caller
	// passes a WalkNamed child, and the tree-sitter binding panics on a
	// nil receiver), so the smallest childless node is the empty
	// literal, which must yield no field and no value.
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}
	src := []byte("package test\n\ntype Holder struct{}\n\nvar _ = Holder{}\n")
	tree := parser.Parse(src, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree, want non-nil")
	}
	defer tree.Close()
	// A leaf type node has no named children, so it exercises the
	// short-children fallback exactly as a key-less element would.
	var leaf *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if leaf == nil && node.Kind() == "type_identifier" {
			cloned := *node
			leaf = &cloned
		}
	})
	if leaf == nil {
		t.Fatal("no type_identifier found, want one")
	}
	if field, value := goKeyedElementFieldAndValue(leaf, src); field != "" || value != nil {
		t.Fatalf("goKeyedElementFieldAndValue(leaf node) = (%q, %v), want (\"\", nil)", field, value)
	}
}

func TestGoSelectorConversionReceiverTypeFromCall(t *testing.T) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}
	src := []byte("package test\n\nvar _ = factory.Build(\"seed\")\nvar _ = Token(\"seed\")\n")
	tree := parser.Parse(src, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree, want non-nil")
	}
	defer tree.Close()
	byFunction := make(map[string]*tree_sitter.Node)
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		if fn := node.ChildByFieldName("function"); fn != nil {
			cloned := *node
			byFunction[shared.NodeText(fn, src)] = &cloned
		}
	})
	selectorCall := byFunction["factory.Build"]
	if selectorCall == nil {
		t.Fatal("no factory.Build call found, want one")
	}
	if got := goSelectorConversionReceiverTypeFromCall(selectorCall, src); got != "" {
		t.Fatalf("selector-function conversion receiver = %q, want \"\"", got)
	}
	identifierCall := byFunction["Token"]
	if identifierCall == nil {
		t.Fatal("no Token call found, want one")
	}
	if got := goSelectorConversionReceiverTypeFromCall(identifierCall, src); got != "token" {
		t.Fatalf("identifier conversion receiver = %q, want \"token\"", got)
	}
	if got := goKnownReceiverTypeFromExpression(nil, src, nil, nil); got != "" {
		t.Fatalf("goKnownReceiverTypeFromExpression(nil) = %q, want \"\"", got)
	}
	if got := goConcreteTypesInExpression(nil, src, nil); len(got) != 0 {
		t.Fatalf("goConcreteTypesInExpression(nil) = %v, want empty", got)
	}
}

func TestCollectRootsSelectorCallInsideFuncLiteral(t *testing.T) {
	const fixture = `package test

type Service struct{}

func (s Service) Serve() {}

func worker() {}

func each(fn func()) {
	fn()
}

func setup() {
	var svc Service
	each(func() {
		svc.Serve()
		worker()
	})
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_literal_reachable_call")
}

func TestCollectRootsUnknownCallInsideFuncLiteral(t *testing.T) {
	const fixture = `package test

func each(fn func()) {
	fn()
}

func setup() {
	each(func() {
		missing()
	})
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty for a call to an unknown function", functionRootKinds)
	}
}

func TestCollectRootsVariadicParam(t *testing.T) {
	const fixture = `package test

func logAll(prefix string, args ...int) {
}

func setup() {
	logAll("start", 1, 2)
}
`
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, fixture)
	requireNoRootKinds(t, functionRootKinds)
	requireNoRootKinds(t, interfaceRootKinds)
	requireNoRootKinds(t, structRootKinds)
}

func TestCollectRootsUnnamedImportedParam(t *testing.T) {
	const fixture = `package test

type impl struct{}

func (i *impl) Process() {}

func sink(external.Widget) {
}

func setup() {
	sink(impl{})
}
`
	functionRootKinds, _, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}

func TestCollectRootsDeepFieldChainMiss(t *testing.T) {
	const fixture = `package test

type Inner struct{}

func (i Inner) Go() {}

type Outer struct {
	Child Inner
}

func (o Outer) Find() {
	o.Child.Missing.Extra.Go()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	// The chain resolves Child to Inner but Missing has no recorded
	// field type, so resolution stops quietly instead of guessing.
	if _, ok := functionRootKinds["inner.go"]; ok {
		t.Fatalf("functionRootKinds has unexpected entry for inner.go: %v", functionRootKinds)
	}
}

func TestCollectRootsSelectorConversionMethodValue(t *testing.T) {
	const fixture = `package test

var callback = factory.Build("seed").Do
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	if len(functionRootKinds) != 0 {
		t.Fatalf("functionRootKinds = %v, want empty for an unresolvable conversion chain", functionRootKinds)
	}
}

func TestGoKeyedElementFallbackWithTwoChildren(t *testing.T) {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}
	src := []byte("package test\n\nfunc setup() {\n\tsink(first, second)\n}\n")
	tree := parser.Parse(src, nil)
	if tree == nil {
		t.Fatal("Parse() returned nil tree, want non-nil")
	}
	defer tree.Close()
	var argList *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if argList == nil && node.Kind() == "argument_list" {
			cloned := *node
			argList = &cloned
		}
	})
	if argList == nil {
		t.Fatal("no argument_list found, want one")
	}
	field, value := goKeyedElementFieldAndValue(argList, src)
	if field != "first" {
		t.Fatalf("fallback field = %q, want \"first\"", field)
	}
	if value == nil || shared.NodeText(value, src) != "second" {
		t.Fatalf("fallback value text = %q, want \"second\"", shared.NodeText(value, src))
	}
}

func TestGoMergeImportedInterfaceParamTargetsKeepsLocal(t *testing.T) {
	targets := map[string]map[int]symbols.InterfaceTarget{
		"consume": {0: {LocalInterface: "processor"}},
	}
	goMergeImportedInterfaceParamTargets(targets, shared.GoImportedInterfaceParamMethods{"consume": {0: {"process"}}})
	got := targets["consume"][0]
	if got.LocalInterface != "processor" || got.Imported {
		t.Fatalf("merged target = %+v, want local processor preserved without import flags", got)
	}
}
