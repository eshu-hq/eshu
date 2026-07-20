// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// TestPackageSourceURLKeyEquivalence locks in the unification from issue #5421:
// canonicalPackageSourceURLKey and repositoryidentity.NormalizedRemoteKey must
// produce identical keys for every input shape. This is the permanent regression
// fixture for the divergence classes enumerated during theory proof.
func TestPackageSourceURLKeyEquivalence(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		// HTTPS variants
		{name: "https no .git", raw: "https://github.com/eshu-hq/eshu"},
		{name: "https with .git", raw: "https://github.com/eshu-hq/eshu.git"},
		{name: "https trailing slash", raw: "https://github.com/eshu-hq/eshu/"},
		{name: "https trailing slash + .git", raw: "https://github.com/eshu-hq/eshu.git/"},

		// SSH / SCP syntax
		{name: "git@ scp", raw: "git@github.com:eshu-hq/eshu.git"},
		{name: "git@ scp no .git", raw: "git@github.com:eshu-hq/eshu"},
		{name: "ssh://", raw: "ssh://git@github.com/eshu-hq/eshu.git"},

		// git+ prefix (npm-style)
		{name: "git+https", raw: "git+https://github.com/eshu-hq/eshu.git"},
		{name: "git+ssh", raw: "git+ssh://git@github.com/eshu-hq/eshu.git"},
		{name: "git+ scp", raw: "git+git@github.com:eshu-hq/eshu.git"},

		// Port dropping (unified: ports are stripped — canonical behavior)
		{name: "https with port", raw: "https://github.com:8443/eshu-hq/eshu.git"},
		{name: "default port 443", raw: "https://github.com:443/eshu-hq/eshu.git"},

		// Userinfo stripping
		{name: "https with userinfo", raw: "https://user@github.com/eshu-hq/eshu.git"},

		// Case folding
		{name: "mixed case host", raw: "https://GitHub.Com/eshu-hq/eshu.git"},
		{name: "mixed case path", raw: "https://github.com/Eshu-Hq/Eshu.git"},

		// SCP with non-git user prefix (unified: any user@ is handled)
		{name: "scp user@ non-git", raw: "user@host.xz:org/repo.git"},
		{name: "scp gitlab user", raw: "gitlab@gitlab.com:org/repo.git"},

		// Edge cases
		{name: "empty", raw: ""},
		{name: "non-URL garbage", raw: "not-a-url"},
		{name: "bare host no path", raw: "https://github.com"},
		{name: "scp with numeric path seg", raw: "git@github.com:8080/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reducerKey := canonicalPackageSourceURLKey(tt.raw)
			repoKey := repositoryidentity.NormalizedRemoteKey(tt.raw)

			if reducerKey != repoKey {
				t.Errorf("divergence after unification:\n  reducer:  %q\n  repo key: %q", reducerKey, repoKey)
			}
		})
	}
}
