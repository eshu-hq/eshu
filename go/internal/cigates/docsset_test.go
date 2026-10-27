// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestIsDocsSetPath(t *testing.T) {
	t.Parallel()
	docs := []string{
		"docs/public/guide.md",
		"go/cmd/collector-cicd-run/README.md",
		"README.md",
		"CHANGELOG.md",
		"skill-fragments/operating-standard.md",
		"apps/console/README.md",
		".github/workflows/notes.md",
		"docs/public/reference/code-coverage-shield.json",
		"package.json",
		"tsconfig.node.json",
		"scripts/lib/ifa_scheduled_inventory.json",
		"docs/mkdocs.yml",
		"mkdocs.yml",
		".agents/skills/x/SKILL.md",
	}
	for _, path := range docs {
		if !IsDocsSetPath(path) {
			t.Errorf("IsDocsSetPath(%q) = false, want true", path)
		}
	}
	code := []string{
		"go/cmd/collector-cicd-run/main.go",
		"go/internal/capabilitycatalog/data/catalog.generated.json",
		"sdk/go/collector/fixture.json",
		"specs/ci-images.v1.json",
		"tests/fixtures/x.json",
		"testdata/golden/x.json",
		".github/workflows/test.yml",
		"Makefile",
	}
	for _, path := range code {
		if IsDocsSetPath(path) {
			t.Errorf("IsDocsSetPath(%q) = true, want false", path)
		}
	}
}

func TestTriggerSelectsDocsPath(t *testing.T) {
	t.Parallel()
	selecting := []string{
		"**",
		"docs/**",
		".agents/**",
		"docs/internal/**",
		"skill-fragments/**",
		"go/internal/parser/**",
		"expected/**",
		"skill-fragments/operating-standard.md",
		"go/**/*.md",
		"docs/**/*.md",
		"**/*.md",
		"*.md",
		"docs/**/*.json",
		"scripts/**/*.json",
		"*.json",
		"mkdocs.yml",
		"docs/public/reference/code-coverage-shield.json",
		"go/internal/envregistry/README.md",
		"scripts/lib/test-verify-parser-relationship-kit-*",
		"docs/dashboards/*.json",
	}
	for _, trigger := range selecting {
		if !triggerSelectsDocsPath(trigger) {
			t.Errorf("triggerSelectsDocsPath(%q) = false, want true", trigger)
		}
	}
	notSelecting := []string{
		"go/**",
		"sdk/go/**",
		"specs/live-tests.d/**",
		"testdata/golden/**",
		"**/*.json",
		"go/**/*.json",
		"Dockerfile",
		"specs/ci-images.v1.json",
		"go/internal/capabilitycatalog/data/surface-inventory.generated.json",
		"go/internal/query/main.go",
	}
	for _, trigger := range notSelecting {
		if triggerSelectsDocsPath(trigger) {
			t.Errorf("triggerSelectsDocsPath(%q) = true, want false", trigger)
		}
	}
}

func TestContentTreesStaySelectable(t *testing.T) {
	t.Parallel()
	// skill-roundtrip (blocking) validates skill-fragments/** and expected/**
	// inputs. These trees hold content files (plus the odd non-docs fixture
	// such as expected/cursor/.cursor/rules/eshu.mdc, which selects normally
	// because it is not docs-set), so their catch-alls must keep selecting
	// for docs-set paths. The selecting-list entries above pin the rule; this
	// pins the registry side still declares both trees on that gate.
	for _, tree := range []string{"skill-fragments/**", "expected/**"} {
		if !triggerSelectsDocsPath(tree) {
			t.Fatalf("triggerSelectsDocsPath(%q) = false, skill-roundtrip would deselect", tree)
		}
	}
}

func TestDocsSetSkipsCodeGatedJobs(t *testing.T) {
	t.Parallel()
	docsOnly := []string{"docs/public/guide.md"}
	mixed := []string{"docs/public/guide.md", "go/cmd/ci-gates/main.go"}
	for job := range codeGatedCIJobs {
		gate := Gate{
			ID:       "code-lane",
			Blocking: true,
			Tier:     TierPrePR,
			Triggers: []string{"docs/**"},
			Local:    &Local{Command: "true"},
			CI:       CI{Workflow: "test.yml", Job: job},
		}
		if gateMatchesAnyPath(gate, docsOnly) {
			t.Errorf("job %q never runs on a docs-only PR yet still matches", job)
		}
		if sel := selectGate(gate, docsOnly, TierPrePR); sel.Selected {
			t.Errorf("job %q must not select locally for docs-only paths: %s", job, sel.Reason)
		}
		if !gateMatchesAnyPath(gate, mixed) {
			t.Errorf("job %q must still match a mixed docs+code PR", job)
		}
		otherWorkflow := gate
		otherWorkflow.CI.Workflow = "sdk-go.yml"
		if !gateMatchesAnyPath(otherWorkflow, docsOnly) {
			t.Errorf("code-gating is test.yml-scoped; %q in another workflow must still match", job)
		}
	}
	running := Gate{
		ID:       "docs-cap",
		Blocking: true,
		Tier:     TierPrePR,
		Triggers: []string{"docs/**"},
		Local:    &Local{Command: "true"},
		CI:       CI{Workflow: "test.yml", Job: "dirgate"},
	}
	if !gateMatchesAnyPath(running, docsOnly) {
		t.Fatal("a job that runs on docs-only PRs must keep matching")
	}
	if sel := selectGate(running, docsOnly, TierPrePR); !sel.Selected {
		t.Fatalf("a job that runs on docs-only PRs must keep selecting: %s", sel.Reason)
	}
}

// TestCodeGatedJobsMatchWorkflowIfs pins codeGatedCIJobs against test.yml:
// the set must equal exactly the jobs whose own `if` requires
// needs.changes.outputs.code, so a newly code-gated lane cannot silently
// strand docs-only PRs.
func TestCodeGatedJobsMatchWorkflowIfs(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobHeader := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):`)
	jobIf := regexp.MustCompile(`^    if: (.*)$`)
	gated := make(map[string]struct{})
	var current string
	var seenIf bool
	for _, line := range strings.Split(string(raw), "\n") {
		if m := jobHeader.FindStringSubmatch(line); m != nil {
			current, seenIf = m[1], false
			continue
		}
		if current == "" || seenIf {
			continue
		}
		if m := jobIf.FindStringSubmatch(line); m != nil {
			seenIf = true
			if strings.Contains(m[1], "needs.changes.outputs.code == 'true'") {
				gated[current] = struct{}{}
			}
		}
	}
	if len(gated) != len(codeGatedCIJobs) {
		t.Fatalf("code-gated jobs in test.yml = %v, want %v", keys(gated), keys(codeGatedCIJobs))
	}
	for job := range codeGatedCIJobs {
		if _, ok := gated[job]; !ok {
			t.Errorf("job %q listed in codeGatedCIJobs but not code-gated in test.yml", job)
		}
	}
}

func keys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// TestLockstepPathsMatchManifest pins the Go denylist to the canonical
// manifest: the only copy is specs/ci-docs-lockstep-paths.txt, everything
// else derives from it.
func TestLockstepPathsMatchManifest(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "ci-docs-lockstep-paths.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		manifest = append(manifest, line)
	}
	if !slices.Equal(slices.Sorted(slices.Values(lockstepDocsPaths)), manifest) {
		t.Fatalf("lockstepDocsPaths drifts from the manifest: go=%v manifest=%v",
			slices.Sorted(slices.Values(lockstepDocsPaths)), manifest)
	}
}

func TestTriggerMatchesChangedPathKeepsMixedPRsWhole(t *testing.T) {
	t.Parallel()
	if !MatchGlob("go/**", "go/cmd/collector-cicd-run/README.md") {
		t.Fatal("sanity: go/** must still glob-match the README at the matcher level")
	}
	gate := Gate{Triggers: []string{"go/**"}}
	if gateMatchesAnyPath(gate, []string{"go/cmd/collector-cicd-run/README.md"}) {
		t.Fatal("nested markdown alone must not select a go/** gate")
	}
	if !gateMatchesAnyPath(gate, []string{"go/cmd/collector-cicd-run/README.md", "go/cmd/collector-cicd-run/main.go"}) {
		t.Fatal("a mixed docs+code PR must still select the gate")
	}
	docsGate := Gate{Triggers: []string{"go/**", "docs/**/*.md"}}
	if !gateMatchesAnyPath(docsGate, []string{"docs/public/guide.md"}) {
		t.Fatal("a docs-aware trigger must still select for docs paths")
	}
}
