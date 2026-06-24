// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestKotlinComprehensiveSymbolExtractionGate is the golden-fixture gate for
// Kotlin symbol extraction required by issue #3486. It asserts that the native
// adapter emits the full declared symbol set (classes, objects, interfaces,
// enums, functions with class context, imports, and calls) for the
// kotlin_comprehensive fixtures. It locks capability state to reality: Kotlin
// extracts symbols, so a regression to "zero extraction" fails here.
func TestKotlinComprehensiveSymbolExtractionGate(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "kotlin_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	classesPayload := mustParse(t, engine, repoRoot, "Classes.kt")
	// Classes: data, sealed, abstract, concrete, enum, companion, nested.
	for _, name := range []string{
		"Point", "Result", "Success", "Failure", "Shape",
		"Circle", "Rectangle", "Color", "Person", "Companion",
	} {
		assertNamedBucketContains(t, classesPayload, "classes", name)
	}
	// Functions are attributed to their declaring type.
	assertFunctionByNameAndClass(t, classesPayload, "distanceTo", "Point")
	assertFunctionByNameAndClass(t, classesPayload, "area", "Circle")
	assertFunctionByNameAndClass(t, classesPayload, "perimeter", "Rectangle")
	assertFunctionByNameAndClass(t, classesPayload, "create", "Person")
	assertFunctionByNameAndClass(t, classesPayload, "greet", "Person")

	interfacesPayload := mustParse(t, engine, repoRoot, "Interfaces.kt")
	for _, name := range []string{"Identifiable", "Describable", "Repository", "Logger"} {
		assertNamedBucketContains(t, interfacesPayload, "interfaces", name)
	}
	for _, name := range []string{"User", "InMemoryRepository"} {
		assertNamedBucketContains(t, interfacesPayload, "classes", name)
	}
	// Interface methods and overriding implementations both extract.
	assertFunctionByNameAndClass(t, interfacesPayload, "findById", "Repository")
	assertFunctionByNameAndClass(t, interfacesPayload, "findById", "InMemoryRepository")
	assertFunctionByNameAndClass(t, interfacesPayload, "describe", "User")
	// Calls inside method bodies are extracted.
	assertNamedBucketContains(t, interfacesPayload, "function_calls", "info")
}

// TestSwiftComprehensiveSymbolExtractionGate is the golden-fixture gate for
// Swift symbol extraction required by issue #3486. It asserts the full declared
// symbol set (classes, structs, enums, protocols, functions with class context,
// imports, and calls) for the swift_comprehensive fixtures, including methods
// declared inside `extension` blocks.
func TestSwiftComprehensiveSymbolExtractionGate(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "swift_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	classesPayload := mustParse(t, engine, repoRoot, "Classes.swift")
	for _, name := range []string{"Animal", "Dog", "GuideDog"} {
		assertNamedBucketContains(t, classesPayload, "classes", name)
	}
	assertNamedBucketContains(t, classesPayload, "imports", "Foundation")
	assertFunctionByNameAndClass(t, classesPayload, "speak", "Animal")
	assertFunctionByNameAndClass(t, classesPayload, "fetch", "Dog")
	assertFunctionByNameAndClass(t, classesPayload, "guide", "GuideDog")

	enumsPayload := mustParse(t, engine, repoRoot, "Enums.swift")
	for _, name := range []string{"Direction", "Result", "NetworkError", "Planet"} {
		assertNamedBucketContains(t, enumsPayload, "enums", name)
	}
	assertFunctionByNameAndClass(t, enumsPayload, "map", "Result")

	protocolsPayload := mustParse(t, engine, repoRoot, "Protocols.swift")
	for _, name := range []string{"Identifiable", "Describable", "Repository", "Logger"} {
		assertNamedBucketContains(t, protocolsPayload, "protocols", name)
	}
	assertNamedBucketContains(t, protocolsPayload, "structs", "User")
	assertNamedBucketContains(t, protocolsPayload, "classes", "InMemoryStore")
	// Protocol requirements, default implementations in extensions, and
	// concrete implementations all carry their declaring context.
	assertFunctionByNameAndClass(t, protocolsPayload, "log", "Logger")
	assertFunctionByNameAndClass(t, protocolsPayload, "info", "Logger")
	assertFunctionByNameAndClass(t, protocolsPayload, "warn", "Logger")
	assertFunctionByNameAndClass(t, protocolsPayload, "error", "Logger")
	assertFunctionByNameAndClass(t, protocolsPayload, "findById", "Repository")
	assertFunctionByNameAndClass(t, protocolsPayload, "findById", "InMemoryStore")
}

func mustParse(t *testing.T, engine *Engine, repoRoot string, file string) map[string]any {
	t.Helper()
	path := filepath.Join(repoRoot, file)
	payload, err := engine.ParsePath(repoRoot, path, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", path, err)
	}
	return payload
}
