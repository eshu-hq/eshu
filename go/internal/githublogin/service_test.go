// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testProvider() ProviderConfig {
	return ProviderConfig{
		ProviderConfigID: "github-dev",
		BaseURL:          "https://github.com",
		APIBaseURL:       "https://api.github.com",
		ClientID:         "client-id",
		RedirectURL:      "https://eshu.example.test/api/v0/auth/github/callback",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		AllowedOrgs:      []string{"eshu-hq"},
	}
}

func testStaticResolver() StaticGrantResolver {
	return StaticGrantResolver{
		GroupRoleMappings: []GroupRoleMapping{{
			Group:   "eshu-hq/developers",
			RoleIDs: []string{"developer"},
		}},
		RoleGrants: []RoleGrant{{
			RoleID:               "developer",
			AllowedScopeIDs:      []string{"scope_a"},
			AllowedRepositoryIDs: []string{"repo_a"},
		}},
	}
}

func TestServiceStartStoresHashOnlyStateAndBuildsAuthorizationRedirect(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeStateStore{}
	service := NewService(Config{
		DefaultProviderID: "github-dev",
		StateTTL:          10 * time.Minute,
		Providers:         []ProviderConfig{testProvider()},
	}, store, StaticGrantResolver{}, fakeConnectorFactory(Identity{}),
		WithNow(func() time.Time { return now }),
		WithSecretGenerator(sequenceSecrets("state-secret")))

	start, err := service.StartGitHubLogin(context.Background(), StartRequest{
		ProviderConfigID: "github-dev",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		ReturnToPath:     "/console",
	})
	if err != nil {
		t.Fatalf("StartGitHubLogin() error = %v", err)
	}
	if !strings.Contains(start.RedirectURL, "state=state-secret") {
		t.Fatalf("redirect URL = %q, want raw state sent only to provider", start.RedirectURL)
	}
	if strings.Contains(start.RedirectURL, "nonce=") {
		t.Fatalf("redirect URL = %q, plain OAuth2 must never carry a nonce param", start.RedirectURL)
	}
	if len(store.created) != 1 {
		t.Fatalf("created states = %d, want 1", len(store.created))
	}
	created := store.created[0]
	if created.StateHash != SHA256Hash("state-secret") {
		t.Fatalf("created state hash = %q, want hashed secret", created.StateHash)
	}
	if created.StateHash == "state-secret" {
		t.Fatalf("created state leaked raw secret: %#v", created)
	}
	if created.RedirectURIHash != SHA256Hash("https://eshu.example.test/api/v0/auth/github/callback") {
		t.Fatalf("created redirect hash = %#v, want hashed redirect uri", created)
	}
	if created.ExpiresAt != now.Add(10*time.Minute) || created.ReturnToPath != "/console" {
		t.Fatalf("created state timing/return = %#v, want ttl and return path", created)
	}
}

func TestServiceCompleteEnforcesOrgAllowListAndMapsTeamsThroughRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	provider := testProvider()
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			ProviderConfigID: provider.ProviderConfigID,
			TenantID:         provider.TenantID,
			WorkspaceID:      provider.WorkspaceID,
			RedirectURIHash:  SHA256Hash(provider.RedirectURL),
			ReturnToPath:     "/console",
			IssuedAt:         now.Add(-time.Minute),
			ExpiresAt:        now.Add(9 * time.Minute),
			UpdatedAt:        now,
		},
		consumeOK: true,
	}
	identity := Identity{
		Subject:     "12345",
		Login:       "octocat",
		Email:       "octocat@example.test",
		ActiveOrgs:  []string{"eshu-hq"},
		TeamHandles: []string{"eshu-hq/developers"},
	}
	service := NewService(Config{
		DefaultProviderID: provider.ProviderConfigID,
		Providers:         []ProviderConfig{provider},
	}, store, testStaticResolver(), fakeConnectorFactory(identity),
		WithNow(func() time.Time { return now }))

	complete, err := service.CompleteGitHubLogin(context.Background(), CompleteRequest{
		State: "state-secret",
		Code:  "auth-code",
	})
	if err != nil {
		t.Fatalf("CompleteGitHubLogin() error = %v", err)
	}
	if complete.Auth.SubjectClass != githubSubjectClass {
		t.Fatalf("subject class = %q, want %q", complete.Auth.SubjectClass, githubSubjectClass)
	}
	wantSubjectHash := SHA256Hash(provider.ProviderConfigID + ":12345")
	if complete.ProviderSubjectID != wantSubjectHash || complete.Auth.SubjectIDHash != wantSubjectHash {
		t.Fatalf("subject id hash = %#v, want %q", complete, wantSubjectHash)
	}
	if len(complete.Auth.RoleIDs) != 1 || complete.Auth.RoleIDs[0] != "developer" {
		t.Fatalf("resolved roles = %v, want [developer]", complete.Auth.RoleIDs)
	}
	wantGroupHash := SHA256Hash("eshu-hq/developers")
	if len(complete.ProviderGroupHashes) != 1 || complete.ProviderGroupHashes[0] != wantGroupHash {
		t.Fatalf("provider group hashes = %v, want [%s]", complete.ProviderGroupHashes, wantGroupHash)
	}
	if complete.ReturnToPath != "/console" {
		t.Fatalf("return to path = %q, want /console", complete.ReturnToPath)
	}
}

func TestServiceCompleteDeniesRedirectURIDrift(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	provider := testProvider()
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			ProviderConfigID: provider.ProviderConfigID,
			TenantID:         provider.TenantID,
			WorkspaceID:      provider.WorkspaceID,
			RedirectURIHash:  SHA256Hash("https://attacker.example.test/callback"),
			ExpiresAt:        now.Add(time.Minute),
		},
		consumeOK: true,
	}
	service := NewService(Config{Providers: []ProviderConfig{provider}}, store, testStaticResolver(),
		fakeConnectorFactory(Identity{}), WithNow(func() time.Time { return now }))

	_, err := service.CompleteGitHubLogin(context.Background(), CompleteRequest{State: "state-secret", Code: "auth-code"})
	if !errors.Is(err, ErrGitHubLoginDenied) {
		t.Fatalf("error = %v, want ErrGitHubLoginDenied", err)
	}
}

func TestServiceCompleteDeniesUnverifiedEmail(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	provider := testProvider()
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			ProviderConfigID: provider.ProviderConfigID,
			TenantID:         provider.TenantID,
			WorkspaceID:      provider.WorkspaceID,
			RedirectURIHash:  SHA256Hash(provider.RedirectURL),
			ExpiresAt:        now.Add(time.Minute),
		},
		consumeOK: true,
	}
	identity := Identity{
		Subject:     "12345",
		Email:       "", // no verified primary email
		ActiveOrgs:  []string{"eshu-hq"},
		TeamHandles: []string{"eshu-hq/developers"},
	}
	service := NewService(Config{Providers: []ProviderConfig{provider}}, store, testStaticResolver(),
		fakeConnectorFactory(identity), WithNow(func() time.Time { return now }))

	_, err := service.CompleteGitHubLogin(context.Background(), CompleteRequest{State: "state-secret", Code: "auth-code"})
	if !errors.Is(err, ErrGitHubLoginDenied) {
		t.Fatalf("error = %v, want ErrGitHubLoginDenied", err)
	}
}

func TestServiceCompleteDeniesUserOutsideAllowedOrgs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	provider := testProvider()
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			ProviderConfigID: provider.ProviderConfigID,
			TenantID:         provider.TenantID,
			WorkspaceID:      provider.WorkspaceID,
			RedirectURIHash:  SHA256Hash(provider.RedirectURL),
			ExpiresAt:        now.Add(time.Minute),
		},
		consumeOK: true,
	}
	identity := Identity{
		Subject:     "12345",
		Email:       "octocat@example.test",
		ActiveOrgs:  []string{"some-other-org"}, // not in provider.AllowedOrgs
		TeamHandles: []string{"some-other-org/developers"},
	}
	service := NewService(Config{Providers: []ProviderConfig{provider}}, store, testStaticResolver(),
		fakeConnectorFactory(identity), WithNow(func() time.Time { return now }))

	_, err := service.CompleteGitHubLogin(context.Background(), CompleteRequest{State: "state-secret", Code: "auth-code"})
	if !errors.Is(err, ErrGitHubLoginDenied) {
		t.Fatalf("error = %v, want ErrGitHubLoginDenied", err)
	}
}

// TestServiceCompleteFiltersTeamsOutsideAllowedOrgs proves a mapping row for
// a team in a non-allowed org can never grant access even if the user
// happens to also be an active member of an allowed org (issue #5166:
// team→role mapping must stay scoped to the org allow-list).
func TestServiceCompleteFiltersTeamsOutsideAllowedOrgs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	provider := testProvider()
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			ProviderConfigID: provider.ProviderConfigID,
			TenantID:         provider.TenantID,
			WorkspaceID:      provider.WorkspaceID,
			RedirectURIHash:  SHA256Hash(provider.RedirectURL),
			ExpiresAt:        now.Add(time.Minute),
		},
		consumeOK: true,
	}
	identity := Identity{
		Subject:    "12345",
		Email:      "octocat@example.test",
		ActiveOrgs: []string{"eshu-hq"}, // active in the allowed org...
		// ...but the only team handle is in a DIFFERENT org. A resolver with
		// a (hypothetical, misconfigured) mapping for
		// "not-allowed-org/developers" must never grant a role from this.
		TeamHandles: []string{"not-allowed-org/developers"},
	}
	resolver := StaticGrantResolver{
		GroupRoleMappings: []GroupRoleMapping{{
			Group:   "not-allowed-org/developers",
			RoleIDs: []string{"developer"},
		}},
		RoleGrants: []RoleGrant{{RoleID: "developer"}},
	}
	service := NewService(Config{Providers: []ProviderConfig{provider}}, store, resolver,
		fakeConnectorFactory(identity), WithNow(func() time.Time { return now }))

	_, err := service.CompleteGitHubLogin(context.Background(), CompleteRequest{State: "state-secret", Code: "auth-code"})
	if !errors.Is(err, ErrGitHubLoginDenied) {
		t.Fatalf("error = %v, want ErrGitHubLoginDenied (team outside allow-list must not grant access)", err)
	}
}

func TestServiceCompleteDeniesExpiredOrUnknownState(t *testing.T) {
	t.Parallel()

	provider := testProvider()
	store := &fakeStateStore{consumeOK: false}
	service := NewService(Config{Providers: []ProviderConfig{provider}}, store, testStaticResolver(),
		fakeConnectorFactory(Identity{}))

	_, err := service.CompleteGitHubLogin(context.Background(), CompleteRequest{State: "state-secret", Code: "auth-code"})
	if !errors.Is(err, ErrGitHubLoginDenied) {
		t.Fatalf("error = %v, want ErrGitHubLoginDenied", err)
	}
}

func TestValidateConfigRejectsEmptyAllowedOrgs(t *testing.T) {
	t.Parallel()

	provider := testProvider()
	provider.AllowedOrgs = nil
	_, err := ValidateConfig(Config{Providers: []ProviderConfig{provider}})
	if err == nil {
		t.Fatal("ValidateConfig() error = nil, want error for empty allowed_orgs")
	}
}

// TestEffectiveAPIBaseURL proves the shared derivation the login path and the
// admin connection tester both use (issue #5166, F-5): a GitHub Enterprise
// Server base_url with no explicit api_base_url must resolve to
// <base_url>/api/v3 — NOT the api.github.com default — so the test-connection
// probe targets the same host login calls. github.com (blank base) resolves
// to api.github.com, and an explicit api_base_url always wins.
func TestEffectiveAPIBaseURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		baseURL    string
		apiBaseURL string
		want       string
	}{
		{"github.com blank base", "", "", "https://api.github.com"},
		{"github.com explicit base", "https://github.com", "", "https://api.github.com"},
		{"ghes base derives api/v3", "https://github.example.com", "", "https://github.example.com/api/v3"},
		{"ghes base with trailing slash", "https://github.example.com/", "", "https://github.example.com/api/v3"},
		{"explicit api_base_url wins", "https://github.example.com", "https://ghe-api.example.com", "https://ghe-api.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := EffectiveAPIBaseURL(tc.baseURL, tc.apiBaseURL); got != tc.want {
				t.Fatalf("EffectiveAPIBaseURL(%q, %q) = %q, want %q", tc.baseURL, tc.apiBaseURL, got, tc.want)
			}
		})
	}
}
