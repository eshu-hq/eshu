// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// sourcedScriptRE captures a repo-relative scripts/ path from a shell `source`
// or `.` line. It anchors on the sourcing keyword, so the `# shellcheck
// source=scripts/lib/...` directive that usually sits above such a line is not
// mistaken for an actual dependency.
var sourcedScriptRE = regexp.MustCompile(`(?m)^[ \t]*(?:\.|source)[ \t]+[^\n]*?(scripts/[A-Za-z0-9_./-]+)`)

// checkScriptTriggerCoverage reports every gate whose own local script — or a
// script that script sources — is not matched by any of that gate's triggers.
//
// A gate's triggers decide whether `make pre-pr` selects it for the changed
// paths. When the gate's verifier, its test mirror, or a case file one of them
// sources is not itself among those paths, a PR that changes only that file
// selects nothing: the local lane prints "SKIPPED <gate> — no trigger matched
// changed paths" and the first run of the edited script happens in CI. That is
// the shape CLAUDE.md's Verification Defaults rules out ("CI stays
// authoritative, but MUST NOT be the first place a credential-free failure
// appears").
//
// The check is deliberately narrow in three ways. It only looks at the script
// path extractScriptPath already derives for the existing on-disk validation,
// so an inline toolchain command ("cd go && go test ./...") is skipped — there
// is no file whose edit could go unselected. CI-only gates (Local == nil) are
// skipped for the same reason. And it follows sourcing one level deep, not
// transitively: every sourced file in this repo today is a leaf case file.
//
// It does NOT check the reverse direction. A trigger matching no file on disk
// is a separate concern, and a gate is free to declare triggers well beyond its
// own scripts.
func checkScriptTriggerCoverage(repoRoot string, reg *Registry) []error {
	var errs []error
	for _, g := range reg.Gates {
		if g.Local == nil {
			continue
		}
		for _, c := range []struct{ field, command string }{
			{"local.command", g.Local.Command},
			{"local.test_command", g.Local.TestCommand},
		} {
			if c.command == "" {
				continue
			}
			script := extractScriptPath(c.command)
			if script == "" {
				continue
			}
			if !anyTriggerMatches(g.Triggers, script) {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q %s runs %q but no trigger of that gate matches it (triggers: %v); "+
						"a PR editing only that script would not select the gate locally and would first fail in CI",
					g.ID, c.field, script, g.Triggers,
				))
			}
			for _, sourced := range sourcedScripts(repoRoot, script) {
				if anyTriggerMatches(g.Triggers, sourced) {
					continue
				}
				errs = append(errs, fmt.Errorf(
					"drift: gate %q %s (%s) sources %q but no trigger of that gate matches it (triggers: %v); "+
						"a PR editing only that sourced file would not select the gate locally and would first fail in CI",
					g.ID, c.field, script, sourced, g.Triggers,
				))
			}
		}
	}
	return errs
}

// sourcedScripts returns the sorted, repo-relative scripts/ paths that script
// sources. An unreadable script yields no paths: its absence is already
// reported by Validate's on-disk check, and double-reporting it here would
// bury the real finding.
func sourcedScripts(repoRoot, script string) []string {
	raw, err := os.ReadFile(filepath.Join(repoRoot, script)) // #nosec G304 -- repoRoot is the operator-provided repo root; script comes from the committed registry
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	for _, m := range sourcedScriptRE.FindAllSubmatch(raw, -1) {
		seen[string(m[1])] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// anyTriggerMatches reports whether path matches at least one trigger glob.
func anyTriggerMatches(triggers []string, path string) bool {
	for _, t := range triggers {
		if MatchGlob(t, path) {
			return true
		}
	}
	return false
}
