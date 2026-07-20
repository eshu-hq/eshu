// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// TestPackageSourceURLKeyPinnedValues locks the unified behavior from issue
// #5421: canonicalPackageSourceURLKey must produce the exact expected key for
// every input shape. This is a value-pinning test, not a comparison test — it
// guards against drift in the unified normalizer itself.
func TestPackageSourceURLKeyPinnedValues(t *testing.T) {
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

		// Port dropping (unified: ports are stripped)
		{name: "https with port", raw: "https://github.com:8443/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "default port 443", raw: "https://github.com:443/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},

		// Userinfo stripping
		{name: "https with userinfo", raw: "https://user@github.com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},

		// Case folding
		{name: "mixed case host", raw: "https://GitHub.Com/eshu-hq/eshu.git", want: "github.com/eshu-hq/eshu"},
		{name: "mixed case path", raw: "https://github.com/Eshu-Hq/Eshu.git", want: "github.com/eshu-hq/eshu"},

		// SCP with non-git user prefix (unified: any user@ is handled)
		{name: "scp user@ non-git", raw: "user@host.xz:org/repo.git", want: "host.xz/org/repo"},
		{name: "scp gitlab user", raw: "gitlab@gitlab.com:org/repo.git", want: "gitlab.com/org/repo"},

		// Edge cases — empty or unparseable
		{name: "empty", raw: "", want: ""},
		{name: "non-URL garbage", raw: "not-a-url", want: ""},
		{name: "bare host no path", raw: "https://github.com", want: ""},
		{name: "scp with numeric path seg", raw: "git@github.com:8080/repo.git", want: "github.com/8080/repo"},

		// Garbage-key rejection (#5421 F1)
		{name: "https empty host", raw: "https:///pathonly", want: ""},
		{name: "https percent host", raw: "https://%41zure.com/org/repo", want: ""},
		{name: "https collision safety", raw: "https:///a/b", want: ""},

		// Percent-encoded and decoded paths (#5421 F2)
		{name: "percent-encoded space path", raw: "https://github.com/org/my%20repo", want: "github.com/org/my repo"},
		{name: "percent-encoded git suffix", raw: "https://github.com/org/repo%2Egit", want: "github.com/org/repo"},
		{name: "duplicate slashes", raw: "https://github.com//org//repo.git", want: "github.com/org/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalPackageSourceURLKey(tt.raw)
			if got != tt.want {
				t.Errorf("canonicalPackageSourceURLKey(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestPackageSourceURLKeyDelegatesToNormalizedRemoteKey is a drift guard:
// canonicalPackageSourceURLKey must be a pure delegation to
// repositoryidentity.NormalizedRemoteKey. It samples a few representative
// inputs to confirm the wrapper has not diverged.
func TestPackageSourceURLKeyDelegatesToNormalizedRemoteKey(t *testing.T) {
	inputs := []string{
		"https://github.com/eshu-hq/eshu.git",
		"git@github.com:eshu-hq/eshu.git",
		"git+https://github.com/eshu-hq/eshu.git",
		"user@host.xz:org/repo.git",
	}

	for _, raw := range inputs {
		reducerKey := canonicalPackageSourceURLKey(raw)
		repoKey := repositoryidentity.NormalizedRemoteKey(raw)
		if reducerKey != repoKey {
			t.Errorf("wrapper drift: canonicalPackageSourceURLKey(%q) = %q, but NormalizedRemoteKey(%q) = %q",
				raw, reducerKey, raw, repoKey)
		}
	}
}
