// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
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
// It exists because splitting the target's query string (SplitTargetQuery)
// splits on the first "?" only. Everything after a second "?" stays inside a
// parameter's value, so a credential lands under a benign name like "next" and
// survives both the key walk and the share-safe gate. Scanning the DECODED
// value also covers the percent-encoded form of the same trick.
//
// Deliberate limits, because a caller must not read more into a pass than is
// there:
//   - Only a "key=value" shape is detected. A bare secret with no "key=" in
//     front of it ("?next=sk-live-...") is invisible, as is a secret under an
//     arbitrary benign name anywhere else in the bundle.
//   - Only one decoding pass. A double-encoded nested query ("%253F") is not
//     unwrapped.
//   - A candidate key containing whitespace is skipped, since a real query
//     key never has one. That keeps prose values ("SELECT token = 1") from
//     costing a maintainer a parameter, at the price of missing a credential
//     written with spaces around its "=".
func embeddedSensitiveKey(value string) (string, bool) {
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

// redactValue walks a decoded JSON value (map[string]any / []any / scalars,
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
			if collector.IsSensitiveKeyName(key) || isInlineContentKey(key) {
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
