// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"net/url"
	"strings"
)

// EscapeWidth is the byte length of one percent-escape, as in "%3F".
const EscapeWidth = 3

// Why this file exists, and why every reader here unwraps EXACTLY ONE layer.
//
// A redaction walk finds a credential by finding the STRUCTURE around it: the
// "=" that joins a name to its value, and the "?", "&" or ";" that ends one
// pair. A browser, an HTTP client, or anything that builds a nested URL writes
// that structure percent-encoded — "?redirect_uri=%2Fcb%3Faccess_token%3D…" is
// the same bytes as "?redirect_uri=/cb?access_token=…" and neither the "?" nor
// the "=" is there for a walk to split on. Both walks in this repository read
// the literal spelling only, so the encoded one shipped whole. It is the same
// defect the separator constants had, one level down: agreeing on WHICH bytes
// bound a pair does not help when the bytes are spelled "%26".
//
// ONE layer, never a loop, for two reasons that both bite:
//
//   - Each further layer describes something no server ever received. "%253F"
//     is a request for the literal text "%3F", not for a "?", so unwrapping it
//     would make the walk redact a pair that does not exist and, worse, let a
//     crafted value drive how many times the loop runs.
//   - The free-text walk EMITS the text it scanned, and its output is scanned
//     again — reportbundle.Capture runs Validate over its own bundle. A reader
//     that peeled until the text stopped changing would hand back a string one
//     layer shallower than it arrived, so the next pass would peel one more and
//     find something new. Capture would then reject the bundle it just built,
//     which is the failure that leaves a reporter with no bundle at all.
//
// So "%253D" reads as the three characters "%25" followed by "3D", stays
// undetected, and that limit is stated wherever the walks are documented.

// DecodedByteAt reports the byte the text starting at s[i] stands for, and how
// many bytes of s that takes: EscapeWidth for a well-formed percent-escape,
// and 1 for anything else — including a "%" with no valid escape behind it,
// which is returned as a literal "%".
//
// Callers advance by the returned width. Advancing by 1 instead would let the
// scan restart inside an escape it has already read, where "3D" of a "%3D"
// looks like ordinary text.
func DecodedByteAt(s string, i int) (byte, int) {
	if i < 0 || i >= len(s) {
		return 0, 0
	}
	if s[i] != '%' || i+EscapeWidth > len(s) {
		return s[i], 1
	}
	high, highOK := unhex(s[i+1])
	low, lowOK := unhex(s[i+2])
	if !highOK || !lowOK {
		return s[i], 1
	}
	return high<<4 | low, EscapeWidth
}

// DecodedEscapeBefore reports the byte a percent-escape ending at s[:i] stands
// for. ok is false when s[:i] does not end in a well-formed escape, which is
// the caller's signal to fall back to reading one ordinary rune.
//
// It exists for the backwards walk that reads a key name leftwards from the
// separator it was found by. That walk cannot use DecodedByteAt, because it
// does not know where the escape starts until it has already stepped past it.
func DecodedEscapeBefore(s string, i int) (byte, bool) {
	if i < EscapeWidth || i > len(s) {
		return 0, false
	}
	decoded, width := DecodedByteAt(s, i-EscapeWidth)
	if width != EscapeWidth {
		return 0, false
	}
	return decoded, true
}

// BoundaryDepth selects which spellings of a structural byte a scan counts as
// that byte.
//
// It exists because "is this an escape for a separator?" has no answer on its
// own — it depends on how deep the text around it was encoded. Making every
// read decode-aware without this distinction turned value CONTENT into
// structure: "token=AAAA%26BBBB" is one credential whose value holds an "&",
// and reading that "%26" as a boundary cut the value in half and shipped
// "BBBB".
type BoundaryDepth int

const (
	// LiteralOnly counts a structural byte only where it is written as itself.
	// It is the depth of text the reporter typed at the surface: a pair joined
	// by a literal "=" sits there, so an escape inside its value belongs to the
	// value.
	LiteralOnly BoundaryDepth = iota
	// LiteralOrEscaped also counts one percent-escape standing for the byte. It
	// is the depth of a pair whose own "=" arrived encoded — an HTTP client
	// that wrote "%3D" wrote "%26" for the separator beside it, so both are
	// structure there.
	LiteralOrEscaped
)

// IndexBoundary returns the byte offset and width of the first position in s
// that stands for one of the bytes in set at the given depth. It returns
// (-1, 0) when there is none. At LiteralOnly the width is always 1.
//
// The width is returned because the caller has to keep the ORIGINAL spelling:
// a walk that replaced "%3D" with "=" would rewrite an endpoint nobody asked to
// have rewritten, and the operator then cannot match it against their own
// config.
func IndexBoundary(s, set string, depth BoundaryDepth) (int, int) {
	if depth == LiteralOnly {
		return IndexBoundaryBySpelling(s, set, "")
	}
	return IndexBoundaryBySpelling(s, set, set)
}

// IndexBoundaryBySpelling is IndexBoundary for a caller whose boundary set is
// not the same in both spellings: literal names the bytes counted when written
// as themselves, escaped names the bytes ALSO counted when written as one
// percent-escape. Passing "" for escaped is LiteralOnly; passing the same set
// for both is LiteralOrEscaped.
//
// The two sets exist because BoundaryDepth alone was too coarse for a walk over
// PROSE. reportbundle's free-text scan ends a value at whitespace, a quote or a
// backtick as well as at a PairSeparators byte, and reading that whole set one
// layer down cut a credential in half: an encoder writes "%20" precisely
// because the space is INSIDE a value, so the escaped spelling is evidence of
// content, not of a boundary. Only "?", "&" and ";" are URL structure at the
// encoded depth. The prose delimiters stay literal-only at both depths, which
// is what this signature lets a caller say.
func IndexBoundaryBySpelling(s, literal, escaped string) (int, int) {
	if escaped == "" {
		// literal holds ASCII only, and every byte of a multi-byte UTF-8 rune
		// is >= 0x80, so a byte scan cannot match inside one.
		if i := strings.IndexAny(s, literal); i >= 0 {
			return i, 1
		}
		return -1, 0
	}
	for i := 0; i < len(s); {
		decoded, width := DecodedByteAt(s, i)
		if width == 0 {
			break
		}
		// Which set applies is decided by how this position was SPELLED, not by
		// what it decodes to: at width 1 the byte is written as itself.
		set := literal
		if width != 1 {
			set = escaped
		}
		if strings.IndexByte(set, decoded) >= 0 {
			return i, width
		}
		i += width
	}
	return -1, 0
}

// Decode unwraps exactly one layer of percent-escapes across a whole string,
// for a caller that needs the decoded TEXT rather than a position in it — a key
// name read out of a URL, or a value being asked whether it hides a query
// string. ok is false when s holds a malformed escape ("%ZZ") or when decoding
// changed nothing, so a caller can skip a redundant second scan.
//
// It is url.PathUnescape, not url.QueryUnescape, and the difference is "+".
// QueryUnescape turns "+" into a space, which is right for one parsed query
// parameter and wrong here: this decoder also feeds a walk over PROSE, where a
// "+" is a plus sign, and it can only lose matches — "token+%3Dx" reads as
// "token =x", whose key holds whitespace and is skipped, while PathUnescape
// reads "token+=x" and finds the key.
func Decode(s string) (string, bool) {
	decoded, err := url.PathUnescape(s)
	if err != nil || decoded == s {
		return s, false
	}
	return decoded, true
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
