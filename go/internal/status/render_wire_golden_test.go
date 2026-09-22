// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden rewrites the committed golden fixtures under testdata/ from
// live RenderJSON/RenderText/key-path output. Run
// `go test ./internal/status -run TestRenderWire -update-golden` after an
// intentional change to the operator-facing status wire shape.
var updateGolden = flag.Bool("update-golden", false, "rewrite the status render-wire golden files from live output")

const renderJSONGoldenPath = "testdata/render_json_golden.json"

// TestRenderWireJSON_ByteForByteGolden locks RenderJSON's output against a
// committed golden fixture byte-for-byte. It is the pre-#6775-nest lock: the
// package is about to be split into family sub-packages, and a moved json
// tag or a dropped section must fail this test loudly rather than shift a
// value silently past a value-only comparison.
func TestRenderWireJSON_ByteForByteGolden(t *testing.T) {
	t.Parallel()

	report := BuildReport(maxRawSnapshot(), DefaultOptions())
	got, err := RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	assertGoldenBytes(t, renderJSONGoldenPath, got)

	// Sanity: the golden itself must be valid JSON so a byte mismatch below
	// is never hidden behind an unmarshal failure.
	var decoded any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("RenderJSON() output does not parse as JSON: %v\n%s", err, got)
	}
}

// assertGoldenBytes compares got against the committed golden file at path,
// rewriting the golden under -update-golden instead of failing.
func assertGoldenBytes(t *testing.T, path string, got []byte) {
	t.Helper()

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %q: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %q: %v (re-run with -update-golden to create it)", path, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	t.Fatalf(
		"output diverges from golden %q (byte mismatch — regenerate with -update-golden if intentional):\nwant (%d bytes):\n%s\ngot (%d bytes):\n%s",
		path, len(want), want, len(got), got,
	)
}
