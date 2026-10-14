// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Failing-body and script-callee gating for drift check 13 (#8014).
//
// A gating if/case covers a dependency only when a failing body
// (exit-nonzero/false) follows within a bounded lookahead, and a repo-script
// invocation covers only when the resolved scripts/ callee gates on its
// positional slot. Everything here is line-based: no set -e,
// function-return, or sourced-file inference, and every narrowing fails
// closed.

// branchLeadRE matches the shell compound openers whose gate lives in the
// branch body: if and case. test, [, and [[ fail the step themselves.
var branchLeadRE = regexp.MustCompile(`^\s*(if\b|case\b)`)

// isBranchLead reports whether a gating line needs a failing body to cover.
func isBranchLead(line string) bool {
	return branchLeadRE.MatchString(line)
}

// blockEndRE matches the line that closes an if/case body scan.
var blockEndRE = regexp.MustCompile(`^\s*(fi|esac)\b`)

// failingLeadRE matches a body line that opens with a failing command: exit
// with a nonzero literal, or a bare false. exit 0, a bare exit, and
// `exit $var` do not match: the check cannot prove they fail.
var failingLeadRE = regexp.MustCompile(`^(exit\s+[1-9][0-9]*|false)\b`)

// failingChainRE matches a failing command in command position after ;, &,
// |, a case arm `)`, or then, so `*) exit 1 ;;` and `then exit 1` count
// while `echo "exit 1"` does not.
var failingChainRE = regexp.MustCompile(`(?:[;&|)]|then)\s+(exit\s+[1-9][0-9]*|false)\b`)

// gatingBodyLookaheadMax caps a failing-body scan when the closing fi/esac
// never comes; the primary bound is the terminator itself.
const gatingBodyLookaheadMax = 30

// hasFailingBody reports whether an exit-nonzero/false line follows the
// gating if/case at lines[from] before its closing fi/esac. Line-based only:
// no set -e, function-return, or sourced-file inference.
func hasFailingBody(lines []string, from int) bool {
	for j := from + 1; j < len(lines) && j-from-1 < gatingBodyLookaheadMax; j++ {
		if blockEndRE.MatchString(lines[j]) {
			return false
		}
		if failingLeadRE.MatchString(strings.TrimSpace(lines[j])) || failingChainRE.MatchString(lines[j]) {
			return true
		}
	}
	return false
}

// shellShebangRE matches a shell interpreter line.
var shellShebangRE = regexp.MustCompile(`^#!.*\b(bash|sh|dash|ksh|zsh)\b`)

// positionalSlotRE matches a positional-argument read: $1, $@, $*, ${1},
// ${@}. $# (the count, not a value) is deliberately excluded.
var positionalSlotRE = regexp.MustCompile(`\$[0-9]+|\$[@*]|\$\{[0-9@*][^}]*\}`)

// argAssignRE matches a variable assignment from a positional slot, so the
// check follows one hop (`capture_result="$5"`, then a gate on
// $capture_result) without general dataflow.
var argAssignRE = regexp.MustCompile(`^\s*(?:local\s+|export\s+)?([A-Za-z_][A-Za-z0-9_]*)=.*(\$[0-9]+|\$[@*]|\$\{[0-9@*][^}]*\})`)

// scriptGatesOnArg resolves the scripts/ callee an invocation line names and
// reports whether the callee gates on its positional slot: the slot (or a
// variable assigned from it) sits in a gating position and a failing command
// follows. A missing, non-shell, or unresolvable callee, and a callee that
// takes the argument and ignores it, each read as uncovered (#8014).
func scriptGatesOnArg(repoRoot, line string) bool {
	rel := strings.Trim(strings.TrimPrefix(strings.TrimSpace(scriptCalleeRE.FindString(line)), "./"), `"'`+"`;,)]}:")
	if rel == "" {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel)) // #nosec G304 -- scripts/-confined callee from the committed workflow
	if err != nil {
		return false
	}
	if !isShellScript(rel, raw) {
		return false
	}
	return scriptBodyGatesOnSlot(string(raw))
}

// isShellScript reports whether a resolved callee reads as shell: a shell
// extension or a shell shebang. Anything else fails closed.
func isShellScript(rel string, raw []byte) bool {
	if strings.HasSuffix(rel, ".sh") || strings.HasSuffix(rel, ".bash") {
		return true
	}
	first, _, _ := strings.Cut(string(raw), "\n")
	return shellShebangRE.MatchString(strings.TrimSpace(first))
}

// scriptBodyGatesOnSlot reports whether a positional slot (or a variable one
// hop from it) sits in a gating line and a failing command is present.
// Scanned uniformly line by line: no function-call or source-graph inference.
func scriptBodyGatesOnSlot(body string) bool {
	lines := strings.Split(body, "\n")
	var argVars []string
	for _, line := range lines {
		if m := argAssignRE.FindStringSubmatch(line); m != nil {
			argVars = append(argVars, m[1])
		}
	}
	argVarREs := make([]*regexp.Regexp, 0, len(argVars))
	for _, v := range argVars {
		argVarREs = append(argVarREs, regexp.MustCompile(`\$`+v+`\b|\$\{`+v+`[}:/]`))
	}
	gated := false
	for _, line := range lines {
		if !shellGatingLeadRE.MatchString(line) {
			continue
		}
		if positionalSlotRE.MatchString(line) {
			gated = true
			break
		}
		for _, re := range argVarREs {
			if re.MatchString(line) {
				gated = true
				break
			}
		}
		if gated {
			break
		}
	}
	if !gated {
		return false
	}
	for _, line := range lines {
		if failingLeadRE.MatchString(strings.TrimSpace(line)) || failingChainRE.MatchString(line) {
			return true
		}
	}
	return false
}
