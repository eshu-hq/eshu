// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// BearerProvider describes one enabled identity provider whose issued OAuth2
// access tokens the bearer resolver accepts. It is deliberately narrower than
// oidclogin.ProviderConfig: bearer-token validation never needs a client
// secret, redirect URL, or scopes, so this type never carries them.
type BearerProvider struct {
	// ProviderConfigID identifies the provider for grant resolution
	// (oidclogin.GrantQuery.ProviderConfigID) and verifier-reuse matching.
	ProviderConfigID string
	// IssuerURL is the provider's OIDC issuer, used both to route an
	// unverified token (by its "iss" claim) to this entry and as the
	// verifier's expected issuer.
	IssuerURL string
	// TenantID scopes the resolved AuthContext and the grant-resolution
	// query.
	TenantID string
	// WorkspaceID scopes the resolved AuthContext and the grant-resolution
	// query. A DB-backed provider config carries no workspace column (see
	// storage/postgres's identity_provider_configs schema note); callers
	// that source BearerProvider from the DB must resolve a concrete
	// WorkspaceID themselves (mirroring cmd/api's oidcDBProviderResolver
	// pattern) before handing it to a ProviderSource, or omit the provider
	// entirely when no unambiguous workspace exists. See this package's
	// README for why oidcbearer does not resolve it internally.
	WorkspaceID string
	// GroupsClaim is the token claim carrying the caller's external group
	// memberships (defaults to "groups" upstream, mirroring oidclogin).
	GroupsClaim string
	// SubjectClaim is the token claim carrying the caller's subject id when
	// it differs from the standard "sub" claim. Empty means "sub".
	SubjectClaim string
	// RevisionID changes whenever the provider's configuration changes
	// (env providers use the constant "env"; DB-backed providers use their
	// active_revision_id). The verifier cache reuses a prior verifier only
	// when both IssuerURL and RevisionID are unchanged from the previous
	// snapshot, so an issuer rotation or revision bump always rebuilds.
	RevisionID string
}

// ProviderSource lists the identity providers currently enabled for bearer
// validation. Implementations must return a defensive copy: the resolver
// caches its result across the configured TTL and never mutates it, but
// must not be handed a slice the caller may mutate concurrently.
type ProviderSource interface {
	ActiveBearerProviders(ctx context.Context) ([]BearerProvider, error)
}

// ProviderSourceFunc adapts a function to a ProviderSource.
type ProviderSourceFunc func(ctx context.Context) ([]BearerProvider, error)

// ActiveBearerProviders implements ProviderSource.
func (f ProviderSourceFunc) ActiveBearerProviders(ctx context.Context) ([]BearerProvider, error) {
	return f(ctx)
}

// multiProviderSource composes several ProviderSource values (env-file plus
// DB-backed, typically) into one. Callers build the concrete DB-backed
// source outside this package (see the package README's boundary note) and
// hand it to ComposeProviderSources alongside the env-file source this
// package itself provides via NewEnvProviderSource.
type multiProviderSource struct {
	sources []ProviderSource
}

// ComposeProviderSources returns one ProviderSource that concatenates every
// non-nil source's active providers. When two sources both return an entry
// for the same IssuerURL, the later source's entry wins the routing table
// (see cache.go's rebuild): callers should order sources so a DB-backed
// provider does not silently shadow an env-file provider of the same issuer,
// or vice versa, without an explicit choice.
func ComposeProviderSources(sources ...ProviderSource) ProviderSource {
	nonNil := make([]ProviderSource, 0, len(sources))
	for _, s := range sources {
		if s != nil {
			nonNil = append(nonNil, s)
		}
	}
	return &multiProviderSource{sources: nonNil}
}

// ActiveBearerProviders implements ProviderSource.
func (m *multiProviderSource) ActiveBearerProviders(ctx context.Context) ([]BearerProvider, error) {
	if m == nil {
		return nil, nil
	}
	var all []BearerProvider
	for _, source := range m.sources {
		providers, err := source.ActiveBearerProviders(ctx)
		if err != nil {
			return nil, fmt.Errorf("oidcbearer: list active bearer providers: %w", err)
		}
		all = append(all, providers...)
	}
	return all, nil
}

// VerifierFactory builds a go-oidc ID-token verifier for one issuer,
// checking the given audience. Production wiring uses
// NewProviderVerifierFactory (discovery + JWKS over the network); tests
// inject a factory backed by oidc.StaticKeySet so the real go-oidc Verify
// path runs hermetically, with no network access.
type VerifierFactory func(ctx context.Context, issuerURL, audience string) (*oidc.IDTokenVerifier, error)

// NewProviderVerifierFactory returns the production VerifierFactory: OIDC
// discovery against issuerURL, then a JWKS-backed verifier scoped to
// audience.
func NewProviderVerifierFactory() VerifierFactory {
	return func(ctx context.Context, issuerURL, audience string) (*oidc.IDTokenVerifier, error) {
		provider, err := oidc.NewProvider(ctx, issuerURL)
		if err != nil {
			return nil, fmt.Errorf("oidcbearer: discover oidc provider: %w", err)
		}
		return provider.Verifier(&oidc.Config{ClientID: audience}), nil
	}
}

// defaultRebuildTimeout bounds one background snapshot rebuild (provider
// listing plus any new verifier discovery) so a hung issuer or database
// cannot leave a rebuild permanently in flight and wedge the "rebuilding"
// guard (see cache.go).
const defaultRebuildTimeout = 20 * time.Second
