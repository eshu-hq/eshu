// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneHVersion pins the Lane H gap-4 fix: the index-status
// schemas document the top-level `version` key both handlers always emit
// (query.StatusHandler.getIndexStatus and getScopedIndexStatus via
// buildinfo.AppVersion). The route description already promises scoped
// callers "only version, scoped=true, ...", so the schema must name the
// key it promises. Both the canonical route (Routes) and its legacy alias
// (IndexLegacy) share the handler, so both fragments carry the fix.
func TestContractShapesLaneHVersion(t *testing.T) {
	t.Parallel()

	versionAt := func(t *testing.T, raw, path, where string) {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimSuffix(trimmed, ",")
		var doc map[string]any
		if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
			t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
		}
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("%s: fragment paths missing %s", where, path)
		}
		get, ok := route["get"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s missing get", where, path)
		}
		responses, ok := get["responses"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s get missing responses", where, path)
		}
		okResp, ok := responses["200"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s get missing 200", where, path)
		}
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 missing content", where, path)
		}
		body, ok := content["application/json"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 missing application/json", where, path)
		}
		schema, ok := body["schema"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 missing schema", where, path)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 schema missing properties", where, path)
		}
		version, ok := props["version"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %s 200 schema missing version", where, path)
		}
		if typ, _ := version["type"].(string); typ != "string" {
			t.Errorf("%s: version type = %v, want string", where, version["type"])
		}
		desc, _ := version["description"].(string)
		if !strings.Contains(desc, "StatusHandler") {
			t.Errorf("%s: version description does not name its query.StatusHandler owner", where)
		}
		if !strings.Contains(desc, "both shapes") && !strings.Contains(desc, "scoped") {
			t.Errorf("%s: version description carries no presence note for the scoped shape", where)
		}
	}

	t.Run("canonical", func(t *testing.T) {
		t.Parallel()
		versionAt(t, Routes, "/api/v0/status/index", "Routes")
	})
	t.Run("legacy", func(t *testing.T) {
		t.Parallel()
		versionAt(t, IndexLegacy, "/api/v0/index-status", "IndexLegacy")
	})
}
