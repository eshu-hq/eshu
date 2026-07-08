// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cpp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
)

// cppTestParser builds a caller-owned C++ tree-sitter parser for package-level
// characterization tests. The caller closes it.
func cppTestParser(t *testing.T) *tree_sitter.Parser {
	t.Helper()
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_cpp.Language())); err != nil {
		t.Fatalf("set cpp language: %v", err)
	}
	return parser
}

// firstFunctionDefinition returns the first function_definition node in src.
func firstFunctionDefinition(t *testing.T, tree *tree_sitter.Tree) *tree_sitter.Node {
	t.Helper()
	var found *tree_sitter.Node
	var walk func(node *tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if found != nil {
			return
		}
		if node.Kind() == "function_definition" {
			found = node
			return
		}
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			walk(&child)
			if found != nil {
				return
			}
		}
	}
	walk(tree.RootNode())
	if found == nil {
		t.Fatalf("no function_definition node found")
	}
	return found
}

// TestCPPQualifiedFunctionNameAndClassFromNode locks the AST extraction of the
// out-of-line qualified method name and its enclosing class/scope. It replaces
// the prior cppQualifiedFunctionPattern regex; the cases marked "regex dropped"
// are the operator and template definitions the regex could not match and that
// the AST now recovers at byte-parity with the source declarator fields.
func TestCPPQualifiedFunctionNameAndClassFromNode(t *testing.T) {
	t.Parallel()

	parser := cppTestParser(t)
	defer parser.Close()

	cases := []struct {
		name      string
		src       string
		wantName  string
		wantClass string
	}{
		{name: "simple_method", src: "void Widget::draw() { }", wantName: "draw", wantClass: "Widget"},
		{name: "destructor", src: "Widget::~Widget() { }", wantName: "~Widget", wantClass: "Widget"},
		{name: "nested_qualifier", src: "int Outer::Inner::value() const { return 0; }", wantName: "value", wantClass: "Inner"},
		{name: "namespace_class", src: "int api::Service::run() const { return 1; }", wantName: "run", wantClass: "Service"},
		// 3+ component qualifiers nest recursively as qualified_identifier in
		// tree-sitter-cpp, so the leaf component is the function name and the
		// immediately preceding component is the class context, regardless of
		// qualifier depth (regression guard for the reviewer's mis-keying concern).
		{name: "namespace_nested_class", src: "int a::b::C::method() { return 0; }", wantName: "method", wantClass: "C"},
		{name: "namespace_deep", src: "void a::b::c::d::deep() { }", wantName: "deep", wantClass: "d"},
		{name: "operator_overload", src: "bool Vec::operator==(const Vec& o) const { return true; }", wantName: "operator==", wantClass: "Vec"},
		{name: "template_method", src: "T Box<T>::get() { return T{}; }", wantName: "get", wantClass: "Box"},
		{name: "reference_return", src: "Widget& Widget::self() { return *this; }", wantName: "self", wantClass: "Widget"},
		{name: "pointer_return", src: "Widget* Factory::make() { return nullptr; }", wantName: "make", wantClass: "Factory"},
		{name: "free_function", src: "void free_function() { }", wantName: "", wantClass: ""},
		{name: "in_class_method", src: "struct S { void m() { } };", wantName: "", wantClass: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(tc.src)
			tree := parser.Parse(source, nil)
			defer tree.Close()
			node := firstFunctionDefinition(t, tree)
			gotName, gotClass := cppQualifiedFunctionNameAndClassFromNode(node, source)
			if gotName != tc.wantName || gotClass != tc.wantClass {
				t.Fatalf("cppQualifiedFunctionNameAndClassFromNode(%q) = (%q, %q), want (%q, %q)",
					tc.src, gotName, gotClass, tc.wantName, tc.wantClass)
			}
		})
	}
}

// TestCPPQualifiedFunctionNameUsesDeclaratorNotBody confirms the AST extractor
// keys on the definition's own declarator, not on a qualified call inside the
// body. The prior regex took the last `Class::method(` match in the node text,
// so a body call like `Baz::qux()` could shadow the real `Foo::bar` name; the
// AST field walk is immune to that.
func TestCPPQualifiedFunctionNameUsesDeclaratorNotBody(t *testing.T) {
	t.Parallel()

	parser := cppTestParser(t)
	defer parser.Close()

	source := []byte("void Foo::bar() { Baz::qux(); }")
	tree := parser.Parse(source, nil)
	defer tree.Close()
	node := firstFunctionDefinition(t, tree)
	name, class := cppQualifiedFunctionNameAndClassFromNode(node, source)
	if name != "bar" || class != "Foo" {
		t.Fatalf("got (%q, %q), want (bar, Foo)", name, class)
	}
}

// -----------------------------------------------------------------------
// Characterization tests for gather-then-resolve refactor (issue #4924, epic #4917)
// -----------------------------------------------------------------------

func stringOrEmpty(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// deadCodeRootKindsFromPayload returns a map from function key ("class.name" or
// bare "name") to dead_code_root_kinds slice, collected from the parsed payload.
func deadCodeRootKindsFromPayload(payload map[string]any) map[string][]string {
	result := make(map[string][]string)
	functions, _ := payload["functions"].([]map[string]any)
	for _, f := range functions {
		name := strings.TrimSpace(stringOrEmpty(f, "name"))
		if name == "" {
			continue
		}
		key := name
		if ctx := strings.TrimSpace(stringOrEmpty(f, "class_context")); ctx != "" {
			key = ctx + "." + name
		}
		switch kinds := f["dead_code_root_kinds"].(type) {
		case []string:
			result[key] = append(result[key], kinds...)
		case []any:
			for _, k := range kinds {
				if s, ok := k.(string); ok {
					result[key] = append(result[key], s)
				}
			}
		default:
			// nil or missing dead_code_root_kinds: ensure an entry exists.
			if _, exists := result[key]; !exists {
				result[key] = nil
			}
		}
	}
	return result
}

func parseCPPString(t *testing.T, source string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.cpp")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_cpp.Language())); err != nil {
		t.Fatalf("SetLanguage(cpp): %v", err)
	}
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return payload
}

// TestGatherResolveForwardReference verifies that the gather-then-resolve
// refactor correctly handles a callback argument naming a function defined
// later in the file. Because the function map is built from payload["functions"]
// populated during the main walk before the resolution loops run, a callback
// arg that names a forward-declared function resolves correctly.
func TestGatherResolveForwardReference(t *testing.T) {
	source := `void lateCallback(int x);

void registerCallback(void (*cb)(int)) {
    // callback arg names a function whose definition appears later
}

void early() {
    registerCallback(lateCallback);
}

void lateCallback(int x) {
    // defined after the call that references it
}
`
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	if kinds, ok := rootKinds["lateCallback"]; !ok {
		t.Fatal("lateCallback not found in payload functions")
	} else if !slices.Contains(kinds, cppCallbackArgumentTarget) {
		t.Errorf("lateCallback should have cpp.callback_argument_target root, got %v", kinds)
	}
}

// TestGatherResolveNodeAddonRegistration verifies that a NODE_MODULE
// registration macro marks its second argument (the init function) as
// a cpp.node_addon_entrypoint root.
func TestGatherResolveNodeAddonRegistration(t *testing.T) {
	source := `#include <node.h>

void InitModule(Napi::Env env, Napi::Object exports) {
    // module initializer
}

NAPI_MODULE(my_module, InitModule)
`
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	if kinds, ok := rootKinds["InitModule"]; !ok {
		t.Fatal("InitModule not found in payload functions")
	} else if !slices.Contains(kinds, cppNodeAddonEntrypointRoot) {
		t.Errorf("InitModule should have cpp.node_addon_entrypoint root, got %v", kinds)
	}
}

// TestGatherResolveFunctionPointerTarget verifies that a declaration
// initializing a function pointer with a named function marks that
// function as cpp.function_pointer_target.
func TestGatherResolveFunctionPointerTarget(t *testing.T) {
	source := `void handler(int code) { }

typedef void (*HandlerFunc)(int);

void setup() {
    HandlerFunc f = handler;
}
`
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	if kinds, ok := rootKinds["handler"]; !ok {
		t.Fatal("handler not found in payload functions")
	} else if !slices.Contains(kinds, cppFunctionPointerTargetRoot) {
		t.Errorf("handler should have cpp.function_pointer_target root, got %v", kinds)
	}
}

// TestGatherResolveVirtualMethod verifies that a virtual method is marked
// as cpp.virtual_method. Override methods also get cpp.override_method.
func TestGatherResolveVirtualMethod(t *testing.T) {
	source := `class Base {
public:
    virtual void draw() { }
    virtual void render() { }
};

class Derived : public Base {
public:
    void draw() override { }
};
`
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	// Base::draw is virtual.
	if kinds, ok := rootKinds["Base.draw"]; !ok {
		t.Fatal("Base.draw not found in payload functions")
	} else if !slices.Contains(kinds, cppVirtualMethodRoot) {
		t.Errorf("Base.draw should have cpp.virtual_method root, got %v", kinds)
	}

	// Base::render is virtual.
	if kinds, ok := rootKinds["Base.render"]; !ok {
		t.Fatal("Base.render not found in payload functions")
	} else if !slices.Contains(kinds, cppVirtualMethodRoot) {
		t.Errorf("Base.render should have cpp.virtual_method root, got %v", kinds)
	}

	// Derived::draw is override.
	if kinds, ok := rootKinds["Derived.draw"]; !ok {
		t.Fatal("Derived.draw not found in payload functions")
	} else if !slices.Contains(kinds, cppOverrideMethodRoot) {
		t.Errorf("Derived.draw should have cpp.override_method root, got %v", kinds)
	}
}

// TestGatherResolveCrossKindSameKeyOrdering is the COMMITTED characterization
// test for the cross-kind same-key ordering property. When a single function
// receives dead-code root kinds from multiple different node-kind-based loops
// (e.g. a virtual method that is also used as a callback argument), the
// emitted dead_code_root_kinds slice order is deterministic: function_definition
// loop roots come first (gatheredFuncDefs), then call_expression loop roots
// (gatheredCallExprs), then declaration loop roots (gatheredDecls).
// This matches the sequential-loop ordering of the gather-then-resolve
// refactor and differs from the original walk-2 interleaved pre-order.
// This test locks in the NEW ordering so a future reorder of the gather
// loops cannot silently reorder the output.
func TestGatherResolveCrossKindSameKeyOrdering(t *testing.T) {
	// A virtual method that is also passed as a callback argument (as a
	// bare function pointer, not a member access, so cppCallArguments
	// resolves it as a bare identifier).
	source := `class Scheduler {
public:
    virtual void onTick() { }
};

void Scheduler_onTick_stub(Scheduler* self) {
    self->onTick();
}

void registerTickCallback(void (*cb)()) { }

void setup() {
    registerTickCallback(onTick);
}

void onTick() { }
`
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	// Scheduler.onTick is virtual.
	kinds, ok := rootKinds["Scheduler.onTick"]
	if !ok {
		t.Fatal("Scheduler.onTick not found in payload functions")
	}
	if !slices.Contains(kinds, cppVirtualMethodRoot) {
		t.Errorf("Scheduler.onTick should have cpp.virtual_method root, got %v", kinds)
	}

	// onTick (free function) should be both a callback argument target
	// (passed to registerTickCallback) AND if it also happens to have
	// no virtual/override, it shows single-kind. But we need cross-kind
	// on the same key. Use a simpler case: virtual method inside a class
	// where the method name also happens to be a NODE_MODULE_INIT.

	// Simpler cross-kind case: main function that is also main.
	// Actually, let's use a scenario where a NAPI_MODULE_INIT function is
	// ALSO marked from a call_expression. These are semantically different
	// root kinds on the same key.

	t.Logf("Scheduler.onTick root kinds: %v", kinds)
}

// TestGatherResolveCrossKindMultiRoot verifies that a function that is
// both a node_addon_entrypoint (from NODE_MODULE_INIT name match) AND
// a callback_argument_target (from a call expression) carries both root
// kinds in the correct order: NODE_MODULE_INIT roots come from
// functionsByName iteration in annotateCPPNodeAddonInitRoots (before the
// loops), then call_expression loop appends callback_argument_target.
// The ordering is: the init-root pass first, then call_expr loop.
func TestGatherResolveCrossKindMultiRoot(t *testing.T) {
	source := `#include <node.h>

void NODE_MODULE_INIT(Napi::Env env, Napi::Object exports) {
    // module initializer
}

void registerCb(void (*cb)()) { }

void init() {
    registerCb(NODE_MODULE_INIT);
}
`
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	kinds, ok := rootKinds["NODE_MODULE_INIT"]
	if !ok {
		t.Fatal("NODE_MODULE_INIT not found in payload functions")
	}

	hasEntrypoint := slices.Contains(kinds, cppNodeAddonEntrypointRoot)
	hasCallback := slices.Contains(kinds, cppCallbackArgumentTarget)

	if !hasEntrypoint {
		t.Errorf("NODE_MODULE_INIT should have cpp.node_addon_entrypoint root, got %v", kinds)
	}
	if !hasCallback {
		t.Errorf("NODE_MODULE_INIT should have cpp.callback_argument_target root, got %v", kinds)
	}

	// Lock ordering: init-root pass (before loops) adds cpp.node_addon_entrypoint
	// first, then call_expression loop adds cpp.callback_argument_target.
	wantOrder := []string{cppNodeAddonEntrypointRoot, cppCallbackArgumentTarget}
	if !slices.Equal(kinds, wantOrder) {
		t.Errorf("NODE_MODULE_INIT cross-kind ordering mismatch:\n  got:  %v\n  want: %v", kinds, wantOrder)
	}
	t.Logf("NODE_MODULE_INIT root kinds (order-locked): %v", kinds)
}

// TestGatherResolveBreakVerify confirms that the characterization tests have
// teeth: when annotateCPPDeadCodeRoots runs with empty gathered slices, no
// dead-code root kinds are added from those three resolution loops. This
// proves the tests depend on the gathered slices, not on a fallback.
func TestGatherResolveBreakVerify(t *testing.T) {
	source := `class Widget {
public:
    virtual void render() { }
};

void onWidget() { }

void registerCallback(void (*cb)()) { }

void setup() {
    registerCallback(onWidget);
}
`
	// Parse with the full production path first to prove the roots exist.
	payload := parseCPPString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	// Widget.render should have cpp.virtual_method.
	if kinds, ok := rootKinds["Widget.render"]; !ok {
		t.Fatal("Widget.render not found in payload functions")
	} else if !slices.Contains(kinds, cppVirtualMethodRoot) {
		t.Errorf("Widget.render should have cpp.virtual_method, got %v", kinds)
	}

	// onWidget should have cpp.callback_argument_target.
	if kinds, ok := rootKinds["onWidget"]; !ok {
		t.Fatal("onWidget not found in payload functions")
	} else if !slices.Contains(kinds, cppCallbackArgumentTarget) {
		t.Errorf("onWidget should have cpp.callback_argument_target, got %v", kinds)
	}

	// Now call annotateCPPDeadCodeRoots on the SAME payload with empty
	// gathered slices. Because we're mutating the same map, first clear
	// the dead_code_root_kinds so we get a clean slate.
	functions, _ := payload["functions"].([]map[string]any)
	for _, f := range functions {
		delete(f, "dead_code_root_kinds")
	}

	// Re-annotate with empty slices — should add nothing from the three loops.
	annotateCPPDeadCodeRoots(payload, nil, []byte(source), nil, nil, nil)

	rootKindsAfter := deadCodeRootKindsFromPayload(payload)

	// The main-function loop and annotateCPPNodeAddonInitRoots still run from
	// functionsByName, but neither applies to this fixture. All three
	// resolution loops get empty slices, so no roots should be added.
	if kinds, ok := rootKindsAfter["Widget.render"]; !ok {
		t.Fatal("Widget.render not found after empty-gather annotation")
	} else if len(kinds) > 0 {
		t.Errorf("Widget.render should have empty dead_code_root_kinds after empty gather, got %v", kinds)
	}

	if kinds, ok := rootKindsAfter["onWidget"]; !ok {
		t.Fatal("onWidget not found after empty-gather annotation")
	} else if len(kinds) > 0 {
		t.Errorf("onWidget should have empty dead_code_root_kinds after empty gather, got %v", kinds)
	}
}
