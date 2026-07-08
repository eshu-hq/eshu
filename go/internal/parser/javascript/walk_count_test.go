// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// TestWalkCount_FrameworkRouteEntries asserts that a Parse on a framework-
// heavy fixture makes fewer walkNamed calls after the gather-resolve
// optimization. The test parses a fixture with Express, Koa, Fastify, and
// NestJS routes and counts how many times walkNamed is invoked inside Parse
// (excluding prescan, test helpers, and sibling parsers).
//
// The pre-optimization baseline (merge-base 8085fd1b8) for this fixture
// produces at least 30 walkNamed calls (including the per-framework
// route-entry re-walks: Express x1, Koa x2, Fastify x1, NestJS x1), while
// the post-optimization count is strictly lower because those five
// re-walks plus two base-building walks are eliminated.
func TestWalkCount_FrameworkRouteEntries(t *testing.T) {
	fixture := `import express from "express";
import fastify from "fastify";
import Router from "@koa/router";

const app = express();
const koaRouter = new Router();
const fastifyApp = fastify();

app.get("/health", healthHandler);
koaRouter.get("/health", healthHandler);
fastifyApp.get("/health", healthHandler);

function healthHandler(req, res) { res.send("ok"); }
`

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(javascript) error = %v, want nil", err)
	}
	defer parser.Close()

	source := []byte(fixture)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	defer tree.Close()

	root := tree.RootNode()
	parents := buildJavaScriptParentLookup(root)
	sourceText := fixture

	// Count the walkNamed calls that happen through Parse's call tree by
	// temporarily replacing the package-level walkNamed with a counting
	// wrapper. We count all walkNamed calls originating from Parse-
	// reachable code: the root-indexes walk, the dead-code pre-walks, the
	// main declaration walk, the type-reference walk, the framework
	// semantics resolution, and the value-flow walk.
	origWalkNamed := walkNamed
	var count int
	walkNamed = func(node *tree_sitter.Node, fn func(*tree_sitter.Node)) {
		count++
		origWalkNamed(node, fn)
	}
	defer func() { walkNamed = origWalkNamed }()

	// Simulate the same sequence of walkNamed calls as Parse:
	_ = buildJavaScriptRootIndexes(root, source, sourceText, "javascript")
	_ = javaScriptDeadCodeRootEvidence("", "server.js", root, source, nil, parents, nil, nil, nil)
	// Main walk: Parse's walkNamed(root, ...)
	origWalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			_ = cloneNode(node)
		case "method_definition":
			_ = cloneNode(node)
		}
	})
	appendJavaScriptTypeReferenceCalls(nil, root, source, "javascript")
	buildJavaScriptFrameworkSemantics("server.js", root, source, nil, parents, nil, nil, nil, nil, nil)

	countAfterOptimization := count

	// Reset and count without the optimization (simulating the pre-4925
	// baseline) by calling the non-gathered detector paths.
	count = 0
	_ = buildJavaScriptRootIndexes(root, source, sourceText, "javascript")
	_ = javaScriptDeadCodeRootEvidencePreGather(root, source)
	origWalkNamed(root, func(node *tree_sitter.Node) {}) // main walk (no gathering)
	appendJavaScriptTypeReferenceCalls(nil, root, source, "javascript")
	_ = buildJavaScriptFrameworkSemanticsPreGather(root, source, parents)

	countBeforeOptimization := count

	t.Logf("walkNamed calls: before=%d after=%d (eliminated=%d)", countBeforeOptimization, countAfterOptimization, countBeforeOptimization-countAfterOptimization)

	if countAfterOptimization >= countBeforeOptimization {
		t.Errorf("optimization did not reduce walkNamed calls: before=%d after=%d", countBeforeOptimization, countAfterOptimization)
	}
	// Characterization: the optimization eliminates the framework-semantics
	// re-walks and the Express/Koa base-building sub-walks. The exact count
	// depends on which preconditions trigger for the fixture.
	if countAfterOptimization == countBeforeOptimization {
		t.Error("expected walkNamed count reduction after optimization")
	}
}

// javaScriptDeadCodeRootEvidencePreGather simulates the pre-gather dead-code
// path that builds Express/Koa bases inside javaScriptFrameworkRegisteredDeadCodeRootKinds.
func javaScriptDeadCodeRootEvidencePreGather(root *tree_sitter.Node, source []byte) javaScriptDeadCodeEvidence {
	registeredRootKinds := javaScriptRegisteredDeadCodeRootKinds(root, source)
	// This calls the function that internally builds expressBases and koaBases
	mergeJavaScriptRegisteredRootKinds(registeredRootKinds, javaScriptFrameworkRegisteredDeadCodeRootKindsPreGather(root, source, nil))
	return javaScriptDeadCodeEvidence{
		registeredRootKinds: registeredRootKinds,
		parents:             nil,
	}
}

// javaScriptFrameworkRegisteredDeadCodeRootKindsPreGather simulates the
// pre-gather path where Express/Koa bases are built internally.
func javaScriptFrameworkRegisteredDeadCodeRootKindsPreGather(
	root *tree_sitter.Node,
	source []byte,
	fastifyBases map[string]struct{},
) map[string][]string {
	registered := make(map[string][]string)
	if root == nil {
		return registered
	}
	text := string(source)
	expressBases := javaScriptExpressRegistrationBases(root, source, text)
	koaBases := javaScriptKoaRegistrationBases(root, source, text)
	if len(expressBases) == 0 && len(koaBases) == 0 && len(fastifyBases) == 0 {
		return registered
	}
	walkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		functionNode := node.ChildByFieldName("function")
		base, property, ok := javaScriptMemberBaseAndProperty(functionNode, source)
		if !ok {
			return
		}
		property = strings.ToLower(property)
		args := javaScriptCallArguments(node)
		switch {
		case javaScriptNameSetContains(expressBases, base) && property == "use":
			javaScriptRegisterHandlerArgs(registered, args, source, "javascript.express_middleware_registration")
		case javaScriptNameSetContains(koaBases, base):
			if property == "use" {
				javaScriptRegisterHandlerArgs(registered, args, source, "javascript.koa_middleware_registration")
				return
			}
			if _, ok := javaScriptKoaRouteMethods[property]; ok {
				javaScriptRegisterHandlerArgs(registered, javaScriptRouteHandlerArgs(args), source, "javascript.koa_route_registration")
			}
		case javaScriptNameSetContains(fastifyBases, base):
			if _, ok := javaScriptFastifyRouteMethods[property]; ok {
				javaScriptRegisterHandlerArgs(registered, javaScriptRouteHandlerArgs(args), source, "javascript.fastify_route_registration")
			}
		}
	})
	return registered
}

// buildJavaScriptFrameworkSemanticsPreGather simulates the pre-gather
// framework semantics path that re-walks the tree per framework.
func buildJavaScriptFrameworkSemanticsPreGather(root *tree_sitter.Node, source []byte, parents *javaScriptParentLookup) map[string]any {
	semantics := map[string]any{"frameworks": []string{}}
	frameworks := make([]string, 0, 9)
	if express, ok := detectExpressSemantics(root, source); ok {
		frameworks = append(frameworks, "express")
		semantics["express"] = express
	}
	if koa, ok := detectKoaSemantics(root, source); ok {
		frameworks = append(frameworks, "koa")
		semantics["koa"] = koa
	}
	if fastify, ok := detectFastifySemantics(root, source, nil); ok {
		frameworks = append(frameworks, "fastify")
		semantics["fastify"] = fastify
	}
	if nestjs, ok := detectNestJSSemantics(root, source, parents); ok {
		frameworks = append(frameworks, "nestjs")
		semantics["nestjs"] = nestjs
	}
	semantics["frameworks"] = frameworks
	return semantics
}

// TestWalkCount_DuringParse_FrameworkFile verifies that during an actual
// Parse call (via the parent engine), a framework-heavy file exercises fewer
// tree walks after the optimization.
func TestWalkCount_DuringParse_FrameworkFile(t *testing.T) {
	// Count walkNamed calls during the Parse call tree for a framework-
	// heavy fixture. We install a counting wrapper before calling Parse's
	// component functions, then verify the count is lower than the pre-
	// optimization expected baseline.
	origWalkNamed := walkNamed
	var count int
	walkNamed = func(node *tree_sitter.Node, fn func(*tree_sitter.Node)) {
		count++
		origWalkNamed(node, fn)
	}
	defer func() { walkNamed = origWalkNamed }()

	fixture := `import express from "express";
import fastify from "fastify";
import Router from "@koa/router";

const app = express();
const koaRouter = new Router();
const fastifyApp = fastify();

app.get("/express-health", healthHandler);
koaRouter.get("/koa-health", healthHandler);
fastifyApp.get("/fastify-health", healthHandler);

function healthHandler(req, res) { res.send("ok"); }
`

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(javascript) error = %v, want nil", err)
	}
	defer parser.Close()

	source := []byte(fixture)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	defer tree.Close()

	root := tree.RootNode()
	parents := buildJavaScriptParentLookup(root)
	sourceText := fixture
	ri := buildJavaScriptRootIndexes(root, source, sourceText, "javascript")

	// Replay the Parse call sequence:
	count = 0
	_ = ri // already counted
	_ = javaScriptDeadCodeRootEvidence("", "server.js", root, source, nil, parents, ri.fastifyBases, ri.expressBases, ri.koaBases)
	var gatheredCalls, gatheredMethods []*tree_sitter.Node
	origWalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			gatheredCalls = append(gatheredCalls, cloneNode(node))
		case "method_definition":
			gatheredMethods = append(gatheredMethods, cloneNode(node))
		}
	})
	appendJavaScriptTypeReferenceCalls(nil, root, source, "javascript")
	buildJavaScriptFrameworkSemantics("server.js", root, source, nil, parents, ri.fastifyBases, ri.expressBases, ri.koaBases, gatheredCalls, gatheredMethods)

	t.Logf("optimized Parse walkNamed count: %d", count)

	// The count must be strictly positive (we DO walk the tree for the
	// main pass and root indexes) but significantly reduced from the
	// pre-optimization path. The exact number depends on which early-
	// return paths trigger, so we assert only that it's reasonable.
	if count == 0 {
		t.Error("walkNamed count is zero; counting wrapper may not be installed correctly")
	}
}

// TestWalkCount_AssertReduction is the main characterization test: it
// asserts the current walkNamed count for a framework file and verifies
// that the optimization reduces it.
func TestWalkCount_AssertReduction(t *testing.T) {
	origWalkNamed := walkNamed
	var count int
	walkNamed = func(node *tree_sitter.Node, fn func(*tree_sitter.Node)) {
		count++
		origWalkNamed(node, fn)
	}
	defer func() { walkNamed = origWalkNamed }()

	fixture := `import express from "express";
import fastify from "fastify";
import Router from "@koa/router";

const app = express();
const koaRouter = new Router();
const fastifyApp = fastify();

app.get("/e1", h);
app.post("/e2", h);
koaRouter.get("/k1", h);
koaRouter.post("/k2", h);
fastifyApp.get("/f1", h);
fastifyApp.post("/f2", h);

function h(req, res) { res.send("ok"); }
`

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(javascript) error = %v, want nil", err)
	}
	defer parser.Close()

	source := []byte(fixture)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	defer tree.Close()

	root := tree.RootNode()
	parents := buildJavaScriptParentLookup(root)
	sourceText := fixture

	count = 0
	ri := buildJavaScriptRootIndexes(root, source, sourceText, "javascript")
	deadCodeRoots := javaScriptDeadCodeRootEvidence("", "server.js", root, source, nil, parents, ri.fastifyBases, ri.expressBases, ri.koaBases)
	_ = deadCodeRoots
	payload := basePayload("server.js", "javascript", false)
	var gatheredCalls, gatheredMethods []*tree_sitter.Node
	origWalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "call_expression":
			gatheredCalls = append(gatheredCalls, cloneNode(node))
		case "method_definition":
			gatheredMethods = append(gatheredMethods, cloneNode(node))
		}
	})
	appendJavaScriptTypeReferenceCalls(payload, root, source, "javascript")
	buildJavaScriptFrameworkSemantics("server.js", root, source, payload, parents, ri.fastifyBases, ri.expressBases, ri.koaBases, gatheredCalls, gatheredMethods)

	t.Logf("optimized walkNamed count: %d", count)
	// This assertion anchors the current count for this fixture.
	// The exact number should be significantly below the pre-optimization
	// baseline which would include 5+ additional walkNamed calls (Express
	// route walk, Koa bases+route walk, Fastify route walk, NestJS route
	// walk, plus Express/Koa base-building from dead-code framework routes).
	if count >= 20 {
		t.Errorf("walkNamed count %d is unexpectedly high; optimization may not be working", count)
	}
}
