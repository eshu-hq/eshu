// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// sourcedScriptRE captures a repo-relative scripts/ path from a shell `source`
// or `.` line. It anchors on the sourcing keyword, so the `# shellcheck
// source=scripts/lib/...` directive that usually sits above such a line is not
// mistaken for an actual dependency.
var sourcedScriptRE = regexp.MustCompile(`(?m)^[ \t]*(?:\.|source)[ \t]+[^\n]*?(scripts/[A-Za-z0-9_./-]+)`)

// checkScriptTriggerCoverage reports every gate whose own local script — or a
// script that script sources, at any depth — is not matched by any of that
// gate's triggers.
//
// A gate's triggers decide whether `make pre-pr` selects it for the changed
// paths. When the gate's verifier, its test mirror, or a case file any of
// them sources is not itself among those paths, a PR that changes only that
// file selects nothing: the local lane prints "SKIPPED <gate> — no trigger
// matched changed paths" and the first run of the edited script happens in
// CI. That is the shape CLAUDE.md's Verification Defaults rules out ("CI
// stays authoritative, but MUST NOT be the first place a credential-free
// failure appears").
//
// This check has several narrowings, some intentional and some accidental —
// see each item below for which. The list is not a closed count either way;
// do not summarize it as "narrow in N ways" in a comment or commit message. A
// narrowing has been missed twice already (#5762 round 10).
//
//   - It only looks at the scripts/ tokens extractScriptPaths derives from
//     each command, once tokenized. A command with no such token (e.g. "cd go
//     && go vet ./...") is skipped — there is no file whose edit could go
//     unselected.
//   - A command starting with "cd " is skipped BEFORE tokenizing runs at all,
//     not because tokenizing found no scripts/ token — extractScriptPaths
//     returns nil on that prefix unconditionally. Every cd-prefixed command
//     in the registry today carries no bare "scripts/" token after the "cd"
//     (verified: 0 of the registry's 20 cd-prefixed local.command /
//     local.test_command entries), so this coincides with the token-based
//     skip above in practice, but a future
//     "cd apps/console && bash scripts/x.sh" would be exempted too, silently.
//     TestDriftCheck_CdPrefixedCommandWithScriptsTokenSkipped pins today's
//     exemption so a reader sees it demonstrated, not only asserted.
//   - CI-only gates (Local == nil) are skipped: no local command means no
//     script to derive.
//   - heredoc-budget's local.command references a scripts/ file through a
//     relative token ("-baseline ../scripts/heredoc-budget-baseline.txt")
//     that extractScriptPaths does not recognise — it only matches a leading
//     "scripts/" prefix, not "../scripts/". That file is already an explicit
//     trigger on the heredoc-budget gate (specs/ci-gates.v1.yaml), so nothing
//     goes unselected today; widen extractScriptPaths if that stops being
//     true.
//   - sourcedScripts (and therefore the transitive walk below) only resolves
//     a `source`/`.` line whose LITERAL TEXT contains "scripts/". A line that
//     sources a computed or variable path — for example
//     `. "${script_dir}/lib/maturity-drift-guard-language-extensions.sh"` in
//     scripts/verify-maturity-drift-guard.sh — is invisible to the walk, at
//     any depth: sourcedScriptRE never matches it, so the walk cannot find
//     the file to check it against. Measured this session by walking every
//     script reachable from a gate's own local.command/local.test_command (as
//     this check itself does) and counting `source`/`.` lines with and
//     without literal "scripts/" text in the target: 37 unresolved lines
//     against 27 resolved, across 131 reachable scripts. (Not cited as a
//     runnable command here — the throwaway test that produced it is not
//     committed, and a reader running a `-run` filter that matches nothing
//     gets a silently passing `ok`, which is the exact false-green shape
//     #5762 exists to remove.) Counting only the lines in a gate's OWN entry
//     script — the file its local.command or local.test_command names
//     directly, NOT the case libs that file transitively sources — 11 such
//     lines name a lib this way, reached by 7 gates:
//     parser-relationship-kit, operator-dashboard and maturity-drift-guard
//     (one line each), telemetry-coverage (two), and three gates that share
//     two test harnesses — scripts/test-verify-golden-corpus-gate.sh carries
//     five and is BOTH golden-corpus-mirror's local.command and
//     golden-corpus-gate's local.test_command, and
//     scripts/test-verify-ifa-fault-injection.sh carries one. That sharing is
//     why 11 distinct lines make 16 gate-line pairs. All 16 pairs resolve to
//     a file that is independently an explicit trigger on that gate (verified
//     against specs/ci-gates.v1.yaml), so no gate is false-green from this
//     gap today. The other 26 of the 37 sit in transitively-sourced case
//     libs and split two ways, neither of them a majority: 11 are `. "$1"`
//     dispatch, which names no fixed target to check, and 15 name a lib
//     through a variable (timing_lib eight times, collector_settle_lib six,
//     demotion_lib once). 14 of those 15 inherit the variable from an
//     ancestor file instead of assigning it locally, so resolving them needs
//     cross-file dataflow, not just a wider regex. All three of those libs
//     are themselves matched by the scripts/lib/golden-corpus-*.sh trigger on
//     both gates that reach them, so the remainder is not false-green either.
//     The gap is real, not hypothetical, and this delta does not close it:
//     widening sourcedScriptRE to resolve computed paths is a separate
//     change.
//     TestDriftCheck_VariableSourcedFileNotResolved pins the boundary with an
//     uncovered `${script_dir}`-sourced lib that produces 0 check-8 errors.
//   - It does NOT check the reverse direction. A trigger matching no file on
//     disk is a separate concern, and a gate is free to declare triggers well
//     beyond its own scripts.
//
// Within those narrowings, sourcing IS followed transitively, with a visited
// set so a sourcing cycle cannot hang the walk — no script in this repo
// sources one that sources it back today, but the walk must stay safe if that
// changes. golden-corpus-lock-cases.sh is the real transitivity case:
// scripts/test-verify-golden-corpus-gate.sh sources it directly, and it in
// turn sources golden-corpus-lock-parse-cases.sh and
// golden-corpus-lock-race-cases.sh, neither of which the gate's local.command
// names directly. Both are resolved (their source lines carry the literal
// "scripts/" text) and are checked exactly like a directly sourced file once
// the walk reaches them.
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
			for _, script := range extractScriptPaths(c.command) {
				if !anyTriggerMatches(g.Triggers, script) {
					errs = append(errs, fmt.Errorf(
						"drift: gate %q %s runs %q but no trigger of that gate matches it (triggers: %v); "+
							"a PR editing only that script would not select the gate locally and would first fail in CI",
						g.ID, c.field, script, g.Triggers,
					))
				}
				for _, sourced := range allSourcedScripts(repoRoot, script) {
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
	}
	return errs
}

// allSourcedScripts returns the sorted, repo-relative scripts/ paths reachable
// from script by following `source`/`.` lines transitively. It walks with a
// visited set keyed on every script seen, starting with script itself, so a
// sourcing cycle cannot loop forever — no script in this repo sources one that
// sources it back today, but the walk must not hang if that changes. script
// itself is never in the result; only what it (transitively) sources is.
func allSourcedScripts(repoRoot, script string) []string {
	visited := map[string]struct{}{script: {}}
	found := make(map[string]struct{})
	queue := []string{script}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, sourced := range sourcedScripts(repoRoot, cur) {
			if _, ok := visited[sourced]; ok {
				continue
			}
			visited[sourced] = struct{}{}
			found[sourced] = struct{}{}
			queue = append(queue, sourced)
		}
	}
	out := make([]string, 0, len(found))
	for p := range found {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// sourcedScripts returns the sorted, repo-relative scripts/ paths that script
// itself sources, one level deep. allSourcedScripts calls this once per
// script discovered to build the full transitive closure. An unreadable
// script yields no paths: its absence is already reported by Validate's
// on-disk check, and double-reporting it here would bury the real finding.
func sourcedScripts(repoRoot, script string) []string {
	// script is a regex capture from a shell `source`/`.` line (sourcedScriptRE
	// above), not a value the committed registry itself controls -- its
	// character class allows "." and "/" after the literal "scripts/" prefix,
	// so a source line such as "source scripts/../../../etc/passwd" captures
	// a script argument that filepath.Join cleans to a path OUTSIDE repoRoot.
	// Reject that before opening anything: an escaping path is never a real
	// in-repo script, so it is treated the same as an unreadable one (#5934
	// review).
	full := filepath.Join(repoRoot, script)
	root := filepath.Clean(repoRoot)
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return nil
	}
	raw, err := os.ReadFile(full) // #nosec G304 -- full is confirmed to resolve within repoRoot above
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
