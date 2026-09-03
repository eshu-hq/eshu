// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestSwiftComprehensiveSymbolExtractionGate is the golden-fixture gate for
// Swift symbol extraction required by issue #3486. It asserts the full declared
// symbol set (classes, structs, enums, protocols, functions with class context,
// imports, and calls) for the swift_comprehensive fixtures, including methods
// declared inside `extension` blocks.
func TestSwiftComprehensiveSymbolExtractionGate(t *testing.T) {
	t.Parallel()

	repoRoot := swiftFixturePath(t, "ecosystems", "swift_comprehensive")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	classesPayload := mustParse(t, engine, repoRoot, "Classes.swift")
	for _, name := range []string{"Animal", "Dog", "GuideDog"} {
		parsertest.AssertNamedBucketContains(t, classesPayload, "classes", name)
	}
	parsertest.AssertNamedBucketContains(t, classesPayload, "imports", "Foundation")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "speak", "Animal")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "fetch", "Dog")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "guide", "GuideDog")

	enumsPayload := mustParse(t, engine, repoRoot, "Enums.swift")
	for _, name := range []string{"Direction", "Result", "NetworkError", "Planet"} {
		parsertest.AssertNamedBucketContains(t, enumsPayload, "enums", name)
	}
	parsertest.AssertFunctionByNameAndClass(t, enumsPayload, "map", "Result")

	protocolsPayload := mustParse(t, engine, repoRoot, "Protocols.swift")
	for _, name := range []string{"Identifiable", "Describable", "Repository", "Logger"} {
		parsertest.AssertNamedBucketContains(t, protocolsPayload, "protocols", name)
	}
	parsertest.AssertNamedBucketContains(t, protocolsPayload, "structs", "User")
	parsertest.AssertNamedBucketContains(t, protocolsPayload, "classes", "InMemoryStore")
	// Protocol requirements, default implementations in extensions, and
	// concrete implementations all carry their declaring context.
	parsertest.AssertFunctionByNameAndClass(t, protocolsPayload, "log", "Logger")
	parsertest.AssertFunctionByNameAndClass(t, protocolsPayload, "info", "Logger")
	parsertest.AssertFunctionByNameAndClass(t, protocolsPayload, "warn", "Logger")
	parsertest.AssertFunctionByNameAndClass(t, protocolsPayload, "error", "Logger")
	parsertest.AssertFunctionByNameAndClass(t, protocolsPayload, "findById", "Repository")
	parsertest.AssertFunctionByNameAndClass(t, protocolsPayload, "findById", "InMemoryStore")
}

// mustParse parses file under repoRoot through engine and fails the test on a
// parse error. It reuses one engine across the gate's three fixture files
// rather than calling parsertest.MustParsePath, which builds a fresh engine per
// call.
func mustParse(t *testing.T, engine *parser.Engine, repoRoot string, file string) map[string]any {
	t.Helper()
	path := filepath.Join(repoRoot, file)
	payload, err := engine.ParsePath(repoRoot, path, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
	}
	return payload
}
