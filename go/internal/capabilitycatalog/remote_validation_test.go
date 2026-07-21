// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
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

func TestLoadRemoteValidationBaselineParsesSlugsIgnoringCommentsAndBlanks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "remote-validation-baseline.txt")
	body := "# header comment\n\nprod-alpha\nprod-beta-two\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	baseline, err := LoadRemoteValidationBaseline(path)
	if err != nil {
		t.Fatalf("LoadRemoteValidationBaseline: %v", err)
	}
	if _, ok := baseline["prod-alpha"]; !ok {
		t.Fatal("baseline missing prod-alpha")
	}
	if _, ok := baseline["prod-beta-two"]; !ok {
		t.Fatal("baseline missing prod-beta-two")
	}
	if len(baseline) != 2 {
		t.Fatalf("baseline = %+v, want exactly 2 entries", baseline)
	}
}

func TestLoadRemoteValidationBaselineMissingFileIsEmptyNotError(t *testing.T) {
	t.Parallel()

	baseline, err := LoadRemoteValidationBaseline(filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if err != nil {
		t.Fatalf("LoadRemoteValidationBaseline(missing): %v", err)
	}
	if len(baseline) != 0 {
		t.Fatalf("baseline = %+v, want empty", baseline)
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

func TestRenderRemoteValidationBaselineListsOnlyDanglingRefsSorted(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeRemoteValidationArtifact(t, repoRoot, "prod-has-artifact")
	matrix := matrixWithRemoteValidationRefs(
		matrixRefSpec{capability: "z.last", ref: "prod-dangling-z"},
		matrixRefSpec{capability: "a.first", ref: "prod-dangling-a"},
		matrixRefSpec{capability: "m.mid", ref: "prod-has-artifact"},
	)

	rendered := RenderRemoteValidationBaseline(matrix, repoRoot)
	if strings.Contains(rendered, "prod-has-artifact") {
		t.Fatalf("rendered baseline must not list a ref with a committed artifact:\n%s", rendered)
	}
	idxA := strings.Index(rendered, "prod-dangling-a")
	idxZ := strings.Index(rendered, "prod-dangling-z")
	if idxA == -1 || idxZ == -1 || idxA > idxZ {
		t.Fatalf("rendered baseline not sorted (a must precede z):\n%s", rendered)
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

	findings := CheckRemoteValidationArtifacts(matrix, repoRoot, baseline)
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
}
