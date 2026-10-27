// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDSResolveRejectsFilesystemCaseAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(root, "source_test.go")
	if got := dsResolve(root, ref, "README.md", true); got != "README.md" {
		t.Errorf("dsResolve exact path = %q, want README.md", got)
	}
	if got := dsResolve(root, ref, "readme.md", true); got != "" {
		t.Errorf("dsResolve case alias = %q, want no tracked match", got)
	}
	var marked string
	if ok := dsMarkNamed(root, ref, "readme.md", func(rel, _ string) { marked = rel }); ok {
		t.Errorf("dsMarkNamed case alias = %q, want no tracked match", marked)
	}
	if got := dsChainResolve(root, ref, []string{"readme.md"}, true); got != "" {
		t.Errorf("dsChainResolve case alias = %q, want no tracked match", got)
	}
}

func TestDSResolveRejectsWrongCaseInDirectory(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "README.md"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(root, "source_test.go")
	want := filepath.ToSlash(filepath.Join("docs", "README.md"))
	if got := dsResolve(root, ref, want, true); got != want {
		t.Errorf("dsResolve exact path = %q, want %q", got, want)
	}
	for _, raw := range []string{"Docs/README.md", "docs/readme.md"} {
		if got := dsResolve(root, ref, raw, true); got != "" {
			t.Errorf("dsResolve(%q) = %q, want no tracked match", raw, got)
		}
	}
}
