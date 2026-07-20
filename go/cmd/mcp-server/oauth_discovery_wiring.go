// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// envAuthResourceDocumentation and envAuthPreregisteredClientID are the two
// optional operator knobs that populate the RFC 9728 discovery document's
// resource_documentation (RFC 9728 OPTIONAL) and eshu_preregistered_client_id
// (RFC 9728 §2 extension member) fields (issue #5163, F-2). Both are omitted
// from the served document when unset.
const (
	envAuthResourceDocumentation = "ESHU_AUTH_RESOURCE_DOCUMENTATION"
	envAuthPreregisteredClientID = "ESHU_AUTH_PREREGISTERED_CLIENT_ID"
)

// mcpAuthProviderStore implements query.AuthProviderStore for cmd/mcp-server by
// listing the active login-provider rows for a tenant. It mirrors cmd/api's
// authProviderListStore but carries none of that store's env-config
// SAML/OIDC/GitHub reconciliation: mcp-server mounts no interactive login
// runtime, so the DB rows are the complete provider set here. DeriveAuthPosture
// only consumes whether the list is non-empty (the F-2 discovery enablement
// gate), so ProviderConfigID and ProviderKind are the only fields mapped.
type mcpAuthProviderStore struct {
	identity *pgstatus.IdentitySubjectStore
}

// ListLoginProviders implements query.AuthProviderStore.
func (s *mcpAuthProviderStore) ListLoginProviders(ctx context.Context, tenantID string) ([]query.AuthProviderItem, error) {
	if s == nil || s.identity == nil {
		return []query.AuthProviderItem{}, nil
	}
	rows, err := s.identity.ListActiveLoginProviders(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	items := make([]query.AuthProviderItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, query.AuthProviderItem{
			ProviderConfigID: row.ProviderConfigID,
			ProviderKind:     row.ProviderKind,
		})
	}
	return items, nil
}

// mcpSignInPolicyStore implements query.SignInPolicyReadStore for
// cmd/mcp-server, mirroring cmd/api's postgresSignInPolicyAdapter's read side.
// DeriveAuthPosture reads only RequireSSO from the returned policy (to set the
// unused-here LocalLoginOffered hint), so only RequireSSO and TenantID are
// mapped; the full read/mutation surface lives in cmd/api, which owns the
// sign-in-policy admin routes.
type mcpSignInPolicyStore struct {
	identity *pgstatus.IdentitySubjectStore
}

// GetSignInPolicy implements query.SignInPolicyReadStore.
func (s *mcpSignInPolicyStore) GetSignInPolicy(ctx context.Context, tenantID string) (query.SignInPolicy, error) {
	if s == nil || s.identity == nil {
		return query.SignInPolicy{}, nil
	}
	policy, err := s.identity.GetSignInPolicy(ctx, tenantID)
	if err != nil {
		return query.SignInPolicy{}, err
	}
	return query.SignInPolicy{
		TenantID:   policy.TenantID,
		RequireSSO: policy.RequireSSO,
	}, nil
}

// buildMCPOAuthDiscovery builds the RFC 9728 discovery handler and the matching
// 401 challenge policy from operator config (issue #5163, F-2). It returns
// (nil, nil) — discovery disabled, 401s stay byte-identically bare — when
// ESHU_AUTH_RESOURCE_URI is unset (a token-only deployment) or is not a valid
// https (or loopback-http) URL free of query/fragment and quote/control
// characters. The handler and the challenge policy share the same provider and
// sign-in-policy stores and TenantID so the challenge never points a client at
// a discovery URL that would itself 404. The returned challenge is a true nil
// interface when disabled, preserving the middleware's no-alloc byte-identical
// 401 path.
func buildMCPOAuthDiscovery(
	getenv func(string) string,
	providers query.AuthProviderStore,
	policy query.SignInPolicyReadStore,
	issuers query.OAuthAuthorizationServerLister,
	logger *slog.Logger,
) (*query.OAuthProtectedResourceHandler, query.OAuthChallengePolicy) {
	resource := strings.TrimSpace(getenv(envAuthResourceURI))
	if resource == "" {
		return nil, nil
	}
	metadataURL, ok := oauthMetadataURL(resource)
	if !ok {
		if logger != nil {
			logger.Warn(
				"oauth discovery disabled: ESHU_AUTH_RESOURCE_URI is not a valid https (or loopback-http) URL without query/fragment; bearer aud validation still enforced",
				"resource", resource,
				telemetry.EventAttr("runtime.startup.warning"),
			)
		}
		return nil, nil
	}

	handler := &query.OAuthProtectedResourceHandler{
		Providers:             providers,
		Policy:                policy,
		TenantID:              pgstatus.BootstrapAdminTenantID,
		Issuers:               issuers,
		Resource:              resource,
		ScopesSupported:       strings.Fields(query.DefaultOAuthChallengeScope),
		ResourceName:          "Eshu MCP Server",
		ResourceDocumentation: strings.TrimSpace(getenv(envAuthResourceDocumentation)),
		PreregisteredClientID: strings.TrimSpace(getenv(envAuthPreregisteredClientID)),
	}
	challenge := &query.PostureOAuthChallengePolicy{
		Providers:   providers,
		Policy:      policy,
		TenantID:    pgstatus.BootstrapAdminTenantID,
		MetadataURL: metadataURL,
		Scope:       query.DefaultOAuthChallengeScope,
	}
	if logger != nil {
		logger.Info(
			"oauth discovery enabled",
			"resource", resource,
			"metadata_url", metadataURL,
			telemetry.EventAttr("runtime.startup"),
		)
	}
	return handler, challenge
}

// oauthMetadataURL validates the operator's ESHU_AUTH_RESOURCE_URI and derives
// the RFC 9728 section 3 protected-resource-metadata URL from it (issue #5163,
// F-2 §F). The resource must be an absolute https URL — or an http URL whose
// host is a loopback address, for local development — with no query, no
// fragment, and no quote or control characters (the value is embedded in a
// quoted-string WWW-Authenticate directive). The metadata URL inserts
// "/.well-known/oauth-protected-resource" between the host and the resource's
// own path: https://host/mcp yields https://host/.well-known/oauth-protected-resource/mcp,
// https://host yields https://host/.well-known/oauth-protected-resource. An
// invalid resource yields ok=false so the caller disables discovery while the
// bearer resolver still enforces the audience.
func oauthMetadataURL(resource string) (string, bool) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return "", false
	}
	for _, r := range resource {
		if r == '"' || r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	u, err := url.Parse(resource)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", false
		}
	default:
		return "", false
	}
	metadataURL := u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource"
	if suffix := strings.Trim(u.Path, "/"); suffix != "" {
		metadataURL += "/" + suffix
	}
	return metadataURL, true
}

// isLoopbackHost reports whether host (with any port already stripped) is a
// loopback name or address, the only case where a plain-http resource URI is
// accepted for local development.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
