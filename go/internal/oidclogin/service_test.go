// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidclogin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestServiceStartStoresHashOnlyStateAndBuildsAuthorizationRedirect(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeStateStore{}
	service := NewService(Config{
		DefaultProviderID: "okta-dev",
		StateTTL:          10 * time.Minute,
		Providers: []ProviderConfig{{
			ProviderConfigID: "okta-dev",
			IssuerURL:        "https://idp.example.test/oauth2/default",
			ClientID:         "client-id",
			RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
			GroupsClaim:      "groups",
		}},
	}, store, StaticGrantResolver{}, fakeConnectorFactory(t), WithNow(func() time.Time { return now }),
		WithSecretGenerator(sequenceOIDCSecrets("state-secret", "nonce-secret")))

	start, err := service.StartOIDCLogin(context.Background(), query.OIDCLoginStartRequest{
		ProviderConfigID: "okta-dev",
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		ReturnToPath:     "/console",
	})
	if err != nil {
		t.Fatalf("StartOIDCLogin() error = %v", err)
	}
	if !strings.Contains(start.RedirectURL, "state=state-secret") ||
		!strings.Contains(start.RedirectURL, "nonce=nonce-secret") {
		t.Fatalf("redirect URL = %q, want raw state and nonce sent only to provider", start.RedirectURL)
	}
	if len(store.created) != 1 {
		t.Fatalf("created states = %d, want 1", len(store.created))
	}
	created := store.created[0]
	if created.StateHash != SHA256Hash("state-secret") ||
		created.NonceHash != SHA256Hash("nonce-secret") ||
		created.RedirectURIHash != SHA256Hash("https://eshu.example.test/api/v0/auth/oidc/callback") {
		t.Fatalf("created state = %#v, want hashed state/nonce/redirect", created)
	}
	if created.ProviderKeyHash != SHA256Hash("https://idp.example.test/oauth2/default\x00client-id") ||
		created.IssuerHash != SHA256Hash("https://idp.example.test/oauth2/default") ||
		created.ClientIDHash != SHA256Hash("client-id") {
		t.Fatalf("created provider seed hashes = %#v, want hashed provider metadata", created)
	}
	if created.StateHash == "state-secret" || created.NonceHash == "nonce-secret" {
		t.Fatalf("created state leaked raw secret: %#v", created)
	}
	if created.ExpiresAt != now.Add(10*time.Minute) || created.ReturnToPath != "/console" {
		t.Fatalf("created state timing/return = %#v, want ttl and return path", created)
	}
}

func TestServiceCompleteValidatesNonceAndMapsGroupsThroughRoles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			NonceHash:        SHA256Hash("nonce-secret"),
			ProviderConfigID: "okta-dev",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
			RedirectURIHash:  SHA256Hash("https://eshu.example.test/api/v0/auth/oidc/callback"),
			ReturnToPath:     "/console",
			IssuedAt:         now.Add(-time.Minute),
			ExpiresAt:        now.Add(9 * time.Minute),
			UpdatedAt:        now,
		},
		consumeOK: true,
	}
	connector := &fakeConnector{
		claims: VerifiedClaims{
			Subject: "external-subject",
			Nonce:   "nonce-secret",
			Groups:  []string{"Eshu Developers"},
		},
	}
	service := NewService(Config{
		DefaultProviderID: "okta-dev",
		StateTTL:          10 * time.Minute,
		Providers: []ProviderConfig{{
			ProviderConfigID: "okta-dev",
			IssuerURL:        "https://idp.example.test/oauth2/default",
			ClientID:         "client-id",
			RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
			GroupsClaim:      "groups",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
		}},
	}, store, StaticGrantResolver{
		GroupRoleMappings: []GroupRoleMapping{{
			Group:   "Eshu Developers",
			RoleIDs: []string{"developer"},
		}},
		RoleGrants: []RoleGrant{{
			RoleID:               "developer",
			PolicyRevisionHash:   "sha256:policy",
			AllowedScopeIDs:      []string{"scope_a"},
			AllowedRepositoryIDs: []string{"repo_a"},
		}},
	}, func(context.Context, ProviderConfig) (Connector, error) {
		return connector, nil
	}, WithNow(func() time.Time { return now }))

	complete, err := service.CompleteOIDCLogin(context.Background(), query.OIDCLoginCompleteRequest{
		State: "state-secret",
		Code:  "auth-code",
	})
	if err != nil {
		t.Fatalf("CompleteOIDCLogin() error = %v", err)
	}
	if store.consumedHash != SHA256Hash("state-secret") {
		t.Fatalf("consumed hash = %q, want state hash", store.consumedHash)
	}
	if connector.exchangedCode != "auth-code" || connector.verifiedIDToken != "id-token" {
		t.Fatalf("connector exchange/verify = %q/%q, want code/id-token", connector.exchangedCode, connector.verifiedIDToken)
	}
	auth := complete.Auth
	if auth.Mode != query.AuthModeScoped ||
		auth.TenantID != "tenant_a" ||
		auth.WorkspaceID != "workspace_a" ||
		auth.SubjectClass != "external_oidc_user" ||
		auth.SubjectIDHash != SHA256Hash("okta-dev:external-subject") ||
		auth.PolicyRevisionHash != "sha256:policy" {
		t.Fatalf("auth = %#v, want scoped OIDC subject", auth)
	}
	if got := auth.RoleIDs; len(got) != 1 || got[0] != "developer" {
		t.Fatalf("RoleIDs = %#v, want [developer]", got)
	}
	if got := auth.AllowedScopeIDs; len(got) != 1 || got[0] != "scope_a" {
		t.Fatalf("AllowedScopeIDs = %#v, want [scope_a]", got)
	}
	if got := auth.AllowedRepositoryIDs; len(got) != 1 || got[0] != "repo_a" {
		t.Fatalf("AllowedRepositoryIDs = %#v, want [repo_a]", got)
	}
	if complete.ReturnToPath != "/console" {
		t.Fatalf("ReturnToPath = %q, want /console", complete.ReturnToPath)
	}
	if complete.ProviderConfigID != "okta-dev" {
		t.Fatalf("ProviderConfigID = %q, want okta-dev", complete.ProviderConfigID)
	}
	if complete.ProviderSubjectID != SHA256Hash("okta-dev:external-subject") {
		t.Fatalf("ProviderSubjectID = %q, want hashed provider subject", complete.ProviderSubjectID)
	}
	if got, want := complete.ProviderGroupHashes, []string{SHA256Hash("Eshu Developers")}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ProviderGroupHashes = %#v, want hashed provider group", got)
	}
	if !complete.ProviderProofAt.Equal(now) {
		t.Fatalf("ProviderProofAt = %v, want %v", complete.ProviderProofAt, now)
	}
}

func TestServiceCompleteDeniesNonceMismatchAndUnmappedGroups(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		claims  VerifiedClaims
		wantErr error
	}{
		{
			name: "nonce mismatch",
			claims: VerifiedClaims{
				Subject: "external-subject",
				Nonce:   "different",
				Groups:  []string{"Eshu Developers"},
			},
			wantErr: query.ErrOIDCLoginDenied,
		},
		{
			name: "unmapped groups",
			claims: VerifiedClaims{
				Subject: "external-subject",
				Nonce:   "nonce-secret",
				Groups:  []string{"Unknown"},
			},
			wantErr: query.ErrOIDCLoginDenied,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStateStore{
				consume: StateRecord{
					StateHash:        SHA256Hash("state-secret"),
					NonceHash:        SHA256Hash("nonce-secret"),
					ProviderConfigID: "okta-dev",
					TenantID:         "tenant_a",
					WorkspaceID:      "workspace_a",
					RedirectURIHash:  SHA256Hash("https://eshu.example.test/api/v0/auth/oidc/callback"),
					IssuedAt:         now.Add(-time.Minute),
					ExpiresAt:        now.Add(9 * time.Minute),
					UpdatedAt:        now,
				},
				consumeOK: true,
			}
			service := NewService(Config{
				DefaultProviderID: "okta-dev",
				StateTTL:          10 * time.Minute,
				Providers: []ProviderConfig{{
					ProviderConfigID: "okta-dev",
					IssuerURL:        "https://idp.example.test/oauth2/default",
					ClientID:         "client-id",
					RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
					GroupsClaim:      "groups",
					TenantID:         "tenant_a",
					WorkspaceID:      "workspace_a",
				}},
			}, store, StaticGrantResolver{
				GroupRoleMappings: []GroupRoleMapping{{Group: "Eshu Developers", RoleIDs: []string{"developer"}}},
				RoleGrants:        []RoleGrant{{RoleID: "developer", PolicyRevisionHash: "sha256:policy", AllowedScopeIDs: []string{"scope_a"}}},
			}, func(context.Context, ProviderConfig) (Connector, error) {
				return &fakeConnector{claims: tt.claims}, nil
			}, WithNow(func() time.Time { return now }))

			_, err := service.CompleteOIDCLogin(context.Background(), query.OIDCLoginCompleteRequest{
				State: "state-secret",
				Code:  "auth-code",
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CompleteOIDCLogin() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceCompleteDeniesEmptyGroupsBeforeGrantResolution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 13, 30, 0, 0, time.UTC)
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			NonceHash:        SHA256Hash("nonce-secret"),
			ProviderConfigID: "okta-dev",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
			RedirectURIHash:  SHA256Hash("https://eshu.example.test/api/v0/auth/oidc/callback"),
			IssuedAt:         now.Add(-time.Minute),
			ExpiresAt:        now.Add(9 * time.Minute),
			UpdatedAt:        now,
		},
		consumeOK: true,
	}
	resolver := &failingGrantResolver{}
	service := NewService(Config{
		DefaultProviderID: "okta-dev",
		StateTTL:          10 * time.Minute,
		Providers: []ProviderConfig{{
			ProviderConfigID: "okta-dev",
			IssuerURL:        "https://idp.example.test/oauth2/default",
			ClientID:         "client-id",
			RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
			GroupsClaim:      "groups",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
		}},
	}, store, resolver, func(context.Context, ProviderConfig) (Connector, error) {
		return &fakeConnector{claims: VerifiedClaims{
			Subject: "external-subject",
			Nonce:   "nonce-secret",
			Groups:  nil,
		}}, nil
	}, WithNow(func() time.Time { return now }))

	_, err := service.CompleteOIDCLogin(context.Background(), query.OIDCLoginCompleteRequest{
		State: "state-secret",
		Code:  "auth-code",
	})
	if !errors.Is(err, query.ErrOIDCLoginDenied) {
		t.Fatalf("CompleteOIDCLogin() error = %v, want %v", err, query.ErrOIDCLoginDenied)
	}
	if resolver.called {
		t.Fatal("grant resolver was called for empty provider groups; want fail-closed denial before storage")
	}
}

func TestServiceCompleteStaticGrantsDoNotEnforcePermissionCatalog(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			NonceHash:        SHA256Hash("nonce-secret"),
			ProviderConfigID: "okta-dev",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
			RedirectURIHash:  SHA256Hash("https://eshu.example.test/api/v0/auth/oidc/callback"),
			IssuedAt:         now.Add(-time.Minute),
			ExpiresAt:        now.Add(9 * time.Minute),
			UpdatedAt:        now,
		},
		consumeOK: true,
	}
	connector := &fakeConnector{
		claims: VerifiedClaims{
			Subject: "external-subject",
			Nonce:   "nonce-secret",
			Groups:  []string{"Eshu Developers"},
		},
	}
	service := NewService(Config{
		DefaultProviderID: "okta-dev",
		StateTTL:          10 * time.Minute,
		Providers: []ProviderConfig{{
			ProviderConfigID: "okta-dev",
			IssuerURL:        "https://idp.example.test/oauth2/default",
			ClientID:         "client-id",
			RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
			GroupsClaim:      "groups",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
		}},
	}, store, StaticGrantResolver{
		GroupRoleMappings: []GroupRoleMapping{{Group: "Eshu Developers", RoleIDs: []string{"developer"}}},
		RoleGrants: []RoleGrant{{
			RoleID:               "developer",
			PolicyRevisionHash:   "sha256:policy",
			AllowedScopeIDs:      []string{"scope_a"},
			AllowedRepositoryIDs: []string{"repo_a"},
		}},
	}, func(context.Context, ProviderConfig) (Connector, error) {
		return connector, nil
	}, WithNow(func() time.Time { return now }))

	complete, err := service.CompleteOIDCLogin(context.Background(), query.OIDCLoginCompleteRequest{
		State: "state-secret",
		Code:  "auth-code",
	})
	if err != nil {
		t.Fatalf("CompleteOIDCLogin() error = %v", err)
	}
	auth := complete.Auth
	// A static role grant carries no permission-catalog snapshot. Enabling
	// enforcement against an empty snapshot would 403 every catalog-gated route
	// (ask/semantic search) the static scope grant actually authorizes.
	if auth.PermissionCatalogEnforced {
		t.Fatal("PermissionCatalogEnforced = true, want false for static grants that supply no catalog snapshot")
	}
	if len(auth.AllowedPermissionFeatures) != 0 || len(auth.AllowedPermissionDataClasses) != 0 {
		t.Fatalf(
			"static permission snapshot = %#v/%#v, want empty",
			auth.AllowedPermissionFeatures,
			auth.AllowedPermissionDataClasses,
		)
	}
	// The operator-declared scope/repo grant still bounds the session.
	if got := auth.AllowedScopeIDs; len(got) != 1 || got[0] != "scope_a" {
		t.Fatalf("AllowedScopeIDs = %#v, want [scope_a]", got)
	}
}

func TestServiceCompleteHonorsResolverDeclaredCatalogEnforcement(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 30, 0, 0, time.UTC)
	store := &fakeStateStore{
		consume: StateRecord{
			StateHash:        SHA256Hash("state-secret"),
			NonceHash:        SHA256Hash("nonce-secret"),
			ProviderConfigID: "okta-dev",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
			RedirectURIHash:  SHA256Hash("https://eshu.example.test/api/v0/auth/oidc/callback"),
			IssuedAt:         now.Add(-time.Minute),
			ExpiresAt:        now.Add(9 * time.Minute),
			UpdatedAt:        now,
		},
		consumeOK: true,
	}
	resolver := fixedGrantResolver{resolution: GrantResolution{
		RoleIDs:                      []string{"analyst"},
		PolicyRevisionHash:           "sha256:policy",
		PermissionCatalogEnforced:    true,
		AllowedPermissionFeatures:    []string{"ask_search"},
		AllowedPermissionDataClasses: []string{"source_content"},
		AllowedScopeIDs:              []string{"scope_a"},
	}}
	service := NewService(Config{
		DefaultProviderID: "okta-dev",
		StateTTL:          10 * time.Minute,
		Providers: []ProviderConfig{{
			ProviderConfigID: "okta-dev",
			IssuerURL:        "https://idp.example.test/oauth2/default",
			ClientID:         "client-id",
			RedirectURL:      "https://eshu.example.test/api/v0/auth/oidc/callback",
			GroupsClaim:      "groups",
			TenantID:         "tenant_a",
			WorkspaceID:      "workspace_a",
		}},
	}, store, resolver, func(context.Context, ProviderConfig) (Connector, error) {
		return &fakeConnector{claims: VerifiedClaims{
			Subject: "external-subject",
			Nonce:   "nonce-secret",
			Groups:  []string{"Eshu Analysts"},
		}}, nil
	}, WithNow(func() time.Time { return now }))

	complete, err := service.CompleteOIDCLogin(context.Background(), query.OIDCLoginCompleteRequest{
		State: "state-secret",
		Code:  "auth-code",
	})
	if err != nil {
		t.Fatalf("CompleteOIDCLogin() error = %v", err)
	}
	auth := complete.Auth
	if !auth.PermissionCatalogEnforced {
		t.Fatal("PermissionCatalogEnforced = false, want true when the resolver declares enforcement")
	}
	if got := auth.AllowedPermissionFeatures; len(got) != 1 || got[0] != "ask_search" {
		t.Fatalf("AllowedPermissionFeatures = %#v, want [ask_search]", got)
	}
	if got := auth.AllowedPermissionDataClasses; len(got) != 1 || got[0] != "source_content" {
		t.Fatalf("AllowedPermissionDataClasses = %#v, want [source_content]", got)
	}
}

func fakeConnectorFactory(t *testing.T) ConnectorFactory {
	t.Helper()
	return func(context.Context, ProviderConfig) (Connector, error) {
		return &fakeConnector{}, nil
	}
}

type fakeStateStore struct {
	created      []StateRecord
	consume      StateRecord
	consumeOK    bool
	consumedHash string
}

func (s *fakeStateStore) CreateState(_ context.Context, record StateRecord) error {
	s.created = append(s.created, record)
	return nil
}

func (s *fakeStateStore) ConsumeState(
	_ context.Context,
	stateHash string,
	_ time.Time,
) (StateRecord, bool, error) {
	s.consumedHash = stateHash
	return s.consume, s.consumeOK, nil
}

type failingGrantResolver struct {
	called bool
}

func (r *failingGrantResolver) ResolveGroupGrants(
	context.Context,
	GrantQuery,
) (GrantResolution, bool, error) {
	r.called = true
	return GrantResolution{}, false, errors.New("grant resolver should not be called")
}

type fixedGrantResolver struct {
	resolution GrantResolution
}

func (r fixedGrantResolver) ResolveGroupGrants(
	context.Context,
	GrantQuery,
) (GrantResolution, bool, error) {
	return r.resolution, true, nil
}

type fakeConnector struct {
	claims          VerifiedClaims
	exchangedCode   string
	verifiedIDToken string
}

func (c *fakeConnector) AuthCodeURL(state string, nonce string) string {
	return "https://idp.example.test/authorize?state=" + state + "&nonce=" + nonce
}

func (c *fakeConnector) Exchange(_ context.Context, code string) (TokenSet, error) {
	c.exchangedCode = code
	return TokenSet{IDToken: "id-token"}, nil
}

func (c *fakeConnector) VerifyIDToken(_ context.Context, rawIDToken string) (VerifiedClaims, error) {
	c.verifiedIDToken = rawIDToken
	if c.claims.Subject == "" {
		return VerifiedClaims{Subject: "external-subject", Nonce: "nonce-secret", Groups: []string{"Eshu Developers"}}, nil
	}
	return c.claims, nil
}

func sequenceOIDCSecrets(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		value := values[index]
		index++
		return value, nil
	}
}
