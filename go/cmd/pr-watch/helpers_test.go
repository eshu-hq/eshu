// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveAccount(t *testing.T) {
	home := t.TempDir()
	personal := filepath.Join(home, "personal-repos", "eshu-wt", "9095")
	other := filepath.Join(home, "repos", "service")

	got, err := resolveAccount("", personal, home)
	if err != nil || got != "linuxdynasty" {
		t.Fatalf("personal-repos path = %q, %v; want linuxdynasty", got, err)
	}
	got, err = resolveAccount("someone", personal, home)
	if err != nil || got != "someone" {
		t.Fatalf("explicit account must win, got %q, %v", got, err)
	}
	if _, err = resolveAccount("", other, home); err == nil || !strings.Contains(err.Error(), "-account") {
		t.Fatalf("an unknown path must fail and name the flag, got %v", err)
	}
	if _, err = resolveAccount("", filepath.Join(home, "personal-repos-evil", "x"), home); err == nil {
		t.Fatalf("a sibling that merely shares the prefix must not match")
	}
}

func TestTailLog(t *testing.T) {
	raw := "2026-10-25T11:00:00.1234567Z \x1b[31;1mred\x1b[0m start\n" +
		"2026-10-25T11:00:01.0000000Z a\n\n" +
		"2026-10-25T11:00:02.0000000Z b\n" +
		"2026-10-25T11:00:03.0000000Z ##[error]boom\n" +
		"2026-10-25T11:00:04.0000000Z cleanup\n"
	if got, want := tailLog(raw, 2), "b | ##[error]boom"; got != want {
		t.Fatalf("anchored tail = %q, want %q", got, want)
	}
	noError := "x1\nx2\nx3\n"
	if got, want := tailLog(noError, 2), "x2 | x3"; got != want {
		t.Fatalf("unanchored tail = %q, want %q", got, want)
	}
	if got := tailLog("", 3); got != "" {
		t.Fatalf("empty log tail = %q", got)
	}
	long := strings.Repeat("y", 5000)
	if got := tailLog(long, 1); len(got) > maxTailBytes+3 {
		t.Fatalf("tail must be capped, got %d bytes", len(got))
	}
}

func TestParseMergeTreeFiles(t *testing.T) {
	out := []byte("0123abcd\nCHANGELOG.md\ngo/a.go\n\nAuto-merging x\n")
	if want, got := []string{"CHANGELOG.md", "go/a.go"}, parseMergeTreeFiles(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestClassifyConflictFollowsTheRecipeList(t *testing.T) {
	generated := []string{
		"specs/live-tests.v1.yaml",
		"scripts/lib/live_postgres_readiness_inventory_gen_*.py",
		".github/workflows/live-postgres-readiness.yml",
	}
	cases := []struct {
		name  string
		files []string
		want  string
	}{
		{"changelog", []string{"CHANGELOG.md"}, "4a"},
		{"content pin and its constant", []string{
			"go/internal/collector/repo/git/testdata/content_version_pin.json",
			"go/internal/collector/repo/git/snapshot_freshness.go",
		}, "4a"},
		{"readiness weights", []string{"specs/live-postgres-readiness-weights.v1.json"}, "4a"},
		{"generated list entry", []string{"specs/live-tests.v1.yaml", ".github/workflows/live-postgres-readiness.yml"}, "4a"},
		{"generated list glob", []string{"scripts/lib/live_postgres_readiness_inventory_gen_aws.py"}, "4a"},
		{"all recipe kinds together", []string{"CHANGELOG.md", "specs/live-tests.v1.yaml"}, "4a"},

		{"surface inventory is not a recipe file", []string{"go/internal/capabilitycatalog/data/surface-inventory.generated.json"}, "count-choice-lines"},
		{"other generated go file", []string{"go/internal/facts/fact_kind_registry.generated.go"}, "count-choice-lines"},
		{"pin file in another directory", []string{"go/other/testdata/content_version_pin.json"}, "count-choice-lines"},
		{"other weights file", []string{"specs/other-readiness-weights.v1.json"}, "count-choice-lines"},
		{"changelog beside source", []string{"CHANGELOG.md", "go/internal/reducer/x.go"}, "count-choice-lines"},
		{"no files", nil, "count-choice-lines"},
	}
	for _, agent := range []string{
		"AGENTS.md", "go/cmd/pr-watch/AGENTS.md", ".agents/skills/eshu-code-review/SKILL.md", ".claude/rules/x.md",
		".claude/settings.json", ".muse/hooks.json", ".codex/config.toml", "docs/internal/agent-guide.md",
		"docs/internal/writing-for-agents.md",
	} {
		cases = append(cases, struct {
			name  string
			files []string
			want  string
		}{"agent rule " + agent, []string{"CHANGELOG.md", agent}, "count-choice-lines"})
	}
	for _, c := range cases {
		if got := classifyConflict(c.files, generated); got != c.want {
			t.Errorf("%s: classifyConflict(%v) = %q, want %q", c.name, c.files, got, c.want)
		}
	}
	// A generated path with no list loaded is not a recipe file.
	if got := classifyConflict([]string{"specs/live-tests.v1.yaml"}, nil); got != "count-choice-lines" {
		t.Errorf("without the generated list, a generated path must not be 4a, got %q", got)
	}
}

func TestReadGeneratedListFromTheCheckout(t *testing.T) {
	dir := t.TempDir()
	if got := readGeneratedList(dir); got != nil {
		t.Fatalf("a missing script must give no list, got %v", got)
	}
	script := "#!/usr/bin/env bash\nGENERATED=(\n  specs/a.yaml\n  # a comment\n\n  scripts/lib/gen_*.py\n)\nrest=1\n"
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "verify-live-postgres-readiness-drift.sh"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if want, got := []string{"specs/a.yaml", "scripts/lib/gen_*.py"}, readGeneratedList(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
}

func TestJobIDFromDetailsURL(t *testing.T) {
	if id, ok := jobID("https://github.com/eshu-hq/eshu/actions/runs/123/job/456?pr=9"); !ok || id != "456" {
		t.Fatalf("jobID = %q, %v", id, ok)
	}
	if _, ok := jobID("https://example.test/status"); ok {
		t.Fatalf("a non-Actions URL must not yield a job id")
	}
}

func TestCheckClassification(t *testing.T) {
	for _, r := range []string{"FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE", "ERROR"} {
		if !(Check{Status: "COMPLETED", Result: r}).Failing() {
			t.Errorf("%s must count as failing", r)
		}
	}
	for _, r := range []string{"SUCCESS", "NEUTRAL", "SKIPPED", ""} {
		if (Check{Status: "COMPLETED", Result: r}).Failing() {
			t.Errorf("%q must not count as failing", r)
		}
	}
	if !(Check{Status: "IN_PROGRESS"}).Pending() || !(Check{Status: "QUEUED"}).Pending() {
		t.Errorf("running and queued runs are pending even with a null conclusion")
	}
	if (Check{Status: "COMPLETED", Result: ""}).Pending() {
		t.Errorf("a completed run is not pending")
	}
	if !(Check{Result: "PENDING"}).Pending() || !(Check{Result: "EXPECTED"}).Pending() {
		t.Errorf("pending status contexts must count as pending")
	}
}

func TestGHEnvStripsAmbientTokens(t *testing.T) {
	env := ghEnv([]string{"A=1", "GH_TOKEN=x", "GITHUB_TOKEN=y"}, "pinned")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "=x") || strings.Contains(joined, "=y") || !strings.Contains(joined, "GH_TOKEN=pinned") {
		t.Fatalf("env = %v", env)
	}
	if env2 := ghEnv([]string{"GH_TOKEN=x"}, ""); strings.Contains(strings.Join(env2, "\n"), "GH_TOKEN") {
		t.Fatalf("an empty token must leave GH_TOKEN unset: %v", env2)
	}
}

func TestFreeTextIsClippedButTailIsNot(t *testing.T) {
	if got := clip(strings.Repeat("é", 400)); len(got) > maxFieldBytes+3 || !strings.HasSuffix(got, "...") {
		t.Fatalf("clip kept %d bytes: %q", len(got), got)
	}
	tail := strings.Repeat("t", 900)
	if got := formatValue(tail); got != tail {
		t.Fatalf("formatValue must not truncate a log tail, got %d bytes", len(got))
	}
}

func TestEventIsAlwaysOneLine(t *testing.T) {
	w := newWorld()
	h := newHarness(t, nil, w)
	h.wt.emit("NEW-COMMENT", 1, "body", "line one\nline two\r\n\"quoted\" = x")
	out := h.out.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("event must be exactly one line, got %q", out)
	}
	if e := parseEvent(t, strings.TrimSpace(out)); e.kv["body"] != `line one line two "quoted" = x` {
		t.Fatalf("body round-trip = %q", e.kv["body"])
	}
}
