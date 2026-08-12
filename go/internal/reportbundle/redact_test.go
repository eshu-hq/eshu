// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// TestRedact_SensitiveKeyNamesUseCollectorRules proves redactValue DROPS every
// sensitive-shaped key (per collector.IsSensitiveKeyName — the exported
// wrapper over sdk/go/collector's fail-closed validatePayloadKeys walk) from
// the output entirely, at any nesting depth, in both objects and arrays,
// while leaving benign keys and values untouched. Dropping (not masking in
// place) is deliberate: validatePayloadKeys flags a key by name alone
// regardless of its value, so a masked-but-present key would trip the
// bundle's own Validate gate — see redact.go's design note. This is the SAME
// predicate the bundle's Validate gate re-checks, so a redactor/validator
// disagreement is impossible by construction (see
// TestRedact_MatchesUnderlyingValidator below).
func TestRedact_SensitiveKeyNamesUseCollectorRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      any
		wantOutput any
		wantRules  []string
	}{
		{
			name:       "top-level sensitive key",
			input:      map[string]any{"api_key": "sk-live-abc123"},
			wantOutput: map[string]any{},
			wantRules:  []string{"api_key"},
		},
		{
			name: "nested under object",
			input: map[string]any{
				"auth": map[string]any{"password": "hunter2"},
				"note": "benign",
			},
			wantOutput: map[string]any{
				"auth": map[string]any{},
				"note": "benign",
			},
			wantRules: []string{"password"},
		},
		{
			name: "nested under array of objects",
			input: map[string]any{
				"items": []any{
					map[string]any{"token": "tok-1", "id": "row-1"},
					map[string]any{"token": "tok-2", "id": "row-2"},
				},
			},
			wantOutput: map[string]any{
				"items": []any{
					map[string]any{"id": "row-1"},
					map[string]any{"id": "row-2"},
				},
			},
			wantRules: []string{"token", "token"},
		},
		{
			name:       "allowlisted join-key field is not redacted",
			input:      map[string]any{"token_policy_join_keys": []any{"sha256:fingerprint"}},
			wantOutput: map[string]any{"token_policy_join_keys": []any{"sha256:fingerprint"}},
			wantRules:  nil,
		},
		{
			name: "embedded citation excerpt is stripped even though it is not credential-shaped",
			input: map[string]any{
				"citations": []any{
					map[string]any{"repo_id": "demo/service", "excerpt": "func Handler() { ... }"},
				},
			},
			wantOutput: map[string]any{
				"citations": []any{
					map[string]any{"repo_id": "demo/service"},
				},
			},
			wantRules: []string{"excerpt"},
		},
		{
			name:       "scalar with no map context is unchanged",
			input:      "just a plain string with the word secret in it",
			wantOutput: "just a plain string with the word secret in it",
			wantRules:  nil,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotOutput, gotRules := redactValue(tt.input)
			if !reflect.DeepEqual(gotOutput, tt.wantOutput) {
				t.Fatalf("redactValue(%#v) output = %#v, want %#v", tt.input, gotOutput, tt.wantOutput)
			}
			sort.Strings(gotRules)
			wantSorted := append([]string(nil), tt.wantRules...)
			sort.Strings(wantSorted)
			if !reflect.DeepEqual(gotRules, wantSorted) {
				t.Fatalf("redactValue(%#v) rules = %#v, want %#v", tt.input, gotRules, wantSorted)
			}
		})
	}
}

// TestRedact_MatchesUnderlyingValidator guards against future drift: for a
// range of key names, redactValue's redaction decision must agree with
// collector.IsSensitiveKeyName exactly, since redact.go is documented to use
// the SAME rule the bundle's fail-closed Validate gate re-checks.
func TestRedact_MatchesUnderlyingValidator(t *testing.T) {
	t.Parallel()

	keys := []string{
		"api_key", "password", "secret", "credential", "authorization",
		"client_secret", "access_token", "token_policy_join_keys",
		"repository_name", "scope_id", "fact_kind",
	}
	for _, key := range keys {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			out, rules := redactValue(map[string]any{key: "value"})
			outMap, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("redactValue returned %T, want map[string]any", out)
			}
			_, stillPresent := outMap[key]
			wasRedacted := !stillPresent
			if wasRedacted != collector.IsSensitiveKeyName(key) {
				t.Fatalf("redactValue redacted %q = %v, collector.IsSensitiveKeyName(%q) = %v (drift)", key, wasRedacted, key, collector.IsSensitiveKeyName(key))
			}
			if wasRedacted && len(rules) != 1 {
				t.Fatalf("redactValue(%q) rules = %v, want exactly one rule recorded", key, rules)
			}
		})
	}
}

// TestEmbeddedSensitiveKey covers the one place this package reads a value.
// The rule is narrow on purpose, and both halves of that need pinning: it must
// find a sensitive key hiding inside a query-shaped value, and it must not fire
// on ordinary values, because a false positive costs a maintainer a parameter
// they needed to reproduce the query.
//
// The "does not fire" cases double as the honest statement of what this does
// NOT protect against — a bare secret with no key= in front of it is invisible
// here, and so is one that survived only a single decoding pass.
func TestEmbeddedSensitiveKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "nested query after a second question mark", value: "/api/v0/x?api_key=sk-live-abc", want: "api_key"},
		{name: "decoded ampersand separated pair", value: "/api/v0/x&access_token=abc", want: "access_token"},
		{name: "semicolon separated pair", value: "/api/v0/x?page=2;password=hunter2", want: "password"},
		{name: "bare pair with no separator", value: "authorization=Bearer-abc", want: "authorization"},
		{name: "inline content key", value: "/api/v0/x?excerpt=func+Handler", want: "excerpt"},

		{name: "benign nested url is kept", value: "/api/v0/x?page=2", want: ""},
		{name: "plain path is kept", value: "/api/v0/services/checkout/story", want: ""},
		{name: "no pair shape is kept", value: "demo/service", want: ""},
		{name: "prose with a spaced key is kept", value: "SELECT token = 1 FROM t", want: ""},
		{name: "empty key is kept", value: "/api/v0/x?=abc", want: ""},

		// Stated limits: neither of these is detected, and pretending
		// otherwise in a doc comment would be worse than the gap itself.
		{name: "LIMIT bare secret under a benign name", value: "sk-live-abc", want: ""},
		{name: "LIMIT double encoded nested query", value: "/api/v0/x%3Fapi_key%3Dsk-live", want: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, found := embeddedSensitiveKey(tt.value)
			if found != (tt.want != "") {
				t.Fatalf("embeddedSensitiveKey(%q) found = %v, want %v", tt.value, found, tt.want != "")
			}
			if got != tt.want {
				t.Fatalf("embeddedSensitiveKey(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
