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

// workflowCodeGatedJobs returns the job IDs in a workflow file whose own
// first `if` requires needs.changes.outputs.code, with that `if` text. A
// second dorny step or a reordered clause does not change the answer: only
// the presence of the code clause in the job's own gate counts.
func workflowCodeGatedJobs(t *testing.T, file string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", file))
	if err != nil {
		t.Fatal(err)
	}
	jobHeader := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):`)
	jobIf := regexp.MustCompile(`^    if: (.*)$`)
	gated := make(map[string]string)
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
				gated[current] = m[1]
			}
		}
	}
	return gated
}

// TestCodeGatedJobsMatchWorkflowIfs pins codeGatedCIJobs against test.yml:
// the set must equal exactly the jobs whose own `if` requires
// needs.changes.outputs.code, so a newly code-gated lane cannot silently
// strand docs-only PRs.
func TestCodeGatedJobsMatchWorkflowIfs(t *testing.T) {
	t.Parallel()
	gated := workflowCodeGatedJobs(t, "test.yml")
	names := make(map[string]struct{}, len(gated))
	for job := range gated {
		names[job] = struct{}{}
	}
	if len(names) != len(codeGatedCIJobs) {
		t.Fatalf("code-gated jobs in test.yml = %v, want %v", keys(names), keys(codeGatedCIJobs))
	}
	for job := range codeGatedCIJobs {
		if _, ok := gated[job]; !ok {
			t.Errorf("job %q listed in codeGatedCIJobs but not code-gated in test.yml", job)
		}
	}
}

// TestLockstepGatedJobsMatchWorkflowIfs pins the P1 fix: every job whose
// `if` requires needs.changes.outputs.code must also accept
// needs.changes.outputs.lockstep, in all three code-gated workflows. A
// lockstep-only PR (a manifest-listed test input, skipped by the code
// filter) would otherwise select the job through the registry while CI
// runtime skips it, and the aggregate await would fail red.
func TestLockstepGatedJobsMatchWorkflowIfs(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"test.yml", "security-scan.yml", "mcp-schema-drift.yml"} {
		for job, cond := range workflowCodeGatedJobs(t, file) {
			if !strings.Contains(cond, "needs.changes.outputs.lockstep == 'true'") {
				t.Errorf("job %s/%s is code-gated but never runs on lockstep-only PRs", file, job)
			}
		}
	}
}

// TestLockstepFilterMatchesManifest pins each code-gated workflow's dorny
// `lockstep` filter to the canonical manifest, entry for entry. The filter
// is the runtime half of the "test inputs stay code" contract: the
// registry selects the lanes, the filter lets them run.
func TestLockstepFilterMatchesManifest(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	manifest := dsManifest(t, root)
	for _, file := range []string{"test.yml", "security-scan.yml", "mcp-schema-drift.yml"} {
		raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", file))
		if err != nil {
			t.Fatal(err)
		}
		// Steps resolve through the shared leaf filter files: the workflows
		// consume .github/leaf-code-filter.yml and
		// .github/leaf-lockstep-filter.yml instead of inline copies (#10980).
		steps, err := DornyFilterStepsResolved(root, raw)
		if err != nil {
			t.Fatalf("%s dorny filters do not resolve: %v", file, err)
		}
		step, ok := DornyFilterStepWithKey(steps, "lockstep")
		if !ok {
			t.Fatalf("%s has no dorny lockstep filter", file)
		}
		if step.Every {
			t.Errorf("%s lockstep filter must use the `some` quantifier: `every` over all-positive entries selects nothing", file)
		}
		if !slices.Equal(step.Filters["lockstep"], manifest) {
			t.Errorf("%s lockstep filter drifts from specs/ci-docs-lockstep-paths.txt", file)
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

func TestGoRaceFilterExcludesNonGoScriptsAndDocs(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	steps, err := DornyFilterStepsResolved(root, raw)
	if err != nil {
		t.Fatalf("resolve test.yml dorny steps: %v", err)
	}
	codeStep, ok := DornyFilterStepWithKey(steps, "code")
	if !ok || !codeStep.Every {
		t.Fatal("test.yml missing every-quantifier code filter")
	}
	lockStep, ok := DornyFilterStepWithKey(steps, "lockstep")
	if !ok || lockStep.Every {
		t.Fatal("test.yml missing some-quantifier lockstep filter")
	}
	raceStep, ok := DornyFilterStepWithKey(steps, "go_race")
	if !ok || !raceStep.Every {
		t.Fatal("test.yml missing every-quantifier go_race filter")
	}
	incStep, ok := DornyFilterStepWithKey(steps, "include")
	if !ok || incStep.Every {
		t.Fatal("test.yml missing some-quantifier go_race_include filter")
	}
	matchesGoRace := func(path string) bool {
		code := DornyEveryMatch(codeStep.Filters["code"], []string{path})
		lockstep := DornySomeMatch(lockStep.Filters["lockstep"], []string{path})
		race := DornyEveryMatch(raceStep.Filters["go_race"], []string{path}) ||
			DornySomeMatch(incStep.Filters["include"], []string{path})
		return (code && race) || lockstep
	}
	for _, path := range []string{
		"README.md", "docs/public/guide.md", "go/cmd/ci-gates/README.md",
		"scripts/ci/test-tools-workflow.py", "tool.py",
		"scripts/verify-tagged-builds.sh", "run.sh",
		"specs/ci-gates.d/gates-01.yaml", "config.yaml",
		".github/workflows/static-contract-gates.yml", "mkdocs.yml",
		"scripts/lib/ifa_scheduled_inventory.json", "package.json",
		".agents/skills/x/SKILL.md",
	} {
		if matchesGoRace(path) {
			t.Errorf("go_race filter unexpectedly selected %q", path)
		}
	}
	for _, path := range []string{
		"go/internal/reducer/awscloud/aws_evidence_cache.go",
		"go/go.mod", "go/go.sum",
		"go/internal/storage/postgres/migrations/001_ingestion_scopes.sql",
		"go/internal/queryplan/testdata/statement-builders.yaml",
		"go/internal/replay/parserfixture/testdata/trees/yaml/service.yaml",
		"sdk/go/collector/client.go", "testdata/golden/sample.json",
		"specs/parser-backing-ledger.v1.yaml", ".github/workflows/test.yml",
		"AGENTS.md", "go/cmd/eshu/cli_extraction_inventory.md",
	} {
		if !matchesGoRace(path) {
			t.Errorf("go_race filter unexpectedly skipped %q", path)
		}
	}
	reg, err := Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load registry: %v", err)
	}
	for _, gate := range reg.Gates {
		if gate.CI.Workflow != "test.yml" || gate.CI.Job != "go-race-complete" {
			continue
		}
		for _, trig := range gate.Triggers {
			if strings.ContainsAny(trig, "*?[") {
				continue
			}
			if !matchesGoRace(trig) {
				t.Errorf("go-race-complete gate %q literal trigger %q is skipped by go_race", gate.ID, trig)
			}
		}
	}
	gated := workflowCodeGatedJobs(t, "test.yml")
	if !strings.Contains(gated["go-race"], "(needs.changes.outputs.code == 'true' && needs.changes.outputs.go_race == 'true') || needs.changes.outputs.lockstep == 'true'") {
		t.Fatalf("test.yml go-race if lacks expected code+go_race||lockstep clause: %s", gated["go-race"])
	}
	for _, want := range []string{
		"RACE_CHANGED: ${{ needs.changes.outputs.go_race }}",
		"LOCKSTEP_CHANGED: ${{ needs.changes.outputs.lockstep }}",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("test.yml go-race-complete missing %q", want)
		}
	}
}

func TestNonGoWorkflowAndScriptPRsSkipGoSecurityMCPAndCoverage(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	pr11377 := []string{"scripts/ci/test-tools-workflow.py"}
	pr11385 := []string{
		".github/actions/retry-upload-artifact/action.yml",
		".github/workflows/golden-corpus-gate.yml",
		"scripts/lib/golden-corpus-cleanup.sh",
		"scripts/lib/golden-corpus-failure-diagnostics.sh",
		"scripts/test-stage-failure-diagnostics.sh",
		"specs/ci-pr-workflows.d/golden-corpus-gate.yml",
	}
	goPR := []string{"go/internal/mcp/handler.go"}

	checkSomeFilter := func(wf, key string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", wf))
		if err != nil {
			t.Fatal(err)
		}
		steps, err := DornyFilterStepsResolved(root, raw)
		if err != nil {
			t.Fatalf("resolve %s dorny steps: %v", wf, err)
		}
		step, ok := DornyFilterStepWithKey(steps, key)
		if !ok {
			t.Fatalf("%s missing dorny filter key %q", wf, key)
		}
		if step.Every {
			t.Fatalf("%s %q filter must use predicate-quantifier: some", wf, key)
		}
		if DornySomeMatch(step.Filters[key], pr11377) {
			t.Errorf("%s %q unexpectedly matched PR #11377 paths", wf, key)
		}
		if DornySomeMatch(step.Filters[key], pr11385) {
			t.Errorf("%s %q unexpectedly matched PR #11385 paths", wf, key)
		}
		if !DornySomeMatch(step.Filters[key], goPR) {
			t.Errorf("%s %q failed to match Go code PR", wf, key)
		}
	}
	checkSomeFilter("security-scan.yml", "go_security")
	checkSomeFilter("mcp-schema-drift.yml", "go_mcp")

	defs, _ := trustedPRWorkflowDefsForTest(t, root)
	plan11377, err := PlanPRWorkflows(defs, pr11377, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("PlanPRWorkflows(pr11377): %v", err)
	}
	if _, ok := plan11377["code-coverage-report.yml"]; ok {
		t.Error("PR #11377 unexpectedly planned code-coverage-report.yml")
	}
	if _, ok := plan11377["build.yml"]; ok {
		t.Error("PR #11377 unexpectedly planned build.yml")
	}
	plan11385, err := PlanPRWorkflows(defs, pr11385, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("PlanPRWorkflows(pr11385): %v", err)
	}
	if _, ok := plan11385["code-coverage-report.yml"]; ok {
		t.Error("PR #11385 unexpectedly planned code-coverage-report.yml")
	}
	planGo, err := PlanPRWorkflows(defs, goPR, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("PlanPRWorkflows(goPR): %v", err)
	}
	if _, ok := planGo["code-coverage-report.yml"]; !ok {
		t.Error("Go PR failed to plan code-coverage-report.yml")
	}
	if _, ok := planGo["build.yml"]; !ok {
		t.Error("Go PR failed to plan build.yml")
	}
}
