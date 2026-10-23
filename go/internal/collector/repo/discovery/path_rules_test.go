// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func pathRulesTestOptions() Options {
	return Options{
		IgnoredDirs:             []string{".git", "Vendor"},
		IgnoredExtensions:       []string{".min.js"},
		IgnoreHidden:            true,
		PreservedHiddenPrefixes: []string{".github/workflows", ".nvmrc"},
		IgnoredPathGlobs: []PathGlobRule{
			{Pattern: "generated/**", Reason: "generated"},
			{Pattern: "docs/*.tmp.md", Reason: "scratch"},
		},
		PreservedPathGlobs: []string{"generated/keep/**"},
	}
}

// TestPathRulesExcludesFile pins each location rule ExcludesFile applies to a
// path the walk never visited, such as a symlink's resolved target (#8809).
func TestPathRulesExcludesFile(t *testing.T) {
	t.Parallel()

	rules := NewPathRules(pathRulesTestOptions())
	tests := []struct {
		rel  string
		want bool
	}{
		{rel: "src/app.go", want: false},
		{rel: ".git/config", want: true},
		{rel: "vendor/x/.nvmrc", want: true},
		{rel: "a/VENDOR/b.go", want: true},
		{rel: ".secret/app.conf", want: true},
		{rel: ".env", want: true},
		{rel: ".nvmrc", want: false},
		{rel: "tools/.nvmrc", want: false},
		{rel: ".github/workflows/ci.yml", want: false},
		{rel: "web/app.min.js", want: true},
		{rel: "generated/out.go", want: true},
		{rel: "generated/keep/out.go", want: false},
		{rel: "docs/x.tmp.md", want: true},
		{rel: "docs/x.md", want: false},
		{rel: "", want: true},
		{rel: ".", want: true},
	}
	for _, tt := range tests {
		if got := rules.ExcludesFile(tt.rel); got != tt.want {
			t.Errorf("ExcludesFile(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

// TestPathRulesZeroValueExcludesNothing proves the zero value is a no-op.
func TestPathRulesZeroValueExcludesNothing(t *testing.T) {
	t.Parallel()

	var rules PathRules
	for _, rel := range []string{".git/config", "vendor/x.go", ".env"} {
		if rules.ExcludesFile(rel) {
			t.Errorf("zero PathRules excluded %q", rel)
		}
	}
}

// TestPathRulesExcludesFileAgreesWithWalk proves ExcludesFile and the walk
// make the same decision for every file in a tree, so the two cannot drift.
func TestPathRulesExcludesFileAgreesWithWalk(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rels := []string{
		"src/app.go", ".git/config", "vendor/x/.nvmrc", "a/VENDOR/b.go",
		".secret/app.conf", ".env", ".nvmrc", "tools/.nvmrc",
		".github/workflows/ci.yml", ".github/CODEOWNERS", "web/app.min.js",
		"generated/out.go", "generated/keep/out.go", "docs/x.tmp.md", "docs/x.md",
	}
	for _, rel := range rels {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts := pathRulesTestOptions()
	files, _, err := collectSupportedFiles(root, func(string) bool { return true }, opts)
	if err != nil {
		t.Fatalf("collectSupportedFiles() error = %v", err)
	}
	walked := make(map[string]bool, len(files))
	for _, file := range files {
		rel, err := filepath.Rel(root, file.Path)
		if err != nil {
			t.Fatal(err)
		}
		walked[filepath.ToSlash(rel)] = true
	}
	rules := NewPathRules(opts)
	for _, rel := range rels {
		if rules.ExcludesFile(rel) == walked[rel] {
			t.Errorf("%q: ExcludesFile = %v but walk admitted = %v", rel, rules.ExcludesFile(rel), walked[rel])
		}
	}
}
