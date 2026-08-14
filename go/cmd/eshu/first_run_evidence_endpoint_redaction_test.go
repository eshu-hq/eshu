// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// endpointSentinel is synthetic. A failing assertion prints the value it
// found, so this must never be replaced with a real credential.
const endpointSentinel = "eshu-endpoint-probe-token-NOT-REAL"

// TestRedactEndpointRedactsCredentialQueryParameters is the regression test
// for the query-string half of the endpoint leak. redactEndpoint stripped
// userinfo but returned parsed.String() with RawQuery untouched, so a token
// passed as a query parameter -- the more common shape by far -- survived into
// the first-run evidence report and the hosted-onboard output.
func TestRedactEndpointRedactsCredentialQueryParameters(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "token parameter is masked, diagnostic parameter survives",
			raw:  "https://host/mcp?repo=demo&token=" + endpointSentinel,
			want: "https://host/mcp?repo=demo&token=redacted",
		},
		{
			name: "api_key parameter",
			raw:  "https://host/mcp?api_key=" + endpointSentinel,
			want: "https://host/mcp?api_key=redacted",
		},
		{
			name: "authorization parameter",
			raw:  "https://host/mcp?authorization=" + endpointSentinel,
			want: "https://host/mcp?authorization=redacted",
		},
		{
			name: "userinfo and query token both go",
			raw:  "https://user:" + endpointSentinel + "@host/mcp?secret=" + endpointSentinel,
			want: "https://redacted@host/mcp?secret=redacted",
		},
		{
			name: "clean query is left exactly as it was",
			raw:  "https://host/mcp?repo=demo&limit=5",
			want: "https://host/mcp?repo=demo&limit=5",
		},
		{
			name: "no query is unchanged",
			raw:  "https://host/mcp",
			want: "https://host/mcp",
		},
		{
			name: "fragment carrying an OAuth implicit-flow token is dropped",
			raw:  "https://host/mcp#access_token=" + endpointSentinel,
			want: "https://host/mcp#redacted",
		},
		{
			// Non-canonical escaping (%41 is "A") makes url.Parse retain
			// RawFragment alongside the decoded Fragment. url.URL.String()
			// prefers RawFragment, but only while it still unescapes to
			// Fragment -- so overwriting Fragment alone makes the two
			// disagree and the redacted value wins. This case exists because
			// that is load-bearing stdlib behaviour: if it ever changed, or if
			// someone set RawFragment instead, the original would come back.
			name: "fragment with non-canonical escaping does not survive via RawFragment",
			raw:  "https://host/mcp#access_token=%41" + endpointSentinel,
			want: "https://host/mcp#redacted",
		},
		{
			// url.Parse accepts these, url.ParseQuery rejects them. The
			// parameters cannot be inspected one by one, so the whole query
			// goes rather than passing through unchecked bytes.
			name: "query with invalid escaping fails closed",
			raw:  "https://host/mcp?%zz=" + endpointSentinel,
			want: "https://host/mcp?redacted",
		},
		{
			name: "credential value with invalid escaping fails closed",
			raw:  "https://host/mcp?a=%zz" + endpointSentinel,
			want: "https://host/mcp?redacted",
		},
		{
			name: "query with a semicolon separator fails closed",
			raw:  "https://host/mcp?token=" + endpointSentinel + ";x=1",
			want: "https://host/mcp?redacted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := redactEndpoint(tc.raw)
			if got != tc.want {
				t.Fatalf("redactEndpoint(%q)\n got = %q\nwant = %q", tc.raw, got, tc.want)
			}
			if strings.Contains(got, endpointSentinel) {
				t.Fatalf("redactEndpoint(%q) leaked the credential sentinel: %s", tc.raw, got)
			}
		})
	}
}

// TestRedactEndpointMatchesCollectorSensitiveKeyRule proves redactEndpoint
// asks collector.IsSensitiveKeyName rather than carrying a second, drifting
// list of its own. If someone widens the collector rule, this test makes the
// endpoint redactor follow automatically; if someone replaces the call with a
// local list, this fails.
func TestRedactEndpointMatchesCollectorSensitiveKeyRule(t *testing.T) {
	for _, key := range []string{
		"token", "secret", "password", "credential", "api_key", "apikey",
		"api-key", "authorization", "repo", "limit", "arch", "profile", "sig",
	} {
		t.Run(key, func(t *testing.T) {
			got := redactEndpoint("https://host/mcp?" + key + "=" + endpointSentinel)
			masked := !strings.Contains(got, endpointSentinel)
			if masked != collector.IsSensitiveKeyName(key) {
				t.Fatalf("redactEndpoint masked %q = %v, collector.IsSensitiveKeyName(%q) = %v (drift between the two rules)\ngot: %s",
					key, masked, key, collector.IsSensitiveKeyName(key), got)
			}
		})
	}
}
