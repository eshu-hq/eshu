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

	"gopkg.in/yaml.v3"
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
//   - A command starting with "cd " is skipped by extractScriptPaths BEFORE
//     tokenizing runs at all, not because tokenizing found no scripts/ token
//     — extractScriptPaths returns nil on that prefix unconditionally. Every
//     cd-prefixed command in the registry today carries no bare "scripts/"
//     token after the "cd" (verified: 0 of the registry's 20 cd-prefixed
//     local.command / local.test_command entries), so this coincides with
//     the token-based skip above in practice. This check no longer lets that
//     stay a SILENT trapdoor, though: hiddenScriptsInCdPrefixedCommand below
//     re-tokenizes a cd-prefixed command WITHOUT the skip and reports any
//     "scripts/" word it finds as its own drift error, so a future
//     "cd apps/console && bash scripts/x.sh" is a loud, actionable finding
//     instead of a silent exemption (#5934 review). TestDriftCheck_-
//     CdPrefixedCommandWithScriptsTokenReported pins that reporting;
//     extractScriptPaths itself is unchanged and still returns nil for the
//     command, so anything actually relying on extractScriptPaths elsewhere
//     is unaffected.
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
//   - The walk follows `source`/`.` edges only. When a gate's local.command is
//     a wrapper (scripts/dev/precommit-go.sh) that EXECUTES another
//     scripts/*.sh directly instead of sourcing it, that execution is
//     invisible here — sourcedScriptRE never matches an execution line, only
//     a `source`/`.` one (#5934 review, codex). This is real, not
//     hypothetical: audited every scripts/dev/precommit-go.sh subcommand a
//     gate's local.command invokes against the committed registry this
//     session and found two instances where the wrapper directly execs a
//     script neither its own triggers nor the sourcing walk could see —
//     perf-evidence's "perf-evidence)" case execs
//     scripts/verify-performance-evidence.sh, and nancy's "nancy)" case execs
//     scripts/dev/nancy-local.sh. Both are now closed the same way this
//     package always closes a check-8 finding: by adding the delegated script
//     as an explicit trigger on its gate (specs/ci-gates.v1.yaml), not by
//     teaching the walk to parse a wrapper's dispatch logic. A general
//     "does this wrapper execute that script" parser has the same unsound
//     shape the trivyskipdirs.go narrowings in this package's AGENTS.md spent
//     three review rounds discovering wasn't worth it: a wrapper can dispatch
//     through a case statement, a lookup table, or a computed path, and no
//     regex distinguishes a real invocation from a mention in a comment or
//     error string reliably. The check remains blind to a THIRD such instance
//     until it is found the same way — by reading the wrapper, not by
//     trusting this list is exhaustive.
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
			for _, hidden := range hiddenScriptsInCdPrefixedCommand(c.command) {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q %s (%q) is cd-prefixed and tokenizes to %q, a scripts/ path "+
						"extractScriptPaths never inspects because of the cd-prefix skip; add %q as an "+
						"explicit trigger of this gate (or widen extractScriptPaths) before relying on it",
					g.ID, c.field, c.command, hidden, hidden,
				))
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

// hiddenScriptsInCdPrefixedCommand returns every "scripts/"-prefixed word in
// a "cd "-prefixed command, the exact set extractScriptPaths (validate.go)
// unconditionally skips before it ever tokenizes such a command. That skip is
// intentional there (a word after "cd" is not confidently repo-root-relative,
// see extractScriptPaths' doc comment), but it must not also be silent here:
// nothing in the registry has such a token today, so this reports zero errors
// now, but the moment one is added it becomes a loud, actionable finding
// instead of a trapdoor check 8 quietly reproduces (#5934 review).
func hiddenScriptsInCdPrefixedCommand(command string) []string {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "cd ") {
		return nil
	}
	var out []string
	for _, w := range strings.Fields(trimmed) {
		if strings.HasPrefix(w, "scripts/") {
			out = append(out, w)
		}
	}
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

// checkCIScriptTriggerCoverage extends checkScriptTriggerCoverage's
// guarantee to the script CI actually executes, not only the local static
// mirror. The two are routinely different files: ifa-determinism's CI job
// runs the live driver scripts/verify-ifa-determinism.sh while
// local.command runs the static mirror scripts/test-verify-ifa-determinism.sh
// (specs/ci-gates.v1.yaml, ifa-determinism). checkScriptTriggerCoverage walks
// outward only from local.command/local.test_command, so a scripts/ token
// that appears ONLY in the CI job's own run: command -- or that command's own
// sourcing graph, at any depth -- was invisible to it. That gap is not
// hypothetical: it is why three cap-driven split files
// (ifa_deployable_unit_live_diagnostics.sh, ifa_deployable_unit_live_converge.sh,
// and a documentation-family cells split) shipped with no trigger row on a
// prior branch and had to be caught by hand, because the live driver sources
// them and nothing walked that graph.
//
// Narrowed to gates whose ci.workflow and ci.job both resolve, AND whose
// (workflow, job) pair is not shared with another gate. ci.workflow must
// name a readable, parseable file under .github/workflows/, and ci.job must
// be a literal key in that file's `jobs:` map -- a gate whose CI job is a
// matrix-dispatched check (ci.job names an append_gate display or a
// ${{ matrix.* }} job, not a literal jobs: key -- see checkJobNamesResolve)
// is skipped rather than false-flagged, since this check does not attempt
// the matrix-dispatch resolution checkPathFilterCoverage already owns for a
// different purpose.
//
// The shared-pair exclusion is required, not a nicety: a first version of
// this check attributed every `run:` step in the resolved job to whichever
// single gate named it, and measured against the committed registry that
// produced 352 false positives from exactly one shape -- test.yml's
// verify-contracts job is a single CI job that runs roughly thirty unrelated
// gates' verify scripts as separate named steps, and several other gates in
// the registry declare ci.job: "verify-contracts" as a shared backstop
// rather than a gate-dedicated job (go-core, docs-helm-hygiene, and several
// others follow the same pattern with different member counts). The exact,
// live member count for verify-contracts and every other shared pair is
// CIScriptTriggerCoverageSummary's own output, not a number restated here --
// see that function's doc comment for why a hardcoded count in prose drifts
// from the registry while a derived one cannot. Walking every step
// of a shared job and attributing it to one gate is exactly the wrong
// direction of narrowing: it invents a claim the registry itself does not
// make (that THIS gate owns THAT step), rather than skipping what genuinely
// cannot be attributed -- the same "carries no correspondence signal, so it
// is skipped rather than guessed" convention scriptWorkflowSoundSubset
// already applies to gate-vs-workflow attribution, applied here to
// gate-vs-job-step attribution instead. A (workflow, job) pair used by
// exactly one gate is unambiguous: every run: step in it is that gate's own,
// which is the ifa-determinism/ifa-fault-injection shape this check exists
// for (each has its own dedicated determinism-matrix / fault-injection job).
//
// Within a resolved, unambiguous job, only `run:` steps count -- exactly the
// same "only executable run: blocks count as executing it" convention
// checkVerifyScriptWorkflowMatch already applies for the same reason (a
// dorny/paths-filter entry watches a path, it does not invoke it).
//
// Shares extractScriptPaths, allSourcedScripts, and anyTriggerMatches with
// checkScriptTriggerCoverage, so it inherits every narrowing documented on
// that function's own doc comment (the cd-prefix skip, the literal-"scripts/"-
// text-only sourcing walk, and so on) applied per run: command instead of per
// local.command/local.test_command.
//
// Blind, visibly: this walks reg.Gates only, never hygiene_hooks. A
// hygiene_hooks entry is a bare id+reason pair (specs/ci-gates.v1.yaml's
// `hygiene_hooks:` list) -- it declares no ci.workflow, no ci.job, no
// local.command, nothing this check (or checkScriptTriggerCoverage) could
// walk a script from. That is why this check could not have caught
// prepr-stamp-verify's own self-test going unwired (#6149 follow-up item 8
// review, P1): the orphaned file was test-prepr-stamp-verify.sh, reachable
// from no gate's local.command/local.test_command and no CI job either, and
// hygiene_hooks is where prepr-stamp-verify already lived at the time,
// outside every walk in this file. Extending this check (or
// checkScriptTriggerCoverage) into hygiene_hooks would need those entries to
// name a script to walk from in the first place, which the schema does not
// carry today -- a real gap, left open rather than silently narrowed further,
// since closing it changes the hygiene_hooks schema, not this function's
// walk.
//
// The remedy that does not need a schema change: a hygiene hook that grows a
// testable script -- its own local.command, or a self-test like
// test-prepr-stamp-verify.sh -- should become a gate, not stay a hook. This
// is not hypothetical; it is what this same PR did four commits earlier --
// see prepr-stamp-verify-selftest (specs/ci-gates.v1.yaml). The underlying
// git pre-push hook (prepr-stamp-verify) correctly stays a hygiene_hooks
// entry -- it still has no testable local.command shape, the chicken-and-egg
// problem prepr-stamp-verify-selftest's own local_only_reason explains -- but
// its self-test did have a testable script and gained a real gate instead of
// staying invisible, which is why this check now sees it. A hygiene_hooks
// entry with nothing to test (a staged-file variant, a commit-msg-stage
// alias) has no remedy and stays a hook; one whose script gains a test does.
//
// Silent, until CIScriptTriggerCoverageSummary: the ONLY visible signal this
// check emits on success is DriftCheck's empty []error, so a clean run reads
// as "no drift" with no scope attached -- 40 of the registry's 93 CI-backed
// gates (test.yml/verify-contracts alone accounts for 11) sit on a shared
// (workflow, job) pair and are silently skipped by the exclusion above,
// leaving only 53 gates this check actually attributes drift to. The skip
// logic itself is correct and stays (see the false-positive count above);
// what was missing was a summary saying so. CIScriptTriggerCoverageSummary
// (below) reports the split so a caller can print "no drift among N
// attributable gates" instead of an unqualified "no drift" (#6149 follow-up
// item 8 review, P2(a)).
func checkCIScriptTriggerCoverage(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	jobGates := ciJobGateIDs(reg)

	var errs []error
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		if len(jobGates[ciJobKey{g.CI.Workflow, g.CI.Job}]) > 1 {
			// A CI job shared by more than one gate carries no per-gate
			// attribution signal -- see the shared-pair discussion above.
			continue
		}
		raw, err := os.ReadFile(filepath.Join(wfDir, g.CI.Workflow)) // #nosec G304 -- wfDir-confined, registry-declared filename
		if err != nil {
			// An unreadable or missing workflow file is a different check's
			// finding (checkWorkflowCompleteness / checkJobNamesResolve);
			// reporting it again here would duplicate, not add, signal.
			continue
		}
		var wf runWorkflowFile
		if yaml.Unmarshal(raw, &wf) != nil {
			continue
		}
		job, ok := wf.Jobs[g.CI.Job]
		if !ok {
			// ci.job did not resolve to a literal jobs: key -- a
			// matrix-dispatched check name, or a genuine mismatch
			// checkJobNamesResolve already reports. Neither is this check's
			// signal to add to.
			continue
		}
		for _, step := range job.Steps {
			if step.Run == "" {
				continue
			}
			for _, script := range extractScriptPaths(step.Run) {
				if !anyTriggerMatches(g.Triggers, script) {
					errs = append(errs, fmt.Errorf(
						"drift: gate %q ci job %q (workflow %s) runs %q but no trigger of that gate matches it (triggers: %v); "+
							"a PR editing only that script would not select the gate locally and would first fail in CI",
						g.ID, g.CI.Job, g.CI.Workflow, script, g.Triggers,
					))
				}
				for _, sourced := range allSourcedScripts(repoRoot, script) {
					if anyTriggerMatches(g.Triggers, sourced) {
						continue
					}
					errs = append(errs, fmt.Errorf(
						"drift: gate %q ci job %q (workflow %s, %s) sources %q but no trigger of that gate matches it (triggers: %v); "+
							"a PR editing only that sourced file would not select the gate locally and would first fail in CI",
						g.ID, g.CI.Job, g.CI.Workflow, script, sourced, g.Triggers,
					))
				}
			}
		}
	}
	return errs
}
