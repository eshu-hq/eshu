// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"regexp"
	"testing"
)

func TestNewGitHubOrgSelectorIsStableAndCanonical(t *testing.T) {
	t.Parallel()

	rules := []Rule{{Kind: "regex", Value: "^acme/api"}, {Kind: "exact", Value: "acme/web"}}
	base := NewGitHubOrgSelector("githubOrg", "Acme", rules, false)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(base.ID) {
		t.Fatalf("selector id = %q, want 32 lowercase hex characters", base.ID)
	}
	if base.Kind != KindGitHubOrg || base.Owner != "acme" {
		t.Fatalf("selector = %+v, want kind %q and lowercased owner acme", base, KindGitHubOrg)
	}

	same := []Selector{
		NewGitHubOrgSelector("githubOrg", " acme ", rules, false),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{rules[1], rules[0], rules[1]}, false),
		NewGitHubOrgSelector("githubOrg", "ACME", []Rule{{Kind: " EXACT ", Value: " acme/web "}, {Kind: "Regex", Value: "^acme/api"}}, false),
	}
	for i, selector := range same {
		if selector.ID != base.ID {
			t.Fatalf("equivalent selector %d id = %s, want %s", i, selector.ID, base.ID)
		}
	}

	different := []Selector{
		NewGitHubOrgSelector("githubOrg", "acme", rules, true),
		NewGitHubOrgSelector("githubOrg", "acme", rules[:1], false),
		NewGitHubOrgSelector("githubOrg", "other", rules, false),
		NewGitHubOrgSelector("explicit", "acme", rules, false),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{{Kind: "exact", Value: "acme/WEB"}, rules[0]}, false),
	}
	for i, selector := range different {
		if selector.ID == base.ID {
			t.Fatalf("distinct selector %d shares id %s", i, base.ID)
		}
	}
}
