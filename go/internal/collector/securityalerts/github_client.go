package securityalerts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	defaultGitHubAPIBaseURL        = "https://api.github.com"
	defaultGitHubDependabotVersion = "2022-11-28"
	githubDependabotOpenAlertState = "open"
	defaultRepositoryAlertLimit    = 100
	defaultRepositoryAlertMaxPages = 1
	maxGitHubErrorBodyBytes        = 4096
)

const (
	// GitHubDependabotFailureAuthDenied classifies missing or forbidden
	// credentials as terminal until operators rotate or grant credentials.
	GitHubDependabotFailureAuthDenied = string(sdk.FailureAuthDenied)
	// GitHubDependabotFailureNotFound classifies missing repositories or
	// disabled Dependabot surfaces as terminal for the current target.
	GitHubDependabotFailureNotFound = string(sdk.FailureNotFound)
	// GitHubDependabotFailureRateLimited classifies GitHub primary or
	// secondary rate-limit responses as retryable.
	GitHubDependabotFailureRateLimited = string(sdk.FailureRateLimited)
	// GitHubDependabotFailureRetryable classifies transient transport or 5xx
	// provider failures as retryable.
	GitHubDependabotFailureRetryable = string(sdk.FailureRetryable)
	// GitHubDependabotFailureTerminal classifies malformed requests and other
	// bounded non-retryable provider failures.
	GitHubDependabotFailureTerminal = string(sdk.FailureTerminal)
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

// GitHubRateLimitInfo captures bounded GitHub rate-limit retry metadata.
type GitHubRateLimitInfo struct {
	Remaining      int
	RemainingKnown bool
	Reset          time.Time
	RetryAfter     time.Duration
}

// GitHubDependabotAlertResult is one bounded repository alert fetch result.
type GitHubDependabotAlertResult struct {
	Alerts       []GitHubDependabotAlert
	PagesFetched int
	Truncated    bool
	ObservedAt   time.Time
	RateLimit    GitHubRateLimitInfo
}

// GitHubDependabotError is a bounded provider error. It deliberately omits
// repositories, tokens, URLs, and raw response bodies from Error().
type GitHubDependabotError struct {
	StatusCode int
	Message    string
	RateLimit  GitHubRateLimitInfo
	Cause      error
}

// Error returns a bounded provider failure message safe for logs and status.
func (e GitHubDependabotError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "github dependabot request failed"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: status %d", message, e.StatusCode)
	}
	return message
}

// Unwrap returns the underlying provider or transport cause when present.
func (e GitHubDependabotError) Unwrap() error {
	return e.Cause
}

// FailureClass returns the bounded workflow retry class for this provider
// failure.
func (e GitHubDependabotError) FailureClass() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden:
		if e.RateLimit.RetryAfter != 0 ||
			(e.RateLimit.RemainingKnown && e.RateLimit.Remaining == 0) {
			return GitHubDependabotFailureRateLimited
		}
		return GitHubDependabotFailureAuthDenied
	case e.StatusCode == http.StatusTooManyRequests:
		return GitHubDependabotFailureRateLimited
	case e.StatusCode == http.StatusNotFound:
		return GitHubDependabotFailureNotFound
	case e.StatusCode >= http.StatusInternalServerError:
		return GitHubDependabotFailureRetryable
	case e.StatusCode != 0:
		return GitHubDependabotFailureTerminal
	default:
		return GitHubDependabotFailureRetryable
	}
}

// TerminalFailure reports whether the failure should stop retrying the current
// work item until configuration changes.
func (e GitHubDependabotError) TerminalFailure() bool {
	switch e.FailureClass() {
	case GitHubDependabotFailureAuthDenied, GitHubDependabotFailureNotFound, GitHubDependabotFailureTerminal:
		return true
	default:
		return false
	}
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
		httpClient = sdk.DefaultHTTPClient(30 * time.Second)
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
	result, err := c.ListRepositoryAlertsPages(ctx, repository, defaultRepositoryAlertMaxPages)
	if err != nil {
		return nil, err
	}
	return result.Alerts, nil
}

// ListRepositoryAlertsPages returns bounded Dependabot alert pages for an
// explicitly allowlisted repository.
func (c GitHubDependabotClient) ListRepositoryAlertsPages(
	ctx context.Context,
	repository string,
	maxPages int,
) (GitHubDependabotAlertResult, error) {
	if c.token == "" {
		return GitHubDependabotAlertResult{}, GitHubDependabotError{
			Message: "github dependabot token is required",
		}
	}
	repository = normalizeRepositoryFullName(repository)
	if repository == "" {
		return GitHubDependabotAlertResult{}, GitHubDependabotError{
			Message: "github dependabot repository scope must not be blank",
		}
	}
	if _, ok := c.allowedRepositories[repository]; !ok {
		return GitHubDependabotAlertResult{}, GitHubDependabotError{
			Message: "github dependabot repository is not allowlisted",
		}
	}
	firstURL, err := c.repositoryAlertsURL(repository)
	if err != nil {
		return GitHubDependabotAlertResult{}, err
	}
	return c.paginateAlerts(ctx, firstURL, maxPages)
}

// ListOrganizationAlertsPages returns bounded Dependabot alert pages for an
// organization via GET /orgs/{org}/dependabot/alerts. Each returned alert
// carries its source repository so callers can fan out per-repository facts.
// Pagination, rate-limit metadata, cross-host link rejection, and the
// state=open filter behave identically to the per-repository path.
func (c GitHubDependabotClient) ListOrganizationAlertsPages(
	ctx context.Context,
	organization string,
	maxPages int,
) (GitHubDependabotAlertResult, error) {
	if c.token == "" {
		return GitHubDependabotAlertResult{}, GitHubDependabotError{
			Message: "github dependabot token is required",
		}
	}
	organization = normalizeOrganizationLogin(organization)
	if organization == "" {
		return GitHubDependabotAlertResult{}, GitHubDependabotError{
			Message: "github dependabot organization scope must not be blank",
		}
	}
	firstURL, err := c.organizationAlertsURL(organization)
	if err != nil {
		return GitHubDependabotAlertResult{}, err
	}
	return c.paginateAlerts(ctx, firstURL, maxPages)
}

// paginateAlerts walks bounded cursor pages for any Dependabot alert listing
// endpoint, sharing pagination, truncation, and per-call alert clamping so the
// repository and organization paths stay in lockstep.
func (c GitHubDependabotClient) paginateAlerts(
	ctx context.Context,
	firstURL string,
	maxPages int,
) (GitHubDependabotAlertResult, error) {
	if maxPages <= 0 {
		maxPages = defaultRepositoryAlertMaxPages
	}
	var out GitHubDependabotAlertResult
	nextURL := firstURL
	for page := 1; page <= maxPages; page++ {
		alerts, rateLimit, next, err := c.listAlertsPage(ctx, nextURL)
		out.RateLimit = rateLimit
		if err != nil {
			return GitHubDependabotAlertResult{}, err
		}
		out.Alerts = append(out.Alerts, alerts...)
		out.PagesFetched++
		nextURL = next
		if nextURL == "" {
			break
		}
		if page == maxPages {
			out.Truncated = true
		}
	}
	if len(out.Alerts) > c.repositoryLimit*maxPages {
		out.Alerts = out.Alerts[:c.repositoryLimit*maxPages]
		out.Truncated = true
	}
	out.ObservedAt = time.Now().UTC()
	return out, nil
}

func (c GitHubDependabotClient) listAlertsPage(
	ctx context.Context,
	endpoint string,
) ([]GitHubDependabotAlert, GitHubRateLimitInfo, string, error) {
	endpoint = strings.TrimSpace(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, GitHubRateLimitInfo{}, "", fmt.Errorf("build github dependabot request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", defaultGitHubDependabotVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, GitHubRateLimitInfo{}, "", GitHubDependabotError{
			Message: "fetch github dependabot alerts",
			Cause: sdk.HTTPError{
				Provider: "github_dependabot",
				Message:  "request failed",
				Cause:    err,
			},
		}
	}
	defer func() { _ = resp.Body.Close() }()
	rateLimit := parseGitHubRateLimit(resp.Header)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxGitHubErrorBodyBytes))
		httpErr := sdk.HTTPError{
			Provider:   "github_dependabot",
			StatusCode: resp.StatusCode,
			Message:    http.StatusText(resp.StatusCode),
			RetryAfter: rateLimit.RetryAfter,
		}
		return nil, rateLimit, "", GitHubDependabotError{
			StatusCode: resp.StatusCode,
			Message:    "fetch github dependabot alerts",
			RateLimit:  rateLimit,
			Cause:      httpErr,
		}
	}
	var alerts []GitHubDependabotAlert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, rateLimit, "", fmt.Errorf("decode github dependabot alerts: %w", err)
	}
	if len(alerts) > c.repositoryLimit {
		alerts = alerts[:c.repositoryLimit]
	}
	return alerts, rateLimit, nextDependabotAlertsURL(endpoint, resp.Header.Get("Link")), nil
}

func (c GitHubDependabotClient) repositoryAlertsURL(repository string) (string, error) {
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" {
		return "", fmt.Errorf("github dependabot repository must be owner/name")
	}
	parsed, err := sdk.ParseBaseURL("github dependabot", c.baseURL)
	if err != nil {
		return "", fmt.Errorf("github dependabot base_url is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/repos/" +
		url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/dependabot/alerts"
	query := parsed.Query()
	query.Set("per_page", fmt.Sprint(c.repositoryLimit))
	query.Set("state", githubDependabotOpenAlertState)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c GitHubDependabotClient) organizationAlertsURL(organization string) (string, error) {
	parsed, err := sdk.ParseBaseURL("github dependabot", c.baseURL)
	if err != nil {
		return "", fmt.Errorf("github dependabot base_url is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/orgs/" +
		url.PathEscape(organization) + "/dependabot/alerts"
	query := parsed.Query()
	query.Set("per_page", fmt.Sprint(c.repositoryLimit))
	query.Set("state", githubDependabotOpenAlertState)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func normalizeOrganizationLogin(organization string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(organization), "/"))
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

func parseGitHubRateLimit(header http.Header) GitHubRateLimitInfo {
	info := GitHubRateLimitInfo{}
	if remaining, err := strconv.Atoi(strings.TrimSpace(header.Get("X-RateLimit-Remaining"))); err == nil {
		info.Remaining = remaining
		info.RemainingKnown = true
	}
	if reset, err := strconv.ParseInt(strings.TrimSpace(header.Get("X-RateLimit-Reset")), 10, 64); err == nil && reset > 0 {
		info.Reset = time.Unix(reset, 0).UTC()
	}
	info.RetryAfter = sdk.ParseRetryAfterHeader(header.Get("Retry-After"))
	return info
}

func nextDependabotAlertsURL(currentEndpoint string, raw string) string {
	current, err := url.Parse(currentEndpoint)
	if err != nil {
		return ""
	}
	for _, link := range strings.Split(raw, ",") {
		if !strings.Contains(link, `rel="next"`) {
			continue
		}
		start := strings.Index(link, "<")
		end := strings.Index(link, ">")
		if start < 0 || end <= start {
			continue
		}
		next, err := url.Parse(strings.TrimSpace(link[start+1 : end]))
		if err != nil || next.Scheme != current.Scheme || next.Host != current.Host {
			continue
		}
		query := next.Query()
		query.Set("state", githubDependabotOpenAlertState)
		next.RawQuery = query.Encode()
		return next.String()
	}
	return ""
}
