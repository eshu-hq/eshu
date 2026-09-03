// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// writeTestFile writes body to path, creating any missing parent directories
// first. parsertest.WriteFile alone cannot create the nested
// `src/Worker/Worker.csproj` fixture these tests use, so this mirrors the
// parent parser suite's own writeTestFile (which calls ensureParentDirectory)
// and the same wrapper the relocated python package keeps for the same reason.
func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	parsertest.WriteFile(t, path, body)
}

// assertBoolFieldValue requires item[key] to hold a bool equal to want, and
// fails closed when the field is absent or carries any other type.
//
// parsertest exports string, int, and string-slice field assertions but no
// bool form. Its AGENTS.md admits a new shared helper only once two external
// parser test packages need the same assertion, and this is currently the only
// one, so the helper stays local. It is a verbatim copy of the root parser
// suite's own assertBoolFieldValue (engine_tsx_advanced_semantics_test.go),
// which these tests used before they moved here, so the failure text and the
// fail-closed type check are unchanged.
func assertBoolFieldValue(t *testing.T, item map[string]any, key string, want bool) {
	t.Helper()

	got, ok := item[key].(bool)
	if !ok {
		t.Fatalf("%s = %T, want bool", key, item[key])
	}
	if got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}
