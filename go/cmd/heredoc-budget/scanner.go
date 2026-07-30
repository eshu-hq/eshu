// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Heredoc is one heredoc body detected in a shell script.
type Heredoc struct {
	// Line is the 1-based line number of the opening `<<DELIM` (or `<<-`,
	// `<<'DELIM'`, `<<"DELIM"` variant), not the closing delimiter line.
	Line int
	// Size is the heredoc body size in bytes: the sum of len(line)+1 for
	// every body line between the opener and the closing delimiter line
	// (exclusive of both).
	Size int
	// Unquoted is true when the delimiter was bare (`<<DELIM`), meaning bash
	// performs parameter/command substitution inside the body. A body that
	// is under the literal byte budget in source can still expand past it
	// at runtime (#5085), so ScanTree applies a stricter effective threshold
	// when this is true. False for `<<'DELIM'`/`<<"DELIM"`, which bash never
	// expands.
	Unquoted bool
}

// Violation is one Heredoc whose Size exceeds the effective budget, tagged
// with the repo-relative file path it was found in.
type Violation struct {
	Path string
	Line int
	Size int
	// Unquoted mirrors Heredoc.Unquoted: true when this violation was found
	// only because the heredoc's bare delimiter allows runtime expansion
	// (see Threshold and unquotedThreshold).
	Unquoted bool
	// Threshold is the effective byte threshold this heredoc was compared
	// against: the raw budget for a quoted heredoc, or the stricter
	// unquotedThreshold(budget) for an unquoted one.
	Threshold int
}

// unquotedMarginDivisor sets how much stricter the effective budget is for
// an UNQUOTED heredoc opener (`<<DELIM`, not `<<'DELIM'`/`<<"DELIM"`): bash
// performs parameter/command substitution inside its body, so a body that is
// under the literal byte budget in source can still expand past it at
// runtime (#5085) — the concrete case was a 496-byte source heredoc whose
// `${fact_families[*]}` expansion crossed the 512-byte macOS pipe-buffer
// deadlock threshold. A quoted heredoc's body is never expanded, so it keeps
// the full literal budget. The 25% margin (384 bytes for the default
// 512-byte budget) is a conservative, documented choice, not a re-derived OS
// constant; expansion only ever grows a body, never shrinks it, so a margin
// below the raw budget is the safe direction to err.
const unquotedMarginDivisor = 4

// unquotedThreshold returns the effective byte threshold for an UNQUOTED
// heredoc: budget reduced by a 1/unquotedMarginDivisor margin. See
// unquotedMarginDivisor for the rationale.
func unquotedThreshold(budget int) int {
	return budget - budget/unquotedMarginDivisor
}

// ScanContent scans shell script source text for heredoc bodies and returns
// one Heredoc per detected heredoc, in source order.
//
// `<<<` here-strings are never treated as heredoc openers. Only one heredoc
// body is measured at a time: once an opener is matched, every subsequent
// line is treated purely as body content (or the close) until the exact
// closing delimiter line is seen, so a DELIM-like word that belongs to a
// different (past or future) heredoc cannot mis-close the current one. When
// a line opens more than one heredoc (`cmd <<A <<B`), the extra openers are
// queued and processed in order immediately after the current one closes —
// matching bash, which reads their bodies back to back right after the
// command line, before moving on. An opener with no matching closing line (a
// malformed script) is dropped rather than reported, since there is no
// well-formed body to measure.
//
// Quote/substitution context (see findAllOpeners in scanner_lexer.go)
// persists across lines: a double-quoted string that spans multiple
// physical lines stays "quoted" on every line until its closing quote is
// actually found, and a `$(...)` opened on one line stays open across lines
// too. Both are frozen (left untouched) while a heredoc body is being
// consumed, since body lines are never lexed for quoting — they are raw
// content, exactly as bash treats them.
func ScanContent(src string) []Heredoc {
	var heredocs []Heredoc
	lines := strings.Split(src, "\n")

	var (
		inBody     bool
		current    opener
		pending    []opener
		openLine   int
		bodySize   int
		quoteStack []byte
	)

	for i, line := range lines {
		lineNo := i + 1
		if inBody {
			if closesHeredoc(line, current) {
				heredocs = append(heredocs, Heredoc{Line: openLine, Size: bodySize, Unquoted: !current.quoted})
				if len(pending) > 0 {
					current, pending = pending[0], pending[1:]
					bodySize = 0
					continue
				}
				inBody = false
				continue
			}
			bodySize += len(line) + 1
			continue
		}
		// A full-line shell comment cannot open a heredoc, UNLESS it is
		// really a continuation of a still-open quoted string from an
		// earlier line (e.g. the closing quote of a multi-line double-quoted
		// string happens to be on a line that starts with "#"). Skipping it
		// keeps a `<<IDENT` written inside a real comment (e.g. "# see the
		// <<EOF below") from phantom-opening the scanner and desyncing it so
		// a later real oversized heredoc is missed — the dangerous fail-open
		// case for this gate. This applies only outside a heredoc body; a
		// comment-looking line inside a body is body content, already
		// handled above. A TRAILING comment (real code followed by a
		// same-line "#", e.g. "echo x # <<EOF") is a different fail-open and
		// is handled inside findAllOpeners itself (scanner_lexer.go), not
		// here, since it needs the same character-by-character quote-aware
		// scan findAllOpeners already does.
		if !inQuoteFrame(quoteStack) && strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		var openers []opener
		openers, quoteStack = findAllOpeners(line, quoteStack)
		if len(openers) > 0 {
			inBody = true
			current, pending = openers[0], openers[1:]
			openLine = lineNo
			bodySize = 0
		}
	}
	return heredocs
}

// ScanFile reads path and scans it via ScanContent.
func ScanFile(path string) ([]Heredoc, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is produced by ScanTree's own filepath.WalkDir over a caller-controlled scan root, not external input.
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return ScanContent(string(data)), nil
}

// ScanTree walks root (typically the repo's scripts/ directory) for *.sh
// files and returns every over-budget heredoc found, keyed by the file's
// path relative to root's parent directory (e.g. "scripts/foo.sh" when root
// is ".../scripts"). Non-.sh files are skipped entirely.
func ScanTree(root string, budget int) (map[string][]Violation, error) {
	repoRoot := filepath.Dir(root)
	violations := make(map[string][]Violation)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sh") {
			return nil
		}
		heredocs, err := ScanFile(path)
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return fmt.Errorf("relativizing %s against %s: %w", path, repoRoot, err)
		}
		relPath = filepath.ToSlash(relPath)
		for _, h := range heredocs {
			threshold := budget
			if h.Unquoted {
				threshold = unquotedThreshold(budget)
			}
			if h.Size > threshold {
				violations[relPath] = append(violations[relPath], Violation{
					Path: relPath, Line: h.Line, Size: h.Size,
					Unquoted: h.Unquoted, Threshold: threshold,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning %s: %w", root, err)
	}
	return violations, nil
}

// closesHeredoc reports whether line is the closing delimiter line for an
// open heredoc. For the `<<-` form, leading tabs are stripped before
// comparison (POSIX tab-stripping); a trailing "\r" is always stripped so
// CRLF-terminated scripts compare correctly.
func closesHeredoc(line string, o opener) bool {
	l := strings.TrimSuffix(line, "\r")
	if o.tabStrip {
		l = strings.TrimLeft(l, "\t")
	}
	return l == o.delim
}
