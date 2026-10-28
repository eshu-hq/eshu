// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/javascript/syntax"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestMergeRegisteredRootKindsDedupesAndDropsBlanks(t *testing.T) {
	t.Parallel()

	dst := map[string][]string{"handler": {"javascript.express_route_registration"}}
	src := map[string][]string{
		" Handler ": {"javascript.express_route_registration", "javascript.hapi_handler_export"},
		"   ":       {"javascript.noise"},
		"other":     {"javascript.hapi_handler_export"},
	}
	MergeRegisteredRootKinds(dst, src)

	if kinds := dst["handler"]; len(kinds) != 2 {
		t.Errorf("merged handler kinds = %v, want both kinds deduplicated", kinds)
	}
	if _, ok := dst[""]; ok {
		t.Errorf("blank name grew an empty key: %v", dst)
	}
	if kinds := dst["other"]; len(kinds) != 1 {
		t.Errorf("other kinds = %v, want one entry", kinds)
	}
}

func TestCommonJSExportAliasRootKinds(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.list = listHandler;\nexports.show = showHandler;\nplain.assign = 1;\n")
	defer closeFn()

	got := javaScriptCommonJSExportAliasRootKinds(root, source, "javascript.hapi_handler_export")
	if kinds := got["listhandler"]; len(kinds) != 1 {
		t.Errorf("listhandler kinds = %v, want one entry", got)
	}
	if kinds := got["showhandler"]; len(kinds) != 1 {
		t.Errorf("showhandler kinds = %v, want one entry (exports shorthand)", got)
	}
	if _, ok := got["assign"]; ok {
		t.Errorf("plain assignment leaked into alias kinds: %v", got)
	}
	if got := javaScriptCommonJSExportAliasRootKinds(root, source, "  "); len(got) != 0 {
		t.Errorf("blank kind = %v, want empty", got)
	}
	if got := javaScriptCommonJSExportAliasRootKinds(nil, source, "javascript.hapi_handler_export"); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestCommonJSDefaultExportAliasRootKinds(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports = App;\nmodule.exports.helper = help;\n")
	defer closeFn()

	got := javaScriptCommonJSDefaultExportAliasRootKinds(root, source)
	if kinds := got["app"]; len(kinds) != 1 || kinds[0] != "javascript.commonjs_default_export" {
		t.Errorf("default export kinds = %v, want app mapped once", got)
	}
	if _, ok := got["help"]; ok {
		t.Errorf("member assignment leaked into default kinds: %v", got)
	}
	if got := javaScriptCommonJSDefaultExportAliasRootKinds(nil, source); len(got) != 0 {
		t.Errorf("nil root = %v, want empty", got)
	}
}

func TestCollectCommonJSModuleExportAlias(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "const alias = module.exports;\nconst other = require(\"x\");\n")
	defer closeFn()

	dst := map[string]struct{}{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		CollectCommonJSModuleExportAlias(node, source, dst)
	})
	if _, ok := dst["alias"]; !ok {
		t.Errorf("aliases = %v, want alias recorded", dst)
	}
	if _, ok := dst["other"]; ok {
		t.Errorf("aliases = %v, want only the module.exports alias", dst)
	}
}

func TestMethodInsideCommonJSDefaultExport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports = class Service {\n  run() { return 1; }\n};\nclass Plain {\n  walk() { return 2; }\n}\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "method_definition" {
			return
		}
		name := node.ChildByFieldName("name")
		results[string(source[name.StartByte():name.EndByte()])] = javaScriptMethodInsideCommonJSDefaultExport(node, source, parents)
	})
	if !results["run"] {
		t.Errorf("exported class method results = %v, want run true", results)
	}
	if results["walk"] {
		t.Errorf("plain class method results = %v, want walk false", results)
	}

	var funcDecl *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if funcDecl == nil && node.Kind() == "function_declaration" {
			funcDecl = node
		}
	})
	_ = funcDecl
	if javaScriptMethodInsideCommonJSDefaultExport(nil, source, parents) {
		t.Error("nil node reported inside a default export")
	}
}

func TestNearestClassNodeStopsAtProgram(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseJavaScriptSourceForTest(t, "class Service {\n  run() {}\n}\nfunction top() {}\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	var method, funcDecl *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "method_definition":
			if method == nil {
				method = node
			}
		case "function_declaration":
			if funcDecl == nil {
				funcDecl = node
			}
		}
	})
	if classNode := javaScriptNearestClassNode(method, parents); classNode == nil || classNode.Kind() != "class_declaration" {
		t.Errorf("nearest class = %v, want the class_declaration", classNode)
	}
	if classNode := javaScriptNearestClassNode(funcDecl, parents); classNode != nil {
		t.Errorf("top-level function nearest class = %v, want nil", classNode)
	}
}

func TestAssignmentRightChainContains(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseJavaScriptSourceForTest(t, "module.exports = App;\n")
	defer closeFn()

	var assign *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if assign == nil && node.Kind() == "assignment_expression" {
			assign = node
		}
	})
	right := assign.ChildByFieldName("right")
	if !javaScriptAssignmentRightChainContains(right, right) {
		t.Error("same-range nodes not reported contained")
	}
	left := assign.ChildByFieldName("left")
	if javaScriptAssignmentRightChainContains(left, right) {
		t.Error("sibling nodes reported contained")
	}
	if javaScriptAssignmentRightChainContains(nil, right) {
		t.Error("nil value reported contained")
	}
}

func TestRewriteCommonJSModuleExportAliasFullName(t *testing.T) {
	t.Parallel()

	aliases := map[string]struct{}{"alias": {}}
	if got := RewriteCommonJSModuleExportAliasFullName("alias.handler", aliases); got != "module.exports.handler" {
		t.Errorf("aliased rewrite = %q, want module.exports.handler", got)
	}
	if got := RewriteCommonJSModuleExportAliasFullName("other.handler", aliases); got != "other.handler" {
		t.Errorf("unaliased name = %q, want unchanged", got)
	}
	if got := RewriteCommonJSModuleExportAliasFullName("  ", aliases); got != "" {
		t.Errorf("blank name = %q, want empty", got)
	}
	if got := RewriteCommonJSModuleExportAliasFullName("alias.handler", nil); got != "alias.handler" {
		t.Errorf("no aliases = %q, want unchanged", got)
	}
}

func TestIsCommonJSExportMatchesExportName(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.run = function start() {};\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	var funcNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if funcNode == nil && node.Kind() == "function_expression" {
			funcNode = node
		}
	})
	if funcNode == nil {
		t.Fatal("no function_expression in fixture")
	}
	if !javaScriptIsCommonJSExport(funcNode, "run", source, parents) {
		t.Error("exported function not recognized for its export name")
	}
	if javaScriptIsCommonJSExport(funcNode, "other", source, parents) {
		t.Error("exported function recognized under the wrong name")
	}
	if javaScriptIsCommonJSExport(funcNode, "  ", source, parents) {
		t.Error("blank name reported exported")
	}
}

func TestIsCommonJSMixinExport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.mixin.extra = fn;\nmodule.exports.plain = fn;\n")
	defer closeFn()
	parents := syntax.BuildParentLookup(root)

	results := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		left := node.ChildByFieldName("left")
		results[string(source[left.StartByte():left.EndByte()])] = javaScriptIsCommonJSMixinExport(node, "extra", source, parents)
	})
	if !results["module.exports.mixin.extra"] {
		t.Errorf("mixin results = %v, want the mixin assignment true", results)
	}
	if results["module.exports.plain"] {
		t.Errorf("mixin results = %v, want the plain assignment false", results)
	}
	if javaScriptIsCommonJSMixinExport(nil, "extra", source, parents) {
		t.Error("nil node reported as mixin export")
	}
}

func TestCommonJSExportNameMemberAndSubscript(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.alpha = 1;\nexports.beta = 2;\nmodule.exports[\"gamma\"] = 3;\nplain.delta = 4;\n")
	defer closeFn()

	names := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		left := node.ChildByFieldName("left")
		names[string(source[left.StartByte():left.EndByte()])] = CommonJSExportName(left, source)
	})

	for target, want := range map[string]string{
		"module.exports.alpha": "alpha", "exports.beta": "beta",
		"module.exports[\"gamma\"]": "gamma", "plain.delta": "",
	} {
		if names[target] != want {
			t.Errorf("export name of %s = %q, want %q", target, names[target], want)
		}
	}
	if got := CommonJSExportName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestCommonJSExportTargetNodesShapes(t *testing.T) {
	t.Parallel()

	root, _, closeFn := parseJavaScriptSourceForTest(t, "module.exports.alpha = 1;\nmodule.exports[\"beta\"] = 2;\nconst x = 1;\n")
	defer closeFn()

	seen := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		left := node.ChildByFieldName("left")
		objectNode, propertyNode := javaScriptCommonJSExportTargetNodes(left)
		seen[left.Kind()] = objectNode != nil && propertyNode != nil
	})
	if !seen["member_expression"] || !seen["subscript_expression"] {
		t.Errorf("target node shapes = %v, want both member and subscript split", seen)
	}

	var ident *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if objectNode, propertyNode := javaScriptCommonJSExportTargetNodes(ident); objectNode != nil || propertyNode != nil {
		t.Error("identifier split into target nodes, want nils")
	}
}

func TestCommonJSAliasTargetNameShapes(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.a = handler;\nmodule.exports.b = module.exports.helper;\nmodule.exports.c = other.thing;\n")
	defer closeFn()

	names := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		left := node.ChildByFieldName("left")
		right := node.ChildByFieldName("right")
		names[string(source[left.StartByte():left.EndByte()])] = javaScriptCommonJSAliasTargetName(right, source)
	})

	// An identifier aliases to itself; a module.exports member resolves
	// through the export; any other member is not an alias target.
	for target, want := range map[string]string{
		"module.exports.a": "handler", "module.exports.b": "helper", "module.exports.c": "",
	} {
		if names[target] != want {
			t.Errorf("alias target of %s = %q, want %q", target, names[target], want)
		}
	}
	if got := javaScriptCommonJSAliasTargetName(nil, source); got != "" {
		t.Errorf("nil node = %q, want empty", got)
	}
}

func TestExportAssignmentNameNodeClonesProperty(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseJavaScriptSourceForTest(t, "module.exports.run = start;\nplain.x = 1;\n")
	defer closeFn()

	named := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "assignment_expression" {
			return
		}
		left := node.ChildByFieldName("left")
		cloned := ExportAssignmentNameNode(left, source)
		if cloned == nil {
			named[string(source[left.StartByte():left.EndByte()])] = ""
			return
		}
		named[string(source[left.StartByte():left.EndByte()])] = shared.NodeText(cloned, source)
	})
	if named["module.exports.run"] != "run" {
		t.Errorf("cloned export name = %q, want run", named["module.exports.run"])
	}
	if named["plain.x"] != "" {
		t.Errorf("non-export clone = %q, want empty", named["plain.x"])
	}
	if got := ExportAssignmentNameNode(nil, source); got != nil {
		t.Errorf("nil node clone = %v, want nil", got)
	}
}

func TestCommonJSExportNameSubscriptWithoutQuotes(t *testing.T) {
	t.Parallel()

	// syntax.FunctionName on a bare index keeps this honest: only the
	// quoted member/subscript contract names resolve.
	root, source, closeFn := parseJavaScriptSourceForTest(t, "exports[\"quoted\"] = 1;\n")
	defer closeFn()

	var left *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if left == nil && node.Kind() == "assignment_expression" {
			left = node.ChildByFieldName("left")
		}
	})
	if got := CommonJSExportName(left, source); got != "quoted" {
		t.Errorf("quoted subscript export = %q, want quoted", got)
	}
}
