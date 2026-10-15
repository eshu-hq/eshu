// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"regexp"
	"testing"
)

// TestSelectorIDsArePinned pins the github_org and explicit selector ids
// captured on origin/main: adding the gitlab_group host to the identity
// encoding must leave them byte-identical (host is omitempty).
func TestSelectorIDsArePinned(t *testing.T) {
	t.Parallel()

	rules := []Rule{{Kind: "regex", Value: "^acme/api"}, {Kind: "exact", Value: "acme/web"}}
	if got := NewGitHubOrgSelector("githubOrg", "Acme", rules, false, appPrincipal).ID; got != "ff4a7c6c39b6009bf3f02382b328164e@app:123:456" {
		t.Fatalf("github_org selector id = %q, want the pinned id", got)
	}
	if got := NewExplicitSelector("explicit", "Acme", []Rule{{Kind: "exact", Value: "acme/web"}}, appPrincipal).ID; got != "3c22184a0b4aa05709448b6d6b84a389@app:123:456" {
		t.Fatalf("explicit selector id = %q, want the pinned id", got)
	}
}

func TestNewGitLabGroupSelector(t *testing.T) {
	t.Parallel()

	rules := []Rule{{Kind: "regex", Value: "^acme/platform/api"}, {Kind: "exact", Value: "gitlab/acme/platform/web"}}
	base := NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "Acme/Platform", rules, false, TokenPrincipal("glpat-example"))
	if !regexp.MustCompile(`^[0-9a-f]{32}@token:[0-9a-f]{16}$`).MatchString(base.ID) {
		t.Fatalf("gitlab_group selector id = %q, want 32 hex chars then a token principal", base.ID)
	}
	if base.Kind != KindGitLabGroup {
		t.Fatalf("selector kind = %q, want %q", base.Kind, KindGitLabGroup)
	}
	if base.Owner != "acme/platform" {
		t.Fatalf("selector owner = %q, want the lowercased group full path", base.Owner)
	}
	if base.Host != "gitlab.com" {
		t.Fatalf("selector host = %q, want gitlab.com", base.Host)
	}

	vary := []Selector{
		// Host participates in the identity.
		NewGitLabGroupSelector("gitlabGroup", "gitlab.corp.example", "Acme/Platform", rules, false, TokenPrincipal("glpat-example")),
		// Group path casing and trimming do not.
		NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "acme/platform", rules, false, TokenPrincipal("glpat-example")),
		NewGitLabGroupSelector("gitlabGroup", "gitlab.com", " acme/platform ", rules, false, TokenPrincipal("glpat-example")),
	}
	for i, selector := range vary[:1] {
		if selector.ID == base.ID {
			t.Fatalf("host-varying selector %d shares id %s", i, base.ID)
		}
	}
	for i, selector := range vary[1:] {
		if selector.ID != base.ID {
			t.Fatalf("case/space-only selector %d id = %s, want %s", i, selector.ID, base.ID)
		}
	}

	different := []Selector{
		NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "acme/other", rules, false, TokenPrincipal("glpat-example")),
		NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "Acme/Platform", rules[:1], false, TokenPrincipal("glpat-example")),
		NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "Acme/Platform", rules, true, TokenPrincipal("glpat-example")),
		NewGitLabGroupSelector("gitlabGroup", "gitlab.com", "Acme/Platform", rules, false, TokenPrincipal("glpat-other")),
		NewGitHubOrgSelector("gitlabGroup", "acme/platform", rules, false, TokenPrincipal("glpat-example")),
	}
	for i, selector := range different {
		if selector.ID == base.ID {
			t.Fatalf("distinct selector %d shares id %s", i, base.ID)
		}
	}
}

func TestSelectorPartition(t *testing.T) {
	t.Parallel()

	githubOrg := NewGitHubOrgSelector("githubOrg", "Acme", nil, false, "")
	if got := githubOrg.Partition(); got != (Partition{Owner: "acme", Host: "github.com"}) {
		t.Fatalf("github_org partition = %+v, want first-segment owner with the github host", got)
	}
	explicit := NewExplicitSelector("explicit", "Acme", nil, "")
	if got := explicit.Partition(); got != (Partition{Owner: "acme"}) {
		t.Fatalf("explicit partition = %+v, want first-segment owner with no host", got)
	}
	group := NewGitLabGroupSelector("gitlabGroup", "gitlab.corp.example", "Acme/Platform", nil, false, "")
	if got := group.Partition(); got != (Partition{Owner: "acme/platform", Host: "gitlab.corp.example", PathPrefix: true}) {
		t.Fatalf("gitlab_group partition = %+v, want path-prefix owner with the gitlab host", got)
	}
}
