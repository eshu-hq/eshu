// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestGithubConnectionTestAPIBaseProbesLoginEndpoint proves the P1 GHES
// false-green fix (issue #5166, F-5): the admin connection tester must derive
// the REST API host it probes from the SAME base_url/api_base_url defaulting
// the login resolver uses. For a GitHub Enterprise Server config with
// base_url set but api_base_url omitted, that is <base_url>/api/v3 — NOT the
// api.github.com default the old code fell back to when it read only
// api_base_url and passed an empty string to TestConnection.
func TestGithubConnectionTestAPIBaseProbesLoginEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		configuration string
		want          string
	}{
		{
			name:          "ghes base_url set, api_base_url omitted -> <base>/api/v3",
			configuration: `{"client_id":"c","base_url":"https://github.example.com","allowed_orgs":["o"]}`,
			want:          "https://github.example.com/api/v3",
		},
		{
			name:          "github.com (no base_url) -> api.github.com",
			configuration: `{"client_id":"c","allowed_orgs":["o"]}`,
			want:          "https://api.github.com",
		},
		{
			name:          "explicit api_base_url wins",
			configuration: `{"client_id":"c","base_url":"https://github.example.com","api_base_url":"https://ghe-api.example.com","allowed_orgs":["o"]}`,
			want:          "https://ghe-api.example.com",
		},
		{
			name:          "malformed configuration falls back to the github.com default",
			configuration: `not json`,
			want:          "https://api.github.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := githubConnectionTestAPIBase(tc.configuration); got != tc.want {
				t.Fatalf("githubConnectionTestAPIBase(%q) = %q, want %q", tc.configuration, got, tc.want)
			}
		})
	}
}
