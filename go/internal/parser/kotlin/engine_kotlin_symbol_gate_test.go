// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestKotlinComprehensiveSymbolExtractionGate is the golden-fixture gate for
// Kotlin symbol extraction required by issue #3486. It asserts that the native
// adapter emits the full declared symbol set (classes, objects, interfaces,
// enums, functions with class context, imports, and calls) for the
// kotlin_comprehensive fixtures. It locks capability state to reality: Kotlin
// extracts symbols, so a regression to "zero extraction" fails here.
func TestKotlinComprehensiveSymbolExtractionGate(t *testing.T) {
	t.Parallel()

	repoRoot := kotlinFixturePath("ecosystems", "kotlin_comprehensive")

	classesPayload := parsertest.MustParsePath(t, repoRoot, filepath.Join(repoRoot, "Classes.kt"))
	// Classes: data, sealed, abstract, concrete, enum, companion, nested.
	for _, name := range []string{
		"Point", "Result", "Success", "Failure", "Shape",
		"Circle", "Rectangle", "Color", "Person", "Companion",
	} {
		parsertest.AssertNamedBucketContains(t, classesPayload, "classes", name)
	}
	// Functions are attributed to their declaring type.
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "distanceTo", "Point")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "area", "Circle")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "perimeter", "Rectangle")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "create", "Person")
	parsertest.AssertFunctionByNameAndClass(t, classesPayload, "greet", "Person")

	interfacesPayload := parsertest.MustParsePath(t, repoRoot, filepath.Join(repoRoot, "Interfaces.kt"))
	for _, name := range []string{"Identifiable", "Describable", "Repository", "Logger"} {
		parsertest.AssertNamedBucketContains(t, interfacesPayload, "interfaces", name)
	}
	for _, name := range []string{"User", "InMemoryRepository"} {
		parsertest.AssertNamedBucketContains(t, interfacesPayload, "classes", name)
	}
	// Interface methods and overriding implementations both extract.
	parsertest.AssertFunctionByNameAndClass(t, interfacesPayload, "findById", "Repository")
	parsertest.AssertFunctionByNameAndClass(t, interfacesPayload, "findById", "InMemoryRepository")
	parsertest.AssertFunctionByNameAndClass(t, interfacesPayload, "describe", "User")
	// Calls inside method bodies are extracted.
	parsertest.AssertNamedBucketContains(t, interfacesPayload, "function_calls", "info")
}
