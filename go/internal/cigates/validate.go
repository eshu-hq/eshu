// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Validate performs integrity checks on the registry against the actual files
// on disk at repoRoot. It accumulates all errors rather than stopping at the
// first. A nil or empty error slice means the registry is consistent with the
// repository.
//
// Checks performed (#4213 AC):
//   - For gates with Local set: the leading script path in Local.Command (and
//     Local.TestCommand, when present) exists on disk relative to repoRoot.
//   - For gates with CI.Workflow set: the workflow file exists under
//     .github/workflows/ relative to repoRoot.
//   - Every gate trigger names something real: a literal trigger is stat-checked
//     (#6055), a glob trigger must select at least one tracked path (#6159).
//
// CI-only gates (Local==nil) skip the script check but still require the
// workflow file to be present.
func (r *Registry) Validate(repoRoot string) []error {
	errs := r.ValidateRequiredStatusChecks()
	// Enumerated once for the whole registry, not once per gate or per
	// trigger: ~500 glob triggers resolve against the same ~20k-path universe.
	// A failure here is reported once and leaves tracked nil, which suppresses
	// the per-trigger glob checks that could no longer be trusted — the run
	// still fails, on the enumeration error rather than on 500 derived ones.
	var tracked *trackedPaths
	if r.hasGlobTrigger() {
		loaded, err := loadTrackedPaths(repoRoot)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"registry glob triggers cannot be verified: %w — an unverifiable trigger must not read as present",
				err,
			))
		}
		tracked = loaded
	}
	for _, g := range r.Gates {
		if g.Local != nil {
			if err := checkScript(repoRoot, g.ID, g.Local.Command); err != nil {
				errs = append(errs, err)
			}
			if g.Local.TestCommand != "" {
				if err := checkScript(repoRoot, g.ID, g.Local.TestCommand); err != nil {
					errs = append(errs, err)
				}
			}
		}
		if g.CI.Workflow != "" {
			wfPath := filepath.Join(repoRoot, ".github", "workflows", g.CI.Workflow)
			if _, err := os.Stat(wfPath); os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("gate %q: workflow file %q not found", g.ID, wfPath))
			}
		}
		errs = append(errs, checkTriggerPathsExist(repoRoot, g, tracked)...)
	}
	for _, check := range r.RequiredStatusChecks {
		if check.Workflow == "" {
			continue
		}
		wfPath := filepath.Join(repoRoot, ".github", "workflows", check.Workflow)
		if _, err := os.Stat(wfPath); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf(
				"required status context %q: workflow file %q not found",
				check.Context,
				wfPath,
			))
		}
	}
	return errs
}

// checkTriggerPathsExist validates that every registry trigger of gate g
// resolves to something real at repoRoot. A trigger activates the gate
// when the named path changes; a trigger whose target was deleted, renamed,
// or moved outside a tracked restructure leaves the gate silently unable to
// ever select on that surface again, with no signal anywhere that it went
// stale. This is the existence-side counterpart to checkPathFilterCoverage:
// that check requires a CI workflow with a resolvable dorny/paths-filter
// mapping and only fires for gates wired that way, while this one runs for
// every gate regardless of CI wiring — it needs nothing but the trigger
// string and a stat call, so it belongs in the base (always-on) Validate
// pass rather than behind the --drift flag.
//
// The two trigger shapes need different evidence, so they are checked
// differently. A LITERAL trigger (see isLiteralTrigger in pathfilter.go) names
// one path and is stat-checked here, along with the containment and
// unresolvable-symlink guards below. A GLOB trigger names a set, and is
// resolved against the tracked path universe by checkGlobTriggerResolves
// (globtrigger.go, #6159); it used to be skipped outright, on the since-
// disproven reasoning that a glob matching nothing was valid future-proofing.
func checkTriggerPathsExist(repoRoot string, g Gate, tracked *trackedPaths) []error {
	var errs []error
	// Resolved once: repoRoot is constant for the whole run, and resolving it
	// per trigger repeated the same filesystem walk for every literal trigger
	// in the registry. A repoRoot that will not resolve is a property of the
	// caller's tree rather than of any one trigger, so it is not reported
	// per-trigger; falling back to the unresolved root still compares two
	// paths and only loses the ability to see through a symlinked root, which
	// makes the containment check stricter, never weaker.
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRoot = repoRoot
	}
	for _, trigger := range g.Triggers {
		if !isLiteralTrigger(trigger) {
			if err := checkGlobTriggerResolves(tracked, g.ID, trigger, repoRoot); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		full := filepath.Join(repoRoot, filepath.FromSlash(trigger))
		// path/filepath.Join concatenates every non-empty element with a
		// separator and then Cleans the result. Two consequences, and reviewers
		// have twice guessed the second one backwards, so both are spelled out:
		//
		//	Join("/repo", "../etc/passwd") == "/etc/passwd"    // Clean resolves ".." -- ESCAPES
		//	Join("/repo", "/etc/passwd")   == "/repo/etc/passwd" // absolute is NOT a reset -- stays inside
		//
		// Discarding everything before a later absolute element is Python's
		// os.path.join and Node's path.join, not Go's -- an easy cross-language
		// mix-up. So only the traversal case can leave repoRoot; an absolute
		// trigger just fails the existence check like any other bad path.
		//
		// Stat-ing an escaped path would let a malformed trigger "exist"
		// against an unrelated host file and pass the very staleness check this
		// function adds. Treat an escaping trigger as a failure of its own,
		// naming it, rather than silently stat-ing outside the tree.
		if !isWithinRoot(repoRoot, full) {
			errs = append(errs, fmt.Errorf(
				"gate %q: trigger %q resolves to %q, outside the repository root %q — a trigger must name a path inside the repo",
				g.ID, trigger, full, repoRoot,
			))
			continue
		}
		// Every stat outcome is accounted for. Treating only IsNotExist as a
		// finding and letting every other error fall through would pass a
		// trigger this function never actually validated — a permission denial
		// or a symlink loop's ELOOP would read as "present". That is the same
		// fail-open shape the existence check itself was added to remove.
		info, err := os.Stat(full)
		if err != nil {
			if os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf(
					"gate %q: trigger %q names %q, which does not exist — a stale trigger silently stops selecting this gate instead of failing loud",
					g.ID, trigger, full,
				))
				continue
			}
			errs = append(errs, fmt.Errorf(
				"gate %q: trigger %q names %q, which could not be checked: %w — an unverifiable trigger must not read as present",
				g.ID, trigger, full, err,
			))
			continue
		}
		// isWithinRoot above is lexical, and os.Stat follows symlinks, so a
		// committed symlink pointing out of the tree (dir -> /etc) would let a
		// lexically-contained trigger like "dir/passwd" satisfy the existence
		// check against a host file — the staleness check silently failing
		// open, which is the defect class this gate exists to remove. Resolving
		// happens only after the path is known to exist, because
		// EvalSymlinks errors on a missing path and would otherwise mask the
		// clearer stale-trigger message above.
		resolved, err := filepath.EvalSymlinks(full)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"gate %q: trigger %q names %q, whose symlinks could not be resolved: %w — containment cannot be confirmed, so the trigger must not pass silently",
				g.ID, trigger, full, err,
			))
			continue
		}
		if !isWithinRoot(resolvedRoot, resolved) {
			errs = append(errs, fmt.Errorf(
				"gate %q: trigger %q resolves through a symlink to %q, outside the repository root %q — a trigger must name a path inside the repo",
				g.ID, trigger, resolved, resolvedRoot,
			))
			continue
		}
		// Existing on disk is necessary but not sufficient: a literal trigger
		// naming a DIRECTORY passes every check above and still can never
		// select its gate. Select matches triggers against changed paths, and
		// those are files — `git diff --name-only` and GitHub's pull-files
		// response both name files, never the directories containing them —
		// so MatchGlob("go/internal/cigates", "go/internal/cigates/glob.go")
		// is false and always will be. #6055 accepted a directory here
		// because os.Stat does; that was the same mistake the glob half made
		// by deriving ancestor directories into its universe, and it is
		// corrected on both halves together so the two shapes answer the same
		// question (#6223 review). Reported last so an escaping or
		// unresolvable trigger still gets its own, more specific message.
		//
		// The committed registry carries no such trigger today (measured:
		// 0 of 916 literal triggers stat as a directory), so this closes the
		// shape rather than fixing a live break.
		if info.IsDir() {
			errs = append(errs, fmt.Errorf(
				"gate %q: trigger %q names the directory %q — selection only ever sees file paths, so a trigger naming a directory can never select this gate; write %q instead",
				g.ID, trigger, full, trigger+"/**",
			))
		}
	}
	return errs
}

// isWithinRoot reports whether cleaned path full sits inside root. It is used to
// reject registry triggers that escape the repository via "..", which would
// otherwise be stat-checked against unrelated host files.
func isWithinRoot(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkScript verifies that the leading script path in a local command exists
// on disk. For inline go-toolchain commands (e.g. "cd go && go test ...") the
// extractor returns "" and the check is skipped — those commands do not
// reference a file path that can be stat-checked.
func checkScript(repoRoot, gateID, command string) error {
	scriptPath := extractScriptPath(command)
	if scriptPath == "" {
		// Inline toolchain command (cd go && go …) or unrecognised pattern —
		// no script file to verify.
		return nil
	}
	full := filepath.Join(repoRoot, scriptPath)
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return fmt.Errorf("gate %q: script %q not found (derived from command %q)", gateID, full, command)
	}
	return nil
}

// extractScriptPaths returns every repo-relative scripts/ token in command,
// in left-to-right order, or nil when command references no scripts/ file
// (e.g. an inline go-toolchain invocation like "cd go && go test ..."). A
// gate's local command can chain more than one scripts/ invocation with
// "&&" — frontend-console-checks and ci-gate-registry both do — and every one
// of them is a file a caller may need to see, not only the first.
//
// Recognised patterns:
//
//   - "bash scripts/foo.sh [args]"       → ["scripts/foo.sh"]
//   - "scripts/foo.sh [args]"            → ["scripts/foo.sh"]
//   - "a && bash scripts/b.sh"           → ["scripts/b.sh"] (every scripts/ token)
//   - "cd go && go test ..."             → nil (inline go command, no script)
//   - "cd go && go run ..."              → nil (inline go command, no script)
//
// Only words beginning with "scripts/" are treated as script refs. Words that
// start with "go/" are Go package paths passed to the toolchain, not files to
// stat. A word that references a scripts/ file through a relative prefix
// ("../scripts/foo.txt") is not recognised — heredoc-budget's local.command
// is the one gate command in the registry with that shape, and its file is
// covered by an explicit registry trigger instead of being derived here (see
// checkScriptTriggerCoverage's doc comment in scripttrigger.go, which also
// documents this function's other narrowings in full).
//
// A command starting with "cd " is skipped BEFORE the word loop below runs at
// all — not because that loop found no "scripts/" word. A "cd "-prefixed
// command that does carry a later "scripts/" token (none do in the committed
// registry today) is skipped just the same. This function's own skip is
// unchanged and stays silent, by design — extractScriptPaths only derives the
// path Validate stat-checks for existence. checkScriptTriggerCoverage
// (scripttrigger.go) is what must not stay silent about it, and no longer
// does: hiddenScriptsInCdPrefixedCommand there re-tokenizes a cd-prefixed
// command WITHOUT this skip and reports any "scripts/" word as its own check-8
// drift finding (#5934 review), so a future cd-prefixed command with a real
// scripts/ token is a loud finding instead of a trapdoor.
func extractScriptPaths(command string) []string {
	trimmed := strings.TrimSpace(command)

	// Skip before tokenizing: a "cd "-prefixed command is an inline shell
	// pipeline, and any word after the "cd" — including one that looks like a
	// scripts/ path — is not confidently repo-root-relative. Callers treat
	// that as "no script to check" rather than erroring on it.
	if strings.HasPrefix(trimmed, "cd ") {
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

// extractScriptPath returns the leading repo-relative script path from a
// shell command string — the one Validate stat-checks for existence — or ""
// when the command references no scripts/ file. Use extractScriptPaths
// directly when every scripts/ token in a compound command matters, not only
// the first.
func extractScriptPath(command string) string {
	paths := extractScriptPaths(command)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
