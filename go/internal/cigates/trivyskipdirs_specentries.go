// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// trivySkipDirsSpecEntries reads specs/trivy-skip-dirs.txt and returns its
// directory entries: one per non-blank, non-comment line, in file order. A
// comment is a line whose first non-whitespace character is '#' -- there is
// no trailing-comment support, so an entry containing '#' anywhere else is
// rejected rather than silently truncated. It errors when the file cannot be
// read, has no entries at all, repeats an entry, contains an entry with
// leading or trailing whitespace, contains an entry with a comma (the shared
// derivation's join delimiter) or a '#' (see above), contains a glob
// metacharacter, or contains an entry that normalizes to a catch-all or
// repository-root-escaping path -- a parity check that silently accepts a
// malformed or dangerous source list is worse than none, because green reads
// as proof the list is sound.
//
// NOT rejected here: shell metacharacters such as '`', '$', '(', ';', or
// '|'. That is a deliberate gap, not an oversight -- this list can carry a
// fork PR's contributed entry (on: pull_request, security-events: write),
// and the reason an injected metacharacter cannot reach a shell as live
// syntax lives downstream of this check entirely: security-scan.yml passes
// skip-dirs to aquasecurity/trivy-action only via `with: skip-dirs`, which
// the action maps to `env: INPUT_SKIP_DIRS` and writes with
// `printf 'export %s=%q\n'` -- %q shell-quotes the value before it is ever
// sourced. Both consumers of this file (scripts/lib/trivy-skip-dirs.sh and
// the CI producer step) also quote the command substitution that reads it.
// If either of those downstream quoting steps ever changes, this list stops
// being safe against a metacharacter entry and this comment is the pointer
// back to why it was safe before.
//
// This function was split out of trivyskipdirs.go to keep that file under
// the package's 500-line-per-file limit (see go/internal/cigates/AGENTS.md).
func trivySkipDirsSpecEntries(specsPath string) ([]string, error) {
	raw, err := os.ReadFile(specsPath) // #nosec G304 -- repo-relative, caller-constructed path
	if err != nil {
		return nil, fmt.Errorf("trivy skip-dirs parity: cannot read %s: %w", trivySkipDirsSpecPath, err)
	}

	var entries []string
	for _, rawLine := range strings.Split(string(raw), "\n") {
		// A CRLF-saved file's lines still carry a trailing '\r' after
		// splitting on '\n' alone -- that is a line-ending artifact, not
		// content, so strip it before judging the line's content (#5925 F5).
		line := strings.TrimSuffix(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line != trimmed {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s has an entry with leading or trailing whitespace: %q -- "+
					"write it with no incidental whitespace, so the shell and Go derivations cannot silently disagree",
				trivySkipDirsSpecPath, line,
			)
		}
		if strings.Contains(trimmed, ",") {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- an entry may not contain a comma: ',' is the "+
					"delimiter the shared derivation's `paste -sd, -` joins entries with, so an embedded "+
					"comma would smuggle extra entries past every other per-entry check (e.g. "+
					"\"examples,.\" is not itself \".\", \"*\", or absolute, but joins into a bare \".\" "+
					"skip-dir); write each directory as its own line instead",
				trivySkipDirsSpecPath, trimmed,
			)
		}
		// A '#' anywhere in an otherwise non-comment line (single-source
		// review P2-2) is not a trailing comment -- neither this parser nor
		// the shared shell derivation supports one, only a whole-line
		// comment (the header above this list says so). Silently keeping it
		// as part of the entry would corrupt the directory trivy actually
		// receives (e.g. "alpha # rationale" is not "alpha"), fail closed on
		// unrelated fixtures, and point a contributor at the wrong cause.
		if strings.Contains(trimmed, "#") {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- an entry may not contain '#': only a WHOLE-LINE "+
					"comment is supported (a line whose first non-whitespace character is '#'), never a "+
					"trailing comment on a directory entry; move the rationale to its own comment line "+
					"above the entry instead",
				trivySkipDirsSpecPath, trimmed,
			)
		}
		// Glob metacharacters are rejected outright, closing the CLASS rather
		// than another instance of it (single-source review P1-1): three
		// review rounds of entry-specific literal checks ("." | "*" | leading
		// "/") kept getting re-defeated by a differently-spelled equivalent
		// -- "**" and "**/*" both match every path under doublestar, the same
		// way a bare "*" would. Trivy's --skip-dirs wants a literal
		// repo-relative path per entry; every entry committed today already
		// is one, so this costs the real list nothing.
		if strings.ContainsAny(trimmed, "*?[]{}") {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- a glob metacharacter ('*', '?', '[', ']', '{', "+
					"'}') is not a literal directory; trivy's --skip-dirs wants a literal repo-relative "+
					"path per entry, not a pattern that can match more than one directory",
				trivySkipDirsSpecPath, trimmed,
			)
		}
		// Normalize the way trivy's own CleanSkipPaths does -- filepath.Clean
		// then trim the leading '/' -- before judging whether the entry is a
		// catch-all: that normalization is what let ".", "./", ".//", and
		// "./." all collapse to the literal "." trivy actually receives,
		// while only a bare "." was rejected before this fix (single-source
		// review P1-1, proven against real trivy 0.72.0: with any of those
		// four as --skip-dirs, a fixture tree with 2 planted secrets found 0).
		norm := strings.TrimLeft(filepath.ToSlash(filepath.Clean(trimmed)), "/")
		if norm == "." {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- normalizes to %q, a catch-all entry that would "+
					"disable the repo's only whole-tree secret scanner; use a specific repo-relative "+
					"directory",
				trivySkipDirsSpecPath, trimmed, norm,
			)
		}
		// A slash-only or otherwise all-slash entry (e.g. "/", "//", "/.")
		// normalizes to the empty string here, NOT to ".". That is a
		// DIFFERENT failure mode than the catch-all above and the message
		// says so rather than reusing it: proven against real trivy 0.72.0
		// on the same 2-planted-secret fixture, --skip-dirs '' (and '/',
		// '//', '/.') left both secrets findable, unlike --skip-dirs '.'
		// which found zero. Read against trivy's own source
		// (pkg/fanal/utils/utils.go): CleanSkipPaths does NOT drop an
		// empty-after-clean entry, it keeps it in the list unchanged; the
		// no-op comes one step later, in SkipPath, which matches each
		// remaining pattern against the walked path with
		// doublestar.Match(pattern, path) -- an empty pattern only matches
		// an empty path string, and no real repo-relative file path
		// normalizes to empty, so the entry can never match anything trivy
		// walks. This entry protects nothing -- it is dead weight, the
		// opposite of a catch-all, and is rejected for that reason
		// (#5925/#5927 round-7 review F1).
		if norm == "" {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- normalizes to the empty string, which trivy's "+
					"path matcher can never match against a real repo-relative file path; this entry "+
					"skips nothing, it is dead weight -- use a specific repo-relative directory",
				trivySkipDirsSpecPath, trimmed,
			)
		}
		// ".." -- and any deeper escape sharing its "../" prefix, e.g.
		// "../.." or "../foo" -- is rejected for a DIFFERENT reason than
		// the catch-all check above, and the message says so rather than
		// overclaiming: unlike ".", it does not zero out coverage (proven
		// against real trivy 0.72.0: --skip-dirs '..' left both planted
		// secrets findable, unlike --skip-dirs '.'). It is rejected
		// because it escapes the repository root the skip-dirs list is
		// defined relative to, which makes it a meaningless entry
		// regardless of its effect on any one scan -- not because it
		// disables the scan the way a catch-all does.
		//
		// Checking the "../" PREFIX rather than only the exact two-char
		// literal closes the CLASS the same way the glob-metacharacter
		// check above closes its own class (round-3 review P2-1): an exact
		// `norm == ".."` literal check left "../.." and "../foo" -- both of
		// which also escape the repository root, for the exact same reason
		// -- unrejected, an instance-by-instance gap this package's three
		// review rounds have repeatedly found and closed elsewhere in this
		// file rather than here. Every entry sharing the "../" prefix
		// escapes the root the same way ".." does, so none of them zero out
		// coverage either -- the error message stays the escape-root
		// message, never the catch-all one.
		if norm == ".." || strings.HasPrefix(norm, "../") {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- normalizes to %q, which escapes the repository "+
					"root the skip-dirs list is defined relative to; use a specific repo-relative directory",
				trivySkipDirsSpecPath, trimmed, norm,
			)
		}
		if strings.HasPrefix(trimmed, "/") {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q -- an absolute path is not repo-relative; use a "+
					"path relative to the repository root",
				trivySkipDirsSpecPath, trimmed,
			)
		}
		entries = append(entries, trimmed)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf(
			"trivy skip-dirs parity: %s has no directory entries -- add at least one non-comment line",
			trivySkipDirsSpecPath,
		)
	}

	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, dup := seen[e]; dup {
			return nil, fmt.Errorf(
				"trivy skip-dirs parity: %s lists %q more than once -- each directory must appear exactly once",
				trivySkipDirsSpecPath, e,
			)
		}
		seen[e] = struct{}{}
	}
	return entries, nil
}
