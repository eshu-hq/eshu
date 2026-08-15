// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidredact

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// embeddedURLPattern matches an absolute URL embedded in free-form text. It
// stops at whitespace and at the quoting characters the evidence renderers use,
// so a URL at the end of a sentence or inside a Markdown code span is matched
// without swallowing the surrounding prose.
var embeddedURLPattern = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.\\-]*://[^\\s<>\"'`]+")

// urlTrailingPunctuation are characters that commonly follow a URL in a sentence
// but are not part of it. They are trimmed before redaction and restored
// afterwards so "reachable at http://host:1/x." keeps its full stop.
const urlTrailingPunctuation = ".,;:!?)]}"

// Text makes a composed, free-form string safe for an operator artifact.
//
// A name-keyed redactor is correct only until a value is composed from a raw
// one: the endpoint fields are redacted while a summary, hint, cause, or
// next-command built by interpolating the same endpoint is not. Free-form text
// carries a credential on three axes, and each of the three shipped a
// credential into a released artifact after the previous one was closed.
//
//   - A KNOWN raw value, interpolated. rawValues carries every raw string the
//     caller already redacts into a structured field, and each is replaced with
//     the same redacted form that field shows, so a composed string and the
//     field it was built from never disagree.
//   - An UNKNOWN absolute URL. Endpoint runs over any URL still present, which
//     catches endpoints this call site does not know about — one wrapped into a
//     transport error, or one restored from a saved envelope where the original
//     inputs are no longer available.
//   - A bare credential pair with NO URL AROUND IT. This is the axis the first
//     two hid. A 401 or 403 body reaches the artifact as the diagnosed cause,
//     and a body of {"error":"unauthorized","api_key":"<credential>"} contains
//     no "scheme://" anywhere, so it went past a walk that only ever looked at
//     URLs. Measured on `eshu first-run report`: five credentials in five
//     free-form fields reached both the Markdown and the JSON artifact, one line
//     under an endpoint field that was correctly redacted. urlredact.FreeText is
//     the same scan internal/reportbundle runs over a support bundle's reporter
//     note, reused rather than rewritten — a third copy of this logic is the
//     defect this file exists to remove.
//
// The URL axis and the other two are applied to DISJOINT spans, not one after
// the other over the whole string. Every absolute URL goes to Endpoint; only the
// text BETWEEN the URLs is scanned for raw values and bare pairs.
//
// Running the raw-value substitution over the URLs corrupted them, because a raw
// filesystem target can be a substring of a URL: a repo target of "//" rewrote
// "https://h/z" to "https:.../h/z", and "/h/z" did the same to the same URL. The
// operator then got an endpoint they could not match against their own config,
// and the URL stage no longer recognized it as a URL either. Nothing leaked, but
// nothing was readable.
//
// Running the free-text scan over the URLs is wrong for a second reason: it ends
// a value at "?", "&" or ";" and replaces the whole pair including its name, so
// "https://h/x?token=<credential>&repo=demo" would come back as
// "https://h/x?[redacted]&repo=demo" instead of the readable
// "https://h/x?token=redacted&repo=demo" Endpoint produces. Endpoint already
// removes a credential-named query value, the userinfo and the fragment, so the
// URL span loses nothing by the split.
//
// Inside one span the raw-value substitution runs FIRST; scrubURLFreeSpan
// carries what that order costs when it is reversed.
//
// Both stages recognize structure; neither judges what a value looks like. A
// credential in a URL PATH SEGMENT, one under a parameter name
// collector.IsSensitiveKeyName does not match, and a bare secret with no key
// beside it ("token is sk-live-abc") all survive.
//
// One limit belongs to the span split itself. The free-text header rule removes
// from a sensitive header name to the end of its span, and a URL ends a span, so
// "Authorization: Bearer https://h/x <credential>" loses the header and keeps
// the trailing word. A credential written after a URL that follows its own key
// is the residue; a credential before the URL, or on a line with no URL, is
// removed.
func Text(text string, rawValues []string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	cursor := 0
	for _, span := range embeddedURLPattern.FindAllStringIndex(text, -1) {
		out.WriteString(scrubURLFreeSpan(text[cursor:span[0]], rawValues))
		out.WriteString(redactEmbeddedURL(text[span[0]:span[1]]))
		cursor = span[1]
	}
	out.WriteString(scrubURLFreeSpan(text[cursor:], rawValues))
	return out.String()
}

// scrubURLFreeSpan cleans one span of text that contains no absolute URL: the
// known raw targets first, then the structural "key=value" / "key: value" walk.
//
// The order is load-bearing and it was measured on a raw target containing a
// space, which is an ordinary path on a developer machine. Substituting first
// turns "token=/Users/bob/my repos/demo" into "token=.../demo", which the pair
// walk then removes whole. Scanning first ends that same value at the space, so
// the pair walk removes "token=/Users/bob/my" and leaves "repos/demo" standing
// — and the substitution can no longer match it, because the raw target it was
// looking for is no longer in the text. Nothing about that residue is a
// credential, but it is the private layout Path exists to keep off the artifact.
//
// The reverse interference does not happen: a substitution writes ".../base",
// which holds no "=" and no ":", so it can neither invent a pair nor break the
// walk-back to a key name.
func scrubURLFreeSpan(span string, rawValues []string) string {
	if span == "" {
		return span
	}
	cleaned, _ := urlredact.FreeText(substituteRawTargets(span, rawValues), nil)
	return cleaned
}

// substituteRawTargets replaces each known raw filesystem target with the same
// redacted form the corresponding report field carries, so a composed string and
// the field it was built from never disagree.
//
// Only targets distinctive enough to identify are substituted; a bare "/a" would
// corrupt "/api/v0". The gate is structural rather than a byte length, because a
// length gate let "/u/bob" (6 bytes) stay whole in composed text while the
// selected-target field one line over already showed ".../bob" — the username
// leak Path exists to prevent, readable on the same artifact.
func substituteRawTargets(span string, rawValues []string) string {
	if span == "" {
		return span
	}
	for _, raw := range rawValues {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.HasPrefix(raw, "/") || strings.Count(raw, "/") < 2 {
			continue
		}
		if !strings.Contains(span, raw) {
			continue
		}
		redacted := Path(raw)
		if redacted == raw {
			continue
		}
		span = strings.ReplaceAll(span, raw, redacted)
	}
	return span
}

// redactEmbeddedURL redacts a single URL matched inside free-form text,
// preserving any trailing sentence punctuation the match absorbed.
func redactEmbeddedURL(match string) string {
	trimmed := strings.TrimRight(match, urlTrailingPunctuation)
	suffix := match[len(trimmed):]
	if trimmed == "" {
		return match
	}
	return Endpoint(trimmed) + suffix
}

// Texts applies Text to every element of a slice, returning a new slice so the
// caller's data is never mutated in place.
func Texts(values []string, rawValues []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, Text(v, rawValues))
	}
	return out
}

// Truth returns a copy of decoded truth metadata with every reachable string
// scrubbed, at any depth.
//
// It walked only the top level at first, on the reasoning that the truth
// vocabulary is a bounded label set. That reasoning does not hold on the re-emit
// path: `eshu first-run report` decodes an operator-supplied envelope into
// map[string]any, so the nesting is whatever that JSON carried, and a string one
// level down went into the artifact verbatim.
func Truth(truth map[string]any, rawValues []string) map[string]any {
	if truth == nil {
		return nil
	}
	out := make(map[string]any, len(truth))
	for k, v := range truth {
		out[k] = Value(v, rawValues)
	}
	return out
}

// Value scrubs every string reachable inside a decoded JSON value, recursing
// through objects and arrays and returning anything else unchanged. It mirrors
// reportbundle's own value walk so the two artifact walks have the same shape,
// and it copies rather than mutating because the caller's result stays raw for
// the run's own error reporting.
func Value(value any, rawValues []string) any {
	switch typed := value.(type) {
	case string:
		return Text(typed, rawValues)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = Value(v, rawValues)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = Value(v, rawValues)
		}
		return out
	default:
		return value
	}
}
