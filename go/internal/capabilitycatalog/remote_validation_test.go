// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func matrixWithRemoteValidationRefs(refs ...matrixRefSpec) Matrix {
	capabilities := make([]MatrixCapability, 0, len(refs))
	for i, spec := range refs {
		capabilities = append(capabilities, MatrixCapability{
			Capability: spec.capability,
			Tools:      []string{"some_tool"},
			Profiles: map[string]MatrixProfile{
				"production": {
					Status: "supported",
					Verification: []MatrixVerification{
						{Kind: "remote_validation", Ref: spec.ref},
					},
				},
			},
		})
		_ = i
	}
	return Matrix{Capabilities: capabilities}
}

type matrixRefSpec struct {
	capability string
	ref        string
}

func writeRemoteValidationArtifact(t *testing.T, repoRoot, ref string) {
	t.Helper()
	dir := filepath.Join(repoRoot, RemoteValidationArtifactDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, ref+".md")
	if err := os.WriteFile(path, []byte("# "+ref+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckRemoteValidationArtifactsCommittedArtifactPasses(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationArtifact(t, repoRoot, "prod-code-search-exact")
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "code_search.exact_symbol", ref: "prod-code-search-exact"})

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, map[string]struct{}{})
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none (artifact is committed)", findings)
	}
}

func TestCheckRemoteValidationArtifactsBaselinedMissingRefPasses(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "code_search.exact_symbol", ref: "prod-code-search-exact"})
	baseline := map[string]struct{}{"prod-code-search-exact": {}}

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, baseline)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none (ref is baselined debt)", findings)
	}
}

func TestCheckRemoteValidationArtifactsUnbaselinedMissingRefFails(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	matrix := matrixWithRemoteValidationRefs(
		matrixRefSpec{capability: "code_search.exact_symbol", ref: "prod-code-search-exact"},
		matrixRefSpec{capability: "code_search.fuzzy", ref: "prod-code-search-fuzzy"},
	)

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, map[string]struct{}{})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want 2 dangling refs", findings)
	}
	if findings[0].Ref != "prod-code-search-exact" || findings[1].Ref != "prod-code-search-fuzzy" {
		t.Fatalf("findings not sorted by ref: %+v", findings)
	}
	if len(findings[0].Subjects) != 1 || findings[0].Subjects[0] != "code_search.exact_symbol/production" {
		t.Fatalf("finding subjects = %+v, want [code_search.exact_symbol/production]", findings[0].Subjects)
	}
}

func TestCheckRemoteValidationArtifactsDedupesRefAcrossCapabilities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	matrix := matrixWithRemoteValidationRefs(
		matrixRefSpec{capability: "a.one", ref: "prod-shared-ref"},
		matrixRefSpec{capability: "b.two", ref: "prod-shared-ref"},
	)

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, map[string]struct{}{})
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one deduplicated finding for the shared ref", findings)
	}
	if got := findings[0].Subjects; len(got) != 2 || got[0] != "a.one/production" || got[1] != "b.two/production" {
		t.Fatalf("finding subjects = %+v, want both citing capabilities sorted", got)
	}
}

// TestRemoteValidationArtifactExistsRejectsPathTraversal proves the
// filename-safety guard (FIX 1, #5407): a ref that is not a valid slug — a
// path-traversal payload or any ref carrying a slash — must never be turned
// into a filesystem path and probed with os.Stat. Without the guard, a crafted
// ref like "../../evil" resolves under repoRoot to an unrelated existing file
// and remoteValidationArtifactExists returns true, silently "verifying" a claim
// that has no committed remote-validation artifact.
func TestRemoteValidationArtifactExistsRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	// Plant a real file at the location a "../../evil" ref would resolve to:
	// filepath.Join(repoRoot, "docs/internal/remote-validation", "../../evil.md")
	// == repoRoot/docs/evil.md. Pre-guard, os.Stat finds it and the ref is
	// falsely treated as backed by a committed artifact.
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", docsDir, err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "evil.md"), []byte("# unrelated\n"), 0o644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}

	traversalRefs := []string{
		"../../evil",
		"../../../etc/passwd",
		"nested/slash",
		"..",
	}
	for _, ref := range traversalRefs {
		if remoteValidationArtifactExists(repoRoot, ref) {
			t.Fatalf("remoteValidationArtifactExists(repoRoot, %q) = true; a non-slug ref must never resolve to a real file", ref)
		}
	}
	// A well-formed slug still resolves when its artifact exists.
	writeRemoteValidationArtifact(t, repoRoot, "prod-valid-slug")
	if !remoteValidationArtifactExists(repoRoot, "prod-valid-slug") {
		t.Fatal("remoteValidationArtifactExists must still resolve a valid slug with a committed artifact")
	}
}

// TestCheckRemoteValidationArtifactsFlagsTraversalRefAsHardFinding proves a
// malformed/traversal ref cited in the matrix is a hard gate failure that names
// the ref and its capability/profile, and cannot be excused by an artifact that
// happens to exist at the resolved-outside path or by a baseline entry.
func TestCheckRemoteValidationArtifactsFlagsTraversalRefAsHardFinding(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	docsDir := filepath.Join(repoRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", docsDir, err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "evil.md"), []byte("# unrelated\n"), 0o644); err != nil {
		t.Fatalf("write decoy: %v", err)
	}
	matrix := matrixWithRemoteValidationRefs(matrixRefSpec{capability: "code_search.exact_symbol", ref: "../../evil"})

	// Even if an attacker also lists the traversal ref in the baseline, it must
	// still be a finding — a non-slug ref is never valid burn-down debt.
	baseline := map[string]struct{}{"../../evil": {}}
	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, baseline)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly one hard finding for the traversal ref", findings)
	}
	if findings[0].Ref != "../../evil" {
		t.Fatalf("finding ref = %q, want the offending traversal ref", findings[0].Ref)
	}
	if len(findings[0].Subjects) != 1 || findings[0].Subjects[0] != "code_search.exact_symbol/production" {
		t.Fatalf("finding subjects = %+v, want [code_search.exact_symbol/production]", findings[0].Subjects)
	}
}

func TestLoadRemoteValidationBaselineParsesSlugsIgnoringCommentsAndBlanks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "remote-validation-baseline.txt")
	body := "# header comment\n# FROZEN_MAX: 2\n\nprod-alpha\nprod-beta-two\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	baseline, err := LoadRemoteValidationBaseline(path)
	if err != nil {
		t.Fatalf("LoadRemoteValidationBaseline: %v", err)
	}
	if _, ok := baseline.Entries["prod-alpha"]; !ok {
		t.Fatal("baseline missing prod-alpha")
	}
	if _, ok := baseline.Entries["prod-beta-two"]; !ok {
		t.Fatal("baseline missing prod-beta-two")
	}
	if len(baseline.Entries) != 2 {
		t.Fatalf("baseline entries = %+v, want exactly 2 entries", baseline.Entries)
	}
	if baseline.Ceiling != 2 {
		t.Fatalf("baseline ceiling = %d, want 2", baseline.Ceiling)
	}
}

// TestLoadRemoteValidationBaselineParsesCeilingDirective covers the FROZEN_MAX
// directive: a well-formed value is parsed, and absent/duplicate/malformed
// directives fail closed.
func TestLoadRemoteValidationBaselineParsesCeilingDirective(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		wantErr bool
		ceiling int
	}{
		{"well-formed directive", "# FROZEN_MAX: 5\nprod-a\n", false, 5},
		{"zero ceiling", "# FROZEN_MAX: 0\n", false, 0},
		{"absent directive fails closed", "prod-a\n", true, 0},
		{"duplicate directive fails closed", "# FROZEN_MAX: 1\n# FROZEN_MAX: 2\n", true, 0},
		{"non-numeric value fails closed", "# FROZEN_MAX: many\nprod-a\n", true, 0},
		{"negative value fails closed", "# FROZEN_MAX: -1\nprod-a\n", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "remote-validation-baseline.txt")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write baseline: %v", err)
			}
			baseline, err := LoadRemoteValidationBaseline(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LoadRemoteValidationBaseline(%q) error = nil, want fail-closed", tc.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadRemoteValidationBaseline(%q): %v", tc.body, err)
			}
			if baseline.Ceiling != tc.ceiling {
				t.Fatalf("ceiling = %d, want %d", baseline.Ceiling, tc.ceiling)
			}
		})
	}
}

func TestLoadRemoteValidationBaselineMissingFileIsEmptyNotError(t *testing.T) {
	t.Parallel()

	baseline, err := LoadRemoteValidationBaseline(filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if err != nil {
		t.Fatalf("LoadRemoteValidationBaseline(missing): %v", err)
	}
	if len(baseline.Entries) != 0 {
		t.Fatalf("baseline entries = %+v, want empty", baseline.Entries)
	}
	// A missing file has a zero ceiling: no debt tracked and no growth
	// allowance, so any dangling ref still fails the artifact check.
	if baseline.Ceiling != 0 {
		t.Fatalf("baseline ceiling = %d, want 0 for a missing file", baseline.Ceiling)
	}
}

func TestLoadRemoteValidationBaselineFailsClosedOnMalformedLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
	}{
		{"two tokens on one line", "prod-alpha prod-beta"},
		{"uppercase", "Prod-Alpha"},
		{"underscore", "prod_alpha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "remote-validation-baseline.txt")
			if err := os.WriteFile(path, []byte(tc.line+"\n"), 0o644); err != nil {
				t.Fatalf("write baseline: %v", err)
			}
			if _, err := LoadRemoteValidationBaseline(path); err == nil {
				t.Fatalf("LoadRemoteValidationBaseline(%q) error = nil, want fail-closed error", tc.line)
			}
		})
	}
}

// TestRemoteValidationBaselineRejectsGrowthAboveCeiling proves the ratcheting
// high-water-mark: an entry count that EXCEEDS the frozen ceiling is baseline
// GROWTH and must be rejected, while count == ceiling and count < ceiling both
// pass. This is the anti-append-smuggling guard: a new unverified
// production:supported row appended to the baseline pushes the count past the
// ceiling and fails the gate.
func TestRemoteValidationBaselineRejectsGrowthAboveCeiling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		entries  int
		ceiling  int
		exceeded bool
	}{
		{"count over ceiling grows the set", 2, 1, true},
		{"count equal to ceiling holds", 1, 1, false},
		{"count under ceiling is a burn-down", 0, 1, false},
		{"zero entries under zero ceiling", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entries := map[string]struct{}{}
			for i := 0; i < tc.entries; i++ {
				entries[fmt.Sprintf("prod-slug-%d", i)] = struct{}{}
			}
			b := RemoteValidationBaseline{Entries: entries, Ceiling: tc.ceiling}
			if got := RemoteValidationBaselineCeilingExceeded(b); got != tc.exceeded {
				t.Fatalf("RemoteValidationBaselineCeilingExceeded(entries=%d, ceiling=%d) = %v, want %v",
					tc.entries, tc.ceiling, got, tc.exceeded)
			}
		})
	}
}

func TestRenderRemoteValidationBaselineListsOnlyDanglingRefsSorted(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationArtifact(t, repoRoot, "prod-has-artifact")
	matrix := matrixWithRemoteValidationRefs(
		matrixRefSpec{capability: "z.last", ref: "prod-dangling-z"},
		matrixRefSpec{capability: "a.first", ref: "prod-dangling-a"},
		matrixRefSpec{capability: "m.mid", ref: "prod-has-artifact"},
	)

	// First generation: no prior ceiling, so FROZEN_MAX is the dangling count (2).
	rendered := RenderRemoteValidationBaseline(matrix, repoRoot, 0, false)
	if strings.Contains(rendered, "prod-has-artifact") {
		t.Fatalf("rendered baseline must not list a ref with a committed artifact:\n%s", rendered)
	}
	idxA := strings.Index(rendered, "prod-dangling-a")
	idxZ := strings.Index(rendered, "prod-dangling-z")
	if idxA == -1 || idxZ == -1 || idxA > idxZ {
		t.Fatalf("rendered baseline not sorted (a must precede z):\n%s", rendered)
	}
	if !strings.Contains(rendered, "# FROZEN_MAX: 2\n") {
		t.Fatalf("rendered baseline missing FROZEN_MAX: 2 for first generation:\n%s", rendered)
	}
	// The rendered baseline must round-trip through the loader with a matching
	// ceiling and no growth violation.
	dir := t.TempDir()
	path := filepath.Join(dir, "remote-validation-baseline.txt")
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		t.Fatalf("write rendered baseline: %v", err)
	}
	loaded, err := LoadRemoteValidationBaseline(path)
	if err != nil {
		t.Fatalf("round-trip load of rendered baseline: %v", err)
	}
	if loaded.Ceiling != 2 || len(loaded.Entries) != 2 {
		t.Fatalf("round-trip: entries=%d ceiling=%d, want 2/2", len(loaded.Entries), loaded.Ceiling)
	}
	if RemoteValidationBaselineCeilingExceeded(loaded) {
		t.Fatal("freshly rendered baseline must not exceed its own ceiling")
	}
}

// TestRenderRemoteValidationBaselineRatchetsCeilingDown proves regeneration
// never raises the ceiling: with a smaller prior ceiling the rendered file
// keeps the prior (lower) ceiling even when more refs dangle, and with a larger
// prior ceiling the ceiling ratchets down to the current count.
func TestRenderRemoteValidationBaselineRatchetsCeilingDown(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	matrix := matrixWithRemoteValidationRefs(
		matrixRefSpec{capability: "a.one", ref: "prod-dangling-a"},
		matrixRefSpec{capability: "b.two", ref: "prod-dangling-b"},
	)

	// Prior ceiling 1 is below the current dangling count 2: -update must NOT
	// raise it, so the rendered file holds 2 entries but a ceiling of 1 and
	// therefore fails the growth check.
	renderedLow := RenderRemoteValidationBaseline(matrix, repoRoot, 1, true)
	if !strings.Contains(renderedLow, "# FROZEN_MAX: 1\n") {
		t.Fatalf("prior ceiling 1 must not be raised:\n%s", renderedLow)
	}

	// Prior ceiling 5 is above the current dangling count 2: the ceiling
	// ratchets DOWN to 2.
	renderedHigh := RenderRemoteValidationBaseline(matrix, repoRoot, 5, true)
	if !strings.Contains(renderedHigh, "# FROZEN_MAX: 2\n") {
		t.Fatalf("prior ceiling 5 must ratchet down to 2:\n%s", renderedHigh)
	}
}

func TestRemoteValidationRealSpecsRespectBaseline(t *testing.T) {
	t.Parallel()

	specsDir := repoSpecsDir(t)
	repoRoot := filepath.Dir(specsDir)
	matrix, err := LoadMatrix(specsDir)
	if err != nil {
		t.Fatalf("LoadMatrix(real specs): %v", err)
	}
	baselinePath := filepath.Join(specsDir, RemoteValidationBaselineFileName)
	baseline, err := LoadRemoteValidationBaseline(baselinePath)
	if err != nil {
		t.Fatalf("LoadRemoteValidationBaseline(real baseline): %v", err)
	}

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, baseline.Entries)
	if len(findings) != 0 {
		var lines []string
		for _, finding := range findings {
			lines = append(lines, finding.Ref+" ("+strings.Join(finding.Subjects, ", ")+")")
		}
		t.Fatalf(
			"%d remote_validation ref(s) have no committed artifact and are not in %s; add the artifact or baseline the ref:\n%s",
			len(findings), baselinePath, strings.Join(lines, "\n"),
		)
	}
	// The committed baseline must not itself exceed its frozen ceiling.
	if RemoteValidationBaselineCeilingExceeded(baseline) {
		t.Fatalf("committed baseline %s holds %d entries above its FROZEN_MAX ceiling %d",
			baselinePath, len(baseline.Entries), baseline.Ceiling)
	}
}
