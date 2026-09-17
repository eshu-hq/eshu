// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "strings"

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
// "||", ";", "|", or "" at end of line/block) -- commandSuppressed uses it
// to recognize "cmd || true" / "cmd; true" / "cmd || :" as a pair of
// commands rather than one, since splitLineCommands already separates them.
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
// splits what remains into commands on unquoted "&&", "||", ";", "|", and
// on the control keywords "then"/"do"/"else" at word boundaries (the
// keyword itself is discarded, not emitted as its own command -- it is
// never a pre-warm invocation). offset is line's byte position within the
// original multi-line text, added to every returned Start so positions stay
// meaningful across the whole run.
//
// Deliberately NOT a keyword: "if". "if scripts/ci/go-mod-download-retry.sh"
// stays ONE command whose first word is "if", so it is never recognized as
// executing the script directly -- which is exactly right: an `if` guard
// consumes the command's exit status, the same failure mode `|| true`
// (prewarmSuppressedRE) exists to catch, so "not recognized as a pre-warm at
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

// commandSuppressed reports whether cmds[idx] is immediately chained to a
// following "true" or "||" ":" -- "cmd || true", "cmd; true", "cmd || :" --
// which always makes the LINE exit 0 regardless of cmd's own result (a GH
// Actions `run:` step's exit status is its last command's, absent `set -e`),
// so a genuinely failing pre-warm no longer fails the job.
func commandSuppressed(cmds []shellCommand, idx int) bool {
	c := cmds[idx]
	if idx+1 >= len(cmds) {
		return false
	}
	next := cmds[idx+1].Text
	switch c.NextOp {
	case ";":
		return next == "true"
	case "||":
		return next == "true" || next == ":"
	default:
		return false
	}
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
		if i+1 < len(words) {
			return unquoteArg(words[i+1]), true
		}
		return "", true
	}
	if (first == "bash" || first == "sh") && i+1 < len(words) && prewarmScriptPaths[words[i+1]] {
		if i+2 < len(words) {
			return unquoteArg(words[i+2]), true
		}
		return "", true
	}
	return "", false
}
