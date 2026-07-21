// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// mockAccessToken is the single, fixed opaque access token this mock hands
// out for every successful code exchange. Opaque is correct here: unlike
// the mock OIDC IdP's ID/access tokens, go/internal/githublogin never treats
// a GitHub access token as a JWT, so there is no claim shape to satisfy.
const mockAccessToken = "mock-github-token" // #nosec G101 -- synthetic dev-only test token, not a real credential

// TeamHandle is one GitHub team the synthetic identity belongs to, matching
// the "org/slug" shape go/internal/githublogin/connector.go's
// fetchTeamHandles produces.
type TeamHandle struct {
	Org  string
	Slug string
}

// IdentityConfig is the synthetic example.test GitHub identity this mock
// hands back for every login. It is intentionally single-identity, mirroring
// mock-oidc-idp: the E2E suites this binary supports (issue #5170) drive one
// scripted GitHub login per container.
type IdentityConfig struct {
	Login  string
	UserID int64
	Email  string
	// Org is the single GitHub org the identity has an ACTIVE membership in.
	Org string
	// Teams lists the identity's team memberships, each scoped to Org (or
	// another org — go/internal/githublogin restricts the returned set to
	// the caller's allowedOrgs regardless of what this mock reports).
	Teams []TeamHandle
}

// ServerConfig configures one mock GitHub instance.
type ServerConfig struct {
	Identity IdentityConfig
}

// Server is a minimal, in-memory stand-in for github.com's OAuth2
// web-application flow and REST identity endpoints. See doc.go for the
// endpoint set and ownership boundary.
type Server struct {
	identity IdentityConfig

	mu    sync.Mutex
	codes map[string]authorizeRequest
}

// authorizeRequest is the state captured from one /login/oauth/authorize
// call and consumed exactly once by the matching /login/oauth/access_token
// call.
type authorizeRequest struct {
	RedirectURI string
}

// NewServer builds a Server ready to mount with Mux.
func NewServer(cfg ServerConfig) (*Server, error) {
	if strings.TrimSpace(cfg.Identity.Login) == "" {
		return nil, fmt.Errorf("mock-github: identity login is required")
	}
	if cfg.Identity.UserID == 0 {
		return nil, fmt.Errorf("mock-github: identity user id is required")
	}
	return &Server{
		identity: cfg.Identity,
		codes:    make(map[string]authorizeRequest),
	}, nil
}

// Mux returns an http.Handler serving every endpoint this mock implements:
// the OAuth2 web flow, the REST identity/org/team endpoints, and the
// unauthenticated API-root reachability probe.
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login/oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("POST /login/oauth/access_token", s.handleAccessToken)
	mux.HandleFunc("GET /user", s.requireBearer(s.handleUser))
	mux.HandleFunc("GET /user/emails", s.requireBearer(s.handleEmails))
	mux.HandleFunc("GET /user/memberships/orgs", s.requireBearer(s.handleOrgMemberships))
	mux.HandleFunc("GET /user/teams", s.requireBearer(s.handleTeams))
	mux.HandleFunc("GET /{$}", s.handleRoot)
	return mux
}

// handleAuthorize immediately redirects to redirect_uri with a freshly
// issued one-time code and the caller's state, with no login UI: this mock
// has exactly one configured identity, so there is nothing to prompt for.
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

	code := s.issueCode(authorizeRequest{RedirectURI: redirectURI})

	callback := url.Values{"code": {code}}
	if state := query.Get("state"); state != "" {
		callback.Set("state", state)
	}
	target.RawQuery = mergeQuery(target.RawQuery, callback)
	// #nosec G710 -- redirecting back to the caller-supplied redirect_uri IS
	// the OAuth2 web-application-flow contract this mock stands in for; it
	// has no client registry to validate it against (mirrors
	// mock-oidc-idp's handleAuthorize).
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// mergeQuery adds the query parameters in add to the already-encoded query
// string existing, without discarding any parameters redirect_uri already
// carried.
func mergeQuery(existing string, add url.Values) string {
	values, err := url.ParseQuery(existing)
	if err != nil {
		return add.Encode()
	}
	for key, vals := range add {
		for _, v := range vals {
			values.Add(key, v)
		}
	}
	return values.Encode()
}

func (s *Server) issueCode(req authorizeRequest) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	code := hex.EncodeToString(buf)
	s.mu.Lock()
	s.codes[code] = req
	s.mu.Unlock()
	return code
}

func (s *Server) consumeCode(code string) (authorizeRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	return req, ok
}

// handleAccessToken exchanges a valid authorization code for the fixed
// mockAccessToken. Matching real GitHub behavior (see
// go/internal/githublogin/connector.go's githubTokenResponse doc comment),
// this ALWAYS responds HTTP 200: an unknown or already-used code carries a
// non-empty "error" field instead of a 4xx status, because
// githubConnector.Exchange treats the error field, not the status code, as
// the failure signal.
func (s *Server) handleAccessToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"error":             "invalid_request",
			"error_description": "invalid form body",
		})
		return
	}
	code := strings.TrimSpace(r.PostFormValue("code"))
	if _, ok := s.consumeCode(code); !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"error":             "bad_verification_code",
			"error_description": "unknown or already-used code",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": mockAccessToken,
		"token_type":   "bearer",
		"scope":        "",
	})
}

// requireBearer wraps a REST identity handler with the "Authorization:
// Bearer <mockAccessToken>" check every real GitHub API call needs, so the
// F-9 leakage suite has a real 401 to assert against for a credential-less
// or garbage-token call.
func (s *Server) requireBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+mockAccessToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Bad credentials"})
			return
		}
		next(w, r)
	}
}

// handleUser serves GET /user: {id, login}, matching githubUser in
// connector.go.
func (s *Server) handleUser(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    s.identity.UserID,
		"login": s.identity.Login,
	})
}

// handleEmails serves GET /user/emails: a single verified primary email,
// matching githubEmail in connector.go.
func (s *Server) handleEmails(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{{
		"email":    s.identity.Email,
		"verified": true,
		"primary":  true,
	}})
}

// handleOrgMemberships serves GET /user/memberships/orgs: one active
// membership in the configured org, matching githubOrgMembership in
// connector.go. Pagination is honored by returning the fixture's single
// membership only on page 1 (or when page is unset), and an empty page
// thereafter — connector.go's fetchActiveOrgs stops paging once a
// shorter-than-per_page page comes back, exactly like this fixture's shape.
func (s *Server) handleOrgMemberships(w http.ResponseWriter, r *http.Request) {
	if requestPage(r) > 1 {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, []map[string]any{{
		"state":        "active",
		"organization": map[string]any{"login": s.identity.Org},
	}})
}

// handleTeams serves GET /user/teams: the configured team handles, matching
// githubTeam in connector.go. Pagination behaves like handleOrgMemberships.
func (s *Server) handleTeams(w http.ResponseWriter, r *http.Request) {
	if requestPage(r) > 1 {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	teams := make([]map[string]any, 0, len(s.identity.Teams))
	for _, team := range s.identity.Teams {
		teams = append(teams, map[string]any{
			"slug":         team.Slug,
			"organization": map[string]any{"login": team.Org},
		})
	}
	writeJSON(w, http.StatusOK, teams)
}

// requestPage parses the "page" query parameter, defaulting to 1 (GitHub's
// own default) on absence or a malformed value.
func requestPage(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("page"))
	if raw == "" {
		return 1
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// handleRoot serves the unauthenticated GET / reachability probe
// go/internal/githublogin/provider_connection_test_probe.go's probeAPIRoot
// calls for the admin console's Test-connection button. Real
// api.github.com's root also requires no auth and returns 200.
func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"current_user_url": "/user"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
