// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// parseJavaScriptSourceForTest parses one JavaScript snippet for the
// hapi-route and CommonJS helper tests. The caller invokes close.
func parseJavaScriptSourceForTest(t *testing.T, source string) (*tree_sitter.Node, []byte, func()) {
	t.Helper()
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage() error = %v, want nil", err)
	}
	bytes := []byte(source)
	tree := parser.Parse(bytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatal("Parse() returned nil tree")
	}
	return tree.RootNode(), bytes, func() {
		tree.Close()
		parser.Close()
	}
}

const hapiRouteFixture = `const server = getServer();
server.route({
  method: "GET",
  path: "/items",
  config: { handler: listItems },
});
function listItems(req, h) { return "ok"; }
server.route({
  method: "POST",
  path: "/inline",
  config: { handler: (req, h) => "created" },
});
module.exports = [
  { method: "PUT", path: "/jobs", handler: runJob },
];
function runJob(req, h) { return "ok"; }
const routes = [{ method: "DELETE", path: "/named", handler: showThing }];
module.exports = routes;
function showThing(req, h) { return "ok"; }
`

// handlerPairsByValue returns every pair keyed "handler" with its name and
// value nodes, in source order.
func handlerPairsByValue(t *testing.T, root *tree_sitter.Node, source []byte) []struct {
	node  *tree_sitter.Node
	name  *tree_sitter.Node
	value *tree_sitter.Node
} {
	t.Helper()
	var pairs []struct {
		node  *tree_sitter.Node
		name  *tree_sitter.Node
		value *tree_sitter.Node
	}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "pair" {
			return
		}
		key := node.ChildByFieldName("key")
		if key == nil || shared.NodeText(key, source) != "handler" {
			return
		}
		pairs = append(pairs, struct {
			node  *tree_sitter.Node
			name  *tree_sitter.Node
			value *tree_sitter.Node
		}{node, key, node.ChildByFieldName("value")})
	})
	if len(pairs) != 4 {
		t.Fatalf("found %d handler pairs, want 4", len(pairs))
	}
	return pairs
}

func TestHapiRouteHandlerReferenceCallResolvesBareReference(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, hapiRouteFixture)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)
	evidence := Evidence{Parents: parents}
	pairs := handlerPairsByValue(t, root, source)

	got := HapiRouteHandlerReferenceCall(pairs[0].node, pairs[0].name, pairs[0].value, source, "javascript", evidence)
	if got == nil {
		t.Fatal("reference handler in server.route returned nil, want a call record")
	}
	if got["name"] != "listItems" || got["full_name"] != "listItems" {
		t.Errorf("record = %v, want name/full_name listItems", got)
	}
	if got["call_kind"] != "javascript.hapi_route_handler_reference" || got["lang"] != "javascript" {
		t.Errorf("record = %v, want hapi reference kind and lang", got)
	}
	if _, ok := got["line_number"]; !ok {
		t.Errorf("record = %v, want a line_number", got)
	}

	// An inline function value is not a bare reference.
	if got := HapiRouteHandlerReferenceCall(pairs[1].node, pairs[1].name, pairs[1].value, source, "javascript", evidence); got != nil {
		t.Errorf("inline handler returned %v, want nil", got)
	}
	// A pair keyed differently is not a handler: borrow the method key.
	var methodKey *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if methodKey == nil && node.Kind() == "pair" &&
			shared.NodeText(node.ChildByFieldName("key"), source) == "method" {
			methodKey = node.ChildByFieldName("key")
		}
	})
	if methodKey == nil {
		t.Fatal("no method pair in fixture")
	}
	if got := HapiRouteHandlerReferenceCall(pairs[0].node, methodKey, pairs[0].value, source, "javascript", evidence); got != nil {
		t.Errorf("wrong key returned %v, want nil", got)
	}
	if got := HapiRouteHandlerReferenceCall(nil, pairs[0].name, pairs[0].value, source, "javascript", evidence); got != nil {
		t.Errorf("nil node returned %v, want nil", got)
	}
}

func TestHapiRouteHandlerReferenceCallThroughExportedCollection(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, hapiRouteFixture)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)
	evidence := Evidence{Parents: parents}
	pairs := handlerPairsByValue(t, root, source)

	// pairs[2] sits in the module.exports array; pairs[3] in the named
	// `routes` array re-exported through module.exports = routes.
	for i, want := range map[int]string{2: "runJob", 3: "showThing"} {
		got := HapiRouteHandlerReferenceCall(pairs[i].node, pairs[i].name, pairs[i].value, source, "javascript", evidence)
		if got == nil {
			t.Errorf("collection handler %d returned nil, want a call record", i)
			continue
		}
		if got["name"] != want {
			t.Errorf("collection handler %d name = %v, want %s", i, got["name"], want)
		}
	}
}

func TestIsHapiRouteConfigHandlerNeedsInlineFunction(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, hapiRouteFixture)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)
	pairs := handlerPairsByValue(t, root, source)

	if javaScriptIsHapiRouteConfigHandler(pairs[0].node, "handler", source, parents) {
		t.Error("bare reference reported as inline config handler")
	}
	if !javaScriptIsHapiRouteConfigHandler(pairs[1].node, "handler", source, parents) {
		t.Error("inline arrow handler not reported as config handler")
	}
	if javaScriptIsHapiRouteConfigHandler(pairs[0].node, "other", source, parents) {
		t.Error("wrong key reported as config handler")
	}
	if javaScriptIsHapiRouteConfigHandler(nil, "handler", source, parents) {
		t.Error("nil node reported as config handler")
	}
}

func TestHapiRouteObjectFindsDirectAndNestedConfigs(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, hapiRouteFixture)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	// The inline config object resolves through its config pair to the
	// enclosing route object; a bare method/path object resolves directly.
	found := 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "object" {
			return
		}
		route := javaScriptHapiRouteObject(node, source, parents)
		if route == nil {
			return
		}
		if !javaScriptObjectHasPairKey(route, source, "method") || !javaScriptObjectHasPairKey(route, source, "path") {
			t.Errorf("resolved route object lacks method/path: %q", string(source[route.StartByte():route.EndByte()]))
			return
		}
		found++
	})
	if found == 0 {
		t.Error("no route objects resolved, want at least the server.route configs")
	}

	if got := javaScriptHapiRouteObject(nil, source, parents); got != nil {
		t.Errorf("nil node resolved to %v, want nil", got)
	}
}

func TestObjectHasPairKeyTable(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, `server.route({ method: "GET", path: "/x" });`)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	var object *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if object == nil && node.Kind() == "object" {
			object = node
		}
	})
	if !javaScriptObjectHasPairKey(object, source, "method") {
		t.Error("method key not found")
	}
	if javaScriptObjectHasPairKey(object, source, "missing") {
		t.Error("missing key reported present")
	}
	if javaScriptObjectHasPairKey(nil, source, "method") {
		t.Error("nil node reported a key")
	}
	if javaScriptObjectHasPairKey(root, source, "method") {
		t.Error("program node reported a pair key")
	}
	_ = parents
}

func TestNodeIsCommonJSExportedValueAndVariableName(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, hapiRouteFixture)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	exported, plain := 0, 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "array" {
			return
		}
		if javaScriptNodeIsCommonJSExportedValue(node, source, parents) {
			exported++
		} else {
			plain++
		}
	})
	if exported != 1 || plain != 1 {
		t.Errorf("exported=%d plain=%d arrays, want 1 exported (module.exports) and 1 plain (const routes)", exported, plain)
	}

	names := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "array" {
			names[javaScriptVariableNameForValue(node, source, parents)] = true
		}
	})
	if !names["routes"] {
		t.Errorf("variable names for arrays = %v, want routes among them", names)
	}
	if got := javaScriptVariableNameForValue(nil, source, parents); got != "" {
		t.Errorf("nil node name = %q, want empty", got)
	}
	if got := javaScriptNodeIsCommonJSExportedValue(nil, source, parents); got {
		t.Error("nil node reported exported")
	}
}

func TestRootExportsIdentifierFindsReexport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, hapiRouteFixture)
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	if !javaScriptRootExportsIdentifier(root, "routes", source, parents) {
		t.Error("routes re-exported through module.exports not found")
	}
	if javaScriptRootExportsIdentifier(root, "missing", source, parents) {
		t.Error("unknown name reported re-exported")
	}
	if javaScriptRootExportsIdentifier(root, "  ", source, parents) {
		t.Error("blank name reported re-exported")
	}
	if javaScriptRootExportsIdentifier(nil, "routes", source, parents) {
		t.Error("nil node reported a re-export")
	}
}

func TestCommonJSAssignmentTargetShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.foo = 1;\nexports.bar = 2;\nplain.baz = 3;\n")
	defer closeFn()

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		left := node.ChildByFieldName("left")
		results[string(source[left.StartByte():left.EndByte()])] = javaScriptCommonJSAssignmentTarget(left, source)
	})

	for target, want := range map[string]bool{"module.exports.foo": true, "exports.bar": true, "plain.baz": false} {
		if results[target] != want {
			t.Errorf("assignment target %s = %v, want %v", target, results[target], want)
		}
	}
	if javaScriptCommonJSAssignmentTarget(nil, source) {
		t.Error("nil node reported as assignment target")
	}
}

func TestFirstNamedDescendantOfKind(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, `server.route({ method: "GET" });`)
	defer closeFn()

	found := javaScriptFirstNamedDescendantOfKind(root, "object")
	if found == nil || found.Kind() != "object" {
		t.Errorf("descendant search = %v, want the route object", found)
	}
	self := javaScriptFirstNamedDescendantOfKind(found, "object")
	if self == nil {
		t.Error("self-kind match returned nil, want a clone of the node")
	}
	if got := javaScriptFirstNamedDescendantOfKind(root, "  "); got != nil {
		t.Errorf("blank kind = %v, want nil", got)
	}
	if got := javaScriptFirstNamedDescendantOfKind(nil, "object"); got != nil {
		t.Errorf("nil node = %v, want nil", got)
	}
	_ = source
}

func TestObjectAliasNamesRespectsKeyFilter(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, `const o = { register: myPlugin, other: 1, register };`)
	defer closeFn()

	var object *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if object == nil && node.Kind() == "object" {
			object = node
		}
	})
	filtered := javaScriptObjectAliasNames(object, source, "register")
	if len(filtered) != 2 || filtered[0] != "myPlugin" || filtered[1] != "register" {
		t.Errorf("filtered names = %v, want [myPlugin register]", filtered)
	}
	unfiltered := javaScriptObjectAliasNames(object, source, "")
	if len(unfiltered) != 2 {
		t.Errorf("unfiltered names = %v, want the two identifier values (other: 1 skipped)", unfiltered)
	}
	none := javaScriptObjectAliasNames(object, source, "missing")
	if len(none) != 0 {
		t.Errorf("missing filter = %v, want empty", none)
	}
}

func TestHapiPluginRegisterAliasRootKinds(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.plugin = { register: myPlugin };\nmodule.exports.other = 1;\n")
	defer closeFn()

	got := javaScriptHapiPluginRegisterAliasRootKinds(root, source)
	if kinds, ok := got["myplugin"]; !ok || len(kinds) != 1 || kinds[0] != "javascript.hapi_plugin_register" {
		t.Errorf("plugin register kinds = %v, want myplugin mapped once", got)
	}
	if _, ok := got["other"]; ok {
		t.Errorf("non-plugin assignment leaked into register kinds: %v", got)
	}
	if got := javaScriptHapiPluginRegisterAliasRootKinds(nil, source); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestHapiPluginRegisterAliasNamesShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "const a = { register: myPlugin };\nconst b = { register };\nconst c = { other: 1 };\n")
	defer closeFn()

	names := [][]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "object" {
			names = append(names, javaScriptHapiPluginRegisterAliasNames(node, source))
		}
	})
	if len(names) != 3 {
		t.Fatalf("found %d objects, want 3", len(names))
	}
	if len(names[0]) != 1 || names[0][0] != "myPlugin" {
		t.Errorf("pair form = %v, want [myPlugin]", names[0])
	}
	if len(names[1]) != 1 || names[1][0] != "register" {
		t.Errorf("shorthand form = %v, want [register]", names[1])
	}
	if len(names[2]) != 0 {
		t.Errorf("unrelated form = %v, want empty", names[2])
	}
}

func TestPairInsideCommonJSPluginObject(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.plugin = { register: myPlugin };\nconst other = { register: nope };\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	// Both fixture pairs key on register, so count the true answers
	// instead of keying by name: only the plugin-object pair qualifies.
	trues := 0
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "pair" && javaScriptPairInsideCommonJSPluginObject(node, source, parents) {
			trues++
		}
	})
	if trues != 1 {
		t.Errorf("%d pairs inside plugin objects, want exactly 1", trues)
	}
	if javaScriptPairInsideCommonJSPluginObject(nil, source, parents) {
		t.Error("nil node reported inside a plugin object")
	}
	var ident *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if javaScriptPairInsideCommonJSPluginObject(ident, source, parents) {
		t.Error("non-pair node reported inside a plugin object")
	}
}

func TestRouteHandlerReferenceValueShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "const o = { a: handler, b: ns.handler, c: make() };")
	defer closeFn()

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "pair" {
			return
		}
		results[shared.NodeText(node.ChildByFieldName("key"), source)] = javaScriptRouteHandlerReferenceValue(node.ChildByFieldName("value"))
	})
	if !results["a"] || !results["b"] {
		t.Errorf("identifier/member values = %v, want both true", results)
	}
	if results["c"] {
		t.Errorf("call value reported as reference: %v", results)
	}
	if javaScriptRouteHandlerReferenceValue(nil) {
		t.Error("nil value reported as reference")
	}
}
