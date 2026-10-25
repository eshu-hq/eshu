// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Recipe labels for a conflict. The recipe list is the one in
// .agents/skills/eshu-code-review/references/conflict-recipes.md.
//
//   - 4a: every conflicted file is a recipe file, so row 4a applies at any size.
//   - count-choice-lines: at least one conflicted file is not a recipe file.
//     The watcher cannot count choice lines. Row 4a may still apply (at most
//     10 choice lines in at most 2 files), otherwise the conflict is row 4b.
//   - rebase: the test merge was clean; a plain rebase is enough.
//   - unknown: the test merge could not run, so there are no files to judge.
const (
	recipeMechanical = "4a"
	recipeCount      = "count-choice-lines"
	recipeRebase     = "rebase"
	recipeUnknown    = "unknown"
)

// generatedListScript names the script whose GENERATED array lists the
// generated readiness files. The list is read from the checkout, not copied
// here, so it cannot drift from the generator.
const generatedListScript = "scripts/verify-live-postgres-readiness-drift.sh"

// mergeTest is the result of a local test merge of a PR head into its base.
type mergeTest struct {
	Clean       bool
	Unavailable bool
	Files       []string
	Reason      string
}

// testMerge fetches the pull head and the base branch and runs a read-only
// `git merge-tree` of the exact SHAs the poll saw. It never touches the
// working tree or index. A failure to fetch is tolerated when the objects are
// already local; it only matters if merge-tree then cannot find them.
func (w *Watcher) testMerge(ctx context.Context, snap Snapshot) mergeTest {
	_, fetchErr := w.exec(ctx, Command{
		Name: w.cfg.Git, Dir: w.cfg.Dir, Env: w.cfg.Environ,
		Args: []string{"fetch", "--quiet", "origin", snap.BaseRef, "refs/pull/" + strconv.Itoa(snap.Number) + "/head"},
	})
	out, err := w.exec(ctx, Command{
		Name: w.cfg.Git, Dir: w.cfg.Dir, Env: w.cfg.Environ,
		Args: []string{"merge-tree", "--write-tree", "--name-only", "--no-messages", snap.BaseOID, snap.Head},
	})
	var exit *ExitError
	switch {
	case err == nil:
		return mergeTest{Clean: true}
	case errors.As(err, &exit) && exit.Code == 1:
		return mergeTest{Files: parseMergeTreeFiles(out)}
	default:
		reason := clip(err.Error())
		if fetchErr != nil {
			reason += "; fetch: " + clip(fetchErr.Error())
		}
		return mergeTest{Unavailable: true, Reason: reason}
	}
}

// parseMergeTreeFiles extracts the conflicted paths from `git merge-tree
// --write-tree --name-only` output: a tree id line, then one path per line up
// to the first blank line. The paths are returned sorted.
func parseMergeTreeFiles(out []byte) []string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	var files []string
	for _, line := range lines[1:] {
		if line == "" {
			break
		}
		files = append(files, line)
	}
	sort.Strings(files)
	return files
}

// recipeFiles are the exact paths whose conflicts the recipes resolve
// mechanically. The content pin's constant lives in non-test Go, but the pin
// recipe re-pins it in the same step, so it is a recipe file too.
var recipeFiles = map[string]bool{
	"CHANGELOG.md": true,
	"go/internal/collector/repo/git/testdata/content_version_pin.json": true,
	"go/internal/collector/repo/git/snapshot_freshness.go":             true,
	"specs/live-postgres-readiness-weights.v1.json":                    true,
}

// classifyConflict returns 4a when every conflicted file is a recipe file:
// one of recipeFiles or a path matching the generated list. Anything else,
// including no files and any agent-rule file, is count-choice-lines.
func classifyConflict(files, generated []string) string {
	if len(files) == 0 {
		return recipeCount
	}
	for _, f := range files {
		if agentRuleFile(f) || !recipeFile(f, generated) {
			return recipeCount
		}
	}
	return recipeMechanical
}

// recipeFile reports whether a conflict in the file is a named recipe case.
func recipeFile(file string, generated []string) bool {
	if recipeFiles[file] {
		return true
	}
	for _, pattern := range generated {
		if ok, err := path.Match(pattern, file); err == nil && ok {
			return true
		}
	}
	return false
}

// agentRuleFile reports whether the file is one of the rule files the review
// regime runs on. A conflict there is never a recipe case: it needs the
// docs-contradiction check and, if it rewrites a rule, row 4b.
func agentRuleFile(file string) bool {
	base := path.Base(file)
	switch {
	case base == "AGENTS.md":
		return true
	case strings.HasPrefix(file, ".agents/skills/"),
		strings.HasPrefix(file, ".claude/rules/"),
		strings.HasPrefix(file, ".codex/"):
		return true
	case file == ".claude/settings.json", file == ".muse/hooks.json",
		file == "docs/internal/writing-for-agents.md":
		return true
	case path.Dir(file) == "docs/internal" && strings.HasPrefix(base, "agent-") && strings.HasSuffix(base, ".md"):
		return true
	}
	return false
}

// readGeneratedList reads the GENERATED array from the checkout's drift script.
// A missing or unparsable script yields no list, so generated paths then fall
// to count-choice-lines instead of being claimed as 4a.
func readGeneratedList(dir string) []string {
	raw, err := os.ReadFile(filepath.Join(dir, generatedListScript)) // #nosec G304 -- dir is the operator-supplied checkout; the script name is a constant.
	if err != nil {
		return nil
	}
	var list []string
	inArray := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case !inArray && strings.HasPrefix(line, "GENERATED=("):
			inArray = true
		case inArray && line == ")":
			return list
		case inArray && line != "" && !strings.HasPrefix(line, "#"):
			list = append(list, line)
		}
	}
	return nil
}

// conflictFields renders a test merge as event fields.
func conflictFields(m mergeTest, generated []string) []string {
	switch {
	case m.Unavailable:
		return []string{"test_merge", "unavailable", "reason", m.Reason, "recipe", recipeUnknown}
	case m.Clean:
		return []string{"test_merge", "clean", "recipe", recipeRebase}
	}
	files := m.Files
	more := ""
	if len(files) > 10 {
		more = fmt.Sprintf(",+%d more", len(files)-10)
		files = files[:10]
	}
	kv := []string{"test_merge", "conflict", "files", strings.Join(files, ",") + more, "recipe", classifyConflict(m.Files, generated)}
	agent := 0
	for _, f := range m.Files {
		if agentRuleFile(f) {
			agent++
		}
	}
	if agent > 0 {
		kv = append(kv, "agent_rule_files", strconv.Itoa(agent))
	}
	return kv
}
