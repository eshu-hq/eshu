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
}

// Violation is one Heredoc whose Size exceeds the configured budget, tagged
// with the repo-relative file path it was found in.
type Violation struct {
	Path string
	Line int
	Size int
}

// opener describes a recognized heredoc opener: its delimiter word and
// whether it uses the `<<-` tab-stripping form.
type opener struct {
	delim    string
	tabStrip bool
}

// ScanContent scans shell script source text for heredoc bodies and returns
// one Heredoc per detected heredoc, in source order.
//
// `<<<` here-strings are never treated as heredoc openers. Only one heredoc
// is tracked "open" at a time: once an opener is matched, every subsequent
// line is treated purely as body content (or the close) until the exact
// closing delimiter line is seen, so a DELIM-like word that belongs to a
// different (past or future) heredoc cannot mis-close the current one. An
// opener with no matching closing line (a malformed script) is dropped
// rather than reported, since there is no well-formed body to measure.
func ScanContent(src string) []Heredoc {
	var heredocs []Heredoc
	lines := strings.Split(src, "\n")

	var (
		inBody   bool
		current  opener
		openLine int
		bodySize int
	)

	for i, line := range lines {
		lineNo := i + 1
		if inBody {
			if closesHeredoc(line, current) {
				heredocs = append(heredocs, Heredoc{Line: openLine, Size: bodySize})
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
		// A `<<IDENT` inside a string literal or a second opener on one line
		// are known limitations (see the "Known limitations" note in doc.go).
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		if o, ok := findOpener(line); ok {
			inBody = true
			current = o
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
			if h.Size > budget {
				violations[relPath] = append(violations[relPath], Violation{Path: relPath, Line: h.Line, Size: h.Size})
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

// findOpener scans line for the first heredoc opener — `<<DELIM`,
// `<<'DELIM'`, `<<"DELIM"`, or the `<<-` tab-stripped variant of each — and
// returns it. `<<<` here-strings are recognized and skipped rather than
// mistaken for a heredoc opener with an empty or malformed delimiter.
func findOpener(line string) (opener, bool) {
	for i := 0; i+1 < len(line); i++ {
		if line[i] != '<' || line[i+1] != '<' {
			continue
		}
		// `<<<` is a here-string, not a heredoc. Skip past the third '<' so
		// the loop cannot re-match the trailing "<<" of "<<<" as its own
		// (bogus) heredoc opener.
		if i+2 < len(line) && line[i+2] == '<' {
			i += 2
			continue
		}
		rest := line[i+2:]
		tabStrip := strings.HasPrefix(rest, "-")
		if tabStrip {
			rest = rest[1:]
		}
		// Bash allows optional blanks between `<<`/`<<-` and the delimiter
		// (`cat << EOF`, `cat <<- 'EOF'`). Trim them so a whitespace-separated
		// heredoc is not missed — a fail-open the gate exists to block. The
		// delimiter must still start with a letter or `_` (parseDelim), so an
		// arithmetic left-shift like `$(( x << 2 ))` is not mistaken for a
		// heredoc opener.
		rest = strings.TrimLeft(rest, " \t")
		if delim, ok := parseDelim(rest); ok {
			return opener{delim: delim, tabStrip: tabStrip}, true
		}
		// Not a valid delimiter after "<<" (e.g. no identifier follows) —
		// keep scanning the rest of the line for another candidate.
	}
	return opener{}, false
}

// parseDelim parses a heredoc delimiter word from the start of s, which is
// the text immediately following "<<" (and any "-"). It accepts a bare
// identifier or a single- or double-quoted identifier, per DELIM =
// [A-Za-z_][A-Za-z0-9_]*.
func parseDelim(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	if s[0] == '\'' || s[0] == '"' {
		quote := s[0]
		end := strings.IndexByte(s[1:], quote)
		if end < 0 {
			return "", false
		}
		name := s[1 : 1+end]
		if isIdentifier(name) {
			return name, true
		}
		return "", false
	}
	j := 0
	for j < len(s) && isIdentByte(s[j], j == 0) {
		j++
	}
	if j == 0 {
		return "", false
	}
	return s[:j], true
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
