// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IdentityConfig is the synthetic example.test identity this mock IdP hands
// back for every /authorize request. It is intentionally single-identity:
// the browser-auth E2E suites this binary supports (issue #4971) drive one
// scripted login per container, choosing the identity through MOCK_OIDC_*
// env vars at startup rather than through a login form.
type IdentityConfig struct {
	Subject string
	Email   string
	Groups  []string
}

// ServerConfig configures one mock OIDC IdP instance.
type ServerConfig struct {
	// Issuer is this IdP's own base URL. It must be the exact URL a client
	// reaches this server at: it is echoed verbatim into the discovery
	// document's "issuer" field and the ID token's "iss" claim, both of
	// which an OIDC client validates against the URL it used for discovery.
	Issuer string
	// Identity is the synthetic user and group membership returned for
	// every authorization request.
	Identity IdentityConfig
	// GroupClaim names the ID token claim carrying Identity.Groups. Empty
	// defaults to "groups", matching the default GroupsClaim
	// oidclogin.ResolveSealedProviderConfig assigns a DB-backed provider
	// (go/internal/oidclogin/db_provider_config.go).
	GroupClaim string
	// TokenTTL is the ID token lifetime. Empty defaults to one hour.
	TokenTTL time.Duration
	// AccessTokenJWT forces every /token response's access_token to be a
	// signed JWT (see signAccessToken) even when the request carries no RFC
	// 8707 "resource" parameter. Defaults to false, which keeps this IdP
	// byte-stable for the #4971 browser-auth suite: that suite never reads
	// access_token, only id_token, so its opaque "mock-access-token" default
	// must never change under it.
	AccessTokenJWT bool
	// AccessTokenAudience is the JWT access token's "aud" claim fallback,
	// used when AccessTokenJWT is true (or a resource-triggered mint) and
	// neither the /authorize nor /token request carried a "resource" value.
	// F-9's scripted MCP OAuth client always sends "resource" explicitly, so
	// this fallback exists for MOCK_OIDC_ACCESS_TOKEN_JWT=true callers that
	// want a fixed audience without repeating it on every request.
	AccessTokenAudience string
	// AccessTokenTTL is the minted JWT access token's lifetime. Empty
	// defaults to ten minutes. A short TTL (e.g. one second) drives the
	// deterministic expired-token E2E probe.
	AccessTokenTTL time.Duration
	// Now overrides the clock; nil defaults to time.Now.
	Now func() time.Time
}

// Server is a minimal, in-memory OIDC Authorization Code provider. It signs
// with a static, synthetic RSA key (see keys.go) and returns one configured
// example.test identity for every login, with no credential prompt. It
// exists only to give local and CI browser-auth E2E suites a deterministic
// OIDC counterparty (issue #4971, epic #4962); nothing outside a test
// client_id should ever point at it.
type Server struct {
	issuer     string
	identity   IdentityConfig
	groupClaim string
	tokenTTL   time.Duration
	now        func() time.Time

	accessTokenJWT      bool
	accessTokenAudience string
	accessTokenTTL      time.Duration

	privateKey *rsa.PrivateKey
	keyID      string

	mu    sync.Mutex
	codes map[string]authorizeRequest
}

// authorizeRequest is the state captured from one /authorize call and
// consumed exactly once by the matching /token call.
type authorizeRequest struct {
	RedirectURI string
	Nonce       string
	// Resource is the RFC 8707 "resource" parameter, when the /authorize
	// call carried one. It threads through to handleToken so the scripted
	// MCP OAuth client (issue #5170) does not have to repeat it on the
	// /token call, matching how a real resource-indicator-aware client can
	// carry the value on either or both requests.
	Resource string
}

// NewServer builds a Server ready to mount with Mux.
func NewServer(cfg ServerConfig) (*Server, error) {
	issuer := strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if issuer == "" {
		return nil, fmt.Errorf("mock-oidc-idp: issuer is required")
	}
	if strings.TrimSpace(cfg.Identity.Subject) == "" {
		return nil, fmt.Errorf("mock-oidc-idp: identity subject is required")
	}
	key, kid, err := loadStaticSigningKey()
	if err != nil {
		return nil, err
	}
	groupClaim := strings.TrimSpace(cfg.GroupClaim)
	if groupClaim == "" {
		groupClaim = "groups"
	}
	tokenTTL := cfg.TokenTTL
	if tokenTTL <= 0 {
		tokenTTL = time.Hour
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	accessTokenTTL := cfg.AccessTokenTTL
	if accessTokenTTL <= 0 {
		accessTokenTTL = 10 * time.Minute
	}
	return &Server{
		issuer:              issuer,
		identity:            cfg.Identity,
		groupClaim:          groupClaim,
		tokenTTL:            tokenTTL,
		now:                 now,
		accessTokenJWT:      cfg.AccessTokenJWT,
		accessTokenAudience: strings.TrimSpace(cfg.AccessTokenAudience),
		accessTokenTTL:      accessTokenTTL,
		privateKey:          key,
		keyID:               kid,
		codes:               make(map[string]authorizeRequest),
	}, nil
}

// Mux returns an http.Handler serving the four endpoints this mock IdP
// implements: OIDC discovery, the authorization endpoint, the token
// endpoint, and the JWKS endpoint.
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.handleDiscovery)
	mux.HandleFunc("GET /authorize", s.handleAuthorize)
	mux.HandleFunc("POST /token", s.handleToken)
	mux.HandleFunc("GET /jwks", s.handleJWKS)
	return mux
}

// handleDiscovery serves the OpenID Connect discovery document. Fields match
// what coreos/go-oidc's Provider.Verifier requires: issuer, the three
// endpoint URLs, and the signing algorithm list.
func (s *Server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.issuer,
		"authorization_endpoint":                s.issuer + "/authorize",
		"token_endpoint":                        s.issuer + "/token",
		"jwks_uri":                              s.issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"claims_supported":                      []string{"sub", "email", s.groupClaim},
		"code_challenge_methods_supported":      []string{},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
	})
}

// handleAuthorize immediately redirects to redirect_uri with a freshly
// issued one-time code and the caller's state, with no login UI: this mock
// IdP has exactly one configured identity, so there is nothing to prompt
// for.
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	redirectURI := strings.TrimSpace(query.Get("redirect_uri"))
	if redirectURI == "" {
		http.Error(w, "redirect_uri is required", http.StatusBadRequest)
		return
	}
	target, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "redirect_uri is invalid", http.StatusBadRequest)
		return
	}

	code := s.issueCode(authorizeRequest{
		RedirectURI: redirectURI,
		Nonce:       strings.TrimSpace(query.Get("nonce")),
		Resource:    strings.TrimSpace(query.Get("resource")),
	})

	callback := url.Values{"code": {code}}
	if state := query.Get("state"); state != "" {
		callback.Set("state", state)
	}
	target.RawQuery = mergeQuery(target.RawQuery, callback)
	// #nosec G710 -- redirecting back to the caller-supplied redirect_uri IS
	// the OAuth2/OIDC authorization endpoint contract (RFC 6749 section
	// 4.1.1); this mock IdP has no client registry to validate it against
	// (see NewServer's doc comment), matching every real IdP's /authorize.
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// mergeQuery adds the query parameters in add to the already-encoded query
// string existing, without discarding any parameters redirect_uri already
// carried.
func mergeQuery(existing string, add url.Values) string {
	values, err := url.ParseQuery(existing)
	if err != nil {
		// The existing query comes from an already-parsed redirect_uri, so a
		// parse failure here is not expected; if it ever happens, return just
		// the added callback params rather than silently dropping the code and
		// state a malformed map would lose.
		return add.Encode()
	}
	for key, vals := range add {
		for _, v := range vals {
			values.Add(key, v)
		}
	}
	return values.Encode()
}

// issueCode records req under a fresh random code and returns the code. The
// code is one-time use: handleToken deletes it on the first (and only valid)
// redemption.
func (s *Server) issueCode(req authorizeRequest) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	code := hex.EncodeToString(buf)
	s.mu.Lock()
	s.codes[code] = req
	s.mu.Unlock()
	return code
}

// consumeCode looks up and deletes code, so a code can be exchanged for a
// token at most once.
func (s *Server) consumeCode(code string) (authorizeRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	return req, ok
}

// handleToken exchanges a valid authorization_code for a signed ID token.
// Client authentication accepts either HTTP Basic (per RFC 6749 section
// 2.3.1, and golang.org/x/oauth2's default AuthStyle) or client_id/
// client_secret as POST body fields; the client_secret value itself is
// never checked; this is a mock IdP with a single fixed identity, not a
// client registry.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}
	if got := r.PostFormValue("grant_type"); got != "authorization_code" {
		http.Error(w, fmt.Sprintf("unsupported grant_type %q", got), http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.PostFormValue("code"))
	req, ok := s.consumeCode(code)
	if !ok {
		http.Error(w, "unknown or already-used code", http.StatusBadRequest)
		return
	}
	if redirectURI := strings.TrimSpace(r.PostFormValue("redirect_uri")); redirectURI != "" && redirectURI != req.RedirectURI {
		http.Error(w, "redirect_uri mismatch", http.StatusBadRequest)
		return
	}

	clientID, _, hasBasicAuth := r.BasicAuth()
	if !hasBasicAuth {
		clientID = strings.TrimSpace(r.PostFormValue("client_id"))
	}

	idToken, err := s.signIDToken(clientID, req.Nonce)
	if err != nil {
		http.Error(w, "failed to sign id token", http.StatusInternalServerError)
		return
	}

	accessToken, expiresIn, err := s.mintAccessToken(req, strings.TrimSpace(r.PostFormValue("resource")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
		"id_token":     idToken,
	})
}

// mintAccessToken returns the access_token value and its expires_in seconds
// for one /token response. By default (no RFC 8707 "resource" anywhere in
// the flow and AccessTokenJWT unset) it returns the fixed opaque string this
// IdP has always returned, keeping the #4971 browser-auth suite byte-stable.
// A resource-triggered or config-forced mint returns a signed JWT instead,
// which go/internal/oidcbearer's Resolver can validate (issue #5170).
func (s *Server) mintAccessToken(req authorizeRequest, tokenResource string) (string, int, error) {
	resource := tokenResource
	if resource == "" {
		resource = req.Resource
	}
	mintJWT := resource != "" || s.accessTokenJWT
	if !mintJWT {
		return "mock-access-token", int(s.tokenTTL.Seconds()), nil
	}
	audience := resource
	if audience == "" {
		audience = s.accessTokenAudience
	}
	if audience == "" {
		return "", 0, fmt.Errorf("mock-oidc-idp: minting a JWT access token requires a resource parameter or MOCK_OIDC_ACCESS_TOKEN_AUDIENCE")
	}
	token, err := s.signAccessToken(audience)
	if err != nil {
		return "", 0, fmt.Errorf("mock-oidc-idp: sign access token: %w", err)
	}
	return token, int(s.accessTokenTTL.Seconds()), nil
}

// signAccessToken builds and signs a JWT access token for the server's
// configured identity, targeting audience (an RFC 8707 resource indicator).
// Claim shape mirrors signIDToken but with no nonce (access tokens are never
// replay-bound to one authorization request the way an ID token's nonce is)
// and the configurable accessTokenTTL rather than the ID token's tokenTTL.
func (s *Server) signAccessToken(audience string) (string, error) {
	now := s.now()
	claims := jwt.MapClaims{
		"iss":        s.issuer,
		"sub":        s.identity.Subject,
		"aud":        audience,
		"exp":        now.Add(s.accessTokenTTL).Unix(),
		"iat":        now.Unix(),
		"email":      s.identity.Email,
		s.groupClaim: append([]string(nil), s.identity.Groups...),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.keyID
	return token.SignedString(s.privateKey)
}

// signIDToken builds and signs the ID token for the server's configured
// identity, targeting audience (the requesting client_id) and echoing nonce
// when the authorization request carried one.
func (s *Server) signIDToken(audience, nonce string) (string, error) {
	now := s.now()
	claims := jwt.MapClaims{
		"iss":        s.issuer,
		"sub":        s.identity.Subject,
		"aud":        audience,
		"exp":        now.Add(s.tokenTTL).Unix(),
		"iat":        now.Unix(),
		"email":      s.identity.Email,
		s.groupClaim: append([]string(nil), s.identity.Groups...),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.keyID
	return token.SignedString(s.privateKey)
}

// handleJWKS serves the JSON Web Key Set an OIDC client's discovery flow
// fetches to verify tokens this IdP signs.
func (s *Server) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, buildJWKS(&s.privateKey.PublicKey, s.keyID))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
