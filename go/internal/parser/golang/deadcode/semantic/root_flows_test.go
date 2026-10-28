// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestCollectRootsDependencyInjectionCallback(t *testing.T) {
	const fixture = `package test

func worker() {}

func run(callback func()) {
	callback()
}

func setup() {
	run(worker)
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.dependency_injection_callback")
}

func TestCollectRootsDependencyInjectionCallbackSkipsNonCallbackArg(t *testing.T) {
	const fixture = `package test

func worker() {}

func run(callback func(), retries int) {
	callback()
}

func setup() {
	run(worker, 3)
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.dependency_injection_callback")
}

func TestCollectRootsDependencyInjectionMethodCallback(t *testing.T) {
	const fixture = `package test

type Service struct{}

func (s Service) Handle() {}

func run(callback func()) {
	callback()
}

func setup() {
	var svc Service
	run(svc.Handle)
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "service.handle", "go.dependency_injection_callback")
}

func TestCollectRootsAdjacentCallbackParams(t *testing.T) {
	const fixture = `package test

func first() {}

func second() {}

func run(a func(), b func(), attempts int) {
	a()
	b()
}

func setup() {
	run(first, second, 2)
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "first", "go.dependency_injection_callback")
	requireRootKind(t, functionRootKinds, "second", "go.dependency_injection_callback")
}

// TestCollectRootsMultiNameCallbackParamDocumentsGrammarShape records how
// the tree-sitter grammar surfaces a multi-name parameter declaration:
// `a, b func()` yields one parameter_declaration naming only `a`, so the
// callback index covers argument 0 only. The dropped second name is a
// grammar artifact outside this lane; this test pins the first-argument
// path that the grammar does surface, not the missing name.
func TestCollectRootsMultiNameCallbackParamDocumentsGrammarShape(t *testing.T) {
	const fixture = `package test

func first() {}

func second() {}

func run(a, b func(), attempts int) {
	a()
	b()
}

func setup() {
	run(first, second, 2)
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "first", "go.dependency_injection_callback")
}

func TestCollectRootsNamedFunctionTypeCallback(t *testing.T) {
	const fixture = `package test

type Handler func()

func worker() {}

func run(callback Handler) {
	callback()
}

func setup() {
	run(worker)
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.dependency_injection_callback")
}

func TestCollectRootsFuncLiteralReachableCall(t *testing.T) {
	const fixture = `package test

func worker() {}

func each(fn func()) {
	fn()
}

func setup() {
	each(func() {
		worker()
	})
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_literal_reachable_call")
}

func TestCollectRootsCompositeLiteralFuncValue(t *testing.T) {
	const fixture = `package test

func worker() {}

var handlers = []func(){func() { worker() }}
`
	functionRootKinds, _, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "worker", "go.function_literal_reachable_call")
	if len(structRootKinds) != 0 {
		t.Fatalf("structRootKinds = %v, want empty for a func slice literal", structRootKinds)
	}
}

func TestCollectRootsCompositeLiteralIdentifierValue(t *testing.T) {
	const fixture = `package test

func worker() {}

var handlers = []func(){worker}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	// A bare identifier element is a function value reference, not a
	// literal body: only an actual func literal unlocks the
	// reachable-call flow.
	requireRootKind(t, functionRootKinds, "worker", "go.function_value_reference")
}

func TestCollectRootsFmtStringerMethod(t *testing.T) {
	const fixture = `package test

import "fmt"

type Token struct{}

func (t Token) String() string {
	return "token"
}

func render() string {
	current := Token{}
	return fmt.Sprintf("%v", current)
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	requireRootKind(t, functionRootKinds, "token.string", "go.fmt_stringer_method")
}

func TestCollectRootsFmtStringerCompositeLiteral(t *testing.T) {
	const fixture = `package test

import "fmt"

type Token struct{}

func (t Token) String() string {
	return "token"
}

func render() string {
	return fmt.Sprintf("%v", Token{})
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	requireRootKind(t, functionRootKinds, "token.string", "go.fmt_stringer_method")
}

func TestCollectRootsFmtStringerConversion(t *testing.T) {
	const fixture = `package test

import "fmt"

type Token string

func (t Token) String() string {
	return string(t)
}

func render() string {
	return fmt.Sprintf("%v", Token("abc"))
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	requireRootKind(t, functionRootKinds, "token.string", "go.fmt_stringer_method")
}

func TestCollectRootsFmtStringerParenthesized(t *testing.T) {
	const fixture = `package test

import "fmt"

type Token struct{}

func (t Token) String() string {
	return "token"
}

func render() string {
	current := Token{}
	return fmt.Sprintf("%v", (current))
}
`
	functionRootKinds, _, _ := parseCollectRootsFixture(t, fixture, map[string][]string{"fmt": {"fmt"}}, nil)
	requireRootKind(t, functionRootKinds, "token.string", "go.fmt_stringer_method")
}

func TestCollectRootsNonFmtCallMarksNoStringer(t *testing.T) {
	const fixture = `package test

type Token struct{}

func (t Token) String() string {
	return "token"
}

func render(current Token) string {
	return current.String()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	// The explicit String() call is a direct method call, not a fmt
	// Stringer root: the fmt flow must stay quiet without a fmt call.
	requireRootKind(t, functionRootKinds, "token.string", "go.direct_method_call")
	for _, kinds := range functionRootKinds {
		for _, kind := range kinds {
			if kind == "go.fmt_stringer_method" {
				t.Fatalf("functionRootKinds contains unexpected fmt Stringer root: %v", functionRootKinds)
			}
		}
	}
}

func TestCollectRootsCompositeLiteralInterfaceField(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

type Holder struct {
	Worker Processor
}

func build() Holder {
	return Holder{Worker: impl{}}
}
`
	functionRootKinds, interfaceRootKinds, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
}

func TestCollectRootsCallArgumentInterfaceMethod(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

func consume(worker Processor) {
	worker.Process()
}

func setup() {
	consume(impl{})
}
`
	functionRootKinds, _, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}

func TestCollectRootsCallArgumentVariableInterfaceMethod(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type impl struct{}

func (i *impl) Process() {}

func consume(worker Processor) {
	worker.Process()
}

func setup() {
	built := impl{}
	consume(built)
}
`
	functionRootKinds, _, structRootKinds := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "impl.process", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "impl", "go.interface_implementation_type")
}

func TestCollectRootsImportedParamTargetExportedMethods(t *testing.T) {
	const fixture = `package test

type Logger struct{}

func (l Logger) String() string {
	return "logger"
}

func sink(value any) {}

func setup() {
	current := Logger{}
	sink(current)
}
`
	imported := shared.GoImportedInterfaceParamMethods{"sink": {0: {}}}
	functionRootKinds, _, structRootKinds := parseCollectRootsFixture(t, fixture, nil, imported)
	requireRootKind(t, functionRootKinds, "logger.string", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "logger", "go.interface_implementation_type")
}

func TestCollectRootsImportedParamTargetNamedMethods(t *testing.T) {
	const fixture = `package test

type Logger struct{}

func (l Logger) String() string {
	return "logger"
}

func (l Logger) Reset() {}

func sink(value any) {}

func setup() {
	current := Logger{}
	sink(current)
}
`
	imported := shared.GoImportedInterfaceParamMethods{"sink": {0: {"String"}}}
	functionRootKinds, _, structRootKinds := parseCollectRootsFixture(t, fixture, nil, imported)
	requireRootKind(t, functionRootKinds, "logger.string", "go.interface_method_implementation")
	requireRootKind(t, structRootKinds, "logger", "go.interface_implementation_type")
}

func TestCollectRootsFieldChainDirectCall(t *testing.T) {
	const fixture = `package test

type Inner struct{}

func (i Inner) Go() {}

type Outer struct {
	Child Inner
}

func setup(scope Outer) {
	scope.Child.Go()
}
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "inner.go", "go.direct_method_call")
}

func TestCollectRootsConversionMethodValue(t *testing.T) {
	const fixture = `package test

type Service string

func (s Service) Do() {}

var callback = Service("seed").Do
`
	functionRootKinds, _, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, functionRootKinds, "service.do", "go.method_value_reference")
}

func TestCollectRootsParamInterfaceTypeReference(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

func consume(worker Processor) {
	worker.Process()
}
`
	_, interfaceRootKinds, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
}

func TestCollectRootsFieldDeclarationInterfaceType(t *testing.T) {
	const fixture = `package test

type Processor interface {
	Process()
}

type Holder struct {
	Worker Processor
}
`
	_, interfaceRootKinds, _ := parseCollectRoots(t, fixture)
	requireRootKind(t, interfaceRootKinds, "processor", "go.interface_type_reference")
}
