// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package auth's login contracts are the versioned core-owned shapes for
// external-provider sign-in (D13, eshu-hq/eshu#9669): the GitHub and OIDC
// login admission ports, the SSO denial wrapper, the bearer-unrecognized
// sentinel, and the token/session resolver interfaces. They moved here from
// root package query so handler-family subpackages (githublogin, oidclogin,
// oidcbearer, scopedtoken) can code against sign-in without importing the
// handler package the removal PR deletes. Root keeps type aliases and one
// function forwarder so every in-tree router caller compiles unchanged.
//
// AuthLoginContractsVersion versions this set. A breaking change to any
// named contract bumps it in the same diff — and the per-family admission
// version beside it — so eshu-api pins the exact shape it codes against.
// Every moved symbol keeps its exact fail-closed semantics: sentinels keep
// their fixed 503/400/403 mapping, denials keep their classification, and
// unknown outcomes keep mapping to denied, never allowed.
package auth

import (
	"context"
	"errors"
	"time"
)

// AuthLoginContractsVersion versions the core-owned login-contract set in
// this file: the GitHub/SSO admission ports, the SSO denial wrapper, the
// bearer-unrecognized sentinel, and the resolver interfaces. See
// querycontract.CoreReadContractsVersion for the read-contract counterpart.
const AuthLoginContractsVersion = "v0"

// GitHub sign-in errors (issue #5166, F-5) let the connector map fail-closed
// outcomes to stable HTTP statuses without leaking provider, org, or team
// details. github.com user login is plain OAuth2, not OIDC — see
// internal/githublogin's doc.go — so these are distinct from
// ErrOIDCLogin*, but GitHubLoginHandler maps them to the identical
// 503/400/403 shape as OIDCLoginHandler.
var (
	ErrGitHubLoginUnavailable    = errors.New("github login unavailable")
	ErrGitHubLoginInvalidRequest = errors.New("github login invalid request")
	ErrGitHubLoginDenied         = errors.New("github login denied")
)

// GitHubLoginAdmissionContractVersion versions the #8872
// admission-contract shape for GitHub login (GET
// /api/v0/auth/github/login, GET /api/v0/auth/github/callback).
// Canonical logins persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned Service implementation satisfies. A renamed,
// added, removed, or re-signed method fails the frozen method-set test
// until that list is deliberately updated in the same diff; bump this
// version alongside so dest consumers can pin the shape they code
// against.
//
// The optional GitHubProviderLister is not part of v0: a dest client
// that implements only the 2 admission methods silently drops runtime
// provider discovery (RegisteredProviders returns nil). Dest must also
// implement the lister or accept DB-row-only discovery. Browser-session
// issuance on callback belongs to the BrowserSessions family's own seam,
// not this contract.
const GitHubLoginAdmissionContractVersion = "v0"

// GitHubLoginAdmission is the versioned admission-contract port the API
// handler depends on instead of a concrete login service. It aliases the
// narrow login service so existing fakes and the core-owned Service
// implementation keep satisfying it unchanged.
type GitHubLoginAdmission = GitHubLoginService

// GitHubLoginService resolves GitHub Authorization Code login requests into
// Eshu browser-session authorization contexts.
type GitHubLoginService interface {
	StartGitHubLogin(context.Context, GitHubLoginStartRequest) (GitHubLoginStartResponse, error)
	CompleteGitHubLogin(context.Context, GitHubLoginCompleteRequest) (GitHubLoginCompleteResponse, error)
}

// GitHubLoginStartRequest selects one configured GitHub provider and the
// tenant/workspace boundary the dashboard session should enter after
// callback.
type GitHubLoginStartRequest struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	ReturnToPath     string
}

// GitHubLoginStartResponse contains the provider authorization redirect URL.
type GitHubLoginStartResponse struct {
	RedirectURL string
}

// GitHubLoginCompleteRequest carries the callback state and authorization
// code.
type GitHubLoginCompleteRequest struct {
	State string
	Code  string
}

// GitHubLoginCompleteResponse carries the resolved Eshu auth context and
// optional local path for post-login browser navigation, mirroring
// OIDCLoginCompleteResponse.
type GitHubLoginCompleteResponse struct {
	Auth                AuthContext
	ProviderConfigID    string
	ProviderSubjectID   string
	ProviderGroupHashes []string
	ProviderProofAt     time.Time
	ReturnToPath        string
}

// DefaultGitHubSessionRefreshWindow bounds external-proof staleness for
// already-issued GitHub browser sessions, mirroring
// DefaultOIDCSessionRefreshWindow.
const DefaultGitHubSessionRefreshWindow = 15 * time.Minute

// GitHubProviderLister is an optional extension on GitHubLoginService
// implementations that can enumerate the set of provider_config_ids they
// manage, mirroring OIDCProviderLister.
type GitHubProviderLister interface {
	ListGitHubProviderIDs() []GitHubRegisteredProvider
}

// GitHubRegisteredProvider carries the identity fields needed for provider
// discovery. Only the safe-to-surface subset of ProviderConfig is included —
// no client id, secret, base URL, or allowed-org list.
type GitHubRegisteredProvider struct {
	ProviderConfigID string
	TenantID         string
}

// OIDC login errors let provider implementations map fail-closed outcomes to
// stable HTTP statuses without leaking provider or claim details.
var (
	ErrOIDCLoginUnavailable    = errors.New("oidc login unavailable")
	ErrOIDCLoginInvalidRequest = errors.New("oidc login invalid request")
	ErrOIDCLoginDenied         = errors.New("oidc login denied")
)

// OIDCLoginAdmissionContractVersion versions the #8872
// admission-contract shape for OIDC login (GET
// /api/v0/auth/oidc/login, GET /api/v0/auth/oidc/callback). Canonical
// logins persist through explicit core-owned admission contracts:
// eshu-api keeps the HTTP shape and calls this versioned port, which the
// core-owned Service implementation satisfies. A renamed, added, removed,
// or re-signed method fails the frozen method-set test until that list is
// deliberately updated in the same diff; bump this version alongside so
// dest consumers can pin the shape they code against.
//
// The optional OIDCProviderLister is not part of v0: a dest client that
// implements only the 2 admission methods silently drops runtime provider
// discovery (RegisteredProviders returns nil). Dest must also implement
// the lister or accept DB-row-only discovery. Browser-session issuance on
// callback belongs to the BrowserSessions family's own seam, not this
// contract.
const OIDCLoginAdmissionContractVersion = "v0"

// OIDCLoginAdmission is the versioned admission-contract port the API
// handler depends on instead of a concrete login service. It aliases the
// narrow login service so existing fakes and the core-owned Service
// implementation keep satisfying it unchanged.
type OIDCLoginAdmission = OIDCLoginService

// OIDCLoginService resolves generic OIDC Authorization Code login requests
// into Eshu browser-session authorization contexts.
type OIDCLoginService interface {
	StartOIDCLogin(context.Context, OIDCLoginStartRequest) (OIDCLoginStartResponse, error)
	CompleteOIDCLogin(context.Context, OIDCLoginCompleteRequest) (OIDCLoginCompleteResponse, error)
}

// OIDCLoginStartRequest selects one configured identity provider and the
// tenant/workspace boundary the dashboard session should enter after callback.
type OIDCLoginStartRequest struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	ReturnToPath     string
}

// OIDCLoginStartResponse contains the provider authorization redirect URL.
type OIDCLoginStartResponse struct {
	RedirectURL string
}

// OIDCLoginCompleteRequest carries the callback state and authorization code.
type OIDCLoginCompleteRequest struct {
	State string
	Code  string
}

// OIDCLoginCompleteResponse carries the resolved Eshu auth context and optional
// local path for post-login browser navigation.
//
// ProviderGroupHashes carries the hashed external group claims that mapped to
// this session's grants at login. They are persisted hash-only so bounded
// active-session refresh can re-resolve the group-to-role mappings and detect
// removed or tombstoned mappings rather than trusting stale role IDs alone.
type OIDCLoginCompleteResponse struct {
	Auth                AuthContext
	ProviderConfigID    string
	ProviderSubjectID   string
	ProviderGroupHashes []string
	ProviderProofAt     time.Time
	ReturnToPath        string
}

// DefaultOIDCSessionRefreshWindow bounds external IdP proof staleness for
// already-issued OIDC browser sessions.
const DefaultOIDCSessionRefreshWindow = 15 * time.Minute

// OIDCProviderLister is an optional extension on OIDCLoginService implementations
// that can enumerate the set of provider_config_ids they manage. The
// AuthProviderListHandler uses it to surface runtime-configured OIDC providers
// for the pre-auth provider discovery endpoint. Each entry is
// (ProviderConfigID, TenantID) — no client secrets, issuer URLs, or claim
// mappings are exposed. Implementations that do not list providers need not
// implement this interface.
type OIDCProviderLister interface {
	// ListOIDCProviderIDs returns the (ProviderConfigID, TenantID) pairs for
	// every OIDC provider registered at runtime. The caller is responsible for
	// deduplicating against DB rows before surfacing to clients.
	ListOIDCProviderIDs() []OIDCRegisteredProvider
}

// OIDCRegisteredProvider carries the identity fields needed for provider
// discovery. Only the safe-to-surface subset of ProviderConfig is included.
type OIDCRegisteredProvider struct {
	ProviderConfigID string
	TenantID         string
}

// SSOLoginDeniedError wraps an SSO login sentinel — ErrGitHubLoginDenied,
// ErrGitHubLoginUnavailable, ErrOIDCLoginDenied, or ErrOIDCLoginUnavailable —
// with the specific classification that produced it (e.g. "org_not_allowed",
// "no_grants"). The githublogin and oidclogin CompleteXLogin implementations
// already compute this classification internally (issue #5601: it used to be
// logged via slog and then discarded); wrapping it here lets the query-layer
// callback handler record WHY a login was denied as a governance-audit
// ReasonCode instead of a generic catch-all, without changing the HTTP
// status mapping in writeGitHubLoginError/writeOIDCLoginError — errors.Is
// against the wrapped Sentinel keeps matching through Unwrap exactly as it
// did before this type existed.
type SSOLoginDeniedError struct {
	// Sentinel is the bare provider sentinel this error wraps
	// (ErrGitHubLoginDenied, ErrGitHubLoginUnavailable, ErrOIDCLoginDenied,
	// or ErrOIDCLoginUnavailable).
	Sentinel error
	// Reason is a stable, low-cardinality classification safe to use
	// verbatim as a governanceaudit.Event.ReasonCode: lowercase ASCII
	// letters, digits, and underscore, at most 64 characters (see
	// governanceaudit.validReasonCode).
	Reason string
}

// Error implements the error interface.
func (e *SSOLoginDeniedError) Error() string {
	if e.Sentinel == nil {
		return e.Reason
	}
	if e.Reason == "" {
		return e.Sentinel.Error()
	}
	return e.Sentinel.Error() + ": " + e.Reason
}

// Unwrap exposes the wrapped sentinel so errors.Is(err, ErrGitHubLoginDenied)
// (or the OIDC/Unavailable equivalents) keeps matching unchanged.
func (e *SSOLoginDeniedError) Unwrap() error { return e.Sentinel }

// SSOLoginDenialReason extracts the classification an SSOLoginDeniedError
// carries via errors.As. Returns fallback when err does not wrap one (or
// wraps one with an empty Reason), so callers always get a valid non-empty
// reason code to audit.
func SSOLoginDenialReason(err error, fallback string) string {
	var denied *SSOLoginDeniedError
	if errors.As(err, &denied) && denied.Reason != "" {
		return denied.Reason
	}
	return fallback
}

// ErrBearerCredentialUnrecognized marks a bearer-credential denial where the
// presented credential was never a recognized issued token for this deployment
// — a JWT whose issuer is not in the active bearer-validation snapshot, or a
// pre-verify unparseable JWT (issue #5163, F-2). A ScopedTokenResolver wraps it
// with %w at exactly those PRE-match outcomes (see
// internal/oidcbearer.Resolver.deny) and never at a POST-match denial (expired,
// bad signature, wrong audience, malformed verified claims, no grants): a
// post-match denial means the credential WAS understood, so steering it to the
// discovery document would be noise. authMiddlewareWithRoutePolicy tests this
// sentinel with errors.Is to decide whether a resolver-error 401 augments its
// WWW-Authenticate challenge (rows 6/7) or stays bare (rows 5/11). An infra
// error from the resolver chain never carries this sentinel, so it fails safe
// to the bare challenge — the deliberate fail-safe against the
// anthropics/claude-code#59467 challenge-on-every-401 bug.
//
// The message's "query:" prefix is historical (it read that way when root
// package query owned this sentinel); it is frozen because middleware tests
// and operator runbooks match it verbatim.
var ErrBearerCredentialUnrecognized = errors.New("query: bearer credential not a recognized issued token")

// ScopedTokenResolver resolves a presented bearer credential into an auth
// context without exposing raw token values to handlers.
type ScopedTokenResolver interface {
	ResolveScopedToken(context.Context, string) (AuthContext, bool, error)
}

// BrowserSessionResolver resolves a session-cookie credential into an auth
// context using only server-side hashes. Raw session and CSRF values are hashed
// by middleware before this interface is called.
type BrowserSessionResolver interface {
	ResolveBrowserSession(
		context.Context,
		string,
		string,
		bool,
		time.Time,
	) (AuthContext, bool, error)
}
