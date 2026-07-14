// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package c

// -----------------------------------------------------------------------
// Characterization tests for gather-then-resolve refactor (issue #4870,
// following the pattern established for C++ in #4924/#4927).
//
// These tests pin the CURRENT annotateCDeadCodeRoots output (including
// dead_code_root_kinds slice ORDER, which is load-bearing) before the
// walk-merge change, and must stay green, byte-identical, after it.
// -----------------------------------------------------------------------

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
)

func stringOrEmpty(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// deadCodeRootKindsFromPayload returns a map from function name to
// dead_code_root_kinds slice, collected from the parsed payload.
func deadCodeRootKindsFromPayload(payload map[string]any) map[string][]string {
	result := make(map[string][]string)
	functions, _ := payload["functions"].([]map[string]any)
	for _, f := range functions {
		name := strings.TrimSpace(stringOrEmpty(f, "name"))
		if name == "" {
			continue
		}
		switch kinds := f["dead_code_root_kinds"].(type) {
		case []string:
			result[name] = append(result[name], kinds...)
		case []any:
			for _, k := range kinds {
				if s, ok := k.(string); ok {
					result[name] = append(result[name], s)
				}
			}
		default:
			if _, exists := result[name]; !exists {
				result[name] = nil
			}
		}
	}
	return result
}

func parseCString(t *testing.T, source string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.c")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_c.Language())); err != nil {
		t.Fatalf("SetLanguage(c): %v", err)
	}
	defer parser.Close()
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return payload
}

// TestGatherResolveForwardReferenceSignalHandler verifies that a signal()
// registration naming a function defined LATER in the file still resolves.
// The functions map is built from payload["functions"], fully populated by
// the main walk before any dead-code-root resolution runs, so forward
// references are safe regardless of whether resolution runs from a second
// full-tree walk or a pre-gathered node slice.
func TestGatherResolveForwardReferenceSignalHandler(t *testing.T) {
	t.Parallel()
	source := `#include <signal.h>

void early(void) {
    signal(2, late_handler);
}

void late_handler(int signum) {
}
`
	payload := parseCString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	kinds, ok := rootKinds["late_handler"]
	if !ok {
		t.Fatal("late_handler not found in payload functions")
	}
	if !slices.Contains(kinds, cSignalHandlerRoot) {
		t.Errorf("late_handler should have c.signal_handler root, got %v", kinds)
	}
}

// TestGatherResolveCrossKindDeclBeforeCall verifies that when a declaration
// (function-pointer target) appears BEFORE a call expression (callback arg)
// in source, the dead_code_root_kinds ordering is
// [c.function_pointer_target, c.callback_argument_target]. A single
// interleaved pre-order resolution pass preserves the original two-walk
// visitation order; separate per-kind grouped loops would reverse this for
// the opposite source layout (see the paired test below), matching the bug
// class fixed for C++ in #4844.
func TestGatherResolveCrossKindDeclBeforeCall(t *testing.T) {
	t.Parallel()
	source := `void foo(void) {}
typedef void (*Handler)(void);
Handler ptr = foo;        /* EARLY declaration -> function_pointer_target */
void reg(void (*cb)(void));
void setup(void) { reg(foo); } /* LATE call -> callback_argument_target */
`
	payload := parseCString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	kinds, ok := rootKinds["foo"]
	if !ok {
		t.Fatal("foo not found in payload functions")
	}
	wantOrder := []string{cFunctionPointerTargetRoot, cCallbackArgumentTarget}
	if !slices.Equal(kinds, wantOrder) {
		t.Errorf("decl-before-call ordering mismatch:\n  got:  %v\n  want: %v", kinds, wantOrder)
	}
}

// TestGatherResolveCrossKindCallBeforeDecl is the mirror of the test above:
// when the call expression appears BEFORE the declaration, the ordering must
// be [c.callback_argument_target, c.function_pointer_target].
func TestGatherResolveCrossKindCallBeforeDecl(t *testing.T) {
	t.Parallel()
	source := `void foo(void) {}
void reg(void (*cb)(void));
void setup(void) { reg(foo); } /* EARLY call -> callback_argument_target */
typedef void (*Handler)(void);
Handler ptr = foo;          /* LATE declaration -> function_pointer_target */
`
	payload := parseCString(t, source)
	rootKinds := deadCodeRootKindsFromPayload(payload)

	kinds, ok := rootKinds["foo"]
	if !ok {
		t.Fatal("foo not found in payload functions")
	}
	wantOrder := []string{cCallbackArgumentTarget, cFunctionPointerTargetRoot}
	if !slices.Equal(kinds, wantOrder) {
		t.Errorf("call-before-decl ordering mismatch:\n  got:  %v\n  want: %v", kinds, wantOrder)
	}
}
