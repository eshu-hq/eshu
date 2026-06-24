// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathSwiftExtensionMethodsCarryClassContext proves that
// methods declared inside a Swift `extension` block are attributed to the
// extended type. The tree-sitter grammar parses `extension Foo { ... }` as a
// class_declaration whose name lives in a user_type node, so a name lookup that
// only accepts type_identifier names drops the context and leaves these methods
// orphaned. Issue #3486 calls out extensions as a required edge case.
func TestDefaultEngineParsePathSwiftExtensionMethodsCarryClassContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "logger.swift")
	writeTestFile(
		t,
		filePath,
		`import Foundation

protocol Logger {
    func log(_ level: String)
}

extension Logger {
    func info(_ message: String) { log("INFO") }
    func warn(_ message: String) { log("WARN") }
}

struct Point {
    var x: Double
}

extension Point: Equatable {
    func translated() -> Point { return self }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	// Methods inside `extension Logger { ... }` must carry class_context=Logger.
	assertFunctionByNameAndClass(t, payload, "info", "Logger")
	assertFunctionByNameAndClass(t, payload, "warn", "Logger")
	// Method inside `extension Point: Equatable { ... }` must carry context Point.
	assertFunctionByNameAndClass(t, payload, "translated", "Point")
}
