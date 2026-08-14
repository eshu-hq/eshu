// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// This file owns the FREE-TEXT redaction domain. It was split out of redact.go
// when that file passed the repo's 500-line cap; the query-input and
// evidence-domain walks stayed there. Everything the two domains share — the
// isRedactedKeyName predicate and collector.IsSensitiveKeyName underneath it —
// still lives in one place, so the split cannot let the domains drift on what a
// sensitive key is.

// The Redaction.Rules entries recorded when the free-text scan removes
// something. Each names the FIELD, not the embedded key that was found:
// "api_key" in the rules list would be indistinguishable from a dropped
// parameter of that name, and the field is what a maintainer accounting for a
// shortened note or a truncated error message needs to look at.
const (
	reporterNoteRule       = "reporter_note"
	errorMessageRule       = "response_error_message"
	errorCorrelationIDRule = "response_error_correlation_id"
)

// freeTextMarker replaces a removed span inside a free-text field.
//
// It must contain neither "=" nor ":", or the text this package just cleaned
// would trip its own scan on the next pass and Capture would emit a bundle its
// own Validate rejects — the same self-inconsistency the design note on
// redactValue explains for masked-in-place object keys. The marker also carries
// no sensitive word, so it survives collector.ValidateShareSafeKeys wherever a
// caller copies it.
const freeTextMarker = "[redacted]"

// freeTextValueTerminators end the VALUE half of a "key=value" pair found in
// free text. Whitespace and quotes bound a pasted shell word; the pair
// separators bound one parameter inside a pasted URL. A credential containing
// one of these would be cut short rather than removed whole, which is why the
// header form below does not use them.
//
// The URL half is urlredact.PairSeparators, spliced in rather than spelled out.
// This was the THIRD copy of "?&;" in the repository and the one this walk
// actually reads — sharing only queryPairSeparators in redact.go left it able
// to drift on its own, which a probe caught: changing the shared constant moved
// cmd/eshu's walk and left this one untouched.
const freeTextValueTerminators = " \t\r\n" + urlredact.PairSeparators + "'\"`"

// isNoteKeyRune reports whether r can be part of a key name found in free text.
// It is deliberately narrower than an HTTP header token: letters, digits, "_"
// and "-" cover every name collector.IsSensitiveKeyName matches
// ("Authorization", "X-Api-Key", "access_token") while keeping the walk-back
// from a separator anchored on one word.
func isNoteKeyRune(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isNotePadRune reports whether r is padding a reporter may leave between a key
// name and its separator, or between a separator and its value: the quote a
// shell or a JSON blob wraps them in, and the spaces prose puts around an "=".
//
// "+" is in the set because it is how a query string spells a space, so
// "?token+=+<credential>" is the same pair as "?token = <credential>" and used
// to walk back to an empty key. It only ever matters directly beside a
// separator, so "a+b=c" still reads its key as "b".
func isNotePadRune(r rune) bool {
	return r == '"' || r == '\'' || r == '`' || r == ' ' || r == '\t' || r == '+'
}

// noteRuneBefore reads one position leftwards from line[:i], unwrapping a
// percent-escape when one ends there. It returns the character that position
// stands for and how many bytes of line it took.
//
// The escape is tried first: "%5F" is a "_", so a name written "api%5Fkey" —
// which is what the endpoint walk has always decoded, and what free text used to
// read as "5Fkey" — walks back as one key.
func noteRuneBefore(line string, i int) (rune, int) {
	if b, ok := urlredact.DecodedEscapeBefore(line, i); ok {
		return rune(b), urlredact.EscapeWidth
	}
	r, size := utf8.DecodeLastRuneInString(line[:i])
	return r, size
}

// sensitiveKeyBefore returns the start index of a sensitive-named key ending
// just before sep. It skips padding first, so `{"api_key":` and
// `-H 'Authorization:` both resolve to the bare name, then walks back over key
// runes.
//
// The name is unwrapped one layer before the predicate sees it, so the
// predicate is asked about "api_key" whether the reporter wrote that or
// "api%5Fkey". The unwrap is on the NAME only and the returned index still
// points into the original bytes, so the text this walk emits is the text it was
// given.
func sensitiveKeyBefore(line string, sep int) (int, bool) {
	end := sep
	for end > 0 {
		r, size := noteRuneBefore(line, end)
		if !isNotePadRune(r) {
			break
		}
		end -= size
	}
	start := end
	for start > 0 {
		r, size := noteRuneBefore(line, start)
		if !isNoteKeyRune(r) {
			break
		}
		start -= size
	}
	key := line[start:end]
	if decoded, changed := urlredact.Decode(key); changed {
		key = decoded
	}
	if key == "" || !isRedactedKeyName(key) {
		return 0, false
	}
	return start, true
}

// noteValueStart returns the index the value half begins at, skipping the
// padding between the separator and the value. Skipping an opening quote
// matters: `api_key="sk-live"` would otherwise end its value at that very quote
// and leave the credential behind.
//
// valueStart is the index just past the separator, which is three bytes past it
// when the separator was written "%3D".
func noteValueStart(line string, valueStart int) int {
	for valueStart < len(line) {
		b, width := urlredact.DecodedByteAt(line, valueStart)
		if width == 0 || !isNotePadRune(rune(b)) {
			break
		}
		valueStart += width
	}
	return valueStart
}

// redactFreeText is the scan for the FREE-TEXT domain: Bundle.ReporterNote,
// Response.Error.Message, and Response.Error.CorrelationID. It returns the
// cleaned text and whether anything was removed.
//
// The domain is "prose that can contain reporter-typed bytes", not "fields the
// reporter typed directly". Response.Error.Message is composed SERVER-side and
// is still in it, because composing is exactly how the reporter's own string
// gets in: query/service_workload_resolution.go:39 formats the caller's service
// selector into "service selector %q matched multiple services", and
// query/service_story_seam.go:129 puts that sentence in the envelope beside a
// details.selector holding the identical value. The structured half was
// redacted and the sentence was not, so a selector of
// "checkout?token=<credential>" shipped inside a bundle stamped
// profile=public / validation=passed. More than twenty sites in the query
// package assign a non-literal Message — some interpolating err.Error()
// directly, most passing a variable composed further up — so the rule belongs
// at this egress boundary rather than at each composer.
//
// CorrelationID is in the domain for the same reason one level over: it is not
// server-minted when the caller sends one. query/documentation.go:470 returns
// the request's own X-Correlation-ID or X-Request-ID header verbatim, and
// query/auth.go:430 puts that straight into an error envelope without the
// character allowlist safeAuditCorrelationID applies on the audit path.
//
// One field elsewhere in the bundle is free text and is deliberately NOT in
// this domain, recorded here rather than left silent because the package rule
// is that an exemption needs a reason beside it: Evidence.FactRefsReason. It is
// a CaptureInput field, so a programmatic caller could put anything in it, but
// no production caller sets it — `eshu report capture` never touches it, so it
// is always defaultFactRefsUnavailableReason, a package constant. Give it a
// reporter-typed route and it belongs in this domain.
//
// Code, Capability and Profiles are NOT scanned. Each is a server-side constant
// — an ErrorCode enum value, a capability name declared as a package const, and
// a QueryProfile pair — with no route from caller input, so there is nothing in
// them for a scan to find.
//
// Why it exists: ReporterNote used to be assigned verbatim, and the
// documentation asks reporters to describe how they reproduced the wrong
// answer (docs/public/guides/report-wrong-answer.md), so the realistic note is
// a pasted curl carrying the reporter's own credential. The bytes
// "next=/api/v0/x?api_key=sk-live" were REJECTED in Query.Target and ACCEPTED
// in ReporterNote — the same text the same person typed, two different
// verdicts, decided by which field held it. The note is reporter-typed input,
// so by this package's provenance rule it belongs in the same domain as
// Query.Target and Query.Params; only the shape of the text differs.
//
// Free text is not a query string, so this cannot reuse embeddedSensitiveKey:
// that function splits on query separators and skips any candidate key holding
// whitespace, which is right for a parsed parameter value and wrong for prose.
// This scans line by line for the two shapes a pasted command actually uses.
//
//   - "key=value" — the URL form, the one the asymmetry above was measured on.
//     The value is removed up to the first freeTextValueTerminators character,
//     so "curl 'https://h/x?repo=demo&api_key=sk-live'" keeps everything except
//     the credential and its key.
//   - "key: value" — the header form, "-H 'Authorization: Bearer sk-live'" and
//     "-H 'X-Api-Key: ...'". Covered because a pasted curl is the expected
//     content and a header is how curl carries auth, so leaving it out would
//     close the asymmetry while missing the likeliest real leak. An HTTP header
//     value may contain spaces, so there is no safe inner boundary: the removal
//     runs from the key to the END OF THE LINE. That over-removes — a second
//     -H on the same line goes with it — and over-removal is the side to err on.
//
// Quotes and spaces around either separator are skipped, so a pasted JSON blob
// ({"api_key": "sk-live"}) and a prose "api_key = sk-live" are both found. That
// is wider than embeddedSensitiveKey, which skips any candidate key holding
// whitespace: there a false positive costs a maintainer a query parameter they
// needed to reproduce the bug, while here it costs part of a sentence — and
// prose is exactly where a spaced pair turns up.
//
// The rule stays structural. It asks collector.IsSensitiveKeyName about the
// token to the left of a separator, exactly as every other walk in this package
// does; it never judges what a value looks like. No entropy check, no
// secret-pattern list. "We scan for secrets" is a claim nobody can falsify, and
// the narrow claim this makes is the one the README and the reporter guide
// state.
//
// A credential carried as a QUERY PARAMETER is found here. The "key=value" rule
// matches anywhere on the line, so a message or note holding
// "https://host/mcp?token=<credential>" keeps the URL and loses the pair.
// redactEndpoint in cmd/eshu/first_run_evidence.go does the same for a
// structured endpoint field.
//
// Sharing collector.IsSensitiveKeyName is what keeps the two walks from
// drifting on which NAMES count. It says nothing about where a pair ENDS, and
// the previous version of this comment claimed the two "cannot disagree" as if
// it did. They did disagree: this file ended a value at freeTextValueTerminators
// (which include "?", "&" and ";") while redactEndpoint split on "&" alone, so
// "?a=1;token=<credential>", "?next=/v0/y?api_key=<credential>" and
// "?redirect_uri=/cb?access_token=<credential>" all reached an operator
// artifact whole. The separators are now defined once, in
// internal/urlredact.PairSeparators, and both walks are driven through the one
// shared corpus in urlredact.BoundaryCases.
//
// Deliberate limits. The first three are embeddedSensitiveKey's own limits, in
// the same words; the rest belong to this walk alone, which is why the list is
// written out rather than claiming parity with a function that carries fewer:
//   - A bare secret with no key in front of it ("I used sk-live-abc") is
//     invisible.
//   - A credential in a path segment, or under a name the sensitive-key
//     predicate does not match, is invisible.
//   - EXACTLY ONE layer of percent-encoding is unwrapped, so "%3D" is an "=" and
//     "%253D" is three characters of text. urlredact/escape.go carries the
//     reason, and it is sharper here than for a detector: this walk EMITS what
//     it scanned, Validate scans that output again, and a reader that peeled
//     until the text stopped changing would make Capture reject its own bundle.
//   - URL USERINFO is invisible: in "https://alice:s3cr3t@host/x" the token to
//     the left of the ":" is "alice", which no sensitive-key rule matches. A
//     structured field gets that case from redactEndpoint; free text does not.
//   - A credential carried onto the NEXT LINE is found only when a shell
//     continuation backslash says the line carried on (see noteLineContinues).
//     A note wrapped by a terminal, with no backslash, leaves a bare secret on
//     its own line, which is the first limit above.
//   - The cost of the header form is prose false positives: "no authorization:
//     the call 403s" loses the rest of that line. That is a shortened note
//     recorded in Redaction.Rules, against a live credential on a public issue.
func redactFreeText(text string) (string, bool) {
	// "%" is in the fast-path set because a separator can be spelled "%3D" or
	// "%3A", with no literal "=" or ":" anywhere in the text.
	if !strings.ContainsAny(text, "=:%") {
		return text, false
	}
	lines := strings.Split(text, "\n")
	redacted := false
	continued := false
	for i, line := range lines {
		if continued {
			lines[i] = freeTextMarker
			redacted = true
			continued = noteLineContinues(line)
			continue
		}
		cleaned, changed, toEnd := redactFreeTextLine(line)
		if !changed {
			continue
		}
		lines[i] = cleaned
		redacted = true
		continued = toEnd && noteLineContinues(line)
	}
	if !redacted {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// noteLineContinues reports whether a shell continuation carries this line's
// text onto the next one.
//
// It is what keeps a credential from surviving a line break in the middle of a
// pair. A reporter who wraps a pasted curl writes
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
// note line that happens to end in a backslash costs nothing.
func noteLineContinues(line string) bool {
	return strings.HasSuffix(strings.TrimRight(line, " \t\r"), `\`)
}

// redactFreeTextLine applies the header rule first, because it removes the
// rest of the line and so subsumes any "key=value" pair sitting after the
// header name on it.
//
// The header rule subsumes only what follows the header name. The text BEFORE
// it still needs the pair walk, because the canonical pasted curl carries both
// shapes on one line:
//
//	curl 'https://h/x?token=AAA' -H 'X-Api-Key: BBB'
//
// Truncating at the header and returning the prefix untouched left "token=AAA"
// in the bundle. Worse, it left the text still dirty, so the next pass removed
// the pair and the scan was no longer idempotent — and Validate runs that next
// pass. Capture rejected its own output ("captured bundle failed its own
// share-safe validation gate") and the reporter got no bundle at all, which is
// exactly the failure capture.go's #5059 note says this package must not cause.
// The third return value reports whether the removal ran to the end of the
// line, which is what noteLineContinues needs to decide if a credential carried
// on past the newline.
func redactFreeTextLine(line string) (string, bool, bool) {
	if start, found := sensitiveHeaderKeyStart(line); found {
		prefix, _, _ := redactNoteKeyValuePairs(line[:start])
		return prefix + freeTextMarker, true, true
	}
	return redactNoteKeyValuePairs(line)
}

// sensitiveHeaderKeyStart returns the byte index where the first
// sensitive-named "key:" on the line begins. The ":" counts whether it is
// written literally or as "%3A".
func sensitiveHeaderKeyStart(line string) (int, bool) {
	for i := 0; i < len(line); {
		separator, width := urlredact.DecodedByteAt(line, i)
		if separator != ':' {
			i += width
			continue
		}
		if start, ok := sensitiveKeyBefore(line, i); ok {
			return start, true
		}
		i += width
	}
	return 0, false
}

// redactNoteKeyValuePairs replaces every sensitive "key=value" span on one line,
// keeping the text around them. Every pair on the line is handled, not only the
// first: a pasted URL can carry two.
//
// The "=" and the terminator that ends the value are both read one layer
// decoded, so "?redirect_uri=%2Fcb%3Faccess_token%3D<credential>" — how an HTTP
// client writes the nested callback URL — is cut at the same places
// "?redirect_uri=/cb?access_token=<credential>" is. The bytes around the removed
// span are copied through in the spelling they arrived in.
//
// It reports whether the last removal reached the end of the line.
func redactNoteKeyValuePairs(line string) (string, bool, bool) {
	var out strings.Builder
	redacted := false
	runsToEnd := false
	// cursor is the start of the text not yet copied into out.
	cursor := 0
	for i := 0; i < len(line); {
		separator, width := urlredact.DecodedByteAt(line, i)
		if separator != '=' {
			i += width
			continue
		}
		start, ok := sensitiveKeyBefore(line, i)
		if !ok || start < cursor {
			i += width
			continue
		}
		valueStart := noteValueStart(line, i+width)
		end := len(line)
		if offset, _ := urlredact.IndexDecodedAny(line[valueStart:], freeTextValueTerminators); offset >= 0 {
			end = valueStart + offset
		}
		out.WriteString(line[cursor:start])
		out.WriteString(freeTextMarker)
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
