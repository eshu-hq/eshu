// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// flowSeedToken is a value the detector does not flag, so a body that keeps it
// leaked it through the YAML structure, not through a detector line.
const flowSeedToken = "Zq19FlowSeedAAAAAAAA"

// flowHostileSeeds are hostile .yarnrc.yml-shaped bodies where a listed key's
// value starts or continues on a line the redactor's more-indented continuation
// does not cover: the independent #8833 review (P1) found them stored verbatim
// with outcome clean. Parser coverage differs by shape. The "..."-prefixed
// fallback bodies decode the token under a listed key only in js-yaml 4.1.1;
// yaml.v3 and PyYAML 6.0.3 reject the bare document after "...". The
// "%YAML 1.2"-prefixed fallback bodies decode it in js-yaml 4.1.1 and PyYAML
// 6.0.3; yaml.v3 rejects the 1.2+ version (found incompatible YAML document).
// The BOM-prefixed, tab-indented, and unspaced ("%YAML1.2") directive
// variants decode it in js-yaml 4.1.1 only (PyYAML reads the BOM-prefixed
// one too); yaml.v3 rejects each, so only the lexical fallback covers them.
// The anchor-with-quoted-value-on-the-next-line bodies decode it in all
// three; yaml.v3 reports the scalar at the anchor, so only the forward
// placement covers a "#" tail. The anchor-on-the-value and tag-on-the-value
// bodies decode in yaml.v3 and PyYAML but error in js-yaml. The
// tab-after-dash bodies likewise decode only in js-yaml, and the
// tab-indented flow value errors only in PyYAML. The
// comment-looking line in a quoted key errors in all three (a multi-line
// quoted simple key): it is an invalid-in-all-parsers fail-closed shape, not a
// decoded-token shape. {T} is replaced with flowSeedToken.
var flowHostileSeeds = map[string]string{
	"flow map value on the next line":                            "{\nnpmAuthToken:\n{T}\n}\n",
	"flow map plain scalar second half":                          "{\nnpmAuthToken: abcd\n{T}\n}\n",
	"scope flow map, value less indented":                        "npmScopes:\n  acme: {npmAuthToken:\n {T}}\n",
	"scope flow map opened above the key":                        "npmScopes: {acme: {\nnpmAuthToken:\n{T}}}\n",
	"pretty-printed JSON":                                        "{\n  \"npmAuthToken\":\n  \"{T}\"\n}\n",
	"flow map in a block mapping":                                "npmScopes:\n  acme:\n    {npmAuthIdent:\n    {T}}\n",
	"compact JSON, value on the next line":                       "{\"npmScopes\": {\"acme\": {\"npmAuthToken\":\n\"{T}\"}}}\n",
	"flow map in a sequence":                                     "- {npmAuthToken:\n{T}}\n",
	"flow sequence of maps":                                      "[{npmAuthToken:\n{T}}]\n",
	"explicit document start":                                    "---\n{\nnpmAuthToken:\n{T}\n}\n",
	"second document":                                            "a: 1\n---\n{npmAuthToken:\n{T}}\n",
	"anchor on the key's value":                                  "{\nnpmAuthToken: &a\n{T}\n}\n",
	"anchor then a plain scalar":                                 "{npmAuthToken: &a abcd\n{T}}\n",
	"tag on the key's value":                                     "{\nnpmAuthToken: !!str\n{T}\n}\n",
	"value tail shares a line with the next key":                 "{\nnpmAuthToken: abcd\n{T}, other: 1\n}\n",
	"flow sequence value, tail with a sibling":                   "{npmAuthToken: [a,\n{T}], other: 1}\n",
	"tail before a URL key the redactor touched":                 "{npmAuthToken: abcd\n{T}, httpsProxy: http://user:pw@h/, z: 1}\n",
	"tail before a detector match":                               "{npmAuthToken: abcd\n{T}, note: " + "AKIA" + "ABCDEFGHIJKLMNOP" + ", z: 1}\n",
	"marker text beside a live tail":                             "{npmAuthToken: abcd\n{T} REDACTED_api_token, b: 1}\n",
	"marker text glued to a live value":                          "{\nnpmAuthToken:\n{T}-REDACTED_api_token\n}\n",
	"marker text before a live value":                            "{\nnpmAuthToken:\nREDACTED_api_token {T}\n}\n",
	"value nested deeper than 64 levels":                         "{npmAuthToken: " + strings.Repeat("[", 70) + "\n{T}" + strings.Repeat("]", 70) + "}\n",
	"comment-looking line in a double-quoted scalar":             "{npmAuthToken: [\"abcd\n#{T}\"]}\n",
	"comment-looking line in a single-quoted scalar":             "{npmAuthToken: ['abcd\n#{T}']}\n",
	"comment-looking line in a quoted flow map value":            "{npmAuthToken: {a: \"abcd\n#{T}\"}}\n",
	"tab then # in a quoted scalar":                              "{npmAuthToken: [\"abcd\n\t#{T}\"]}\n",
	"comment-looking line in a quoted key":                       "{npmAuthToken: {\"abcd\n#{T}\": 1}}\n",
	"comment-looking line after a tag":                           "{npmAuthToken: [!!str \"abcd\n#{T}\"]}\n",
	"hash line in a block scalar in a sequence":                  "npmAuthToken:\n  - |\n    #{T}\n",
	"comma-led line in an unparsable body":                       "...\n{npmAuthToken: [a\n, {T}]}\n",
	"closers then text in an unparsable body":                    "...\n{npmAuthToken: [[a,\n],{T}\n]}\n",
	"entry-like line in an unparsable body":                      "...\n{npmAuthToken: {\na: {T}\n}}\n",
	"quoted comment-like line in an unparsable body":             "...\n{npmAuthToken: [\"abcd\n#{T}\"]}\n",
	"plain quote earlier, then a quoted scalar (fallback)":       "...\n{npmAuthToken: [a\"b, \"abcd\n#{T}\"]}\n",
	"mid-scalar colon quote, then a quoted scalar (fallback)":    "...\n{npmAuthToken: [ab:\"x\", \"abcd\n#{T}\"]}\n",
	"mid-scalar colon quote, parsed twin":                        "{npmAuthToken: [ab:\"x\", \"abcd\n#{T}\"]}\n",
	"mid-scalar colon single quote, then a quoted scalar":        "...\n{npmAuthToken: [ab:'x', 'abcd\n#{T}']}\n",
	"apostrophe earlier, then a quoted scalar (fallback)":        "...\n{npmAuthToken: [it's, 'abcd\n#{T}']}\n",
	"closed quote, plain quote, then a quoted scalar (fallback)": "...\n{npmAuthToken: [\"x\", b\"c, \"abcd\n#{T}\"]}\n",
	"plain quote earlier, parsed twin":                           "{npmAuthToken: [a\"b, \"abcd\n#{T}\"]}\n",
	"apostrophe earlier, parsed twin":                            "{npmAuthToken: [it's, 'abcd\n#{T}']}\n",
	"quoted scalar after a tag (fallback)":                       "...\n{npmAuthToken: [it's, !!str \"abcd\n#{T}\"]}\n",
	"tagged quoted scalar in a flow sequence (fallback)":         "...\n{npmAuthToken: [!!str \"abcd\n#{T}\"]}\n",
	"anchored quoted scalar after a comma (fallback)":            "...\n{npmAuthToken: [a, &x 'abcd\n#{T}']}\n",
	"bracket opened on its own line (fallback)":                  "...\n{npmAuthToken:\n[\n{T}]}\n",
	"brace opened on its own line (fallback)":                    "...\n{npmAuthToken:\n{\na: {T}}}\n",
	"mapping value opened with an entry on its line (fallback)":  "...\n{npmAuthToken:\n{a: {T}}}\n",
	"CRLF":                                                                   "{\r\nnpmAuthToken:\r\n{T}\r\n}\r\n",
	"CRLF plain scalar second half":                                          "{\r\nnpmAuthToken: abcd\r\n{T}\r\n}\r\n",
	"comment between the key and the value":                                  "{\nnpmAuthToken: # c\n{T}\n}\n",
	"blank lines between the key and the value":                              "{\nnpmAuthToken:\n\n\n{T}\n}\n",
	"value is a flow sequence":                                               "{\nnpmAuthToken: [\n{T}\n]\n}\n",
	"value is a flow map":                                                    "{\nnpmAuthToken: {\na: {T}\n}\n}\n",
	"value is a flow map at the key's line":                                  "npmAuthToken: {\na: {T}\n}\n",
	"value is a flow sequence at the key's line":                             "npmAuthToken: [\n{T}\n]\n",
	"URL key, value on the next line":                                        "{\nnpmRegistryServer:\nhttps://user:{T}@registry.example.com/\n}\n",
	"URL key, plain scalar second half":                                      "{\nhttpsProxy: http://registry.example.com\nuser:{T}@proxy.example.com\n}\n",
	"comment line closes a block continuation":                               "npmAuthToken:\n# c\n  {T}\n",
	"comment and blank lines before the value":                               "npmScopes:\n  acme:\n    npmAuthToken:\n# c\n\n      {T}\n",
	"tab after the sequence dash":                                            "npmAuthToken:\n-\t{T}\n",
	"tab after the dash, nested":                                             "npmScopes:\n  acme:\n    npmAuthToken:\n    -\t{T}\n",
	"tab-indented flow value":                                                "{\n\t\tnpmAuthToken:\n\t{T}\n}\n",
	"double quoted, closes on a later line":                                  "{\nnpmAuthToken: \"abcd\n{T}\"\n}\n",
	"single quoted, closes on a later line":                                  "{\nnpmAuthToken: 'abcd\n{T}'\n}\n",
	"document-end-prefixed double-quoted continuation, closed low":           "...\nnpmAuthToken:\n  \"abcd\n{T}\"\n",
	"document-end-prefixed single-quoted continuation, closed low":           "...\nnpmAuthToken:\n  'abcd\n{T}'\n",
	"version-directive-prefixed double-quoted continuation, closed low":      "%YAML 1.2\n---\nnpmAuthToken:\n  \"abcd\n{T}\"\n",
	"version-directive-prefixed single-quoted continuation, closed low":      "%YAML 1.2\n---\nnpmAuthToken:\n  'abcd\n{T}'\n",
	"BOM-prefixed directive double-quoted continuation, closed low":          "\uFEFF%YAML 1.2\n---\nnpmAuthToken:\n  \"abcd\n{T}\"\n",
	"BOM-prefixed directive single-quoted continuation, closed low":          "\uFEFF%YAML 1.2\n---\nnpmAuthToken:\n  'abcd\n{T}'\n",
	"tab-indented directive double-quoted continuation, closed low":          "\t%YAML 1.2\n---\nnpmAuthToken:\n  \"abcd\n{T}\"\n",
	"tab-indented directive single-quoted continuation, closed low":          "\t%YAML 1.2\n---\nnpmAuthToken:\n  'abcd\n{T}'\n",
	"unspaced directive double-quoted continuation, closed low":              "%YAML1.2\n---\nnpmAuthToken:\n  \"abcd\n{T}\"\n",
	"unspaced directive single-quoted continuation, closed low":              "%YAML1.2\n---\nnpmAuthToken:\n  'abcd\n{T}'\n",
	"JSON key, quoted value folded":                                          "{\n  \"npmAuthToken\": \"ab\n{T}\"\n}\n",
	"quoted scalar opened on the next line, closed at the key indent":        "npmAuthToken:\n  \"abcd\n{T}\"\n",
	"single-quoted scalar opened on the next line, closed at the key indent": "npmAuthToken:\n  'abcd\n{T}'\n",
	"nested quoted scalar opened on the next line, closed low":               "npmScopes:\n  acme:\n    npmAuthToken:\n      \"abcd\n{T}\"\n",
	"URL key quoted scalar opened on the next line, closed low":              "npmRegistryServer:\n  \"https://user:abcd\n{T}@registry.example.com/\"\n",
	"anchor with double-quoted value on the next line, hash tail":            "npmAuthToken: &a\n  \"abcd\n#{T}\"\n",
	"anchor with single-quoted value on the next line, hash tail":            "npmAuthToken: &a\n  'abcd\n#{T}'\n",
}

func flowSeedBody(template string) string {
	return strings.ReplaceAll(template, "{T}", flowSeedToken)
}

// TestRedactMarkerFlowValueOffTheKeyLineNeverLeaks pins the #8833 P1: a valid
// .yarnrc.yml whose listed key's value sits on a later line that the redactor's
// continuation does not cover must not be stored with the value in it. Each
// body either fails closed (errYAMLIndirection, a metadata row) or comes back
// without the token. None may be a verbatim clean result.
func TestRedactMarkerFlowValueOffTheKeyLineNeverLeaks(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	store := registry.ContentStoreForPath(".yarnrc.yml")
	if store.Policy != parser.ContentStoreRedact {
		t.Fatalf(".yarnrc.yml binds %q, want redact", store.Policy)
	}
	for name, template := range flowHostileSeeds {
		body := flowSeedBody(template)
		result := ApplyContentStore(store, body)
		if strings.Contains(result.Body, flowSeedToken) {
			t.Errorf("%s: the token was stored (outcome %s):\n%s", name, result.Outcome, result.Body)
		}
		if result.Outcome != StoreOutcomeRedactorFailed && result.Outcome != StoreOutcomeRedacted {
			t.Errorf("%s: outcome = %s, want redactor_failed or redacted", name, result.Outcome)
		}
		if result.Outcome == StoreOutcomeRedactorFailed && result.Body != "" {
			t.Errorf("%s: a failed result carries a body: %q", name, result.Body)
		}
	}
}

// TestRedactMarkerFlowShapesFailClosed pins the exact answer for the shapes the
// review named: the redactor cannot reach the value, so the body becomes a
// metadata row with reason yaml_indirection.
func TestRedactMarkerFlowShapesFailClosed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"flow map value on the next line",
		"flow map plain scalar second half",
		"scope flow map, value less indented",
		"scope flow map opened above the key",
		"pretty-printed JSON",
		"flow map in a block mapping",
		"compact JSON, value on the next line",
		"flow sequence of maps",
		"explicit document start",
		"second document",
		"CRLF",
		"value is a flow sequence",
		"value is a flow map",
		"quoted scalar opened on the next line, closed at the key indent",
		"single-quoted scalar opened on the next line, closed at the key indent",
		"document-end-prefixed double-quoted continuation, closed low",
		"document-end-prefixed single-quoted continuation, closed low",
		"version-directive-prefixed double-quoted continuation, closed low",
		"version-directive-prefixed single-quoted continuation, closed low",
		"BOM-prefixed directive double-quoted continuation, closed low",
		"BOM-prefixed directive single-quoted continuation, closed low",
		"tab-indented directive double-quoted continuation, closed low",
		"tab-indented directive single-quoted continuation, closed low",
		"unspaced directive double-quoted continuation, closed low",
		"unspaced directive single-quoted continuation, closed low",
		"nested quoted scalar opened on the next line, closed low",
		"URL key quoted scalar opened on the next line, closed low",
		"anchor with double-quoted value on the next line, hash tail",
		"anchor with single-quoted value on the next line, hash tail",
	} {
		body := flowSeedBody(flowHostileSeeds[name])
		_, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if !errors.Is(err, errYAMLIndirection) {
			t.Errorf("%s: err = %v, want errYAMLIndirection", name, err)
		}
		if got := failureReason(err); got != FailureYAMLIndirection {
			t.Errorf("%s: failure reason = %q, want %q", name, got, FailureYAMLIndirection)
		}
	}
}

// TestRedactMarkerQuotedContinuationFailsClosed is the P0 regression: a quoted
// scalar opened on the line after a bare listed key and closed at an indent at
// or below the key's indent leaves its tail where the redactor's continuation
// does not reach, so the body must fail closed. It covers the flat,
// single-quoted, nested-key, and URL-userinfo variants, which all leaked the
// same way before the quote bytes joined the YAML-indirection trigger set.
func TestRedactMarkerQuotedContinuationFailsClosed(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"flat double-quoted": "npmAuthToken:\n  \"abc\ndef\"\n",
		"flat single-quoted": "npmAuthToken:\n  'abc\ndef'\n",
		"nested":             "npmScopes:\n  acme:\n    npmAuthToken:\n      \"abc\ndef\"\n",
		"url userinfo":       "npmRegistryServer:\n  \"https://user:abc\ndef@registry.example.com/\"\n",
	} {
		_, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if !errors.Is(err, errYAMLIndirection) {
			t.Errorf("%s: err = %v, want errYAMLIndirection", name, err)
		}
		if got := failureReason(err); got != FailureYAMLIndirection {
			t.Errorf("%s: failure reason = %q, want %q", name, got, FailureYAMLIndirection)
		}
	}
}

// TestRedactMarkerDocumentEndQuotedContinuationFailsClosed is the #8833 P1
// regression: a "..."-prefixed body whose quoted scalar opens on the line
// after a bare listed key and closes at the key's indent. yaml.v3 rejects the
// bare document after "...", so the parsed walk sees nothing and only the
// lexical fallback can cover it; js-yaml 4.1.1 (Yarn's parser) decodes the
// token under the listed key. Before the "..."-and-quotes arm joined the
// fallback trigger, both shapes were stored with the tail in the clear.
func TestRedactMarkerDocumentEndQuotedContinuationFailsClosed(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"document-end double-quoted": "...\nnpmAuthToken:\n  \"abc\ndef\"\n",
		"document-end single-quoted": "...\nnpmAuthToken:\n  'abc\ndef'\n",
	} {
		_, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if !errors.Is(err, errYAMLIndirection) {
			t.Errorf("%s: err = %v, want errYAMLIndirection", name, err)
		}
		if got := failureReason(err); got != FailureYAMLIndirection {
			t.Errorf("%s: failure reason = %q, want %q", name, got, FailureYAMLIndirection)
		}
	}
}

// TestRedactMarkerVersionDirectiveQuotedContinuationFailsClosed is the #8833
// residual-P1 regression: a "%YAML 1.2"-prefixed body whose quoted scalar opens
// on the line after a bare listed key and closes at the key's indent. yaml.v3
// rejects the 1.2+ version (found incompatible YAML document), so the parsed
// walk sees nothing and only the lexical fallback can cover it; js-yaml 4.1.1
// and PyYAML 6.0.3 decode the token under the listed key. Before the directive
// arm joined the fallback trigger, both shapes were stored with the tail in
// the clear.
func TestRedactMarkerVersionDirectiveQuotedContinuationFailsClosed(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"version-directive double-quoted": "%YAML 1.2\n---\nnpmAuthToken:\n  \"abc\ndef\"\n",
		"version-directive single-quoted": "%YAML 1.2\n---\nnpmAuthToken:\n  'abc\ndef'\n",
	} {
		_, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if !errors.Is(err, errYAMLIndirection) {
			t.Errorf("%s: err = %v, want errYAMLIndirection", name, err)
		}
		if got := failureReason(err); got != FailureYAMLIndirection {
			t.Errorf("%s: failure reason = %q, want %q", name, got, FailureYAMLIndirection)
		}
	}
}

// TestRedactMarkerVersionDirectiveVariantQuotedContinuationFailsClosed is the
// #8833 follow-up regression: three "%YAML"-directive spellings outside the
// original column-zero matcher are rejected by yaml.v3 (the parsed walk is
// silent) yet read by js-yaml 4.1.1 — a BOM-prefixed directive (PyYAML 6.0.3
// reads it too), a tab-indented directive, and "%YAML1.2" with no space — so a
// quoted scalar opened after a bare listed key and closed at the key's indent
// stored its tail in the clear until the directive arm widened to them. Each
// of the three shapes must fail closed in both quote styles.
func TestRedactMarkerVersionDirectiveVariantQuotedContinuationFailsClosed(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"BOM-prefixed double-quoted": "\uFEFF%YAML 1.2\n---\nnpmAuthToken:\n  \"abc\ndef\"\n",
		"BOM-prefixed single-quoted": "\uFEFF%YAML 1.2\n---\nnpmAuthToken:\n  'abc\ndef'\n",
		"tab-indented double-quoted": "\t%YAML 1.2\n---\nnpmAuthToken:\n  \"abc\ndef\"\n",
		"tab-indented single-quoted": "\t%YAML 1.2\n---\nnpmAuthToken:\n  'abc\ndef'\n",
		"unspaced double-quoted":     "%YAML1.2\n---\nnpmAuthToken:\n  \"abc\ndef\"\n",
		"unspaced single-quoted":     "%YAML1.2\n---\nnpmAuthToken:\n  'abc\ndef'\n",
	} {
		_, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if !errors.Is(err, errYAMLIndirection) {
			t.Errorf("%s: err = %v, want errYAMLIndirection", name, err)
		}
		if got := failureReason(err); got != FailureYAMLIndirection {
			t.Errorf("%s: failure reason = %q, want %q", name, got, FailureYAMLIndirection)
		}
	}
}

// TestRedactMarkerFlowFailClosedIsNotAFalsePositive pins the bodies the new rule
// must leave alone: ordinary block files, a flow map whose listed values sit on
// the key line, pretty-printed flow with closer-only lines, comments, block
// and quoted scalars at greater indent, and a value on the next line that the
// continuation covers.
func TestRedactMarkerFlowFailClosedIsNotAFalsePositive(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"typical block file": "nodeLinker: node-modules\nnpmRegistryServer: \"https://registry.example.com/\"\n" +
			"npmAuthToken: abcdef0123456789\nenableTelemetry: false\n",
		"scopes block": "npmScopes:\n  acme:\n    npmAuthToken: " + flowSeedToken + "\n    npmRegistryServer: \"https://r.example.com/\"\n" +
			"  other:\n    npmAuthIdent: " + flowSeedToken + "\n",
		"registries block": "npmRegistries:\n  \"//registry.example.com\":\n    npmAuthToken: " + flowSeedToken + "\n    npmAlwaysAuth: true\n",
		"comments between keys": "# header\nnpmScopes:\n  # scope\n  acme:\n    # token\n    npmAuthToken: " + flowSeedToken +
			"\n\n    # tail\n  other: {}\n",
		"folded scalar":                            "npmAuthToken: >\n  " + flowSeedToken + "\n  more\nnext: ok\n",
		"literal scalar":                           "npmAuthToken: |\n  " + flowSeedToken + "\nnext: ok\n",
		"quoted value":                             "npmAuthToken: \"" + flowSeedToken + "\"\nnext: ok\n",
		"quoted multi-line at greater":             "npmAuthToken: \"abcd\n  " + flowSeedToken + "\"\nnext: ok\n",
		"value on the next line":                   "npmAuthToken:\n  " + flowSeedToken + "\nnext: ok\n",
		"same-indent sequence":                     "npmAuthToken:\n- " + flowSeedToken + "\nnext: ok\n",
		"env reference on the next":                "npmAuthToken:\n  ${NPM_TOKEN}\n",
		"flow on one line":                         "npmScopes: {acme: {npmAuthToken: " + flowSeedToken + "}}\n",
		"JSON on one line":                         "{\"npmScopes\": {\"acme\": {\"npmAuthToken\": \"" + flowSeedToken + "\"}}}\n",
		"pretty JSON, values inline":               "{\n  \"npmAuthToken\": \"" + flowSeedToken + "\",\n  \"nodeLinker\": \"node-modules\"\n}\n",
		"pretty flow with closers":                 "npmScopes: {\n  acme: {\n    npmAuthToken: " + flowSeedToken + "\n  }\n}\n",
		"pretty flow, trailing comma":              "npmScopes: {\n  acme: {npmAuthToken: " + flowSeedToken + "},\n  other: {}\n}\n",
		"flow sequence unrelated":                  "supportedArchitectures:\n  os: [linux, darwin]\n  cpu: [x64, arm64]\nnpmAuthToken: " + flowSeedToken + "\n",
		"multi-line flow sequence":                 "packageExtensions:\n  a@1:\n    peerDependencies: {\n      b: \"*\",\n    }\nnpmAuthToken: " + flowSeedToken + "\n",
		"flow value then block sibling":            "plugins: [a, b]\nnpmAuthToken: " + flowSeedToken + "\nlast: [1,\n  2]\n",
		"document markers":                         "---\nnpmAuthToken: " + flowSeedToken + "\n...\n",
		"flow key on the key line":                 "{npmAuthToken: " + flowSeedToken + ",\nnodeLinker: pnp}\n",
		"CRLF block file":                          "npmScopes:\r\n  acme:\r\n    npmAuthToken: " + flowSeedToken + "\r\n",
		"non-YAML text with braces":                "npmAuthToken: " + flowSeedToken + "\n  bad: [\n",
		"flow value with a mid-scalar colon quote": "...\n{npmAuthToken: [ab:\"x\", " + flowSeedToken + "]\n# comment\n}\n",
	} {
		out, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if err != nil {
			t.Errorf("%s: err = %v, want the body redacted", name, err)
			continue
		}
		if strings.Contains(out, flowSeedToken) {
			t.Errorf("%s: the token survived:\n%s", name, out)
		}
	}
}

// realisticYarnrcFiles are ordinary .yarnrc.yml files in the shapes the Yarn
// documentation and public repositories use: block mappings and sequences,
// flow collections for short lists and maps, comments, quoted values, folded
// and literal scalars, CRLF endings, and a document start. None may fail
// closed: that is the false-positive floor of the flow rule.
var realisticYarnrcFiles = map[string]string{
	"minimal": "nodeLinker: node-modules\n",
	"registry and scope": "nodeLinker: node-modules\nnpmRegistryServer: \"https://registry.example.com/\"\n" +
		"npmScopes:\n  acme:\n    npmRegistryServer: \"https://npm.acme.example/\"\n    npmAlwaysAuth: true\n    npmAuthToken: ${ACME_NPM_TOKEN}\n",
	"registries by host": "npmRegistries:\n  //registry.example.com:\n    npmAlwaysAuth: true\n    npmAuthIdent: ${NPM_IDENT:-}\n" +
		"  \"//npm.pkg.github.com/\":\n    npmAuthToken: " + flowSeedToken + "\n",
	"plugins and log filters": "plugins:\n  - path: .yarn/plugins/@yarnpkg/plugin-workspace-tools.cjs\n    spec: \"@yarnpkg/plugin-workspace-tools\"\n" +
		"logFilters:\n  - code: YN0013\n    level: discard\n  - pattern: \"*@types/*\"\n    level: warning\n",
	"flow lists and maps": "supportedArchitectures:\n  os: [linux, darwin]\n  cpu: [x64, arm64]\n  libc: [glibc]\n" +
		"packageExtensions:\n  \"debug@*\":\n    peerDependenciesMeta: {supports-color: {optional: true}}\n" +
		"npmScopes: {acme: {npmAuthToken: " + flowSeedToken + ", npmAlwaysAuth: true}}\n",
	"multi-line flow with closers": "packageExtensions:\n  react-dom@*: {\n    peerDependencies: {\n      react: \"*\",\n    },\n  }\n" +
		"npmScopes:\n  acme: {\n    npmAuthToken: " + flowSeedToken + ",\n    npmRegistryServer: \"https://npm.acme.example/\"\n  }\n",
	"pretty JSON": "{\n  \"nodeLinker\": \"node-modules\",\n  \"npmScopes\": {\n    \"acme\": {\n      \"npmAuthToken\": \"" + flowSeedToken + "\",\n" +
		"      \"npmRegistryServer\": \"https://npm.acme.example/\"\n    }\n  },\n  \"enableTelemetry\": false\n}\n",
	"tab-indented JSON": "{\n\t\"nodeLinker\": \"node-modules\",\n\t\"npmAuthToken\": \"" + flowSeedToken + "\",\n\t\"enableTelemetry\": false\n}\n",
	"comments everywhere": "# Yarn config\n\nnpmScopes: # scopes\n  # acme\n  acme:\n    # the token\n    npmAuthToken: " + flowSeedToken +
		" # rotate yearly\n\n    # end\n  other: {} # none\n",
	"folded and literal":     "npmAuthToken: >-\n  " + flowSeedToken + "\n\nenableMessageNames: |\n  yes\nchecksumBehavior: update\n",
	"quoted multi-line":      "npmScopes:\n  acme:\n    npmAuthToken: \"" + flowSeedToken + "\n      more\"\n    npmAlwaysAuth: true\n",
	"document start":         "---\nnodeLinker: pnp\nnpmAuthToken: " + flowSeedToken + "\n",
	"CRLF":                   "nodeLinker: pnp\r\nnpmScopes:\r\n  acme: {npmAuthToken: " + flowSeedToken + "}\r\npackageExtensions: {}\r\n",
	"empty credential keys":  "npmScopes:\n  acme:\n    npmAuthToken:\n    npmAuthIdent: \"\"\n  other:\n    npmAuthToken: ${TOKEN}\n",
	"key last with comments": "npmAuthToken: " + flowSeedToken + "\n# trailing\n\n",
	"list of flow maps":      "logFilters:\n  - {code: YN0013, level: discard}\n  - {pattern: \"x\", level: info}\nnpmAuthToken: " + flowSeedToken + "\n",
}

// TestRedactMarkerRealisticYarnrcFilesDoNotFailClosed pins the false-positive
// floor: an ordinary file is redacted, never turned into a metadata row, and
// keeps no token.
func TestRedactMarkerRealisticYarnrcFilesDoNotFailClosed(t *testing.T) {
	t.Parallel()

	store := parser.DefaultRegistry().ContentStoreForPath(".yarnrc.yml")
	for name, body := range realisticYarnrcFiles {
		result := ApplyContentStore(store, body)
		if result.Outcome == StoreOutcomeRedactorFailed {
			t.Errorf("%s: failed closed (%s)", name, result.Reason)
			continue
		}
		if strings.Contains(result.Body, flowSeedToken) {
			t.Errorf("%s: the token survived:\n%s", name, result.Body)
		}
		if strings.Count(result.Body, "\n") != strings.Count(body, "\n") {
			t.Errorf("%s: line count changed", name)
		}
	}
}
