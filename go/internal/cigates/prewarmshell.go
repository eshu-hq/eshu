// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"regexp"
	"strings"
)

// This file is checkSetupGoPrewarmOrdering's command-position recognizer
// (#6615 PR #6743 Codex P1): a pre-warm is credited only when it is the
// command GitHub Actions' bash would actually execute, not merely text that
// appears in the step -- a commented-out line, an echoed/printf'd path, or
// a mention inside another string no longer counts. Not a shell parser: it
// has exactly one job, told in prose at each function below, and every
// simplification is named rather than left as a silent gap.

// shellCommand is one command this file believes bash would execute, in
// execution order. Text is its comment-stripped source (for pattern
// matching); Start is its byte offset into the ORIGINAL run text, so callers
// can still report source lines and "ran before/after" using real
// coordinates. NextOp is the operator that terminated this command ("&&",
// "||", ";", "|", "&", or "" at end of line/block) -- commandSuppression
// uses it to tell a command whose failure reaches the job from one chained,
// piped, or backgrounded so that it does not.
type shellCommand struct {
	Text   string
	Start  int
	NextOp string
}

// prewarmScriptPaths are the forms of the pre-warm script's own path this
// file recognizes in command position: the plain repo-root-relative form
// every real invocation in this repo uses today, and the explicit
// "./"-prefixed form (needs no working-directory resolution, just a literal
// match). A "../"-prefixed path is deliberately NOT recognized: resolving it
// needs the step's actual working directory -- the `working-directory:`
// field, or any preceding `cd` this file does not track -- which is
// genuinely ambiguous to resolve statically, and no real job in this repo
// uses that form. A `bash`/`sh` prefix before either form is also
// recognized; see prewarmInvocation.
var prewarmScriptPaths = map[string]bool{
	"scripts/ci/go-mod-download-retry.sh":   true,
	"./scripts/ci/go-mod-download-retry.sh": true,
}

// shellCommandsInRun returns every command in run (a workflow step's `run:`
// text, single- or multi-line) that this file believes bash would execute,
// in source order. It processes one physical line at a time: quote state
// does NOT carry across a newline, so a quoted string or a backslash line
// continuation spanning multiple lines is not tracked correctly (rare in
// this repo's run: blocks; none do today). $() / backtick command
// substitution, here-docs, and brace/subshell grouping are not modeled.
func shellCommandsInRun(run string) []shellCommand {
	var out []shellCommand
	lineStart := 0
	for {
		nl := strings.IndexByte(run[lineStart:], '\n')
		var line string
		if nl < 0 {
			line = run[lineStart:]
		} else {
			line = run[lineStart : lineStart+nl]
		}
		out = append(out, splitLineCommands(line, lineStart)...)
		if nl < 0 {
			break
		}
		lineStart += nl + 1
	}
	return out
}

// splitLineCommands strips line's trailing unquoted shell comment, then
// splits what remains into commands on unquoted "&&", "||", ";", "|", "&",
// and on the control keywords "then"/"do"/"else" at word boundaries (the
// keyword itself is discarded, not emitted as its own command -- it is
// never a pre-warm invocation). offset is line's byte position within the
// original multi-line text, added to every returned Start so positions stay
// meaningful across the whole run.
//
// Deliberately NOT a keyword: "if". "if scripts/ci/go-mod-download-retry.sh"
// stays ONE command whose first word is "if", so it is never recognized as
// executing the script directly -- which is exactly right: an `if` guard
// consumes the command's exit status, the same failure mode
// commandSuppression exists to catch, so "not recognized as a pre-warm at
// all" and "recognized but ineffective" reach the same outcome without a
// second code path.
func splitLineCommands(line string, offset int) []shellCommand {
	line = stripShellComment(line)

	var out []shellCommand
	inSingle, inDouble := false, false
	segStart := 0
	emit := func(end int, op string) {
		text := strings.TrimSpace(line[segStart:end])
		if text != "" {
			out = append(out, shellCommand{Text: text, Start: offset + segStart, NextOp: op})
		}
	}

	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			i++
		case c == '"' && !inSingle:
			inDouble = !inDouble
			i++
		case inSingle || inDouble:
			i++
		case c == '&' && i+1 < len(line) && line[i+1] == '&':
			emit(i, "&&")
			i += 2
			segStart = i
		case c == '&' && isRedirectionAmpersand(line, i):
			i++
		case c == '&':
			emit(i, "&")
			i++
			segStart = i
		case c == '|' && i+1 < len(line) && line[i+1] == '|':
			emit(i, "||")
			i += 2
			segStart = i
		case c == '|':
			emit(i, "|")
			i++
			segStart = i
		case c == ';':
			emit(i, ";")
			i++
			segStart = i
		default:
			if kwEnd, ok := matchKeyword(line, i); ok {
				emit(i, "")
				i = kwEnd
				segStart = i
			} else {
				i++
			}
		}
	}
	emit(len(line), "")
	return out
}

// isRedirectionAmpersand reports whether the "&" at line[i] belongs to a
// redirection rather than to the background operator: ">&", "<&" (as in
// "2>&1") and "&>" (as in "cmd &>log"). Splitting a command there would tear
// a real pre-warm invocation in half and stop recognizing it.
func isRedirectionAmpersand(line string, i int) bool {
	if i+1 < len(line) && line[i+1] == '>' {
		return true
	}
	for j := i - 1; j >= 0; j-- {
		switch line[j] {
		case ' ', '\t':
			continue
		case '>', '<':
			return true
		default:
			return false
		}
	}
	return false
}

// pipefailSetRE matches a `set` that changes pipefail either way: capture 1
// is "-" for "set -o pipefail" / "set -eo pipefail" / "set -euo pipefail",
// and "+" for "set +o pipefail", which turns it back off.
var pipefailSetRE = regexp.MustCompile(`^set\s+([-+])[a-zA-Z]*o\s+pipefail\b`)

// rhsCannotSucceedRE matches a `||` right-hand side that cannot itself exit
// 0, so the failure still reaches the job. Bare `exit` and `return` count:
// they carry the failing command's own status. Measured on /bin/bash -e:
// `false || exit 1` and `false || false` both exit 1, while `false || true`,
// `false || echo x` and `false || exit 0` all exit 0.
//
// The status argument is capped at 1-255 because a wait status is 8 bits:
// `false || exit 512` exits 0 (measured), so treating 512 as "cannot
// succeed" would be a false green in the direction that matters.
var rhsCannotSucceedRE = regexp.MustCompile(`^(false|(exit|return)(\s+([1-9][0-9]?|1[0-9][0-9]|2[0-4][0-9]|25[0-5]))?)$`)

// commandSuppression describes how cmds[idx]'s own exit status is kept from
// reaching the job, or "" when the status does reach it. shellPipefail is
// true when the step declared a shell that sets `-o pipefail`.
//
// A GitHub Actions `run:` step runs under `bash -e {0}` by default, and an
// explicit `shell: bash` under `bash --noprofile --norc -eo pipefail {0}`
// (workflow-syntax reference). Measured on /bin/bash -e:
//
//	false || true -> 0   false || echo x -> 0   false || exit 0 -> 0
//	false | tee   -> 0   false &         -> 0   false && echo x -> 1
//
// so a "||" whose right-hand side SUCCEEDS swallows the failure, a pipeline
// reports only its last command unless pipefail is set, and a backgrounded
// command reports nothing and has not even finished.
//
// Not suppression, each measured rather than reasoned about:
//   - "&&" -- the RHS never runs and the list keeps the non-zero status.
//   - "cmd; X" -- `-e` exits AT the failing cmd, so X never runs
//     (`bash -e -c 'false; true'` exits 1). This holds only while `-e` is in
//     effect; a `set +e` earlier in the SAME run: block would change it, and
//     this file does not track `set +e`. required-gates.yml uses one, but in
//     a later step than its pre-warm, so none precedes a pre-warm today.
//   - "|| exit 1", "|| false" -- an RHS that cannot exit 0.
//
// Known over-rejection: a brace or subshell group RHS (`|| { echo x; exit
// 1; }`) is split at its own ";" and cannot be recognized as failing, so it
// is reported. Grouping is already an unmodelled construct here.
func commandSuppression(cmds []shellCommand, idx int, shellPipefail bool) string {
	if cmds[idx].NextOp == "&" {
		return "its exit status is suppressed: it is backgrounded with &, so the step exits 0 immediately and the job moves on while the download is still running"
	}
	if idx+1 >= len(cmds) {
		return ""
	}
	next := cmds[idx+1].Text
	switch cmds[idx].NextOp {
	case "||":
		if rhsCannotSucceedRE.MatchString(next) {
			return ""
		}
		return "its exit status is suppressed (chained with || " + next + ", which succeeds, so the step exits 0 when the pre-warm fails)"
	case "|":
		if runSetsPipefail(cmds[:idx], shellPipefail) {
			return ""
		}
		return "its exit status is suppressed: the pipeline into " + next +
			" reports only that last command, and the default runner shell is bash -e WITHOUT pipefail -- add shell: bash or set -o pipefail"
	}
	return ""
}

// runSetsPipefail reports whether pipefail is on by the time the command
// after earlier runs. pipefail is state, not a one-way switch, so the LAST
// `set ±o pipefail` wins: `set -o pipefail; set +o pipefail` leaves it off.
// on is the starting state from the step's shell selector, which a later
// `set +o pipefail` in the body can still turn off.
//
// Limit, named rather than left silent: a `set` inside a conditional still
// counts, because `then`/`do`/`else` are split keywords and this file does
// not model which branch runs. That direction over-credits pipefail, so a
// pipeline guarded by a conditional `set -o pipefail` is accepted.
func runSetsPipefail(earlier []shellCommand, on bool) bool {
	for _, c := range earlier {
		if m := pipefailSetRE.FindStringSubmatch(c.Text); m != nil {
			on = m[1] == "-"
		}
	}
	return on
}

// stripShellComment returns line with a trailing unquoted "#" comment
// removed: a "#" counts as a comment start only at the very start of the
// line or immediately after whitespace, and only when it is not inside a
// single- or double-quoted string.
func stripShellComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return line[:i]
			}
		}
	}
	return line
}

// matchKeyword reports whether one of the control keywords "then"/"do"/
// "else" starts at line[pos], bounded by whitespace/line-edges on both
// sides (so it does not fire inside a longer word like "sudo"), returning
// the offset immediately after it.
func matchKeyword(line string, pos int) (end int, ok bool) {
	if pos > 0 && !isShellSpace(line[pos-1]) {
		return 0, false
	}
	for _, kw := range [...]string{"then", "do", "else"} {
		n := len(kw)
		if pos+n <= len(line) && line[pos:pos+n] == kw {
			if pos+n == len(line) || isShellSpace(line[pos+n]) {
				return pos + n, true
			}
		}
	}
	return 0, false
}

func isShellSpace(b byte) bool {
	return b == ' ' || b == '\t'
}

// splitWords splits a single (already comment-stripped, single-command)
// shell command into whitespace-separated words, keeping a quoted string
// (its delimiters included) as one word -- so `printf '%s' scripts/ci/...`
// tokenizes to ["printf", "'%s'", "scripts/ci/..."] rather than splitting
// the quoted argument apart.
func splitWords(cmd string) []string {
	var words []string
	inSingle, inDouble := false, false
	start := -1
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			if start < 0 {
				start = i
			}
		case c == '"' && !inSingle:
			inDouble = !inDouble
			if start < 0 {
				start = i
			}
		case isShellSpace(c) && !inSingle && !inDouble:
			if start >= 0 {
				words = append(words, cmd[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		words = append(words, cmd[start:])
	}
	return words
}

// isAssignment reports whether word is a leading shell VAR=value assignment
// (e.g. "GOFLAGS=-mod=mod"): starts with a shell-identifier name, then "=",
// with the whole word carrying no internal whitespace (already guaranteed by
// splitWords).
func isAssignment(word string) bool {
	eq := strings.IndexByte(word, '=')
	if eq <= 0 {
		return false
	}
	name := word[:eq]
	for i, c := range name {
		isLetterOrUnderscore := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isLetterOrUnderscore {
			return false
		}
		if i > 0 && !isLetterOrUnderscore && !isDigit {
			return false
		}
	}
	return true
}

// prewarmInvocation reports whether cmd (one shellCommand from
// shellCommandsInRun) executes the pre-warm script in command position, and
// if so, its module argument (unquoted; "" means the bare/default form).
func prewarmInvocation(cmd shellCommand) (moduleArg string, isPrewarm bool) {
	words := splitWords(cmd.Text)
	i := 0
	for i < len(words) && isAssignment(words[i]) {
		i++
	}
	if i >= len(words) {
		return "", false
	}
	first := words[i]
	if prewarmScriptPaths[first] {
		return firstModuleArg(words[i+1:]), true
	}
	if (first == "bash" || first == "sh") && i+1 < len(words) && prewarmScriptPaths[words[i+1]] {
		return firstModuleArg(words[i+2:]), true
	}
	return "", false
}

// redirectionWordRE matches a word that is a redirection rather than an
// argument: ">log", ">>log", "2>&1", "<in", "&>log". A redirection may
// appear anywhere in a command, so the module argument is the first word
// that is not one -- otherwise `retry.sh 2>&1` reports the script as warming
// a module named "2>&1" and fails a workflow that is in fact correct.
var redirectionWordRE = regexp.MustCompile(`^[0-9]*(&?>>?|<)`)

// firstModuleArg returns the pre-warm's module-directory argument from the
// words following the script path, or "" when it has none. A redirection
// word whose target is detached ("> log") also consumes the word after it.
func firstModuleArg(words []string) string {
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !redirectionWordRE.MatchString(w) {
			return unquoteArg(w)
		}
		if strings.HasSuffix(w, ">") || strings.HasSuffix(w, "<") {
			i++
		}
	}
	return ""
}
