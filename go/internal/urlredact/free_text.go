// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// This file owns the FREE-TEXT walk: the scan that finds a credential written
// as "key=value" or "key: value" inside PROSE. Query, one file over, scans a
// parsed query string. Both remove the value of a pair whose name
// collector.IsSensitiveKeyName flags, and neither judges what a value looks
// like.
//
// The walk lives here rather than in the package that first needed it because
// the two walks have to agree on where a pair ends and twice did not. The
// terminator set below splices in PairSeparators, and this file was the third
// copy of "?&;" in the repository before that — the one that was easy to miss,
// because it reads the separators through its own constant rather than by
// calling Query. Keeping both walks in one package is what makes a boundary
// change reach both of them.
//
// Two consumers drive it: internal/reportbundle scrubs a support bundle's
// reporter note and error envelope, and internal/cli/evidredact scrubs the
// first-run evidence artifact's free-form fields.

// FreeTextMarker replaces a removed span inside free text.
//
// It contains neither "=" nor ":", so text this walk just cleaned does not trip
// the walk again on the next pass. That matters because both consumers re-scan
// their own output: reportbundle.Capture runs Validate over the bundle it just
// wrote, and a first-run evidence artifact is re-rendered from a saved
// envelope. A marker that tripped the scan would make each of them reject or
// rewrite work it had already finished.
//
// It also carries no sensitive word, so it survives
// collector.ValidateShareSafeKeys wherever a caller copies it.
const FreeTextMarker = "[redacted]"

// freeTextValueTerminators end the VALUE half of a "key=value" pair found in
// free text. Whitespace and quotes bound a pasted shell word; the pair
// separators bound one parameter inside a pasted URL. A credential containing
// one of these would be cut short rather than removed whole, which is why the
// header form below does not use them.
//
// The URL half is PairSeparators, spliced in rather than spelled out.
//
// Every byte here ends a value in its LITERAL spelling. Only the
// PairSeparators half also ends one when written as a percent-escape, and only
// inside a pair whose own "=" arrived encoded — see
// freeTextEscapedValueTerminators for what counting the prose half one layer
// down did to a credential.
const freeTextValueTerminators = " \t\r\n" + PairSeparators + "'\"`"

// FreeText removes every credential-shaped "key=value" and "key: value" span it
// finds in text, returning the cleaned text and whether anything was removed.
//
// alsoSensitive widens which NAMES count. It is OR'd with
// collector.IsSensitiveKeyName rather than replacing it, so a caller can only
// add names, never drop the shared credential set — reportbundle passes its
// inline-content key predicate through it, and a caller with nothing to add
// passes nil.
//
// The two shapes are the ones a pasted command actually uses:
//
//   - "key=value" — the URL form. The value is removed up to the first
//     freeTextValueTerminators character, so
//     "curl 'https://h/x?repo=demo&api_key=sk-live'" keeps everything except
//     the credential and its key.
//   - "key: value" — the header form, "-H 'Authorization: Bearer sk-live'" and
//     "-H 'X-Api-Key: ...'". An HTTP header value may contain spaces, so there
//     is no safe inner boundary: the removal runs from the key to the END OF
//     THE LINE. That over-removes — a second -H on the same line goes with it —
//     and over-removal is the side to err on.
//
// Quotes and spaces around either separator are skipped, so a pasted JSON blob
// ({"api_key": "sk-live"}) and a prose "api_key = sk-live" are both found.
//
// The rule stays structural. It asks the shared name predicate about the token
// to the left of a separator; it never inspects a value. No entropy check, no
// secret-pattern list. "We scan for secrets" is a claim nobody can falsify.
//
// Deliberate limits:
//   - A bare secret with no key in front of it ("I used sk-live-abc") is
//     invisible.
//   - A credential in a path segment, or under a name the sensitive-key
//     predicate does not match, is invisible.
//   - EXACTLY ONE layer of percent-encoding is unwrapped, so "%3D" is an "=" and
//     "%253D" is three characters of text. escape.go carries the reason, and it
//     is sharper here than for a detector: this walk EMITS what it scanned and
//     its callers scan that output again, so a reader that peeled until the text
//     stopped changing would make a caller reject its own artifact.
//   - URL USERINFO is invisible: in "https://alice:s3cr3t@host/x" the token to
//     the left of the ":" is "alice", which no sensitive-key rule matches. A
//     structured endpoint field gets that case from a URL walk; free text does
//     not.
//   - A credential carried onto the NEXT LINE is found only when a shell
//     continuation backslash says the line carried on (see
//     freeTextLineContinues). A note wrapped by a terminal, with no backslash,
//     leaves a bare secret on its own line, which is the first limit above.
//   - The cost of the header form is prose false positives: "no authorization:
//     the call 403s" loses the rest of that line.
//
// The result is a fixed point: FreeTextMarker trips none of the rules, so a
// second pass over the output changes nothing.
func FreeText(text string, alsoSensitive func(name string) bool) (string, bool) {
	// "%" is in the fast-path set because a separator can be spelled "%3D" or
	// "%3A", with no literal "=" or ":" anywhere in the text.
	if !strings.ContainsAny(text, "=:%") {
		return text, false
	}
	sensitive := freeTextKeyPredicate(alsoSensitive)
	lines := strings.Split(text, "\n")
	redacted := false
	continued := false
	for i, line := range lines {
		if continued {
			lines[i] = FreeTextMarker
			redacted = true
			continued = freeTextLineContinues(line)
			continue
		}
		cleaned, changed, toEnd := redactFreeTextLine(line, sensitive)
		if !changed {
			continue
		}
		lines[i] = cleaned
		redacted = true
		continued = toEnd && freeTextLineContinues(line)
	}
	if !redacted {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// freeTextKeyPredicate builds the name test this walk asks, as the union of the
// shared credential predicate and the caller's optional extra names.
//
// The union is built here rather than taken from the caller so the shared half
// cannot be narrowed. A caller that handed in a whole predicate could quietly
// drop "authorization" and every walk in the repo would stop agreeing about
// what a sensitive key is, which is the drift this package exists to end.
func freeTextKeyPredicate(alsoSensitive func(name string) bool) func(string) bool {
	if alsoSensitive == nil {
		return collector.IsSensitiveKeyName
	}
	return func(name string) bool {
		return collector.IsSensitiveKeyName(name) || alsoSensitive(name)
	}
}

// isFreeTextKeyRune reports whether r can be part of a key name found in free
// text. It is deliberately narrower than an HTTP header token: letters, digits,
// "_" and "-" cover every name collector.IsSensitiveKeyName matches
// ("Authorization", "X-Api-Key", "access_token") while keeping the walk-back
// from a separator anchored on one word.
func isFreeTextKeyRune(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isFreeTextPadRune reports whether r is padding a writer may leave between a
// key name and its separator, or between a separator and its value: the quote a
// shell or a JSON blob wraps them in, and the spaces prose puts around an "=".
//
// "+" is in the set because it is how a query string spells a space, so
// "?token+=+<credential>" is the same pair as "?token = <credential>" and used
// to walk back to an empty key. It only ever matters directly beside a
// separator, so "a+b=c" still reads its key as "b".
func isFreeTextPadRune(r rune) bool {
	return r == '"' || r == '\'' || r == '`' || r == ' ' || r == '\t' || r == '+'
}

// freeTextRuneBefore reads one position leftwards from line[:i], unwrapping a
// percent-escape when one ends there. It returns the character that position
// stands for and how many bytes of line it took.
//
// The escape is tried first: "%5F" is a "_", so a name written "api%5Fkey" —
// which is what Query has always decoded, and what this walk used to read as
// "5Fkey" — walks back as one key.
func freeTextRuneBefore(line string, i int) (rune, int) {
	if b, ok := DecodedEscapeBefore(line, i); ok {
		return rune(b), EscapeWidth
	}
	r, size := utf8.DecodeLastRuneInString(line[:i])
	return r, size
}

// freeTextKeyBefore returns the start index of a sensitive-named key ending just
// before sep. It skips padding first, so `{"api_key":` and
// `-H 'Authorization:` both resolve to the bare name, then walks back over key
// runes.
//
// The name is unwrapped one layer before the predicate sees it, so the predicate
// is asked about "api_key" whether the writer typed that or "api%5Fkey". The
// unwrap is on the NAME only and the returned index still points into the
// original bytes, so the text this walk emits is the text it was given.
func freeTextKeyBefore(line string, sep int, sensitive func(string) bool) (int, bool) {
	end := sep
	for end > 0 {
		r, size := freeTextRuneBefore(line, end)
		if !isFreeTextPadRune(r) {
			break
		}
		end -= size
	}
	start := end
	for start > 0 {
		r, size := freeTextRuneBefore(line, start)
		if !isFreeTextKeyRune(r) {
			break
		}
		start -= size
	}
	key := line[start:end]
	if decoded, changed := Decode(key); changed {
		key = decoded
	}
	if key == "" || !sensitive(key) {
		return 0, false
	}
	return start, true
}

// freeTextValueStart returns the index the value half begins at, skipping the
// padding between the separator and the value. Skipping an opening quote
// matters: `api_key="sk-live"` would otherwise end its value at that very quote
// and leave the credential behind.
//
// valueStart is the index just past the separator, which is three bytes past it
// when the separator was written "%3D".
func freeTextValueStart(line string, valueStart int) int {
	for valueStart < len(line) {
		b, width := DecodedByteAt(line, valueStart)
		if width == 0 || !isFreeTextPadRune(rune(b)) {
			break
		}
		valueStart += width
	}
	return valueStart
}

// freeTextLineContinues reports whether a shell continuation carries this line's
// text onto the next one.
//
// It is what keeps a credential from surviving a line break in the middle of a
// pair. Somebody who wraps a pasted curl writes
//
//	-H 'Authorization: \
//	Bearer <credential>'
//
// and every rule in this file stops at the newline, so the header rule removed
// the empty remainder of the first line and left the credential standing alone
// on the second. The following line is then removed WHOLE: there is no key on it
// to anchor a narrower boundary, and a line reached this way is the tail of a
// value that was already judged sensitive.
//
// Only a line whose redaction ran to its very end can continue, so an ordinary
// line that happens to end in a backslash costs nothing.
func freeTextLineContinues(line string) bool {
	return strings.HasSuffix(strings.TrimRight(line, " \t\r"), `\`)
}

// redactFreeTextLine applies the header rule first, because it removes the rest
// of the line and so subsumes any "key=value" pair sitting after the header name
// on it.
//
// The header rule subsumes only what follows the header name. The text BEFORE it
// still needs the pair walk, because the canonical pasted curl carries both
// shapes on one line:
//
//	curl 'https://h/x?token=AAA' -H 'X-Api-Key: BBB'
//
// Truncating at the header and returning the prefix untouched left "token=AAA"
// in the output. Worse, it left the text still dirty, so the next pass removed
// the pair and the scan was no longer idempotent — and both callers run that
// next pass over their own output.
//
// The third return value reports whether the removal ran to the end of the line,
// which is what freeTextLineContinues needs to decide if a credential carried on
// past the newline.
func redactFreeTextLine(line string, sensitive func(string) bool) (string, bool, bool) {
	if start, found := freeTextHeaderKeyStart(line, sensitive); found {
		prefix, _, _ := redactFreeTextPairs(line[:start], sensitive)
		return prefix + FreeTextMarker, true, true
	}
	return redactFreeTextPairs(line, sensitive)
}

// freeTextHeaderKeyStart returns the byte index where the first sensitive-named
// "key:" on the line begins. The ":" counts whether it is written literally or
// as "%3A".
func freeTextHeaderKeyStart(line string, sensitive func(string) bool) (int, bool) {
	for i := 0; i < len(line); {
		separator, width := DecodedByteAt(line, i)
		if separator != ':' {
			i += width
			continue
		}
		if start, ok := freeTextKeyBefore(line, i, sensitive); ok {
			return start, true
		}
		i += width
	}
	return 0, false
}

// freeTextEscapedValueTerminators returns the terminators that ALSO end a value
// when written as a percent-escape, given how the "=" that opened the pair was
// spelled. Everything in freeTextValueTerminators ends a value in its literal
// spelling at either depth; this is only about the escaped one.
//
// A pair joined by a LITERAL "=" is text at the depth the writer typed, so an
// escape inside its value is characters OF the credential: "?token=aa%26bb" is a
// token whose value is "aa&bb". Reading that escape as a boundary cut the
// credential at the "%26" and left "bb" behind — a partial leak introduced by
// making the terminator scan decode-aware without asking at which depth. "%3B",
// "%20", "%27" and "%3F" all did the same thing. So nothing is escaped-structure
// there, and the empty set says exactly that.
//
// A pair whose "=" arrived percent-encoded sits one layer down, where an HTTP
// client wrote the whole structure encoded. There a "%26" IS the separator it
// stands for, which is what
// "?redirect_uri=%2Fcb%3Faccess_token%3D<credential>%26repo%3Ddemo" needs.
//
// But only PairSeparators, NOT the whole terminator set. Whitespace, quotes and
// a backtick are prose delimiters, and an encoder writes "%20" precisely because
// the space is inside a value — the escaped spelling is evidence of content, not
// of a boundary. Counting them one layer down cut the credential out of that same
// nested callback URL and left the tail behind: "%20", "%22", "%27", "%09" and
// "%0A" every one of them. This is the depth rule in the direction opposite to
// the paragraph above, and both directions leak when they are collapsed into one
// set.
func freeTextEscapedValueTerminators(separatorWidth int) string {
	if separatorWidth == EscapeWidth {
		return PairSeparators
	}
	return ""
}

// redactFreeTextPairs replaces every sensitive "key=value" span on one line,
// keeping the text around them. Every pair on the line is handled, not only the
// first: a pasted URL can carry two.
//
// The "=" is read one layer decoded, so
// "?redirect_uri=%2Fcb%3Faccess_token%3D<credential>" — how an HTTP client
// writes the nested callback URL — is found where
// "?redirect_uri=/cb?access_token=<credential>" is. Which SPELLINGS of a
// terminator end the value then depends on how that "=" was written
// (freeTextEscapedValueTerminators). The bytes around the removed span are
// copied through in the spelling they arrived in.
//
// It reports whether the last removal reached the end of the line.
func redactFreeTextPairs(line string, sensitive func(string) bool) (string, bool, bool) {
	var out strings.Builder
	redacted := false
	runsToEnd := false
	// cursor is the start of the text not yet copied into out.
	cursor := 0
	for i := 0; i < len(line); {
		separator, width := DecodedByteAt(line, i)
		if separator != '=' {
			i += width
			continue
		}
		start, ok := freeTextKeyBefore(line, i, sensitive)
		if !ok || start < cursor {
			i += width
			continue
		}
		valueStart := freeTextValueStart(line, i+width)
		end := len(line)
		if offset, _ := IndexBoundaryBySpelling(line[valueStart:], freeTextValueTerminators, freeTextEscapedValueTerminators(width)); offset >= 0 {
			end = valueStart + offset
		}
		out.WriteString(line[cursor:start])
		out.WriteString(FreeTextMarker)
		cursor = end
		redacted = true
		runsToEnd = end == len(line)
		// Resume at the terminator that ended the value. end is always past i,
		// because valueStart is, so the scan cannot stall here.
		i = end
	}
	if !redacted {
		return line, false, false
	}
	out.WriteString(line[cursor:])
	return out.String(), true, runsToEnd
}
