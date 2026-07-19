// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPIVersion  = "2022-11-28"
	githubHTTPTimeout = 15 * time.Second
	// githubTeamPageSize matches GitHub's documented maximum per_page for
	// /user/teams (see connector.go's fetchTeams doc comment).
	githubTeamPageSize = 100
	// githubMaxTeamPages bounds pagination so a user with an unusually large
	// number of team memberships cannot make a single login hang or issue
	// unbounded API calls.
	githubMaxTeamPages = 20
	// githubOrgPageSize matches GitHub's documented maximum per_page for
	// /user/memberships/orgs; githubMaxOrgPages bounds pagination the same way
	// githubMaxTeamPages bounds /user/teams, so a user in a pathological number
	// of orgs cannot hang a login.
	githubOrgPageSize = 100
	githubMaxOrgPages = 20
)

// githubConnector implements Connector against the real GitHub OAuth2 and
// REST API (github.com or a GitHub Enterprise Server instance). See doc.go
// for why this package exists instead of extending oidclogin: github.com
// (and GHES) expose no OIDC discovery document, no ID token, and no JWKS —
// only a plain OAuth2 authorization-code exchange plus REST identity calls.
//
// Endpoints (verified against GitHub's REST/OAuth documentation, not
// guessed, per this repo's research-before-deciding rule):
//   - authorize: GET  {BaseURL}/login/oauth/authorize
//   - token:     POST {BaseURL}/login/oauth/access_token (Accept: application/json)
//   - user:      GET  {APIBaseURL}/user
//   - emails:    GET  {APIBaseURL}/user/emails      (scope: user:email)
//   - org membership: GET {APIBaseURL}/user/memberships/orgs (scope: read:org)
//   - teams:     GET  {APIBaseURL}/user/teams       (scope: read:org), paginated
type githubConnector struct {
	httpClient   *http.Client
	baseURL      string
	apiBaseURL   string
	clientID     string
	clientSecret string
	redirectURL  string
	scopes       []string
}

// NewGitHubConnector constructs a connector backed by the real GitHub OAuth2
// and REST endpoints.
func NewGitHubConnector(_ context.Context, provider ProviderConfig) (Connector, error) {
	clientSecret := strings.TrimSpace(provider.ClientSecret)
	if clientSecret == "" {
		var err error
		clientSecret, err = readClientSecret(provider.ClientSecretFile)
		if err != nil {
			return nil, err
		}
	}
	if clientSecret == "" {
		return nil, errors.New("github provider client secret is required")
	}
	return &githubConnector{
		httpClient:   &http.Client{Timeout: githubHTTPTimeout},
		baseURL:      strings.TrimRight(provider.BaseURL, "/"),
		apiBaseURL:   strings.TrimRight(provider.APIBaseURL, "/"),
		clientID:     provider.ClientID,
		clientSecret: clientSecret,
		redirectURL:  provider.RedirectURL,
		scopes:       append([]string(nil), provider.Scopes...),
	}, nil
}

func (c *githubConnector) AuthCodeURL(state string) string {
	values := url.Values{}
	values.Set("client_id", c.clientID)
	values.Set("redirect_uri", c.redirectURL)
	values.Set("state", state)
	if len(c.scopes) > 0 {
		values.Set("scope", strings.Join(c.scopes, " "))
	}
	return c.baseURL + "/login/oauth/authorize?" + values.Encode()
}

// githubTokenResponse is the JSON shape of GitHub's access-token exchange
// response when Accept: application/json is sent. A successful exchange
// carries access_token; a failed one carries error/error_description
// instead (GitHub returns HTTP 200 for both — the error fields, not the
// status code, signal failure).
type githubTokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *githubConnector) Exchange(ctx context.Context, code string) (TokenSet, error) {
	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", c.redirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, fmt.Errorf("build github token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TokenSet{}, fmt.Errorf("exchange github authorization code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenSet{}, fmt.Errorf("read github token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return TokenSet{}, fmt.Errorf("github token exchange returned status %d", resp.StatusCode)
	}
	var parsed githubTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return TokenSet{}, fmt.Errorf("decode github token response: %w", err)
	}
	if parsed.Error != "" {
		return TokenSet{}, fmt.Errorf("github token exchange error: %s", parsed.Error)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return TokenSet{}, errors.New("github token response missing access_token")
	}
	return TokenSet{AccessToken: parsed.AccessToken}, nil
}

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
	Primary  bool   `json:"primary"`
}

type githubOrgMembership struct {
	State        string `json:"state"`
	Organization struct {
		Login string `json:"login"`
	} `json:"organization"`
}

type githubTeam struct {
	Slug         string `json:"slug"`
	Organization struct {
		Login string `json:"login"`
	} `json:"organization"`
}

// FetchIdentity resolves the verified GitHub identity for accessToken:
// the numeric user id and login (GET /user), the verified primary email
// (GET /user/emails), the orgs the user has an ACTIVE membership in (GET
// /user/memberships/orgs) restricted to allowedOrgs, and the user's team
// memberships (GET /user/teams, paginated) restricted to teams within an
// allowed org. Restricting the org-membership and team calls' resulting set
// to allowedOrgs (rather than fetching and returning every org/team the
// token can see) keeps membership detail for orgs outside this connector's
// tenant boundary out of the returned Identity entirely.
func (c *githubConnector) FetchIdentity(ctx context.Context, accessToken string, allowedOrgs []string) (Identity, error) {
	user, err := c.fetchUser(ctx, accessToken)
	if err != nil {
		return Identity{}, err
	}
	email, err := c.fetchVerifiedPrimaryEmail(ctx, accessToken)
	if err != nil {
		return Identity{}, err
	}
	allowed := make(map[string]struct{}, len(allowedOrgs))
	for _, org := range allowedOrgs {
		allowed[strings.ToLower(strings.TrimSpace(org))] = struct{}{}
	}
	activeOrgs, err := c.fetchActiveOrgs(ctx, accessToken, allowed)
	if err != nil {
		return Identity{}, err
	}
	teamHandles, err := c.fetchTeamHandles(ctx, accessToken, allowed)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		Subject:     strconv.FormatInt(user.ID, 10),
		Login:       user.Login,
		Email:       email,
		ActiveOrgs:  activeOrgs,
		TeamHandles: teamHandles,
	}, nil
}

func (c *githubConnector) fetchUser(ctx context.Context, accessToken string) (githubUser, error) {
	var user githubUser
	if err := c.getJSON(ctx, accessToken, "/user", &user); err != nil {
		return githubUser{}, fmt.Errorf("fetch github user: %w", err)
	}
	if user.ID == 0 {
		return githubUser{}, errors.New("github user response missing id")
	}
	return user, nil
}

func (c *githubConnector) fetchVerifiedPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	var emails []githubEmail
	if err := c.getJSON(ctx, accessToken, "/user/emails", &emails); err != nil {
		return "", fmt.Errorf("fetch github user emails: %w", err)
	}
	for _, email := range emails {
		if email.Primary && email.Verified {
			return strings.ToLower(strings.TrimSpace(email.Email)), nil
		}
	}
	return "", nil
}

// fetchActiveOrgs pages through GET /user/memberships/orgs (max 100 per page,
// per GitHub's documented maximum) and returns the user's active memberships
// restricted to allowed orgs. Pagination matters for the allow-list decision:
// a user in more than one page of orgs whose allowed org lands on a later page
// would otherwise be wrongly denied (CompleteGitHubLogin rejects before
// team-role resolution when no allowed org is present). Bounded to
// githubMaxOrgPages, and stops early once every configured allowed org has
// been confirmed (no later page can change the decision then).
func (c *githubConnector) fetchActiveOrgs(ctx context.Context, accessToken string, allowed map[string]struct{}) ([]string, error) {
	found := make(map[string]struct{}, len(allowed))
	orgs := make([]string, 0, len(allowed))
	for page := 1; page <= githubMaxOrgPages; page++ {
		var memberships []githubOrgMembership
		path := fmt.Sprintf("/user/memberships/orgs?state=active&per_page=%d&page=%d", githubOrgPageSize, page)
		if err := c.getJSON(ctx, accessToken, path, &memberships); err != nil {
			return nil, fmt.Errorf("fetch github org memberships: %w", err)
		}
		for _, membership := range memberships {
			if !strings.EqualFold(membership.State, "active") {
				continue
			}
			login := strings.ToLower(strings.TrimSpace(membership.Organization.Login))
			if login == "" {
				continue
			}
			if _, ok := allowed[login]; !ok {
				continue
			}
			if _, seen := found[login]; seen {
				continue
			}
			found[login] = struct{}{}
			orgs = append(orgs, login)
		}
		// Every configured allowed org is confirmed; no later page can change
		// the allow-list decision, so stop paging.
		if len(found) == len(allowed) {
			break
		}
		// Last page reached (fewer than a full page returned).
		if len(memberships) < githubOrgPageSize {
			break
		}
	}
	return cleanLowerStrings(orgs), nil
}

// fetchTeamHandles pages through GET /user/teams (max 100 per page, per
// GitHub's documented maximum) and returns "org/team-slug" for every team
// whose org is in allowed. Bounded to githubMaxTeamPages pages so a
// pathological number of memberships cannot hang a login.
func (c *githubConnector) fetchTeamHandles(ctx context.Context, accessToken string, allowed map[string]struct{}) ([]string, error) {
	handles := make([]string, 0)
	for page := 1; page <= githubMaxTeamPages; page++ {
		var teams []githubTeam
		path := fmt.Sprintf("/user/teams?per_page=%d&page=%d", githubTeamPageSize, page)
		if err := c.getJSON(ctx, accessToken, path, &teams); err != nil {
			return nil, fmt.Errorf("fetch github user teams: %w", err)
		}
		for _, team := range teams {
			org := strings.ToLower(strings.TrimSpace(team.Organization.Login))
			slug := strings.ToLower(strings.TrimSpace(team.Slug))
			if org == "" || slug == "" {
				continue
			}
			if _, ok := allowed[org]; !ok {
				continue
			}
			handles = append(handles, org+"/"+slug)
		}
		if len(teams) < githubTeamPageSize {
			break
		}
	}
	return cleanLowerStrings(handles), nil
}

func (c *githubConnector) getJSON(ctx context.Context, accessToken string, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build github api request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call github api %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read github api %s response: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api %s returned status %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode github api %s response: %w", path, err)
	}
	return nil
}

func readClientSecret(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- reads operator-managed GitHub client secret file at a path from deployment config, not an HTTP/MCP request param
	if err != nil {
		return "", fmt.Errorf("read github client secret file: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}
