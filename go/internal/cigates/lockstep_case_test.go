// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDSResolveTriesExactCandidateAfterCaseAlias(t *testing.T) {
	root := t.TempDir()
	refDir := filepath.Join(root, "caller")
	for _, dir := range []string{filepath.Join(root, "Docs"), filepath.Join(refDir, "docs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "CaseProbePage.md"), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ref := filepath.Join(refDir, "source_test.go")
	want := "caller/docs/CaseProbePage.md"
	if got := dsResolve(root, ref, "docs/CaseProbePage.md", true); got != want {
		t.Errorf("dsResolve after case alias = %q, want %q", got, want)
	}
}

func TestDSChainResolveTriesExactCandidateAfterCaseAlias(t *testing.T) {
	root := t.TempDir()
	refDir := filepath.Join(root, "caller")
	for _, dir := range []string{filepath.Join(root, "docs"), filepath.Join(refDir, "Docs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "CaseProbePage.md"), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ref := filepath.Join(refDir, "source_test.go")
	if got := dsChainResolve(root, ref, []string{"docs", "CaseProbePage.md"}, true); got != "docs/CaseProbePage.md" {
		t.Errorf("dsChainResolve after case alias = %q, want docs/CaseProbePage.md", got)
	}
}

func TestDSChainDirTriesExactCandidateAfterCaseAlias(t *testing.T) {
	root := t.TempDir()
	refDir := filepath.Join(root, "caller")
	for _, dir := range []string{filepath.Join(root, "fixtures"), filepath.Join(refDir, "Fixtures")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ref := filepath.Join(refDir, "source_test.go")
	if got := dsChainDir(root, ref, []string{"fixtures"}, true); got != "fixtures" {
		t.Errorf("dsChainDir after case alias = %q, want fixtures", got)
	}
}

func TestDSScanMultiArgReadIgnoresCaseAlias(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		wantR7    bool
	}{
		{name: "exact", raw: "CaseProbePage.md", wantR7: true},
		{name: "case alias", raw: "caseprobepage.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			goRoot := filepath.Join(root, "go")
			if err := os.Mkdir(goRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "CaseProbePage.md"), []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			source := `package fixture
func TestRead() { _, _ = os.OpenFile("` + tc.raw + `", os.O_RDONLY, 0) }
`
			if err := os.WriteFile(filepath.Join(goRoot, "source_test.go"), []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, unresolved := dsScan(t, root, goRoot)
			gotR7 := false
			for _, demand := range unresolved {
				if strings.Contains(demand, "("+tc.raw+")") {
					gotR7 = true
				}
			}
			if gotR7 != tc.wantR7 {
				t.Errorf("R7 for %q = %t, want %t; demands: %v", tc.raw, gotR7, tc.wantR7, unresolved)
			}
		})
	}
}
