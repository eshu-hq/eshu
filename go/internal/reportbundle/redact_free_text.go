// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"
	"unicode"
	"unicode/utf8"
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
// free text. Whitespace and quotes bound a pasted shell word; "?", "&" and ";"
// bound one parameter inside a pasted URL. A credential containing one of these
// would be cut short rather than removed whole, which is why the header form
// below does not use them.
const freeTextValueTerminators = " \t\r\n?&;'\"`"

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
func isNotePadRune(r rune) bool {
	return r == '"' || r == '\'' || r == '`' || r == ' ' || r == '\t'
}

// sensitiveKeyBefore returns the start index of a sensitive-named key ending
// just before sep. It skips padding first, so `{"api_key":` and
// `-H 'Authorization:` both resolve to the bare name, then walks back over key
// runes.
func sensitiveKeyBefore(line string, sep int) (int, bool) {
	end := sep
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:end])
		if !isNotePadRune(r) {
			break
		}
		end -= size
	}
	start := end
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if !isNoteKeyRune(r) {
			break
		}
		start -= size
	}
	key := line[start:end]
	if key == "" || !isRedactedKeyName(key) {
		return 0, false
	}
	return start, true
}

// noteValueStart returns the index the value half begins at, skipping the
// padding between the separator and the value. Skipping an opening quote
// matters: `api_key="sk-live"` would otherwise end its value at that very quote
// and leave the credential behind.
func noteValueStart(line string, sep int) int {
	start := sep + 1
	for start < len(line) {
		r, size := utf8.DecodeRuneInString(line[start:])
		if !isNotePadRune(r) {
			break
		}
		start += size
	}
	return start
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
// "https://host/mcp?token=<credential>" keeps the URL and loses the pair. The
// sibling scrub in cmd/eshu/first_run_evidence.go used to keep that query string
// intact; redactEndpoint there now drops a sensitive parameter's value too, and
// it asks the same collector.IsSensitiveKeyName predicate this file does, so the
// two cannot disagree about which names count.
//
// Deliberate limits, the same ones embeddedSensitiveKey carries:
//   - A bare secret with no key in front of it ("I used sk-live-abc") is
//     invisible.
//   - A credential in a path segment, or under a name the sensitive-key
//     predicate does not match, is invisible.
//   - URL USERINFO is invisible: in "https://alice:s3cr3t@host/x" the token to
//     the left of the ":" is "alice", which no sensitive-key rule matches. A
//     structured field gets that case from redactEndpoint; free text does not.
//   - The cost of the header form is prose false positives: "no authorization:
//     the call 403s" loses the rest of that line. That is a shortened note
//     recorded in Redaction.Rules, against a live credential on a public issue.
func redactFreeText(text string) (string, bool) {
	if !strings.ContainsAny(text, "=:") {
		return text, false
	}
	lines := strings.Split(text, "\n")
	redacted := false
	for i, line := range lines {
		cleaned, changed := redactFreeTextLine(line)
		if changed {
			lines[i] = cleaned
			redacted = true
		}
	}
	if !redacted {
		return text, false
	}
	return strings.Join(lines, "\n"), true
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
func redactFreeTextLine(line string) (string, bool) {
	if start, found := sensitiveHeaderKeyStart(line); found {
		prefix, _ := redactNoteKeyValuePairs(line[:start])
		return prefix + freeTextMarker, true
	}
	return redactNoteKeyValuePairs(line)
}

// sensitiveHeaderKeyStart returns the byte index where the first
// sensitive-named "key:" on the line begins.
func sensitiveHeaderKeyStart(line string) (int, bool) {
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		if start, ok := sensitiveKeyBefore(line, i); ok {
			return start, true
		}
	}
	return 0, false
}

// redactNoteKeyValuePairs replaces every sensitive "key=value" span on one line,
// keeping the text around them. Every pair on the line is handled, not only the
// first: a pasted URL can carry two.
func redactNoteKeyValuePairs(line string) (string, bool) {
	var out strings.Builder
	redacted := false
	// cursor is the start of the text not yet copied into out.
	cursor := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '=' {
			continue
		}
		start, ok := sensitiveKeyBefore(line, i)
		if !ok || start < cursor {
			continue
		}
		valueStart := noteValueStart(line, i)
		end := len(line)
		if offset := strings.IndexAny(line[valueStart:], freeTextValueTerminators); offset >= 0 {
			end = valueStart + offset
		}
		out.WriteString(line[cursor:start])
		out.WriteString(freeTextMarker)
		cursor = end
		redacted = true
		// Resume at the terminator that ended the value; the loop's i++ lands
		// exactly there.
		i = end - 1
	}
	if !redacted {
		return line, false
	}
	out.WriteString(line[cursor:])
	return out.String(), true
}
