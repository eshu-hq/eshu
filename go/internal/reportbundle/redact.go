// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// inlineContentKeys names JSON object keys that carry verbatim inline content
// bytes rather than metadata, and so must never appear in a public-profile
// bundle even though they are not credential-shaped. "excerpt" mirrors
// query.evidenceCitation's own JSON tag
// (go/internal/query/evidence_citation.go:75): a query response commonly
// embeds an evidence/citation list directly inside its Data payload (not only
// via the separate Citations field this package's own CitationRef type
// exposes), and that embedded citation's Excerpt is exactly the "inline
// content bytes" the plan requires be stripped from every public bundle. This
// is a second, independent redaction rule from collector.IsSensitiveKeyName —
// it is not about credentials, it is about never inlining source content —
// so it is checked separately rather than folded into the SDK's
// sensitive-key predicate.
var inlineContentKeys = map[string]struct{}{
	"excerpt": {},
}

func isInlineContentKey(key string) bool {
	_, ok := inlineContentKeys[strings.ToLower(key)]
	return ok
}

// isRedactedKeyName is the single key-name predicate both walks and Validate
// ask, so no caller can drift from the others about what a sensitive key is.
func isRedactedKeyName(key string) bool {
	return collector.IsSensitiveKeyName(key) || isInlineContentKey(key)
}

// queryPairSeparators are the characters that end one key=value pair inside a
// query-string-shaped value: "?" opens a nested query, "&" and ";" separate
// pairs within one. A raw "&" only reaches a parsed value percent-encoded,
// because net/url would otherwise have split on it into its own parameter.
const queryPairSeparators = "?&;"

// embeddedSensitiveKey reports the first sensitive-named key found inside a
// query-string-shaped VALUE, such as the "api_key" in
// "/api/v0/x?api_key=sk-live-...".
//
// This is the one place this package looks at a value rather than a key, and
// the exception is narrow on purpose. It does not judge value CONTENT and it
// does not add a second sensitive-key heuristic: it re-parses a value that is
// itself shaped like a query string back into key=value pairs, then asks the
// same collector.IsSensitiveKeyName predicate everything else here asks. What
// changes is where key names are looked for, not what counts as one.
//
// It exists because a reporter-typed query input can carry a key=value pair in
// a place no key walk can reach. SplitTargetQuery splits the target on the
// first "?" only, so everything after a second "?" stays inside a parameter's
// value; --params can hand over the same string directly, at any nesting
// depth; and an error envelope's Details echoes a caller selector back. All
// three are the same object wearing different costumes, which is why this scan
// is attached to the query-input DOMAIN (see redactQueryInput) rather than to
// whichever arrival path a reviewer happened to find first.
//
// The value is scanned twice: as it arrived, and once percent-decoded. Only the
// target's query string is decoded upstream, by url.ParseQuery, so
// "/x%3Fapi_key%3Dsk-live" kept its escapes all the way here when it arrived
// through --params or a programmatic CaptureInput — no literal "?" and no
// literal "=" for the split to find, and both Capture and the Validate half
// that mirrors it waved it through. Which flag the reporter used is not a
// property of the text, so the decode happens here, not at one arrival point.
//
// Deliberate limits, because a caller must not read more into a pass than is
// there:
//   - Only a "key=value" shape is detected. A bare secret with no "key=" in
//     front of it ("?next=sk-live-...") is invisible, as is a secret under an
//     arbitrary benign name anywhere else in the bundle.
//   - EXACTLY ONE decode, whose result is never fed back in. Re-decoding until
//     the text stops changing would let a crafted value drive the loop, and
//     each extra layer describes something the server never received. So
//     "%253F" unwraps to "%3F" and stops there, undetected.
//   - A candidate key containing whitespace is skipped, since a real query
//     key never has one. That keeps prose values ("SELECT token = 1") from
//     costing a maintainer a parameter, at the price of missing a credential
//     written with spaces around its "=".
func embeddedSensitiveKey(value string) (string, bool) {
	if key, found := scanQueryShapedValue(value); found {
		return key, true
	}
	// QueryUnescape fails on a malformed escape ("%ZZ"); there is nothing to
	// scan then, and the raw pass above already ran. It also turns "+" into a
	// space, which can only suppress a match, since a candidate key holding
	// whitespace is skipped below.
	decoded, err := url.QueryUnescape(value)
	if err != nil || decoded == value {
		return "", false
	}
	return scanQueryShapedValue(decoded)
}

// scanQueryShapedValue is the structural half of embeddedSensitiveKey: it splits
// one string on the query separators and asks the shared key-name predicate
// about the left side of each pair. It is a separate function so the raw and
// decoded passes provably run the same rule.
func scanQueryShapedValue(value string) (string, bool) {
	if !strings.ContainsAny(value, queryPairSeparators) && !strings.Contains(value, "=") {
		return "", false
	}
	for _, pair := range strings.FieldsFunc(value, func(r rune) bool {
		return strings.ContainsRune(queryPairSeparators, r)
	}) {
		key, _, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		if key == "" || strings.ContainsFunc(key, unicode.IsSpace) {
			continue
		}
		if collector.IsSensitiveKeyName(key) || isInlineContentKey(key) {
			return key, true
		}
	}
	return "", false
}

// carriesEmbeddedSensitiveKey reports whether a value embeds a sensitive-named
// key in query-string-shaped text. A repeated or list-valued parameter is
// flagged when ANY of its elements is: the parameter is what gets dropped, and
// keeping the surviving elements of a list whose sibling held a credential
// would report a list the reporter never sent.
//
// Nested objects are not walked here. redactQueryInput recurses into them
// itself, so the drop lands on the smallest enclosing key rather than throwing
// away a whole object because one leaf three levels down was query-shaped.
func carriesEmbeddedSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case string:
		_, found := embeddedSensitiveKey(typed)
		return found
	case []any:
		for _, item := range typed {
			if carriesEmbeddedSensitiveKey(item) {
				return true
			}
		}
	}
	return false
}

// redactQueryInput is the walk for the REPORTER-TYPED QUERY-INPUT domain:
// Query.Target's parsed parameters, Query.Params, and an error envelope's
// Details, all of which hold text a reporter typed or a server echoed straight
// back from it. On top of the key-name rule redactValue applies, it drops any
// key whose VALUE is query-string-shaped and embeds a sensitive-named key
// (see embeddedSensitiveKey).
//
// Why a domain and not a path: three review rounds each closed this leak for
// one arrival route — the target string, then its parse failure, then its
// nested values — and each time the same credential reappeared through the
// next route. "May carry a reporter-typed credential inside a string value" is
// a property of where the data CAME FROM, not of how it got here. Capture
// merges every source into one map and then calls this once, so there is no
// path into Query.Params that can skip the scan.
//
// The dropped key is the outer parameter name, not the embedded one: "next" is
// what is missing from the bundle, and that is what a maintainer reading
// Redaction.Rules is trying to account for.
//
// A value that merely looks query-shaped survives. A Package URL's qualifier
// segment is literally "?arch=amd64&distro=debian-12", and `package_id` on the
// supply-chain routes accepts one; neither qualifier is a sensitive key name,
// so the parameter the report is about stays in the bundle.
func redactQueryInput(value any) (any, []string) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		var rules []string
		for key, child := range typed {
			if isRedactedKeyName(key) || carriesEmbeddedSensitiveKey(child) {
				rules = append(rules, key)
				continue
			}
			redactedChild, childRules := redactQueryInput(child)
			out[key] = redactedChild
			rules = append(rules, childRules...)
		}
		return out, rules
	case []any:
		out := make([]any, len(typed))
		var rules []string
		for i, child := range typed {
			redactedChild, childRules := redactQueryInput(child)
			out[i] = redactedChild
			rules = append(rules, childRules...)
		}
		return out, rules
	default:
		return value, nil
	}
}

// redactValue is the walk for the SERVER-PRODUCED EVIDENCE domain:
// Response.Data. It applies the key-name rule ONLY, and deliberately does not
// run the embedded-key scan redactQueryInput does.
//
// The exemption is recorded rather than assumed. Data holds the answer under
// investigation — search results, code topics, citation metadata — so a value
// there can legitimately contain "key=value" text that a query-input parameter
// never would, and dropping it would strip the evidence the bundle exists to
// carry. The known cost is that Eshu's API echoes some request parameters back
// into data (for example data.input on the supply-chain impact routes), so a
// credential the reporter typed can return through the server's echo. That gap
// is named in the package README and is not closed here.
//
// It walks a decoded JSON value (map[string]any / []any / scalars,
// the shape produced by json.Unmarshal into `any`) and, for every object key
// that collector.IsSensitiveKeyName or isInlineContentKey flags at any
// nesting depth, DROPS that key-value pair from the output entirely. It
// returns the redacted value plus the sorted-at-call-site list of key names
// it redacted (with a duplicate entry per occurrence, so a caller can see how
// many places a given key was found); Capture folds this into the
// bundle-level, de-duplicated Redaction.Rules manifest.
//
// Design note — key REMOVAL, not a same-key masked-value marker: the plan
// this package implements
// (docs/internal/design/4595-wrong-answer-report-capture-plan.md) describes
// redaction as replacing a sensitive key's VALUE with a fixed
// "[REDACTED:key-name]" marker while keeping the key. That is not
// mechanically consistent with mechanism 3 (reportbundle.Validate's
// fail-closed collector.ValidateShareSafeKeys gate over the finished
// document): the underlying validatePayloadKeys walk
// (sdk/go/collector/validation.go:276-299) flags a key by NAME alone,
// regardless of its value, and sensitiveQueryPattern is a substring match, so
// no renamed-but-recognizable key survives it either. Keeping
// "api_key": "[REDACTED:api_key]" in the tree would make Validate reject
// every bundle that ever redacted anything — the opposite of "a public bundle
// that trips it is a bug." Dropping the key instead means a properly redacted
// bundle can never disagree with its own validator: the sensitive key name is
// simply absent from the document tree. The stripped key names are still
// recorded, in Redaction.Rules — a []string of VALUES, which
// validatePayloadKeys never inspects as key names — so the artifact still
// documents which fields were removed and why.
func redactValue(value any) (any, []string) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		var rules []string
		for key, child := range typed {
			if isRedactedKeyName(key) {
				rules = append(rules, key)
				continue
			}
			redactedChild, childRules := redactValue(child)
			out[key] = redactedChild
			rules = append(rules, childRules...)
		}
		return out, rules
	case []any:
		out := make([]any, len(typed))
		var rules []string
		for i, child := range typed {
			redactedChild, childRules := redactValue(child)
			out[i] = redactedChild
			rules = append(rules, childRules...)
		}
		return out, rules
	default:
		return value, nil
	}
}
