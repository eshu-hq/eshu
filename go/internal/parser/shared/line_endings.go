// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "bytes"

// NormalizeLineEndings returns source with every BARE carriage return -- a
// '\r' that is not immediately followed by '\n' -- rewritten to '\n'. A CRLF
// pair is left exactly as it is, and a source with no bare CR is returned as
// the caller's own slice with no copy.
//
// Why this exists (issue #6306). A classic-Mac file separates lines with a
// lone '\r' and carries no '\n' anywhere. Nothing in the parser tree counts
// that as a line break:
//
//   - tree-sitter advances StartPosition().Row only on '\n', so EVERY
//     AST-derived line_number on such a file is 1 -- measured directly: three
//     C functions on physical lines 1, 2 and 3 all report Row 0. That is why
//     patching the hand-rolled scanners alone could not have been enough.
//   - bufio.ScanLines splits only on '\n' (it merely TRIMS a trailing '\r'),
//     so the whole file arrives as one token.
//   - strings.Split(src, "\n"), strings.IndexByte(rest, '\n') and the
//     `(?m)$` regex anchor all share the same blind spot.
//
// The failure mode is silence: zero dependencies from a lockfile, zero public
// header roots, or every row stamped line 1. Normalizing once at the read
// boundary fixes the whole class instead of the individual scanners.
//
// The substitution is deliberately LENGTH-PRESERVING, and that property is
// load-bearing rather than incidental. Byte offsets into the returned buffer
// stay valid against the on-disk file, so everything downstream that maps an
// offset back to a position keeps working untouched: the JSONC offset
// translator (issue #5358), the SQL entity spans, and every IndexSource
// snippet. Collapsing CRLF to LF would have shifted all of those.
//
// Two consequences worth stating, because callers depend on them:
//
//   - An LF-only or CRLF-only source is returned unchanged, as the SAME
//     slice. Those two forms already produced correct line numbers, so this
//     function must not perturb them, and byte identity is the strongest
//     available proof that it does not.
//   - The caller's slice is never mutated. The rewrite allocates a copy. The
//     git collector hands the same bytes to both the parser and the content
//     snapshot whose digest becomes content identity, so an in-place edit
//     here would silently change the stored content of every classic-Mac
//     file.
func NormalizeLineEndings(source []byte) []byte {
	index := bytes.IndexByte(source, '\r')
	if index < 0 {
		return source
	}
	// Walk the CRs before allocating: a CRLF-only file is common (every file
	// authored on Windows) and must not pay a copy. Only a bare CR forces
	// one.
	for index >= 0 {
		if index+1 >= len(source) || source[index+1] != '\n' {
			return rewriteBareCarriageReturns(source, index)
		}
		next := bytes.IndexByte(source[index+2:], '\r')
		if next < 0 {
			return source
		}
		index += 2 + next
	}
	return source
}

// rewriteBareCarriageReturns copies source and replaces every bare '\r' from
// first onward with '\n'. first is the offset of the bare CR that forced the
// copy, so the prefix before it is known to need no rewriting.
func rewriteBareCarriageReturns(source []byte, first int) []byte {
	normalized := make([]byte, len(source))
	copy(normalized, source)
	for index := first; index < len(normalized); index++ {
		if normalized[index] != '\r' {
			continue
		}
		if index+1 < len(normalized) && normalized[index+1] == '\n' {
			index++
			continue
		}
		normalized[index] = '\n'
	}
	return normalized
}
