// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const renderJSONKeyPathsGoldenPath = "testdata/render_json_key_paths_golden.txt"

// TestRenderWireJSON_KeyPathsGolden asserts the complete sorted set of dotted
// JSON key paths reachable from RenderJSON's output. Unlike the byte-for-byte
// golden (render_wire_golden_test.go), this survives a value change: it is
// the field-name lock alone, so a renamed, added, or dropped json tag fails
// this test even when nothing else in the payload changed value.
func TestRenderWireJSON_KeyPathsGolden(t *testing.T) {
	t.Parallel()

	report := BuildReport(maxRawSnapshot(), DefaultOptions())
	raw, err := RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("RenderJSON() output does not parse as JSON: %v\n%s", err, raw)
	}

	paths := map[string]struct{}{}
	collectJSONKeyPaths(decoded, "", paths)

	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)

	got := []byte(strings.Join(sorted, "\n") + "\n")
	assertGoldenBytes(t, renderJSONKeyPathsGoldenPath, got)
}

// collectJSONKeyPaths walks a decoded JSON document and records the dotted
// path of every leaf value into out. Arrays are walked without an index
// component (an array of objects contributes the same dotted paths as one
// object, since the assertion locks field names, not element counts), so the
// same field seen with or without an optional value across elements still
// yields exactly one path.
func collectJSONKeyPaths(node any, prefix string, out map[string]struct{}) {
	switch v := node.(type) {
	case map[string]any:
		if len(v) == 0 {
			if prefix != "" {
				out[prefix] = struct{}{}
			}
			return
		}
		for key, child := range v {
			childPrefix := key
			if prefix != "" {
				childPrefix = prefix + "." + key
			}
			collectJSONKeyPaths(child, childPrefix, out)
		}
	case []any:
		if len(v) == 0 {
			if prefix != "" {
				out[prefix] = struct{}{}
			}
			return
		}
		for _, elem := range v {
			collectJSONKeyPaths(elem, prefix, out)
		}
	default:
		if prefix != "" {
			out[prefix] = struct{}{}
		}
	}
}
