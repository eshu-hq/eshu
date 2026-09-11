// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// TestParsedFileDataDeadCodeRootKindsTypedMatchesRawRead proves the typed
// dead_code_file_root_kinds accessor returns the same root-kind slice the
// pre-typing raw toStringSlice read produced for every shape the JavaScript
// producer emits (a []string of root-kind literals) and for the JSONB []any
// round-trip shape. This is the byte-identity guard for migrating
// javascript.FileRootCallerID off the raw map read (issue #4750 S1).
func TestParsedFileDataDeadCodeRootKindsTypedMatchesRawRead(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		fileData map[string]any
	}{
		{
			name: "producer_string_slice",
			fileData: map[string]any{
				"dead_code_file_root_kinds": []string{
					"javascript.node_package_entrypoint",
					"javascript.node_package_bin",
				},
			},
		},
		{
			name: "jsonb_any_slice",
			fileData: map[string]any{
				"dead_code_file_root_kinds": []any{
					"javascript.node_package_export",
				},
			},
		},
		{
			name:     "absent_key",
			fileData: map[string]any{"lang": "javascript"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rawRead := payloadcore.ToStringSlice(tc.fileData["dead_code_file_root_kinds"])
			typedRead := DeadCodeFileRootKinds(tc.fileData)
			if !reflect.DeepEqual(rawRead, typedRead) {
				t.Fatalf("typed read = %#v, raw read = %#v; must be byte-identical", typedRead, rawRead)
			}
		})
	}
}

// TestParsedFileDataGomodModulePathTypedMatchesRawRead proves the typed
// gomod_state accessor resolves the same declared module path the pre-typing
// raw goModuleDeclaredPath read produced, for a parsed go.mod, a malformed
// go.mod (no module_path), and a go.sum state envelope.
func TestParsedFileDataGomodModulePathTypedMatchesRawRead(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		fileData map[string]any
		want     string
	}{
		{
			name: "parsed_gomod_with_module_path",
			fileData: map[string]any{
				"lang": "gomod",
				"gomod_state": map[string]any{
					"state":       "parsed",
					"module_path": "github.com/eshu-hq/eshu",
				},
			},
			want: "github.com/eshu-hq/eshu",
		},
		{
			name: "malformed_gomod_no_module_path_falls_back_to_variables",
			fileData: map[string]any{
				"lang": "gomod",
				"gomod_state": map[string]any{
					"state":       "malformed",
					"parse_error": "boom",
				},
				"variables": []map[string]any{
					{"config_kind": "module_declaration", "value": "example.com/fallback"},
				},
			},
			want: "example.com/fallback",
		},
		{
			name: "gosum_state_no_module_path",
			fileData: map[string]any{
				"lang": "gomod",
				"gomod_state": map[string]any{
					"state":          "parsed",
					"checksum_count": float64(3),
				},
			},
			want: "",
		},
		{
			name:     "not_a_gomod_file",
			fileData: map[string]any{"lang": "go"},
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := goModuleDeclaredPath(tc.fileData)
			if got != tc.want {
				t.Fatalf("goModuleDeclaredPath = %q, want %q", got, tc.want)
			}
		})
	}
}
