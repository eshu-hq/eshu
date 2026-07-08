// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

// fastifyParseSource parses source as JavaScript and returns the root node,
// the raw source bytes, and the rootIndexes built from the same tree.
func fastifyParseSource(t *testing.T, source string) (*tree_sitter.Node, []byte, javaScriptRootIndexes) {
	t.Helper()
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_javascript.Language())); err != nil {
		parser.Close()
		t.Fatalf("SetLanguage(javascript) error = %v, want nil", err)
	}
	sourceBytes := []byte(source)
	tree := parser.Parse(sourceBytes, nil)
	if tree == nil {
		parser.Close()
		t.Fatal("Parse returned nil tree")
	}
	t.Cleanup(func() {
		tree.Close()
		parser.Close()
	})
	root := tree.RootNode()
	ri := buildJavaScriptRootIndexes(root, sourceBytes, source, "javascript")
	return root, sourceBytes, ri
}

// fastifyStandaloneBases is the reference standalone computation of fastify
// registration bases. It must match rootIndexes.fastifyBases byte-for-byte.
func fastifyStandaloneBases(root *tree_sitter.Node, source []byte) map[string]struct{} {
	return javaScriptFastifyRegistrationBases(root, source, string(source))
}

// fastifyBasesEqual reports whether two fastify base sets are equal.
func fastifyBasesEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// TestFastifyThreading_BasesMatchStandalone asserts that the precomputed
// fastifyBases inside buildJavaScriptRootIndexes is byte-identical to the
// standalone javaScriptFastifyRegistrationBases computation.
func TestFastifyThreading_BasesMatchStandalone(t *testing.T) {
	root, source, ri := fastifyParseSource(t, `import fastify from "fastify";
const app = fastify();
app.get("/health", handler);
function handler(req, reply) { reply.send("ok"); }
`)
	standalone := fastifyStandaloneBases(root, source)

	if !fastifyBasesEqual(ri.fastifyBases, standalone) {
		t.Errorf("rootIndexes.fastifyBases does not match standalone:\n  precomputed: %v\n  standalone:  %v", ri.fastifyBases, standalone)
	}
}

// TestFastifyThreading_UsedBeforeDeclaration verifies that a fastify base variable
// used in a route call BEFORE its declaration in source order is still detected
// correctly by both standalone and threaded paths, and that threaded and standalone
// produce identical dead-code root kinds for the handler.
func TestFastifyThreading_UsedBeforeDeclaration(t *testing.T) {
	// app.get("/health", healthHandler) is hoisted: it calls fastifyBase.get()
	// before `const fastifyBase = fastify()` appears. Both standalone and threaded
	// must resolve fastifyBase as a fastify instance.
	source := `import fastify from "fastify";
app.get("/health", healthHandler);
const app = fastify();
function healthHandler(req, reply) { reply.send("ok"); }
`
	root, sourceBytes, ri := fastifyParseSource(t, source)
	standalone := fastifyStandaloneBases(root, sourceBytes)

	if !fastifyBasesEqual(ri.fastifyBases, standalone) {
		t.Fatalf("rootIndexes.fastifyBases does not match standalone:\n  precomputed: %v\n  standalone:  %v", ri.fastifyBases, standalone)
	}

	// Verify that the standalone and threaded paths produce identical
	// framework-registered dead-code root kinds.
	deadCodeFromStandalone := javaScriptFrameworkRegisteredDeadCodeRootKinds(root, sourceBytes, standalone, ri.expressBases, ri.koaBases)
	deadCodeFromThreaded := javaScriptFrameworkRegisteredDeadCodeRootKinds(root, sourceBytes, ri.fastifyBases, ri.expressBases, ri.koaBases)

	if got, want := len(deadCodeFromThreaded), len(deadCodeFromStandalone); got != want {
		t.Errorf("deadCodeRootKinds count mismatch: threaded=%d standalone=%d", got, want)
	}
	for k, v := range deadCodeFromStandalone {
		if !stringSlicesEqual(deadCodeFromThreaded[k], v) {
			t.Errorf("deadCodeRootKinds[%q] mismatch:\n  threaded:   %v\n  standalone: %v", k, deadCodeFromThreaded[k], v)
		}
	}

	// Verify route semantics identical.
	threadedSem, threadedOK := detectFastifySemantics(root, sourceBytes, ri.fastifyBases)
	standaloneSem, standaloneOK := detectFastifySemantics(root, sourceBytes, standalone)
	if threadedOK != standaloneOK {
		t.Errorf("detectFastifySemantics ok mismatch: threaded=%v standalone=%v", threadedOK, standaloneOK)
	}
	if !fastifySemanticsEqual(threadedSem, standaloneSem) {
		t.Errorf("framework semantics mismatch:\n  threaded:   %v\n  standalone: %v", threadedSem, standaloneSem)
	}

	// NEGATIVE: passing wrong bases (nil) must change the output.
	wrongDeadCode := javaScriptFrameworkRegisteredDeadCodeRootKinds(root, sourceBytes, nil, nil, nil)
	if len(wrongDeadCode) == len(deadCodeFromStandalone) && len(wrongDeadCode) > 0 {
		t.Errorf("NEGATIVE: passing nil fastifyBases should NOT match standalone (got %d entries)", len(wrongDeadCode))
	}
	wrongSem, wrongOK := detectFastifySemantics(root, sourceBytes, nil)
	if wrongOK && len(standalone) > 0 {
		t.Errorf("NEGATIVE: detectFastifySemantics with nil bases should return ok=false")
		_ = wrongSem
	}
}

// TestFastifyThreading_AliasedInstance verifies that an aliased/reassigned
// fastify instance (const app = fastify(); const server = app;) is handled
// correctly: only the direct fastify() binding is a base.
func TestFastifyThreading_AliasedInstance(t *testing.T) {
	source := `import fastify from "fastify";
const app = fastify();
const server = app;
server.get("/health", handler);
function handler(req, reply) { reply.send("ok"); }
`
	root, sourceBytes, ri := fastifyParseSource(t, source)
	standalone := fastifyStandaloneBases(root, sourceBytes)

	if !fastifyBasesEqual(ri.fastifyBases, standalone) {
		t.Fatalf("rootIndexes.fastifyBases does not match standalone:\n  precomputed: %v\n  standalone:  %v", ri.fastifyBases, standalone)
	}

	// Only "app" should be a base, not "server".
	if _, hasServer := ri.fastifyBases["server"]; hasServer {
		t.Error("alias 'server' should NOT be a fastify base")
	}
	if _, hasApp := ri.fastifyBases["app"]; !hasApp {
		t.Error("'app' should be a fastify base")
	}

	// Route route_route on "server" should not be detected as a fastify route
	// since "server" is not a base.
	deadCode := javaScriptFrameworkRegisteredDeadCodeRootKinds(root, sourceBytes, ri.fastifyBases, ri.expressBases, ri.koaBases)
	if kinds := deadCode["handler"]; len(kinds) > 0 {
		t.Errorf("handler should not have route registration kinds via aliased server (got %v)", kinds)
	}
}

// TestFastifyThreading_NoFastifyImport verifies that a file without a fastify import
// produces empty bases, and both consumers early-return exactly as before.
func TestFastifyThreading_NoFastifyImport(t *testing.T) {
	source := `const app = require("express");
app.get("/health", handler);
function handler(req, reply) { reply.send("ok"); }
`
	root, sourceBytes, ri := fastifyParseSource(t, source)
	standalone := fastifyStandaloneBases(root, sourceBytes)

	if !fastifyBasesEqual(ri.fastifyBases, standalone) {
		t.Fatalf("rootIndexes.fastifyBases does not match standalone:\n  precomputed: %v\n  standalone:  %v", ri.fastifyBases, standalone)
	}
	if len(ri.fastifyBases) != 0 {
		t.Error("expected empty fastifyBases for file without fastify import")
	}

	// Both consumers should produce empty output.
	deadCode := javaScriptFrameworkRegisteredDeadCodeRootKinds(root, sourceBytes, ri.fastifyBases, ri.expressBases, ri.koaBases)
	if len(deadCode) != 0 {
		t.Errorf("expected empty deadCodeRootKinds, got %d entries", len(deadCode))
	}
	sem, ok := detectFastifySemantics(root, sourceBytes, ri.fastifyBases)
	if ok {
		t.Errorf("expected detectFastifySemantics ok=false, got %v", sem)
	}
}

// TestFastifyThreading_MultipleInstances verifies that multiple fastify instances
// are all collected as bases.
func TestFastifyThreading_MultipleInstances(t *testing.T) {
	source := `import fastify from "fastify";
const app = fastify();
const adminApp = fastify();
app.get("/health", healthHandler);
adminApp.get("/admin", adminHandler);
function healthHandler(req, reply) { reply.send("ok"); }
function adminHandler(req, reply) { reply.send("ok"); }
`
	root, sourceBytes, ri := fastifyParseSource(t, source)
	standalone := fastifyStandaloneBases(root, sourceBytes)

	if !fastifyBasesEqual(ri.fastifyBases, standalone) {
		t.Fatalf("rootIndexes.fastifyBases does not match standalone:\n  precomputed: %v\n  standalone:  %v", ri.fastifyBases, standalone)
	}

	if _, ok := ri.fastifyBases["app"]; !ok {
		t.Error("expected 'app' in fastifyBases")
	}
	if _, ok := ri.fastifyBases["adminapp"]; !ok {
		t.Error("expected 'adminapp' in fastifyBases")
	}

	// Dead code root kinds should have entries for both handlers.
	deadCode := javaScriptFrameworkRegisteredDeadCodeRootKinds(root, sourceBytes, ri.fastifyBases, ri.expressBases, ri.koaBases)
	if kinds, ok := deadCode["healthhandler"]; !ok || len(kinds) == 0 {
		t.Errorf("expected healthhandler in deadCodeRootKinds, got %v", kinds)
	}
	if kinds, ok := deadCode["adminhandler"]; !ok || len(kinds) == 0 {
		t.Errorf("expected adminhandler in deadCodeRootKinds, got %v", kinds)
	}

	// Route semantics should pick up both bases.
	sem, ok := detectFastifySemantics(root, sourceBytes, ri.fastifyBases)
	if !ok {
		t.Fatal("expected detectFastifySemantics ok=true")
	}
	if sem == nil {
		t.Fatal("expected non-nil semantics")
	}
}

// stringSlicesEqual reports whether a and b contain the same elements.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, c := range seen {
		if c != 0 {
			return false
		}
	}
	return true
}

// fastifySemanticsEqual does a shallow comparison of framework semantics maps.
func fastifySemanticsEqual(a, b map[string]any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		// Shallow compare for this characterization test; framework semantics
		// values are slices/maps that are constructed from the same input.
		if !fastifySemValueEqual(va, vb) {
			return false
		}
	}
	return true
}

func fastifySemValueEqual(a, b any) bool {
	switch av := a.(type) {
	case []string:
		bv, ok := b.([]string)
		if !ok {
			return false
		}
		return stringSlicesEqual(av, bv)
	case []map[string]string:
		bv, ok := b.([]map[string]string)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			for k, va := range av[i] {
				if bv[i][k] != va {
					return false
				}
			}
		}
		return true
	default:
		return true
	}
}
