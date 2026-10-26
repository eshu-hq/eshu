// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"testing"
)

var yarnrcCredentialKeys = []string{"npmAuthIdent", "npmAuthToken"}

// TestRedactMarkerFailsClosedOnYAMLIndirection pins the #8833 security review:
// a YAML alias can leave a secret on its anchor's line, and an explicit
// "? key", an escaped key spelling, or a CRLF explicit key puts the key where
// no line-wise match sees it. Every alias, and every listed key off its line,
// fails closed, so the caller stores a metadata row.
func TestRedactMarkerFailsClosedOnYAMLIndirection(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"alias value":                    "base: &tok Zq1SeedAAAAAAAAA\nnpmAuthToken: *tok\n",
		"alias to a mapping":             "creds: &c\n  t: Zq14SeedAAAAAAAA\nnpmScopes:\n  acme:\n    npmAuthToken: *c\n",
		"alias under a registry":         "x: &a Zq15SeedAAAAAAAA\nnpmRegistries:\n  \"//r\":\n    npmAuthIdent: *a\n",
		"alias in a flow map":            "a: &s Zq16SeedAAAAAAAA\nnpmScopes: {acme: {npmAuthToken: *s}}\n",
		"explicit key":                   "? npmAuthToken\n: Zq6SeedAAAAAAAAA\n",
		"explicit key in a list":         "npmScopes:\n  - ? npmAuthToken\n    : Zq7SeedAAAAAAAA\n",
		"explicit key in a flow":         "npmScopes: {acme: {? npmAuthToken\n  : Zq9SeedAAAAAAAA}}\n",
		"explicit key after a comma":     "npmScopes: {a: 1, ? npmAuthIdent : Zq10SeedAAAAAAA}\n",
		"bare value line":                "npmAuthToken\n: Zq11SeedAAAAAAA\n",
		"URL key alias":                  "u: &u https://Zq12tok@registry.example.com/\nnpmRegistryServer: *u\n",
		"explicit key with CRLF":         "? npmAuthToken\r\n: Zq13SeedAAAAAAA\r\n",
		"bare indicator with CRLF":       "?\r\n  npmAuthToken\r\n:\r\n  Zq22SeedAAAAAAA\r\n",
		"anchor on the credential value": "npmAuthToken: &tok Zq2SeedAAAAAAAA\nother: *tok\n",
		"alias on another key":           "base: &b https://r.example.com\nnpmRegistryServer: *b\n",
		"merge key":                      "defs: &d\n  npmAuthToken: Zq8SeedAAAAAAAAA\nnpmScopes:\n  acme:\n    <<: *d\n",
		"value line with CRLF":           "npmScopes:\r\n  ? npmAuthIdent\r\n  : Zq14SeedAAAAAAA\r\n",
		"alias on the next line":         "base: &t Zq15SeedAAAAAAA\nnpmAuthToken:\n  *t\n",
		"escaped key spelling":           "\"npmAuth\\x54oken\": Zq17SeedAAAAAAA\n",
	} {
		_, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, []string{"npmRegistryServer", "httpsProxy"})
		if !errors.Is(err, errYAMLIndirection) {
			t.Errorf("%s: err = %v, want errYAMLIndirection", name, err)
		}
		if got := failureReason(err); got != FailureYAMLIndirection {
			t.Errorf("%s: failure reason = %q, want %q", name, got, FailureYAMLIndirection)
		}
	}
	for name, body := range map[string]string{
		"question mark in a value":    "npmAuthToken: \"?abcdefgh\"\nx: \"? y\"\n",
		"value on the next line":      "npmAuthToken:\n  Zq18SeedAAAAAAA\n",
		"flow map on one line":        "npmScopes: {acme: {npmAuthToken: Zq7SeedAAAAAAAA}}\n",
		"invalid YAML still redacted": "npmAuthToken: Zq23SeedAAAAAAA\n  bad: [\n",
	} {
		if _, _, err := redactMarker(body, yarnrcCredentialKeys); err != nil {
			t.Errorf("%s: err = %v, want the body redacted", name, err)
		}
	}
}
