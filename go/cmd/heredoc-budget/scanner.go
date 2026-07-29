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

// opener describes a recognized heredoc opener: its delimiter word, whether
// it uses the `<<-` tab-stripping form, and whether the delimiter itself was
// quoted (`<<'DELIM'`/`<<"DELIM"`), which disables runtime expansion of the
// body.
type opener struct {
	delim    string
	tabStrip bool
	quoted   bool
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
func ScanContent(src string) []Heredoc {
	var heredocs []Heredoc
	lines := strings.Split(src, "\n")

	var (
		inBody   bool
		current  opener
		pending  []opener
		openLine int
		bodySize int
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
		// A full-line shell comment cannot open a heredoc. Skipping it keeps a
		// `<<IDENT` written inside a comment (e.g. "# see the <<EOF below")
		// from phantom-opening the scanner and desyncing it so a later real
		// oversized heredoc is missed — the dangerous fail-open case for this
		// gate. This applies only outside a heredoc body; a comment-looking
		// line inside a body is body content, already handled above.
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		if openers := findAllOpeners(line); len(openers) > 0 {
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

// findAllOpeners scans line for every heredoc opener — `<<DELIM`,
// `<<'DELIM'`, `<<"DELIM"`, or the `<<-` tab-stripped variant of each — and
// returns them in left-to-right order. `<<<` here-strings are recognized and
// skipped rather than mistaken for a heredoc opener with an empty or
// malformed delimiter.
//
// The scan tracks single- and double-quote state as it goes, so a `<<IDENT`
// written inside a string literal (e.g. `echo "a <<X b"`) is not mistaken
// for a real opener (#5079) — bash itself never treats `<<` as redirection
// inside a quoted string. Because the scan keeps going after a match instead
// of stopping at the first one, a line that opens more than one heredoc
// (`cmd <<A <<B`) yields every opener, not just the first (#5079); bash
// reads their bodies back to back immediately after the command line, so
// ScanContent processes them in the same left-to-right order.
func findAllOpeners(line string) []opener {
	var openers []opener
	var quote byte // 0 outside any quote; else '\'' or '"'

	for i := 0; i < len(line); {
		c := line[i]

		if quote != 0 {
			// Inside a double-quoted string, `\"` escapes the quote so it
			// does not end the string early. Single-quoted strings have no
			// escapes in POSIX shell — a literal `'` cannot appear inside
			// one at all — so no special-casing is needed there.
			if quote == '"' && c == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}

		switch c {
		case '\'', '"':
			quote = c
			i++
			continue
		case '<':
			if i+1 >= len(line) || line[i+1] != '<' {
				i++
				continue
			}
			// `<<<` is a here-string, not a heredoc. Skip past the third '<'
			// so it cannot be re-matched as its own (bogus) heredoc opener.
			if i+2 < len(line) && line[i+2] == '<' {
				i += 3
				continue
			}
			rest := line[i+2:]
			tabStrip := strings.HasPrefix(rest, "-")
			if tabStrip {
				rest = rest[1:]
			}
			// Bash allows optional blanks between `<<`/`<<-` and the
			// delimiter (`cat << EOF`, `cat <<- 'EOF'`). Trim them so a
			// whitespace-separated heredoc is not missed — a fail-open the
			// gate exists to block. The delimiter must still start with a
			// letter or `_` (parseDelim), so an arithmetic left-shift like
			// `$(( x << 2 ))` is not mistaken for a heredoc opener.
			trimmed := strings.TrimLeft(rest, " \t")
			blanks := len(rest) - len(trimmed)
			if delim, quoted, consumed, ok := parseDelim(trimmed); ok {
				openers = append(openers, opener{delim: delim, tabStrip: tabStrip, quoted: quoted})
				advance := 2 + blanks + consumed
				if tabStrip {
					advance++
				}
				i += advance
				continue
			}
			// Not a valid delimiter after "<<" (e.g. no identifier follows)
			// — keep scanning the rest of the line for another candidate.
			i++
		default:
			i++
		}
	}
	return openers
}

// parseDelim parses a heredoc delimiter word from the start of s, which is
// the text immediately following "<<"/"<<-" and any blanks. It accepts a
// bare identifier or a single- or double-quoted identifier, per DELIM =
// [A-Za-z_][A-Za-z0-9_]*. It reports whether the delimiter was quoted (which
// disables runtime expansion of the body, see Heredoc.Unquoted) and how many
// bytes of s the delimiter token consumed, so the caller can resume scanning
// immediately after it on the same line.
func parseDelim(s string) (name string, quoted bool, consumed int, ok bool) {
	if s == "" {
		return "", false, 0, false
	}
	if s[0] == '\'' || s[0] == '"' {
		q := s[0]
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return "", false, 0, false
		}
		name := s[1 : 1+end]
		if !isIdentifier(name) {
			return "", false, 0, false
		}
		return name, true, end + 2, true // consumed = opening quote + name + closing quote
	}
	j := 0
	for j < len(s) && isIdentByte(s[j], j == 0) {
		j++
	}
	if j == 0 {
		return "", false, 0, false
	}
	return s[:j], false, j, true
}

// isIdentifier reports whether s matches [A-Za-z_][A-Za-z0-9_]* in full.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i], i == 0) {
			return false
		}
	}
	return true
}

// isIdentByte reports whether b is a valid byte at the given position of a
// [A-Za-z_][A-Za-z0-9_]* identifier; first distinguishes the leading byte
// (which cannot be a digit) from the rest.
func isIdentByte(b byte, first bool) bool {
	switch {
	case b == '_', b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return !first
	default:
		return false
	}
}
