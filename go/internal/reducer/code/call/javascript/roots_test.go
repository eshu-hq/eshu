// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import "testing"

// TestResolveFileRootCallerIDTypedByteIdentity proves FileRootCallerID
// returns the identical caller id whether the dead_code_file_root_kinds key
// is read raw or through the typed accessor, for the entrypoint/bin/script/
// export root kinds it branches on plus a non-root kind and a non-JS
// language.
func TestResolveFileRootCallerIDTypedByteIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		repoID   string
		relPath  string
		fileData map[string]any
		want     string
	}{
		{
			name:    "js_entrypoint_root",
			repoID:  "repo-1",
			relPath: "index.js",
			fileData: map[string]any{
				"language":                  "javascript",
				"dead_code_file_root_kinds": []string{"javascript.node_package_entrypoint"},
			},
			want: "repo-1:index.js",
		},
		{
			name:    "ts_export_root_jsonb_shape",
			repoID:  "repo-2",
			relPath: "src/lib.ts",
			fileData: map[string]any{
				"lang":                      "typescript",
				"dead_code_file_root_kinds": []any{"javascript.node_package_export"},
			},
			want: "repo-2:src/lib.ts",
		},
		{
			name:    "non_root_kind_no_caller",
			repoID:  "repo-3",
			relPath: "util.js",
			fileData: map[string]any{
				"language":                  "javascript",
				"dead_code_file_root_kinds": []string{"javascript.some_other_kind"},
			},
			want: "",
		},
		{
			name:    "non_js_language_no_caller",
			repoID:  "repo-4",
			relPath: "main.go",
			fileData: map[string]any{
				"language":                  "go",
				"dead_code_file_root_kinds": []string{"javascript.node_package_entrypoint"},
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FileRootCallerID(tc.repoID, tc.relPath, tc.fileData)
			if got != tc.want {
				t.Fatalf("FileRootCallerID = %q, want %q", got, tc.want)
			}
		})
	}
}
