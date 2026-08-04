// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Paths of the four artifacts checkTrivySkipDirsParity wires together:
// specs/trivy-skip-dirs.txt is the single authoritative skip-dirs list (one
// directory per line, '#' comments allowed); scripts/lib/trivy-skip-dirs.sh is
// the single shared derivation that reads it and prints the comma-joined
// list; the local wrapper and the CI workflow must each be provably wired to
// INVOKE THE HELPER, not to independently derive or hard-code a value
// (#5925 F2). trivyCIJobKey is the trivy-fs job's key (not its display
// `name:`) in trivyCIWorkflowPath -- security-scan.yml distinguishes the
// filesystem scan from the image scan by JOB (trivy-fs vs. trivy-image), not
// by a field within a shared job, so locating the right job by key is
// sufficient to find the right step.
const (
	trivySkipDirsSpecPath   = "specs/trivy-skip-dirs.txt"
	trivySkipDirsHelperPath = "scripts/lib/trivy-skip-dirs.sh"
	trivyLocalScriptPath    = "scripts/dev/trivy-fs-local.sh"
	trivyCIWorkflowPath     = ".github/workflows/security-scan.yml"
	trivyCIJobKey           = "trivy-fs"
	trivyActionUsesPrefix   = "aquasecurity/trivy-action"
)

// trivySourceLineRE matches a bash `source <path>` or `. <path>` command
// whose argument names trivySkipDirsHelperPath -- so a mention of the
// helper's path inside a string literal, an echo, or (already excluded by
// stripWholeLineComments) a comment does not match, only the shell construct
// that actually loads the helper's function definitions into the running
// shell (#5927 review). It anchors on the command word at the start of a
// line (after optional indentation) and requires the very next
// whitespace-delimited token to contain the path, so a source line for an
// unrelated file followed by a same-line trailing mention of this path does
// not match either.
//
// FAIL-CLOSED LIMITATION (P3-1, #5927 round-5 review): anchoring on the
// start of a line, rather than on "the start of a shell command" wherever
// one may begin, also rejects a `source` that is a real, live command but
// does not sit at column 0 of its own line -- e.g. after a `;`, `&&`, or
// `||` on a shared line, inside a `{ }` or `( )` group written on one line,
// after a line continuation (`\` at end of line), or inside `if …; then
// source scripts/lib/trivy-skip-dirs.sh; fi`. None of these are hypothetical
// shapes a real script could never take; they are ordinary bash. The
// consequence is fail-closed and symmetric with the rest of this package's
// design: a real committed script written in one of these forms reads as
// NOT sourcing the helper and fails the drift gate loudly, a false alarm a
// maintainer can see and fix by moving the `source` to its own line, rather
// than a script that only claims to source the helper passing silently.
// Recognizing a command anywhere a shell could start one needs the same
// command-separator tracking this package's design exists to avoid (see the
// top of this file and the retired trivyOutputAssignmentPattern in
// trivyskipdirs_ci.go); trivyPipefailRE below shares the identical
// limitation for the same reason.
var trivySourceLineRE = regexp.MustCompile(
	`(?m)^[ \t]*(?:source|\.)[ \t]+\S*` + regexp.QuoteMeta(trivySkipDirsHelperPath) + `\S*`,
)

// trivySkipDirsCsvCallRE matches a call to the helper's exported function,
// trivy_skip_dirs_csv, as a whole word -- so the identifier appearing inside
// an unrelated longer word does not match.
var trivySkipDirsCsvCallRE = regexp.MustCompile(`\btrivy_skip_dirs_csv\b`)

// trivyPipefailRE matches a `set` command line that establishes pipefail --
// `set -euo pipefail`, the narrower `set -o pipefail`, or any other flag
// combination naming `pipefail` -- anchored on the command word at the start
// of a line, the same convention trivySourceLineRE uses, and the same
// fail-closed limitation documented on trivySourceLineRE above: a live `set
// -o pipefail` after a `;`/`&&`/`||`, inside a one-line `{ }`/`( )` group, or
// inside `if …; then set -o pipefail; fi` is not at the start of its own
// line and will not match, reading as absent rather than present. Without
// it, an unreadable specs file failing inside trivy_skip_dirs_csv's
// `sed | grep | grep | paste` pipeline would not fail the local script: the
// pipeline's exit status is `paste`'s (0), so the script would silently
// derive an empty skip-dirs value instead of failing loudly. This is the
// local-script half of the same failure-mode-parity requirement
// checkCIWorkflowSkipDirsFromHelper enforces CI-side via `shell: bash`
// (#5925 F12, single-source review P2-3: local and CI must not diverge on
// failure mode).
//
// `[^\n#;&|]*` (not `[^\n]*`) stops at '#' as well as end-of-line -- round-3
// review P2-2, the same trailing-comment hole the now-retired
// trivyOutputAssignmentPattern (#5927, removed round 5) had: without it,
// `set -x   # remember to add pipefail here` or
// `set -e  # pipefail intentionally omitted` would both read as establishing
// pipefail because "pipefail" appears LATER on the physical line, inside a
// trailing comment that is never part of the `set` command's own arguments.
//
// It also stops at ';', '&', and '|' -- round-6 review P3-1: the round-3
// class stopped at '#' but not at a command separator, so any line
// STARTING with `set` that later mentions "pipefail" outside a comment, in
// an unrelated command sharing the same physical line, read as establishing
// it -- the opposite failure direction from every other limitation
// documented in this file: fail-OPEN, not fail-closed. Proven against this
// regex directly: `set -eu; printf "remember pipefail\n"` matched before
// this fix even though pipefail is never set, because `[^\n#]*` freely
// crossed the `;` to reach "pipefail" inside the unrelated `printf` call's
// string argument. Stopping the run at ';', '&', and '|' -- the shell's
// command separators -- confines the match to the `set` command's own
// argument list: that same input now correctly does not match, while
// `set -euo pipefail`, `set -o pipefail`, and the trailing-comment cases
// round 3 closed are unaffected, because in each of those the only
// characters between `set` and `pipefail` are the flags themselves. This
// narrows, rather than closes, the class the FAIL-CLOSED limitation above
// documents: a live `set -o pipefail` written after one of these
// separators on a shared line still does not match (fails closed, a false
// alarm), and a bare mention of "pipefail" after one of these separators on
// a `set`-prefixed line no longer matches either (closing the fail-open
// gap this fix targets). It does not attempt the full command-separator
// tracking this package's design exists to avoid -- e.g. one of these
// characters appearing inside a quoted string between `set` and `pipefail`
// would still stop the match early, the same fail-closed shape as the rest
// of this file.
var trivyPipefailRE = regexp.MustCompile(`(?m)^[ \t]*set\b[^\n#;&|]*\bpipefail\b`)

// checkTrivySkipDirsParity validates that scripts/lib/trivy-skip-dirs.sh,
// scripts/dev/trivy-fs-local.sh, and .github/workflows/security-scan.yml's
// trivy-fs job are all provably wired to specs/trivy-skip-dirs.txt, the
// single authoritative skip-dirs list -- the helper being the ONE place that
// actually reads the specs file, and the other two being wired to the helper
// rather than to the specs file directly.
//
// This check used to compare two independently-maintained skip-dirs string
// literals -- one bash, one YAML -- for set equality. #5925 shipped that
// shape: a single column-0 `skip_dirs="..."` capture on the bash side, a
// `skip-dirs:` capture on the YAML side, and a set comparison between them.
// Successive attempts to also prove each literal GOVERNED its own scan --
// flag arity, invocation arity, comment stripping -- were tried and
// discarded here rather than shipped; across three review rounds (#5925) that
// direction accumulated eighteen ways to defeat it, and round 3's findings
// were rounds 1 and 2's re-expressed in different bash syntax. That is the
// proof the "parse bash correctly" bar was itself the unsound part, not any
// one fix.
//
// The redesign that followed removed the parsing problem instead of chasing
// it further: specs/trivy-skip-dirs.txt became the single authoritative
// skip-dirs list, and the local script and CI workflow each had to prove they
// read it. That first pass still left the local script and the CI workflow
// each carrying their OWN copy of the `grep -v '^#' | grep -v '^$' | paste -sd,
// -` derivation pipeline -- identical today, but nothing enforced that, and a
// change to one side (e.g. adding inline-comment support) would silently
// diverge the two without failing any check: a smaller-scale version of the
// original two-string-literals bug. scripts/lib/trivy-skip-dirs.sh (#5925 F2)
// removes that too: it is the ONE place the derivation pipeline exists, and
// both consumers must be provably wired to INVOKE it rather than to carry
// their own copy.
//
// IMPORTANT BOUNDARY: this check proves WIRING, not VALUE FLOW. It confirms
// that the helper reads the specs file, and that the local script and the CI
// workflow's producer step each `source` the helper and call the function it
// defines, trivy_skip_dirs_csv (checkSourcedAndCalled) -- not that nothing
// downstream of that call later overwrites the value it produced. A producer
// step that correctly sources and calls the helper and then appends a second
// line like `echo "dirs=." >> "$GITHUB_OUTPUT"`, or a local script that calls
// the helper and then reassigns `skip_dirs="."` on the next line, is
// deliberately out of scope: proving that would mean interpreting shell
// control and data flow, exactly the unsound direction the original
// bash-parsing design kept being pulled back into across all three review
// rounds. That class of change is a downstream mutation of an otherwise
// correctly-wired invocation, and it would show up in review of a
// security-workflow diff the same way any other suspicious edit to a CI
// security gate would.
//
// checkSourcedAndCalled (#5927 review) replaced an earlier, weaker shape: the
// producer step and local script used to only need to MENTION the helper's
// path anywhere in whole-line-comment-stripped content (strings.Contains /
// bare reference-counting), so a mention inside a string literal or an echo
// -- while a hard-coded value governed the actually emitted skip-dirs --
// read as "invokes the helper". Requiring proof of BOTH a
// `source` (or `. `) line AND a call to trivy_skip_dirs_csv closes that class
// for the SOURCE half structurally: trivySourceLineRE anchors on the command
// word at the start of a line, so a bare mention that is not itself a
// `source`/`.` command -- including one placed in a trailing comment on the
// SAME line as unrelated code -- never matches. The CALL half
// (trivySkipDirsCsvCallRE) is a plain word-bounded identifier match over the
// same whole-line-comment-stripped content, so it keeps the same narrower,
// long-standing residual gap the rest of this file accepts: a mention of
// `trivy_skip_dirs_csv` in a trailing comment on the SAME line as otherwise
// unrelated code is not (in principle) distinguished from a live call.
// Closing that would mean reintroducing the mid-line bash parsing this
// redesign exists to avoid; it is left as the same kind of review-visible
// risk as the downstream-mutation gap above, not a defect in this check.
//
// The check makes four assertions, in order:
//
//  1. Does the specs file exist, is it non-empty, does it have no duplicate
//     entries, no entry with leading/trailing whitespace (which the shell and
//     Go derivations could tokenize differently), no entry containing a
//     comma (the delimiter the shared derivation's `paste -sd, -` joins
//     entries with, so an embedded comma would smuggle extra entries past
//     every other per-entry check), no entry containing a '#' (only a
//     WHOLE-LINE comment is supported, never a trailing one), no entry
//     containing a glob metacharacter ('*', '?', '[', ']', '{', '}' -- a
//     literal repo-relative path is required, not a pattern), no entry that
//     NORMALIZES (filepath.Clean, the way trivy's own CleanSkipPaths does) to
//     a catch-all ("." or "") that would disable the scan's coverage
//     entirely, no entry that normalizes to ".." or a path escaping the
//     repository root via a leading "../" -- rejected for a narrower,
//     DIFFERENT reason its own error message states: proven against real
//     trivy 0.72.0, ".." alone does not disable coverage the way "." does,
//     it is meaningless because it escapes the root the list is defined
//     relative to -- and no entry that IS ITSELF an absolute path (a leading
//     "/")? That last check runs on the RAW, un-normalized entry, not the
//     normalized value the catch-all and escape-root checks above use:
//     filepath.Clean plus a trimmed leading "/" would otherwise strip an
//     entry's leading "/" before either of those questions is asked, so
//     "/etc" would normalize to the same "etc" a legitimate relative entry
//     would and read as accepted -- it is not; a leading "/" is rejected by
//     its own, separate check on the entry as written.
//     (trivySkipDirsSpecEntries)
//  2. Does scripts/lib/trivy-skip-dirs.sh reference the specs file's path
//     exactly once, over whole-line-comment-stripped content?
//     (checkFileReferencesPathOnce)
//  3. Does scripts/dev/trivy-fs-local.sh both `source` the HELPER exactly
//     once and call trivy_skip_dirs_csv exactly once, over
//     whole-line-comment-stripped content -- not merely mention the helper's
//     path once, and not zero or more than one of either half -- and does it
//     `set -o pipefail` (e.g. `set -euo pipefail`), the local-script half of
//     the same failure-mode-parity requirement (4) enforces CI-side via
//     `shell: bash`? (checkScriptInvokesHelper / checkSourcedAndCalled)
//  4. Is the CI workflow's trivy-fs job's trivy-action step's skip-dirs input
//     EXACTLY a `${{ steps.<id>.outputs.<name> }}` expression -- not a value
//     that merely contains one -- and does the step with that id both
//     `source` the HELPER and call trivy_skip_dirs_csv in its run: block,
//     over whole-line-comment-stripped content -- the same source-and-call
//     proof as (3), not merely a mention of the helper's path -- and does
//     that step declare `shell: bash`? (It does NOT verify the step writes
//     that exact output key into `$GITHUB_OUTPUT` -- that assertion was
//     attempted and retired after five review rounds; see AGENTS.md.)
//     (checkCIWorkflowSkipDirsFromHelper / checkSourcedAndCalled)
//
// Exact string equality on the workflow input kills the append-a-directory-
// at-the-call-site bypass class for free: there is no partial-match branch
// left to defeat by appending `,node_modules` after the expression. The
// CI-side read stays job-scoped via YAML, never regexed file-wide, for the
// same reason it always was: security-scan.yml has multiple jobs, and a
// whole-file scan cannot tell a skip-dirs input or a step id that belongs to
// trivy-fs from one that belongs to an unrelated job. Fail loudly at every
// step rather than skipping -- a parity check that silently passes when it
// cannot find its subject is worse than none, because green reads as proof of
// agreement.
//
// All four artifacts absent is the shape of this package's synthetic drift
// fixtures, which build a minimal repo out of a pre-commit config and a
// workflow or two; skipping there avoids failing every such fixture on files
// it never claimed to have. The real repo always has all four artifacts.
func checkTrivySkipDirsParity(repoRoot string) []error {
	specsPath := filepath.Join(repoRoot, trivySkipDirsSpecPath)
	helperPath := filepath.Join(repoRoot, trivySkipDirsHelperPath)
	localPath := filepath.Join(repoRoot, trivyLocalScriptPath)
	ciPath := filepath.Join(repoRoot, trivyCIWorkflowPath)

	if !regularFileExists(specsPath) && !regularFileExists(helperPath) &&
		!regularFileExists(localPath) && !regularFileExists(ciPath) {
		return nil
	}

	var errs []error
	if _, err := trivySkipDirsSpecEntries(specsPath); err != nil {
		errs = append(errs, err)
	}
	if err := checkFileReferencesPathOnce(helperPath, trivySkipDirsHelperPath, trivySkipDirsSpecPath); err != nil {
		errs = append(errs, err)
	}
	if err := checkScriptInvokesHelper(localPath, trivyLocalScriptPath); err != nil {
		errs = append(errs, err)
	}
	if err := checkCIWorkflowSkipDirsFromHelper(ciPath); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// trivySkipDirsSpecEntries is defined in trivyskipdirs_specentries.go (split
// out to keep this file under the package's 500-line-per-file limit).

// checkFileReferencesPathOnce reports an error unless the file at path
// references target exactly once, over content with whole-line comments
// stripped. Zero references means the file is no longer wired to target at
// all (e.g. reverted to a hard-coded value); more than one means the parity
// check cannot tell which reference is the real, governing one (e.g. one live
// and one inside a branch that never executes). It answers the one remaining
// question in this package that is genuinely a plain path reference rather
// than a shell invocation (#5925 F2): does scripts/lib/trivy-skip-dirs.sh
// itself read specs/trivy-skip-dirs.txt as data (the specs path is opened as
// a file, not sourced and called). Whether scripts/dev/trivy-fs-local.sh and
// the CI producer step actually INVOKE the helper -- rather than merely
// mentioning its path -- is a different, stronger question, answered by
// checkSourcedAndCalled instead (#5927 review): a bare reference count cannot
// tell a live `source` line from a mention inside a string literal or an
// echo, so counting mentions of the helper's path let a hard-coded value
// hiding behind an unrelated mention of the path read as "wired".
//
// Only WHOLE comment lines are stripped -- never truncating a line mid-way at
// an unquoted '#', the way the design this replaces did. That mattered for a
// value-flow check that had to tell a live invocation from a disabled one at
// arbitrary column; it does not matter for counting path references, and the
// simpler rule means a misclassified line can only ever lower the count,
// never raise it. For the 0/1 boundary this check actually gates on (want
// exactly 1), lowering can only fail red: an undercount from 1 to 0 is the
// "0 references" failure above. A drop from 2 to 1 would silently read as
// the passing case instead of the ambiguous one that count exists to catch,
// but that needs a genuine reference to sit on a line whose first
// non-whitespace character is '#' while still being live code -- not
// reachable in a plain bash script the way it could be inside a heredoc or
// quoted block, so no known input exploits it.
func checkFileReferencesPathOnce(path, fileLabel, target string) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative, caller-constructed path
	if err != nil {
		return fmt.Errorf("trivy skip-dirs parity: cannot read %s: %w", fileLabel, err)
	}
	stripped := stripWholeLineComments(raw)
	count := strings.Count(string(stripped), target)
	if count != 1 {
		return fmt.Errorf(
			"trivy skip-dirs parity: %s references %s %d time(s), want exactly 1 -- "+
				"the parity check cannot tell whether %s actually reads it",
			fileLabel, target, count, fileLabel,
		)
	}
	return nil
}

// stripWholeLineComments blanks out every line of raw whose first
// non-whitespace character is '#', leaving all other lines byte-for-byte
// untouched. It deliberately does not track quoting or truncate mid-line: see
// checkFileReferencesPathOnce's doc comment for why that simplicity is safe
// for this narrower "how many times is this path mentioned" question.
func stripWholeLineComments(raw []byte) []byte {
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// checkScriptInvokesHelper reports an error unless the file at path, after
// whole-line-comment-stripping, both `source`s scripts/lib/trivy-skip-dirs.sh
// and calls the function it defines, trivy_skip_dirs_csv -- see
// checkSourcedAndCalled for why both halves are required. It is
// scripts/dev/trivy-fs-local.sh's half of the #5927 fix; the CI producer
// step's run: block is already in memory (read as YAML), so
// checkCIWorkflowSkipDirsFromHelper calls checkSourcedAndCalled directly
// instead of going through this file-reading wrapper.
//
// It also requires the script to `set -o pipefail` (trivyPipefailRE) --
// scripts/dev/trivy-fs-local.sh's half of the local/CI failure-mode-parity
// requirement checkCIWorkflowSkipDirsFromHelper enforces via `shell: bash`
// (#5925 F12, single-source review P2-3): without pipefail, an unreadable
// specs file failing inside trivy_skip_dirs_csv's pipeline would not fail the
// script, the opposite of what security-scan.yml's own comment on the CI
// producer step promises.
func checkScriptInvokesHelper(path, fileLabel string) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative, caller-constructed path
	if err != nil {
		return fmt.Errorf("trivy skip-dirs parity: cannot read %s: %w", fileLabel, err)
	}
	stripped := stripWholeLineComments(raw)
	if err := checkSourcedAndCalled(stripped, fileLabel); err != nil {
		return err
	}
	if !trivyPipefailRE.Match(stripped) {
		return fmt.Errorf(
			"trivy skip-dirs parity: %s does not `set -o pipefail` (e.g. `set -euo pipefail`) -- "+
				"without it, an unreadable %s failing inside trivy_skip_dirs_csv's "+
				"`sed | grep | grep | paste` pipeline would not fail the script, silently deriving an "+
				"empty skip-dirs value instead",
			fileLabel, trivySkipDirsSpecPath,
		)
	}
	return nil
}

// checkSourcedAndCalled reports an error unless strippedContent -- already
// stripped of whole-line comments -- both `source`s (or `. `s)
// scripts/lib/trivy-skip-dirs.sh exactly once (trivySourceLineRE) AND calls
// the function it defines, trivy_skip_dirs_csv, exactly once
// (trivySkipDirsCsvCallRE). fileLabel names the subject in the error text: a
// script path for checkScriptInvokesHelper's caller, or a description of the
// CI producer step for checkCIWorkflowSkipDirsFromHelper's.
//
// This replaces two shapes of counting mere MENTIONS of the helper's path as
// proof of invocation (#5927 review): checkCIWorkflowSkipDirsFromHelper used
// to accept any strings.Contains match of the helper's path anywhere in the
// producer step's run: block, and checkFileReferencesPathOnce (still used for
// the helper-reads-specs-file question) counts references the same
// mention-agnostic way. Either shape lets a step or script that MENTIONS the
// helper's path -- inside a string literal or an echo -- while a hard-coded
// value actually governs the emitted skip-dirs read as "invokes the helper".
// Requiring proof of BOTH the `source` line (which loads the helper's
// definitions) and the function call (which actually runs the derivation)
// closes that gap: a mention that is not a `source` line
// never satisfies the first half, and a `source` line with no call never
// satisfies the second.
//
// Checking source and call arity separately, rather than folding them into
// one combined count, is what lets the error name which half is missing
// instead of only reporting "not wired" -- required by the review that
// requested this fix. Requiring exactly one of each, not merely "at least
// one", mirrors checkFileReferencesPathOnce's existing arity convention in
// this file: more than one source line or more than one call means the
// parity check cannot tell which invocation actually governs (e.g. a live
// one and one inside a dead branch), the same ambiguity checkFileReferencesPathOnce
// already refuses to guess through.
func checkSourcedAndCalled(strippedContent []byte, fileLabel string) error {
	switch sourced := trivySourceLineRE.FindAll(strippedContent, -1); len(sourced) {
	case 1:
		// Exactly one `source` line -- proceed to the call check below.
	case 0:
		return fmt.Errorf(
			"trivy skip-dirs parity: %s does not `source` (or `. `) %s -- "+
				"a mention of the path is not enough, it must load the helper's "+
				"definitions before it can call trivy_skip_dirs_csv",
			fileLabel, trivySkipDirsHelperPath,
		)
	default:
		return fmt.Errorf(
			"trivy skip-dirs parity: %s sources %s %d time(s), want exactly 1 -- "+
				"the parity check cannot tell which invocation governs",
			fileLabel, trivySkipDirsHelperPath, len(sourced),
		)
	}

	switch called := trivySkipDirsCsvCallRE.FindAll(strippedContent, -1); len(called) {
	case 1:
		return nil
	case 0:
		return fmt.Errorf(
			"trivy skip-dirs parity: %s sources %s but never calls trivy_skip_dirs_csv -- "+
				"the skip-dirs value is not wired to the shared derivation",
			fileLabel, trivySkipDirsHelperPath,
		)
	default:
		return fmt.Errorf(
			"trivy skip-dirs parity: %s calls trivy_skip_dirs_csv %d time(s), want exactly 1 -- "+
				"the parity check cannot tell which call governs",
			fileLabel, len(called),
		)
	}
}

// regularFileExists reports whether path is an existing regular file. A
// directory or an unreadable entry counts as absent: the parity check wants a
// file it can parse, and treating anything else as present would turn a
// stat quirk into a spurious drift error.
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
