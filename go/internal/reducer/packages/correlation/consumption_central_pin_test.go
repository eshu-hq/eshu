// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import "testing"

// TestIsCentralVersionDeclarationAgreesWithSidecar holds the reducer's pin
// predicate to the same verdict as the Postgres consumption sidecar
// (IsCentralVersionDeclaration) and the readiness SQL
// (NotCentralVersionDeclarationSQL) on every payload shape. The rows mirror
// centralVersionDeclarationCases in
// go/internal/storage/postgres/package_manifest_central_pin_test.go; the
// postgres package is not imported, because the reducer must not depend on
// storage. A shape the three classify differently would let a pin count as
// consumption in one reader and not another: for example a NBSP-padded "true"
// that a Unicode trim accepts and the ASCII-blank BTRIM rejects.
func TestIsCentralVersionDeclarationAgreesWithSidecar(t *testing.T) {
	t.Parallel()

	metadata := func(value any) map[string]any {
		return map[string]any{"entity_metadata": map[string]any{
			"config_kind": "dependency", "central_version_declaration": value,
		}}
	}
	cases := []struct {
		name    string
		payload map[string]any
		pin     bool
	}{
		{"metadata bool true (parser shape)", metadata(true), true},
		{"metadata string true", metadata("true"), true},
		{"metadata string TRUE padded", metadata("  TRUE "), true},
		{"metadata bool false", metadata(false), false},
		{"metadata string false", metadata("false"), false},
		{"metadata empty string", metadata(""), false},
		{"metadata number 1", metadata(1), false},
		{"metadata null", metadata(nil), false},
		{"metadata leading no-break space", metadata(" true"), false},
		{"metadata trailing next-line control", metadata("true\u0085"), false},
		{"top-level literal <nil> text wins over metadata true", map[string]any{
			"central_version_declaration": "<nil>",
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, false},
		{"flag absent", map[string]any{"entity_metadata": map[string]any{"config_kind": "dependency"}}, false},
		{"no metadata", map[string]any{"entity_type": "Variable"}, false},
		{"top-level true", map[string]any{"central_version_declaration": true}, true},
		{"top-level false overrides metadata true", map[string]any{
			"central_version_declaration": false,
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, false},
		{"top-level empty falls back to metadata true", map[string]any{
			"central_version_declaration": "",
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, true},
		{"top-level null falls back to metadata true", map[string]any{
			"central_version_declaration": nil,
			"entity_metadata":             map[string]any{"central_version_declaration": true},
		}, true},
	}
	for _, tc := range cases {
		if got := isCentralVersionDeclaration(tc.payload); got != tc.pin {
			t.Errorf("%s: isCentralVersionDeclaration() = %v, want %v", tc.name, got, tc.pin)
		}
	}
}
