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
