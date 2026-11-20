// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestDocsSetPatternsMatchDocsset pins specs/ci-docs-set-patterns.txt to the
// Go docs set it generates: same patterns, same order. The generated leaf
// filter negates exactly these, so a drift between the spec and docsset.go
// would silently split CI and local selection.
func TestDocsSetPatternsMatchDocsset(t *testing.T) {
	t.Parallel()
	patterns, err := DocsSetPatterns(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(patterns, docsSetPatterns) {
		t.Fatalf("specs/ci-docs-set-patterns.txt = %q, want docsset.go %q", patterns, docsSetPatterns)
	}
}

// TestGenerateLeafCodeFilter pins the generated dorny `code` filter
// byte-for-byte: it must stay identical to the inline blocks the three
// code-gated workflows carry today, or switching them to the shared file
// changes docs-only selection.
func TestGenerateLeafCodeFilter(t *testing.T) {
	t.Parallel()
	got, err := GenerateLeafCodeFilter([]string{
		"docs/**",
		"**/*.md",
		"*.json",
		"docs/**/*.json",
		"scripts/**/*.json",
		"mkdocs.yml",
		".github/**/*.md",
		".agents/**",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := leafFilterHeader("specs/ci-docs-set-patterns.txt") +
		"code:\n" +
		"  - '**'\n" +
		"  - '!docs/**'\n" +
		"  - '!*.md'\n" +
		"  - '!**/*.md'\n" +
		"  - '!*.json'\n" +
		"  - '!docs/**/*.json'\n" +
		"  - '!scripts/**/*.json'\n" +
		"  - '!mkdocs.yml'\n" +
		"  - '!.github/**/*.md'\n" +
		"  - '!.agents/**'\n"
	if got != want {
		t.Fatalf("generated code filter differs:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenerateLeafCodeFilterRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := GenerateLeafCodeFilter(nil); err == nil {
		t.Fatal("empty patterns accepted: would emit code: ['**'], selecting every PR including docs-only")
	}
	if _, err := GenerateLeafCodeFilter([]string{"docs/**", "!*.md"}); err == nil {
		t.Fatal("negated pattern accepted: the spec holds positive docs-set globs, negation is the generator's job")
	}
}

// TestGenerateLeafLockstepFilter pins manifest-order, single-quoted
// rendering of the dorny `lockstep` filter from the canonical manifest.
func TestGenerateLeafLockstepFilter(t *testing.T) {
	t.Parallel()
	got, err := GenerateLeafLockstepFilter([]string{"AGENTS.md", "docs/public/guide.md"})
	if err != nil {
		t.Fatal(err)
	}
	want := leafFilterHeader("specs/ci-docs-lockstep-paths.txt") +
		"lockstep:\n" +
		"  - 'AGENTS.md'\n" +
		"  - 'docs/public/guide.md'\n"
	if got != want {
		t.Fatalf("generated lockstep filter differs:\n got:\n%s\nwant:\n%s", got, want)
	}
	if _, err := GenerateLeafLockstepFilter(nil); err == nil {
		t.Fatal("empty lockstep manifest accepted")
	}
	if _, err := GenerateLeafLockstepFilter([]string{"it's.md"}); err == nil {
		t.Fatal("single-quote entry accepted: would break the generated YAML quoting")
	}
}

// TestCheckLeafFilterFreshness is the seeded-violation pair for the drift
// gate: a planted byte fails with the regenerate command, the generated pair
// passes.
func TestCheckLeafFilterFreshness(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("specs/ci-docs-set-patterns.txt", "docs/**\n**/*.md\n")
	write("specs/ci-docs-lockstep-paths.txt", "AGENTS.md\n")
	code, err := GenerateLeafCodeFilter([]string{"docs/**", "**/*.md"})
	if err != nil {
		t.Fatal(err)
	}
	lockstep, err := GenerateLeafLockstepFilter([]string{"AGENTS.md"})
	if err != nil {
		t.Fatal(err)
	}
	write(".github/leaf-code-filter.yml", code)
	write(".github/leaf-lockstep-filter.yml", lockstep)
	if errs := checkLeafFilterFreshness(root); len(errs) != 0 {
		t.Fatalf("fresh pair rejected: %v", errs)
	}
	write(".github/leaf-code-filter.yml", code+"  - '!planted/**'\n")
	errs := checkLeafFilterFreshness(root)
	if len(errs) == 0 {
		t.Fatal("planted code-filter drift accepted")
	}
	if !strings.Contains(errs[0].Error(), "generate-leaf-filters") {
		t.Fatalf("drift error names no regenerate command: %v", errs[0])
	}
}

// TestCheckLeafFilterFreshnessSkipsBareFixtures pins the synthetic-fixture
// skip: a repo with none of the four leaf filter artifacts has nothing to
// prove fresh. Any subset present is checkable, so a lone manifest without
// its patterns spec still fails loudly instead of passing silently.
func TestCheckLeafFilterFreshnessSkipsBareFixtures(t *testing.T) {
	t.Parallel()
	if errs := checkLeafFilterFreshness(t.TempDir()); len(errs) != 0 {
		t.Fatalf("bare fixture rejected: %v", errs)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "specs", "ci-docs-lockstep-paths.txt"), []byte("AGENTS.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := checkLeafFilterFreshness(root); len(errs) == 0 {
		t.Fatal("lone manifest without patterns spec accepted")
	}
}

// TestDornyFilterStepsResolved pins external `filters:` file resolution: an
// external ref resolves like inline, inline keeps working, and a dangling or
// escaping ref fails closed instead of reading as empty.
func TestDornyFilterStepsResolved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflows, 0o755); err != nil {
		t.Fatal(err)
	}
	shared := ".github/leaf-code-filter.yml"
	if err := os.WriteFile(filepath.Join(root, shared), []byte("code:\n  - '**'\n  - '!docs/**'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(workflows, "leaf.yml")
	raw := "jobs:\n  changes:\n    steps:\n" +
		"      - uses: dorny/paths-filter@v3\n        id: filter\n        with:\n          predicate-quantifier: every\n          filters: " + shared + "\n" +
		"      - uses: dorny/paths-filter@v3\n        id: inline\n        with:\n          filters: |\n            extra:\n              - 'go/**'\n"
	if err := os.WriteFile(workflow, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	steps, err := DornyFilterStepsResolved(root, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := DornyFilterStepWithKey(steps, "code")
	if !ok || !got.Every || len(got.Filters["code"]) != 2 {
		t.Fatalf("external code step unresolved: %+v %v", steps, ok)
	}
	if _, ok := DornyFilterStepWithKey(steps, "extra"); !ok {
		t.Fatal("inline step lost after resolution was added")
	}
	bad := "jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          filters: .github/missing.yml\n"
	if _, err := DornyFilterStepsResolved(root, []byte(bad)); err == nil {
		t.Fatal("dangling filters ref accepted")
	}
	evil := "jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          filters: ../../outside.yml\n"
	if _, err := DornyFilterStepsResolved(root, []byte(evil)); err == nil {
		t.Fatal("escaping filters ref accepted")
	}
}
