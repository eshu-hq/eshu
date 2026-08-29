// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "bytes"

// NormalizeLineEndings returns source with every carriage return rewritten to
// '\n', but ONLY when source contains no '\n' at all. Any source that already
// carries a newline is returned as the caller's own slice, untouched.
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
// # Why the rule is file-scoped, not byte-scoped
//
// A '\r' byte carries no intrinsic meaning. The SAME byte is a line
// terminator in a classic-Mac file and ordinary payload inside a Go raw
// string, a regex, or a wire-format constant. Nothing local to the byte
// separates the two, so the decision has to be made about the FILE.
//
// The file-level signal is the absence of '\n'. A source with even one
// newline has an established LF or CRLF convention; its lines already number
// correctly, and every '\r' in it is therefore either half of a CRLF pair or
// literal data. Rewriting one of those changes a parsed VALUE in a file that
// had nothing wrong with it -- measured: a `GET /foo\rbar` route, whose Go
// runtime value is "/foobar" because strconv.Unquote drops '\r' from a raw
// string, came back as "GET /foo\nbar" with its method degraded from GET to
// ANY. A source with no newline at all has no other candidate terminator, so
// its '\r' bytes can only be separators.
//
// State plainly what this rule cannot do:
//
//   - A MIXED file -- LF or CRLF lines plus a bare '\r' that the author meant
//     as a separator -- keeps that '\r' and keeps the merged line it causes.
//     That case is byte-for-byte indistinguishable from a data '\r' in an
//     LF file, and between corrupting a literal in a healthy file and leaving
//     one line of a malformed file merged, the second is the smaller loss.
//   - A classic-Mac file that itself embeds a data '\r' inside a literal has
//     that byte rewritten too. Nothing can separate it from the surrounding
//     separators, and such a file parses as a single line today, so there is
//     no working behavior to protect.
//
// # Why the substitution is length-preserving
//
// A '\r' becomes a '\n' in place; no byte is added or removed. Byte offsets
// into the returned buffer stay valid against the on-disk file, so everything
// downstream that maps an offset back to a position keeps working untouched:
// the JSONC offset translator (issue #5358), the SQL entity spans, and every
// IndexSource snippet. Collapsing CRLF to LF would have shifted all of those,
// which is a second reason a CRLF file is never rewritten.
//
// Two consequences worth stating, because callers depend on them:
//
//   - Any source containing '\n' -- which is every LF and CRLF file -- is
//     returned as the SAME slice. Those forms already produced correct line
//     numbers, so this function must not perturb them, and byte identity is
//     the strongest available proof that it does not.
//   - The caller's slice is never mutated. The rewrite allocates a copy. The
//     git collector hands the same bytes to both the parser and the content
//     snapshot whose digest becomes content identity, so an in-place edit
//     here would silently change the stored content of every classic-Mac
//     file.
func NormalizeLineEndings(source []byte) []byte {
	// Test for '\n' first, not '\r'. Nearly every real file has a newline
	// within its first line, so this returns after a few dozen bytes; probing
	// for '\r' first would scan an entire LF-only file to prove a negative.
	if bytes.IndexByte(source, '\n') >= 0 {
		return source
	}
	first := bytes.IndexByte(source, '\r')
	if first < 0 {
		return source
	}
	return rewriteCarriageReturns(source, first)
}

// rewriteCarriageReturns copies source and replaces every '\r' from first
// onward with '\n'. It is only reached for a source with no '\n' anywhere, so
// there is no CRLF pair to preserve and every '\r' is a line separator. first
// is the offset of the leading '\r', so the prefix before it needs no scan.
func rewriteCarriageReturns(source []byte, first int) []byte {
	normalized := make([]byte, len(source))
	copy(normalized, source)
	for index := first; index < len(normalized); index++ {
		if normalized[index] == '\r' {
			normalized[index] = '\n'
		}
	}
	return normalized
}
