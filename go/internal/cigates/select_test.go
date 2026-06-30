// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// buildRegistry builds a minimal in-memory registry for selector tests.
func buildRegistry(gates []cigates.Gate) *cigates.Registry {
	return &cigates.Registry{
		Version: "v1",
		Gates:   gates,
	}
}

func gate(id string, tier cigates.Tier, cat cigates.Category, triggers []string, local *cigates.Local, ciOnly string) cigates.Gate {
	return cigates.Gate{
		ID:           id,
		Name:         id,
		Category:     cat,
		Tier:         tier,
		Blocking:     true,
		Triggers:     triggers,
		Local:        local,
		CI:           cigates.CI{Workflow: "test.yml", Job: "test"},
		Requirements: []cigates.Requirement{cigates.ReqGo},
		CIOnlyReason: ciOnly,
	}
}

func localCmd(cmd string) *cigates.Local {
	return &cigates.Local{Command: cmd}
}

// TestSelect_FrontendLane proves the #4216 frontend-gate selection: an
// apps/console change selects the console frontend gates under category=frontend
// while a Go-only gate stays out, and a root-site change selects the site gate.
func TestSelect_FrontendLane(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("frontend-site", cigates.TierPrePush, cigates.CategoryFrontend,
			[]string{"src/**", "index.html"}, localCmd("npm run typecheck && npm test && npm run build"), ""),
		gate("console-a11y", cigates.TierPrePush, cigates.CategoryFrontend,
			[]string{"apps/console/**"}, localCmd("npm run console:a11y"), ""),
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash x"), ""),
	})

	consoleSel := cigates.FilterByCategory(
		reg.Select([]string{"apps/console/src/App.tsx"}, cigates.TierPrePush),
		[]cigates.Category{cigates.CategoryFrontend})
	sel := collectSelected(consoleSel)
	if _, ok := sel["console-a11y"]; !ok {
		t.Error("apps/console change should select console-a11y under category=frontend")
	}
	if _, ok := sel["frontend-site"]; ok {
		t.Error("apps/console change should NOT select frontend-site (src trigger)")
	}
	if _, ok := sel["go-lint"]; ok {
		t.Error("go-lint (hygiene) should be filtered out of the frontend lane")
	}

	siteSel := cigates.FilterByCategory(
		reg.Select([]string{"src/main.ts"}, cigates.TierPrePush),
		[]cigates.Category{cigates.CategoryFrontend})
	if _, ok := collectSelected(siteSel)["frontend-site"]; !ok {
		t.Error("root src change should select frontend-site")
	}
}

// TestSelect_RaceLane proves the #4215 race-gate selection: a graph-write
// package change selects the targeted race-graph-writes gate (category race),
// while a non-graph Go change does not — so pre-pr's lane 2 (scoped race) owns
// the latter.
func TestSelect_RaceLane(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("race-graph-writes", cigates.TierPrePR, cigates.CategoryRace,
			[]string{
				"go/internal/storage/cypher/**", "go/internal/reducer/**",
				"go/internal/projector/**", "go/internal/correlation/**",
				"go/internal/content/shape/**", "go/internal/relationships/**",
			},
			localCmd("cd go && go test -race ./internal/reducer/..."), ""),
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint-all"), ""),
	})

	// A reducer (graph-write) change selects the race gate under category=race.
	graphSel := cigates.FilterByCategory(
		reg.Select([]string{"go/internal/reducer/handler.go"}, cigates.TierPrePR),
		[]cigates.Category{cigates.CategoryRace})
	if _, ok := collectSelected(graphSel)["race-graph-writes"]; !ok {
		t.Error("reducer change should select race-graph-writes under category=race")
	}

	// A non-graph Go change does NOT select the targeted race gate (pre-pr lane
	// 2's scoped race covers it instead).
	otherSel := cigates.FilterByCategory(
		reg.Select([]string{"go/internal/queueclient/client.go"}, cigates.TierPrePR),
		[]cigates.Category{cigates.CategoryRace})
	if _, ok := collectSelected(otherSel)["race-graph-writes"]; ok {
		t.Error("non-graph change should NOT select race-graph-writes")
	}
}

// TestUncoveredPaths_ExcludesRunnableRaceGates proves the #4215 scoped-race
// exclusion is registry-derived: a path covered by any locally-runnable race
// gate (graph-write OR replay) is excluded, while a path covered only by a
// CI-only race gate, or by no race gate, is returned for local scoped racing.
func TestUncoveredPaths_ExcludesRunnableRaceGates(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("race-graph-writes", cigates.TierPrePR, cigates.CategoryRace,
			[]string{"go/internal/reducer/**"}, localCmd("cd go && go test -race ./internal/reducer/..."), ""),
		gate("go-test-race", cigates.TierPrePR, cigates.CategoryRace,
			[]string{"go/internal/replay/inputtape/**"}, localCmd("cd go && go test -race ./internal/replay/inputtape/..."), ""),
		// CI-only race gate (Local==nil): must NOT suppress local scoped racing.
		ciOnlyRaceGate("reducer-contention", []string{"schema/data-plane/postgres/**"}),
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash x"), ""),
	})
	changed := []string{
		"go/internal/reducer/r.go",            // covered (race-graph-writes, runnable)
		"go/internal/replay/inputtape/i.go",   // covered (go-test-race, runnable)
		"go/internal/queueclient/q.go",        // covered by no race gate
		"schema/data-plane/postgres/0001.sql", // covered only by CI-only gate
	}
	got := reg.UncoveredPaths(changed, []cigates.Category{cigates.CategoryRace}, cigates.TierPrePR)
	want := map[string]bool{
		"go/internal/queueclient/q.go":        true,
		"schema/data-plane/postgres/0001.sql": true,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d uncovered, got %d: %v", len(want), len(got), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("path %q should have been covered (excluded), but was returned uncovered", p)
		}
	}
}

func ciOnlyRaceGate(id string, triggers []string) cigates.Gate {
	return cigates.Gate{
		ID: id, Name: id, Category: cigates.CategoryRace, Tier: cigates.TierPrePR,
		Blocking: true, Triggers: triggers, Local: nil,
		CI:           cigates.CI{Workflow: "reducer-contention-gate.yml", Job: "x"},
		Requirements: []cigates.Requirement{cigates.ReqGo, cigates.ReqPostgres},
		CIOnlyReason: "needs Postgres",
	}
}

// TestFilterByCategory_KeepsRequestedDropsOthers proves the #4214 category
// filter: a Go change selects both a hygiene and an exactness gate, but
// filtering to "exactness" leaves only the exactness gate selected while the
// hygiene gate becomes skipped with a category reason (not dropped from the
// list). This is what lets `make pre-pr` run only the exactness/telemetry lane.
func TestFilterByCategory_KeepsRequestedDropsOthers(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("openapi-surface", cigates.TierPrePR, cigates.CategoryExactness,
			[]string{"go/internal/query/**"}, localCmd("bash scripts/verify-openapi.sh"), ""),
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint-all"), ""),
	})
	changed := []string{"go/internal/query/handler.go"}

	// Without a filter both are selected.
	base := reg.Select(changed, cigates.TierPrePR)
	if s := collectSelected(base); len(s) != 2 {
		t.Fatalf("expected 2 selected without filter, got %d", len(s))
	}

	// Filtered to exactness, only openapi-surface remains selected.
	filtered := cigates.FilterByCategory(base, []cigates.Category{cigates.CategoryExactness})
	selected := collectSelected(filtered)
	skipped := collectSkipped(filtered)
	if _, ok := selected["openapi-surface"]; !ok {
		t.Error("openapi-surface (exactness) should stay selected")
	}
	if _, ok := selected["go-lint"]; ok {
		t.Error("go-lint (hygiene) should be filtered out")
	}
	if _, ok := skipped["go-lint"]; !ok {
		t.Error("go-lint should be reported as skipped, not dropped")
	}

	// Empty category list is a no-op.
	if got := len(collectSelected(cigates.FilterByCategory(base, nil))); got != 2 {
		t.Errorf("empty filter should be a no-op; expected 2 selected, got %d", got)
	}
}

func TestSelect_DocOnlyChangeSelectsOnlyDocsGate(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("docs-build-changed", cigates.TierPrePush, cigates.CategoryDocs,
			[]string{"docs/**"}, localCmd("bash scripts/verify-docs-build-changed.sh"), ""),
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint"), ""),
	})
	changed := []string{"docs/public/reference/local-testing.md"}
	sels := reg.Select(changed, cigates.TierPrePR)

	selected := collectSelected(sels)
	skipped := collectSkipped(sels)

	if _, ok := selected["docs-build-changed"]; !ok {
		t.Error("docs-build-changed should be selected")
	}
	if _, ok := selected["go-lint"]; ok {
		t.Error("go-lint should NOT be selected for docs-only change")
	}
	if _, ok := skipped["go-lint"]; !ok {
		t.Error("go-lint should be in skipped")
	}
}

func TestSelect_GoChangeSelectsGoGate(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint"), ""),
		gate("docs-build-changed", cigates.TierPrePush, cigates.CategoryDocs,
			[]string{"docs/**"}, localCmd("bash scripts/verify-docs-build-changed.sh"), ""),
	})
	changed := []string{"go/internal/query/handler.go"}
	sels := reg.Select(changed, cigates.TierPrePR)
	selected := collectSelected(sels)
	if _, ok := selected["go-lint"]; !ok {
		t.Error("go-lint should be selected for Go file change")
	}
	if _, ok := selected["docs-build-changed"]; ok {
		t.Error("docs-build-changed should not be selected for Go file change")
	}
}

func TestSelect_TierPreCommitExcludesPrePROnlyGate(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("pre-commit-gate", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint"), ""),
		gate("pre-pr-gate", cigates.TierPrePR, cigates.CategoryExactness,
			[]string{"go/**"}, localCmd("bash scripts/verify-openapi.sh"), ""),
	})
	changed := []string{"go/internal/foo.go"}
	sels := reg.Select(changed, cigates.TierPreCommit)
	selected := collectSelected(sels)
	skipped := collectSkipped(sels)
	if _, ok := selected["pre-commit-gate"]; !ok {
		t.Error("pre-commit-gate should be selected at tier pre-commit")
	}
	if _, ok := selected["pre-pr-gate"]; ok {
		t.Error("pre-pr-gate should NOT be selected at tier pre-commit")
	}
	// pre-pr-gate should appear in skipped with a tier reason
	if _, ok := skipped["pre-pr-gate"]; !ok {
		t.Error("pre-pr-gate should be in skipped")
	}
}

func TestSelect_CIHeavyNeverSelectedLocally(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("ci-heavy-gate", cigates.TierCIHeavy, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/heavy.sh"), ""),
	})
	changed := []string{"go/internal/foo.go"}
	sels := reg.Select(changed, cigates.TierPrePR)
	for _, s := range sels {
		if s.Gate.ID == "ci-heavy-gate" && s.Selected {
			t.Error("ci-heavy gate must not be selected at pre-pr tier")
		}
	}
}

func TestSelect_CIOnlyGateNotSelected(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("ci-only-gate", cigates.TierPrePR, cigates.CategoryHygiene,
			[]string{"go/**"}, nil, "needs Docker"),
	})
	changed := []string{"go/internal/foo.go"}
	sels := reg.Select(changed, cigates.TierPrePR)
	ciOnly := collectCIOnly(sels)
	if _, ok := ciOnly["ci-only-gate"]; !ok {
		t.Error("ci-only-gate should appear in ci_only list")
	}
	selected := collectSelected(sels)
	if _, ok := selected["ci-only-gate"]; ok {
		t.Error("ci-only-gate must not be in selected list")
	}
}

func TestSelect_RegistryOrder(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("gate-b", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/b.sh"), ""),
		gate("gate-a", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/a.sh"), ""),
	})
	changed := []string{"go/internal/foo.go"}
	sels := reg.Select(changed, cigates.TierPrePR)
	if len(sels) != 2 {
		t.Fatalf("expected 2 selections, got %d", len(sels))
	}
	if sels[0].Gate.ID != "gate-b" || sels[1].Gate.ID != "gate-a" {
		t.Errorf("order not preserved: got %q, %q", sels[0].Gate.ID, sels[1].Gate.ID)
	}
}

func TestSelect_NoChangedPaths(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint"), ""),
	})
	sels := reg.Select(nil, cigates.TierPrePR)
	selected := collectSelected(sels)
	if _, ok := selected["go-lint"]; ok {
		t.Error("go-lint should not be selected when no paths changed")
	}
}

func TestSelect_ReasonContainsTrigger(t *testing.T) {
	t.Parallel()
	reg := buildRegistry([]cigates.Gate{
		gate("go-lint", cigates.TierPreCommit, cigates.CategoryHygiene,
			[]string{"go/**"}, localCmd("bash scripts/dev/precommit-go.sh lint"), ""),
	})
	changed := []string{"go/internal/foo.go"}
	sels := reg.Select(changed, cigates.TierPrePR)
	for _, s := range sels {
		if s.Gate.ID == "go-lint" && s.Selected {
			if s.Reason == "" {
				t.Error("selected gate should have non-empty reason")
			}
			return
		}
	}
	t.Error("go-lint not found in selections")
}

// helpers

func collectSelected(sels []cigates.Selection) map[string]cigates.Selection {
	m := make(map[string]cigates.Selection)
	for _, s := range sels {
		if s.Selected && s.Gate.Local != nil {
			m[s.Gate.ID] = s
		}
	}
	return m
}

func collectSkipped(sels []cigates.Selection) map[string]cigates.Selection {
	m := make(map[string]cigates.Selection)
	for _, s := range sels {
		if !s.Selected && s.Gate.Local != nil && s.Gate.CIOnlyReason == "" {
			m[s.Gate.ID] = s
		}
	}
	return m
}

func collectCIOnly(sels []cigates.Selection) map[string]cigates.Selection {
	m := make(map[string]cigates.Selection)
	for _, s := range sels {
		if s.Gate.CIOnlyReason != "" {
			m[s.Gate.ID] = s
		}
	}
	return m
}
