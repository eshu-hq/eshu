// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
)

var appPrincipal = GitHubAppPrincipal("123", "456")

func TestNewGitHubOrgSelectorIsStableAndCanonical(t *testing.T) {
	t.Parallel()

	rules := []Rule{{Kind: "regex", Value: "^acme/api"}, {Kind: "exact", Value: "acme/web"}}
	base := NewGitHubOrgSelector("githubOrg", "Acme", rules, false, appPrincipal)
	if !regexp.MustCompile(`^[0-9a-f]{32}@app:123:456$`).MatchString(base.ID) {
		t.Fatalf("selector id = %q, want 32 lowercase hex characters then @app:123:456", base.ID)
	}
	if base.Kind != KindGitHubOrg || base.Owner != "acme" {
		t.Fatalf("selector = %+v, want kind %q and lowercased owner acme", base, KindGitHubOrg)
	}

	same := []Selector{
		NewGitHubOrgSelector("githubOrg", " acme ", rules, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{rules[1], rules[0], rules[1]}, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "ACME", []Rule{{Kind: " EXACT ", Value: " acme/web "}, {Kind: "Regex", Value: "^acme/api"}}, false, appPrincipal),
		// F7: exact values are normalized the way the collector matches them.
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{rules[0], {Kind: "exact", Value: "/acme//web/"}}, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{rules[0], {Kind: "exact", Value: `acme\web`}}, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{rules[0], {Kind: "exact", Value: "./acme/./web"}}, false, appPrincipal),
	}
	for i, selector := range same {
		if selector.ID != base.ID {
			t.Fatalf("equivalent selector %d id = %s, want %s", i, selector.ID, base.ID)
		}
	}

	different := []Selector{
		NewGitHubOrgSelector("githubOrg", "acme", rules, true, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", rules[:1], false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "other", rules, false, appPrincipal),
		NewGitHubOrgSelector("explicit", "acme", rules, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{{Kind: "exact", Value: "acme/WEB"}, rules[0]}, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", []Rule{{Kind: "regex", Value: "^acme//api"}, rules[1]}, false, appPrincipal),
		NewGitHubOrgSelector("githubOrg", "acme", rules, false, GitHubAppPrincipal("123", "789")),
		NewGitHubOrgSelector("githubOrg", "acme", rules, false, TokenPrincipal("ghp_example")),
		NewExplicitSelector("githubOrg", "acme", rules, appPrincipal),
	}
	for i, selector := range different {
		if selector.ID == base.ID {
			t.Fatalf("distinct selector %d shares id %s", i, base.ID)
		}
	}
}

func TestSelectorPrincipals(t *testing.T) {
	t.Parallel()

	if got := GitHubAppPrincipal(" 123 ", "456"); got != "app:123:456" {
		t.Fatalf("GitHubAppPrincipal() = %q, want app:123:456", got)
	}
	const token = "ghp_secretvalue0123456789"
	sum := sha256.Sum256([]byte("eshu-repo-selector:" + token))
	want := "token:" + hex.EncodeToString(sum[:])[:16]
	if got := TokenPrincipal(token); got != want {
		t.Fatalf("TokenPrincipal() = %q, want %q", got, want)
	}
	selector := NewGitHubOrgSelector("githubOrg", "acme", nil, false, TokenPrincipal(token))
	if strings.Contains(selector.ID, token) || !strings.HasSuffix(selector.ID, "@"+want) {
		t.Fatalf("selector id %q must end in the salted token hash and never carry the token", selector.ID)
	}
	if TokenPrincipal(token) == TokenPrincipal(token+"x") {
		t.Fatal("token rotation must produce a new principal")
	}
	if got := NewGitHubOrgSelector("githubOrg", "acme", nil, false, " ").ID; !strings.HasSuffix(got, "@anonymous") {
		t.Fatalf("blank principal id = %q, want @anonymous", got)
	}
}

func TestNewExplicitSelector(t *testing.T) {
	t.Parallel()

	selector := NewExplicitSelector("explicit", "Acme", []Rule{{Kind: "exact", Value: "acme/web"}}, appPrincipal)
	if selector.Kind != KindExplicit || selector.Owner != "acme" {
		t.Fatalf("explicit selector = %+v, want kind %q owner acme", selector, KindExplicit)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}@app:123:456$`).MatchString(selector.ID) {
		t.Fatalf("explicit selector id = %q", selector.ID)
	}
	if other := NewExplicitSelector("explicit", "other", []Rule{{Kind: "exact", Value: "acme/web"}}, appPrincipal); other.ID == selector.ID {
		t.Fatal("explicit selectors for two owners share an id")
	}
}
