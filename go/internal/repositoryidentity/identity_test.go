// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryidentity

import "testing"

func TestNormalizedRemoteKey(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// HTTPS variants
		{name: "https no .git", raw: "https://github.com/eshu-hq/eshu", want: "github.com/eshu-hq/eshu"},
		{name: "https with .git", raw: "https://github.com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "https trailing slash", raw: "https://github.com/eshu-hq/eshu/", want: "github.com/eshu-hq/eshu"},
		{name: "https trailing slash + .git", raw: "https://github.com/eshu-hq/eshu.git/", want: "github.com/eshu-hq/eshu"},

		// SSH / SCP syntax
		{name: "git@ scp", raw: "git@github.com:eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "git@ scp no .git", raw: "git@github.com:eshu-hq/eshu", want: "github.com/eshu-hq/eshu"},
		{name: "ssh://", raw: "ssh://git@github.com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},

		// git+ prefix (npm-style)
		{name: "git+https", raw: "git+https://github.com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "git+ssh", raw: "git+ssh://git@github.com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "git+ scp", raw: "git+git@github.com:eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},

		// Port dropping (canonical: ports are not part of git repo identity)
		{name: "https with port", raw: "https://github.com:8443/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "default port", raw: "https://github.com:443/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},

		// Userinfo stripping
		{name: "https with userinfo", raw: "https://user@github.com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},

		// Case folding
		{name: "mixed case host", raw: "https://GitHub.Com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "mixed case path", raw: "https://github.com/Eshu-Hq/Eshu.git", want: "github.com/eshu-hq/eshu"},

		// SCP with non-git user prefix (handled by generic SCP parser)
		{name: "scp user@ non-git", raw: "user@host.xz:org/repo.git", want: "host.xz/org/repo"},
		{name: "scp gitlab user", raw: "gitlab@gitlab.com:org/repo.git", want: "gitlab.com/org/repo"},

		// Edge cases — empty or unparseable
		{name: "empty", raw: "", want: ""},
		{name: "non-URL garbage", raw: "not-a-url", want: ""},
		{name: "bare host", raw: "https://github.com", want: ""},
		{name: "git@ bare host", raw: "git@github.com", want: ""},
		{name: "only git+ prefix", raw: "git+", want: ""},
		{name: "scp with numeric path seg", raw: "git@github.com:8080/repo.git", want: "github.com/8080/repo"},

		// Already-normalized key (no :// scheme) — returns ""
		{name: "already normalized key", raw: "github.com/eshu-hq/eshu", want: ""},

		// Regression: garbage leaking through NormalizeRemoteURL passthrough (#5421 F1)
		{name: "https with empty host", raw: "https:///pathonly", want: ""},
		{name: "https with percent host", raw: "https://%41zure.com/org/repo", want: ""},
		{name: "https with userinfo portless", raw: "https://user@:8080/path", want: ""},
		{name: "https collision safety", raw: "https:///a/b", want: ""},
		{name: "scp with percent host", raw: "user@%41zure.com:org/repo.git", want: ""},

		// Regression: control chars and bad percent-encoding surviving NormalizeRemoteURL passthrough (#5421 N1)
		// url.Parse re-validation at the mechanism layer catches all of these.
		{name: "tab in host", raw: "https://git\thub.com/org/repo.git", want: ""},
		{name: "newline in host", raw: "https://git\nhub.com/org/repo.git", want: ""},
		{name: "cr in host", raw: "https://git\rhub.com/org/repo.git", want: ""},
		{name: "bad percent zz", raw: "https://github.com/org/repo%zz", want: ""},
		{name: "bad percent incomplete", raw: "https://github.com/org/repo%2", want: ""},
		{name: "bad percent dot git", raw: "https://github.com/org/repo%.git", want: ""},
		{name: "tab in path", raw: "https://github.com\t/org/repo", want: ""},
		{name: "git+ prefix with tab", raw: "git+https://git\thub.com/org/repo.git", want: ""},
		{name: "scp with tab in host", raw: "user@git\thub.com:org/repo.git", want: ""},

		// F2 additional divergence classes — value-pinned intended behavior
		// Percent-encoded paths: url.Parse decodes %20→space, %2E→. in Path.
		// The decoded form is canonical and consistent with raw input.
		{name: "percent-encoded path", raw: "https://github.com/org/my%20repo", want: "github.com/org/my repo"},
		// %2Egit → .git → stripped (it is the literal .git suffix).
		{name: "percent-encoded git suffix", raw: "https://github.com/org/repo%2Egit", want: "github.com/org/repo"},
		{name: "percent-encoded git suffix with literal git", raw: "https://github.com/org/repo%2Egit.git", want: "github.com/org/repo.git"},
		// Duplicate-slashes: NormalizeRemoteURL's FieldsFunc collapses //→/.
		{name: "duplicate slashes", raw: "https://github.com//org//repo.git", want: "github.com/org/repo"},
		// Scheme-relative: url.Parse accepts //host/path, but NormalizedRemoteKey
		// routes the no-"://" form to scpKey which requires ":" → rejected.
		{name: "scheme relative", raw: "//github.com/org/repo", want: ""},
		// IPv6 bracket drop: url.Parse's Hostname() strips [ ] from IPv6 hosts.
		{name: "ipv6 brackets", raw: "https://[::1]/org/repo.git", want: "::1/org/repo"},
		// Double-encoding: url.Parse decodes one level; the decoded path is stable.
		{name: "double-encoded space", raw: "https://github.com/org/repo%2520name", want: "github.com/org/repo%20name"},
		// Spaces and non-ASCII in path: url.Parse decodes %20→space, %C3%A9→é.
		// The decoded representation is consistent across literal and encoded
		// inputs, so these are legitimate (not rejected).
		{name: "literal space in path", raw: "https://github.com/org/my repo", want: "github.com/org/my repo"},
		{name: "non-ASCII path", raw: "https://github.com/org/r%C3%A9po", want: "github.com/org/répo"},
		{name: "non-ASCII raw path", raw: "https://github.com/org/répo", want: "github.com/org/répo"},
		// Trailing .git with percent-encoded path components
		{name: "trailing git after percent path", raw: "https://github.com/eshu-hq/eshu.git.git", want: "github.com/eshu-hq/eshu.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizedRemoteKey(tt.raw)
			if got != tt.want {
				t.Errorf("NormalizedRemoteKey(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizedRemoteKeyConsistentWithNormalizeRemoteURL(t *testing.T) {
	// NormalizedRemoteKey and NormalizeRemoteURL must agree on the host/path
	// identity: NormalizedRemoteKey(raw) must equal the key extracted from
	// NormalizeRemoteURL(raw) by stripping the https:// prefix.
	inputs := []string{
		"https://github.com/eshu-hq/eshu",
		"https://github.com/eshu-hq/eshu.git",
		"git@github.com:eshu-hq/eshu.git",
		"git+https://github.com/eshu-hq/eshu",
		"ssh://git@github.com/eshu-hq/eshu.git",
	}

	for _, input := range inputs {
		key := NormalizedRemoteKey(input)
		canonical := NormalizeRemoteURL(input)
		keyFromCanonical := NormalizedRemoteKey(canonical)
		if keyFromCanonical != key {
			t.Errorf("NormalizedRemoteKey(NormalizeRemoteURL(%q)) = %q, want %q (NormalizedRemoteKey(%q))",
				input, keyFromCanonical, key, input)
		}
	}
}

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https", raw: "https://github.com/eshu-hq/eshu", want: "https://github.com/eshu-hq/eshu"},
		{name: "https .git", raw: "https://github.com/eshu-hq/eshu.git", want: "https://github.com/eshu-hq/eshu"},
		{name: "git@ scp", raw: "git@github.com:eshu-hq/eshu.git", want: "https://github.com/eshu-hq/eshu"},
		{name: "ssh://", raw: "ssh://git@github.com/eshu-hq/eshu.git", want: "https://github.com/eshu-hq/eshu"},
		{name: "mixed case", raw: "git@GitHub.Com:eshu-hq/eshu.git", want: "https://github.com/eshu-hq/eshu"},
		{name: "trailing slash", raw: "https://github.com/eshu-hq/eshu/", want: "https://github.com/eshu-hq/eshu"},
		{name: "empty", raw: "", want: ""},
		{name: "non-URL", raw: "not-a-url", want: "not-a-url"}, // preserved as-is
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRemoteURL(tt.raw)
			if got != tt.want {
				t.Errorf("NormalizeRemoteURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
