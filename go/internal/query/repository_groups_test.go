package query

import "testing"

// TestDeriveRepositoryGroupEvidenceFromRemoteURL proves that a source repository
// with no repo_slug but a populated remote_url groups by the org/owner segment of
// the remote, rather than collapsing into the missing-evidence bucket (issue #3393).
func TestDeriveRepositoryGroupEvidenceFromRemoteURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		repo       map[string]any
		wantKey    string
		wantSource string
		wantKind   string
	}{
		{
			name: "https remote without slug groups by org",
			repo: map[string]any{
				"remote_url":    "https://github.com/acme-corp/widget-service",
				"is_dependency": false,
			},
			wantKey:    "Acme Corp",
			wantSource: repositoryGroupSourceRemoteOwner,
			wantKind:   "source",
		},
		{
			name: "ssh remote without slug groups by org",
			repo: map[string]any{
				"remote_url":    "git@example.org:platform-team/billing.git",
				"is_dependency": false,
			},
			wantKey:    "Platform Team",
			wantSource: repositoryGroupSourceRemoteOwner,
			wantKind:   "source",
		},
		{
			name: "repo_slug still wins over remote_url",
			repo: map[string]any{
				"repo_slug":     "preferred/leaf",
				"remote_url":    "https://github.com/other-org/leaf",
				"is_dependency": false,
			},
			wantKey:    "Preferred",
			wantSource: repositoryGroupSourceSlugNamespace,
			wantKind:   "source",
		},
		{
			name: "dependency flag still wins",
			repo: map[string]any{
				"remote_url":    "https://github.com/acme-corp/widget-service",
				"is_dependency": true,
			},
			wantKey:    "Dependencies",
			wantSource: repositoryGroupSourceDependencyFlag,
			wantKind:   "dependency",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveRepositoryGroupEvidence(tc.repo)
			if got.Key != tc.wantKey {
				t.Errorf("Key = %q, want %q", got.Key, tc.wantKey)
			}
			if got.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tc.wantSource)
			}
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Truth != repositoryGroupTruthDerived {
				t.Errorf("Truth = %q, want %q", got.Truth, repositoryGroupTruthDerived)
			}
		})
	}
}

// TestDeriveRepositoryGroupEvidenceStillMissingWithoutEvidence proves a source
// repository with neither repo_slug nor remote_url stays in the honest
// missing-evidence bucket so the empty state is not faked.
func TestDeriveRepositoryGroupEvidenceStillMissingWithoutEvidence(t *testing.T) {
	t.Parallel()

	got := deriveRepositoryGroupEvidence(map[string]any{
		"name":          "local-only",
		"is_dependency": false,
	})
	if got.Source != repositoryGroupSourceMissing {
		t.Errorf("Source = %q, want %q", got.Source, repositoryGroupSourceMissing)
	}
	if got.Truth != repositoryGroupTruthMissing {
		t.Errorf("Truth = %q, want %q", got.Truth, repositoryGroupTruthMissing)
	}
	if got.Key != "" {
		t.Errorf("Key = %q, want empty", got.Key)
	}
}

// TestRepositoryRemoteOwner covers org/owner extraction across remote shapes.
func TestRepositoryRemoteOwner(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://github.com/acme-corp/widget":     "acme-corp",
		"git@github.com:acme-corp/widget.git":     "acme-corp",
		"ssh://git@example.org/team/repo":         "team",
		"https://gitlab.com/group/sub/deep-repo":  "group",
		"":                                        "",
		"not-a-url":                               "",
		"https://github.com/single":               "",
	}
	for remote, want := range cases {
		if got := repositoryRemoteOwner(remote); got != want {
			t.Errorf("repositoryRemoteOwner(%q) = %q, want %q", remote, got, want)
		}
	}
}
