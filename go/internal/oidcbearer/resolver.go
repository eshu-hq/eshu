// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// defaultTTL is the verifier-cache staleness window: how long a background
// rebuild is deferred after the last one completed. It is the mechanism that
// makes provider CRUD (enable/disable/rotate) visible without a restart —
// see the package README for why a fixed TTL, not a push notification, is
// the correct (and only honest) mechanism across independent api/mcp-server
// processes with no shared event bus.
const defaultTTL = 30 * time.Second

// subjectClassExternalOIDCUser matches oidclogin's own (unexported)
// oidcSubjectClass constant exactly, so a bearer-resolved AuthContext is
// indistinguishable in shape from an interactive-login one for the same
// external IdP subject.
const subjectClassExternalOIDCUser = "external_oidc_user"

// ErrProviderSourceRequired, ErrGrantResolverRequired, and
// ErrAudienceRequired are returned by NewResolver when a required
// dependency is missing. Wiring code should treat ESHU_AUTH_RESOURCE_URI
// being unset as "do not construct a Resolver at all" (see the package
// README's activation contract), not as a reason to construct one and let
// it fail these checks.
var (
	ErrProviderSourceRequired = errors.New("oidcbearer: provider source is required")
	ErrGrantResolverRequired  = errors.New("oidcbearer: grant resolver is required")
	ErrAudienceRequired       = errors.New("oidcbearer: audience is required")
)

// Config configures a Resolver. Source, GrantResolver, and Audience are
// required; every other field has a production-sane default.
type Config struct {
	// Source lists the currently enabled bearer-token IdPs. Compose an
	// env-file source (NewEnvProviderSource) with a caller-supplied
	// DB-backed source via ComposeProviderSources.
	Source ProviderSource
	// GrantResolver maps a verified token's hashed external groups to Eshu
	// roles and grants. Wiring must pass the SAME resolver composition the
	// interactive OIDC login path uses (a DB-backed resolver falling back
	// to the static env-file resolver) so AC #3's grant equivalence holds.
	GrantResolver oidclogin.GrantResolver
	// Audience is the canonical Eshu resource URI (RFC 8707,
	// ESHU_AUTH_RESOURCE_URI) every accepted access token's "aud" must
	// carry.
	Audience string
	// VerifierFactory builds a go-oidc verifier for one issuer. Defaults to
	// NewProviderVerifierFactory() (real discovery + JWKS). Tests inject a
	// factory backed by oidc.StaticKeySet.
	VerifierFactory VerifierFactory
	// TTL is the verifier-cache staleness window. Defaults to 30s.
	TTL time.Duration
	// Now overrides the clock. Defaults to time.Now.
	Now func() time.Time
	// Instruments records eshu_dp_oidc_bearer_validation_total. Optional.
	Instruments *telemetry.Instruments
	// Logger records structured warnings (never the raw token). Optional.
	Logger *slog.Logger
}

// Resolver implements query.ScopedTokenResolver for IdP-issued OAuth2 bearer
// access tokens. See the package doc and README for the full algorithm and
// the security reasoning behind each denial path.
type Resolver struct {
	cache         *cache
	grantResolver oidclogin.GrantResolver
	now           func() time.Time
	logger        *slog.Logger
}

// NewResolver constructs a Resolver and synchronously builds its first
// verifier-cache snapshot (including the zero-provider snapshot) so the very
// first ResolveScopedToken call already observes a real, non-nil snapshot
// rather than treating a nil "never built yet" state as a special case.
func NewResolver(ctx context.Context, cfg Config) (*Resolver, error) {
	if cfg.Source == nil {
		return nil, ErrProviderSourceRequired
	}
	if cfg.GrantResolver == nil {
		return nil, ErrGrantResolverRequired
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, ErrAudienceRequired
	}
	factory := cfg.VerifierFactory
	if factory == nil {
		factory = NewProviderVerifierFactory()
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	c := &cache{
		source:          cfg.Source,
		verifierFactory: factory,
		audience:        strings.TrimSpace(cfg.Audience),
		ttl:             ttl,
		now:             now,
		instruments:     cfg.Instruments,
		logger:          cfg.Logger,
	}
	c.rebuildSync(ctx)
	return &Resolver{cache: c, grantResolver: cfg.GrantResolver, now: now, logger: cfg.Logger}, nil
}

// ResolveScopedToken implements query.ScopedTokenResolver.
//
// Three outcomes:
//
//   - (zero, false, nil): the credential was never a JWT, or no bearer IdP
//     is currently enabled. The caller's resolver chain should keep trying
//     the next resolver (identity tokens are opaque hashes, never JWTs, so
//     this is always safe: a JWT-shaped credential can never coincidentally
//     match an opaque token's hash lookup either).
//   - (zero, false, err): the credential was JWT-shaped AND at least one
//     bearer IdP is enabled, but validation denied it (unknown issuer,
//     expired, wrong audience, bad signature, malformed claims, or no
//     resolvable grants). Once both conditions hold, this resolver owns the
//     verdict outright — it never falls through to a resolver that could
//     not have understood a JWT anyway, and the composite resolver's
//     err != nil check stops the chain and returns 401 (AC #2 distinct
//     denied reason, recorded via the eshu_dp_oidc_bearer_validation_total
//     outcome label and a structured log carrying only issuer and subject
//     hash — never the raw token).
//   - (auth, true, nil): the token verified and mapped to at least one
//     granted role.
func (r *Resolver) ResolveScopedToken(ctx context.Context, credential string) (query.AuthContext, bool, error) {
	credential = strings.TrimSpace(credential)
	if !isJWTShaped(credential) {
		return query.AuthContext{}, false, nil
	}

	snap := r.cache.currentAndMaybeRebuild(ctx)
	if snap.empty() {
		return query.AuthContext{}, false, nil
	}

	ctx, span := startResolveSpan(ctx)
	defer span.End()

	iss, err := peekUnverifiedIssuer(credential)
	if err != nil {
		return query.AuthContext{}, false, r.deny(ctx, "", outcomeMalformed)
	}

	entry, matched := snap.byIssuer[iss]
	if !matched {
		return query.AuthContext{}, false, r.deny(ctx, iss, outcomeUnknownIssuer)
	}

	token, err := entry.verifier.Verify(ctx, credential)
	if err != nil {
		return query.AuthContext{}, false, r.deny(ctx, iss, classifyVerifyError(err))
	}

	claims, err := extractVerifiedClaims(token, entry.provider.SubjectClaim, entry.provider.GroupsClaim)
	if err != nil || claims.Subject == "" {
		return query.AuthContext{}, false, r.deny(ctx, iss, outcomeMalformed)
	}

	groupHashes := hashStrings(claims.Groups)
	if len(groupHashes) == 0 {
		return query.AuthContext{}, false, r.deny(ctx, iss, outcomeNoGrants)
	}

	now := r.now().UTC()
	grants, ok, err := r.grantResolver.ResolveGroupGrants(ctx, oidclogin.GrantQuery{
		ProviderConfigID: entry.provider.ProviderConfigID,
		TenantID:         entry.provider.TenantID,
		WorkspaceID:      entry.provider.WorkspaceID,
		GroupHashes:      groupHashes,
		AsOf:             now,
	})
	if err != nil {
		r.logger0().Error("oidc bearer grant resolution failed; token denied",
			"iss", iss, "provider_config_id", entry.provider.ProviderConfigID, "error", err)
		return query.AuthContext{}, false, r.deny(ctx, iss, outcomeNoGrants)
	}
	if !ok || len(grants.RoleIDs) == 0 {
		return query.AuthContext{}, false, r.deny(ctx, iss, outcomeNoGrants)
	}

	subjectIDHash := oidclogin.SHA256Hash(entry.provider.ProviderConfigID + ":" + claims.Subject)
	auth := query.AuthContext{
		Mode:                         query.AuthModeScoped,
		TenantID:                     entry.provider.TenantID,
		WorkspaceID:                  entry.provider.WorkspaceID,
		SubjectClass:                 subjectClassExternalOIDCUser,
		SubjectIDHash:                subjectIDHash,
		PolicyRevisionHash:           grants.PolicyRevisionHash,
		RoleIDs:                      append([]string(nil), grants.RoleIDs...),
		PermissionCatalogEnforced:    grants.PermissionCatalogEnforced,
		AllScopes:                    grants.AllScopes,
		AllowedScopeIDs:              append([]string(nil), grants.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), grants.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), grants.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), grants.AllowedPermissionDataClasses...),
		ExternalProviderConfigID:     entry.provider.ProviderConfigID,
	}
	r.cache.recordOutcome(ctx, outcomeValid)
	r.logger0().Info("oidc bearer token validated",
		"iss", iss, "subject_hash", subjectIDHash, "outcome", outcomeValid)
	return auth, true, nil
}

// deny records the outcome counter, logs a structured warning carrying only
// the issuer and outcome (never the raw token or any claim value), and
// returns the generic wrapped error the middleware maps to a 401.
func (r *Resolver) deny(ctx context.Context, iss, outcome string) error {
	r.cache.recordOutcome(ctx, outcome)
	r.logger0().Warn("oidc bearer token denied", "iss", iss, "outcome", outcome)
	return fmt.Errorf("oidcbearer: bearer token denied: %s", outcome)
}

func (r *Resolver) logger0() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default()
}
