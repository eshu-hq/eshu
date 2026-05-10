package rust

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveModuleRowFileCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		currentFile string
		row         map[string]any
		want        ModuleResolution
	}{
		{
			name:        "library root direct declaration",
			currentFile: "src/lib.rs",
			row: map[string]any{
				"name":        "api",
				"module_kind": "declaration",
			},
			want: ModuleResolution{
				CandidatePaths: []string{"src/api.rs", "src/api/mod.rs"},
			},
		},
		{
			name:        "mod file direct declaration",
			currentFile: "src/foo/mod.rs",
			row: map[string]any{
				"name":        "bar",
				"module_kind": "declaration",
			},
			want: ModuleResolution{
				CandidatePaths: []string{"src/foo/bar.rs", "src/foo/bar/mod.rs"},
			},
		},
		{
			name:        "non mod file direct declaration",
			currentFile: "src/foo.rs",
			row: map[string]any{
				"name":        "bar",
				"module_kind": "declaration",
			},
			want: ModuleResolution{
				CandidatePaths: []string{"src/foo/bar.rs", "src/foo/bar/mod.rs"},
			},
		},
		{
			name:        "path attribute anchors to current file directory",
			currentFile: "src/lib.rs",
			row: map[string]any{
				"name":                     "os",
				"module_kind":              "declaration",
				"declared_path_candidates": []string{"platform/unix.rs"},
				"module_path_source":       "path_attribute",
			},
			want: ModuleResolution{
				CandidatePaths: []string{"src/platform/unix.rs"},
			},
		},
		{
			name:        "inline module has no filesystem candidates",
			currentFile: "src/lib.rs",
			row: map[string]any{
				"name":        "worker",
				"module_kind": "inline",
			},
			want: ModuleResolution{},
		},
		{
			name:        "macro origin keeps blocker unresolved",
			currentFile: "src/lib.rs",
			row: map[string]any{
				"name":          "generated",
				"module_kind":   "declaration",
				"module_origin": "macro_invocation",
			},
			want: ModuleResolution{
				Blockers: []string{"macro_expansion_unavailable"},
			},
		},
		{
			name:        "macro origin preserves existing blocker",
			currentFile: "src/lib.rs",
			row: map[string]any{
				"name":               "generated",
				"module_kind":        "declaration",
				"module_origin":      "macro_invocation",
				"exactness_blockers": []string{"macro_expansion_unavailable"},
			},
			want: ModuleResolution{
				Blockers: []string{"macro_expansion_unavailable"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveModuleRowFileCandidates(tt.currentFile, tt.row)
			got.CandidatePaths = slashPaths(got.CandidatePaths)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveModuleRowFileCandidates() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func slashPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	slashed := make([]string, 0, len(paths))
	for _, path := range paths {
		slashed = append(slashed, filepath.ToSlash(path))
	}
	return slashed
}
