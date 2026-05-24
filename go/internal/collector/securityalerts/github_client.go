package securityalerts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultGitHubAPIBaseURL        = "https://api.github.com"
	defaultGitHubDependabotVersion = "2022-11-28"
	defaultRepositoryAlertLimit    = 100
)

// GitHubDependabotClientConfig configures the bounded GitHub Dependabot alert
// source. Token and repository allowlists are required before any HTTP request.
type GitHubDependabotClientConfig struct {
	BaseURL              string
	Token                string
	AllowedRepositories  []string
	RepositoryAlertLimit int
	HTTPClient           *http.Client
}

// GitHubDependabotClient reads repository-scoped GitHub Dependabot alerts.
type GitHubDependabotClient struct {
	baseURL             string
	token               string
	allowedRepositories map[string]struct{}
	repositoryLimit     int
	httpClient          *http.Client
}

// NewGitHubDependabotClient builds a GitHub Dependabot alert client. It does
// not validate credentials until a request is made so callers can construct
// disabled sources from optional config.
func NewGitHubDependabotClient(config GitHubDependabotClientConfig) GitHubDependabotClient {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultGitHubAPIBaseURL
	}
	limit := config.RepositoryAlertLimit
	if limit <= 0 || limit > defaultRepositoryAlertLimit {
		limit = defaultRepositoryAlertLimit
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	allowed := make(map[string]struct{}, len(config.AllowedRepositories))
	for _, repository := range config.AllowedRepositories {
		if normalized := normalizeRepositoryFullName(repository); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	return GitHubDependabotClient{
		baseURL:             baseURL,
		token:               strings.TrimSpace(config.Token),
		allowedRepositories: allowed,
		repositoryLimit:     limit,
		httpClient:          httpClient,
	}
}

// ListRepositoryAlerts returns one bounded page of Dependabot alerts for an
// explicitly allowlisted repository.
func (c GitHubDependabotClient) ListRepositoryAlerts(
	ctx context.Context,
	repository string,
) ([]GitHubDependabotAlert, error) {
	if c.token == "" {
		return nil, fmt.Errorf("github dependabot token is required")
	}
	repository = normalizeRepositoryFullName(repository)
	if repository == "" {
		return nil, fmt.Errorf("github dependabot repository scope must not be blank")
	}
	if _, ok := c.allowedRepositories[repository]; !ok {
		return nil, fmt.Errorf("github dependabot repository %q is not allowlisted", repository)
	}
	endpoint, err := c.repositoryAlertsURL(repository)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build github dependabot request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", defaultGitHubDependabotVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch github dependabot alerts: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch github dependabot alerts: status %d", resp.StatusCode)
	}
	var alerts []GitHubDependabotAlert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, fmt.Errorf("decode github dependabot alerts: %w", err)
	}
	if len(alerts) > c.repositoryLimit {
		alerts = alerts[:c.repositoryLimit]
	}
	return alerts, nil
}

func (c GitHubDependabotClient) repositoryAlertsURL(repository string) (string, error) {
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" {
		return "", fmt.Errorf("github dependabot repository must be owner/name")
	}
	parsed, err := url.Parse(c.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("github dependabot base_url is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/repos/" +
		url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/dependabot/alerts"
	query := parsed.Query()
	query.Set("per_page", fmt.Sprint(c.repositoryLimit))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func normalizeRepositoryFullName(repository string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	owner := strings.ToLower(strings.TrimSpace(parts[0]))
	repo := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git"))
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}
