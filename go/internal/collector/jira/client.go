// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

const (
	defaultHTTPTimeout      = 30 * time.Second
	defaultIssueLimit       = 50
	defaultChangelogLimit   = 50
	defaultRemoteLinkLimit  = 50
	defaultMetadataLimit    = 100
	maxIssueLimit           = 100
	maxChangelogLimit       = 100
	maxRemoteLinkLimit      = 100
	maxMetadataLimit        = 500
	maxJiraErrorBodyBytes   = 4096
	jiraTimeLayout          = "2006-01-02T15:04:05.000-0700"
	jiraJQLTimeLayout       = "2006-01-02 15:04"
	jiraSearchEndpoint      = "/rest/api/3/search/jql"
	jiraIssueEndpointPrefix = "/rest/api/3/issue"
)

// HTTPClientConfig configures the bounded Jira Cloud REST client.
type HTTPClientConfig struct {
	BaseURL string
	Email   string
	Token   string
	Client  *http.Client
}

// HTTPClient reads Jira work-item evidence through bounded REST API calls.
type HTTPClient struct {
	baseURL    *url.URL
	email      string
	token      string
	httpClient *http.Client
}

type searchRequest struct {
	JQL           string   `json:"jql"`
	MaxResults    int      `json:"maxResults"`
	Fields        []string `json:"fields,omitempty"`
	Expand        string   `json:"expand,omitempty"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type searchResponse struct {
	Issues        []searchIssue `json:"issues"`
	NextPageToken string        `json:"nextPageToken"`
	IsLast        bool          `json:"isLast"`
}

type searchIssue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields issueFields `json:"fields"`
}

type issueFields struct {
	Summary        string    `json:"summary"`
	Created        string    `json:"created"`
	Updated        string    `json:"updated"`
	ResolutionDate string    `json:"resolutiondate"`
	IssueType      Reference `json:"issuetype"`
	Status         Reference `json:"status"`
	Project        Reference `json:"project"`
	Assignee       Reference `json:"assignee"`
	Reporter       Reference `json:"reporter"`
}

type changelogResponse struct {
	StartAt    int              `json:"startAt"`
	MaxResults int              `json:"maxResults"`
	Total      int              `json:"total"`
	IsLast     bool             `json:"isLast"`
	Values     []changelogEntry `json:"values"`
}

type changelogEntry struct {
	ID        string          `json:"id"`
	Author    Reference       `json:"author"`
	CreatedAt string          `json:"created"`
	Items     []changelogItem `json:"items"`
}

type changelogItem struct {
	Field     string `json:"field"`
	From      string `json:"fromString"`
	To        string `json:"toString"`
	FromID    string `json:"from"`
	ToID      string `json:"to"`
	FieldType string `json:"fieldtype"`
	FieldID   string `json:"fieldId"`
}

type remoteLinkResponse struct {
	ID           any             `json:"id"`
	GlobalID     string          `json:"globalId"`
	Application  LinkApplication `json:"application"`
	Relationship string          `json:"relationship"`
	Object       LinkObject      `json:"object"`
}

type jiraErrorResponse struct {
	ErrorMessages []string `json:"errorMessages"`
}

// NewHTTPClient builds a Jira Cloud REST client. It validates only local
// configuration; credentials are not checked until requests are made.
func NewHTTPClient(config HTTPClientConfig) (HTTPClient, error) {
	base, err := sdk.ParseBaseURL("jira", config.BaseURL)
	if err != nil {
		return HTTPClient{}, err
	}
	if strings.TrimSpace(config.Token) == "" {
		return HTTPClient{}, fmt.Errorf("jira token is required")
	}
	client := config.Client
	if client == nil {
		client = sdk.DefaultHTTPClient(defaultHTTPTimeout)
	}
	return HTTPClient{
		baseURL:    base,
		email:      strings.TrimSpace(config.Email),
		token:      strings.TrimSpace(config.Token),
		httpClient: client,
	}, nil
}

// CollectWorkItemEvidence fetches changed Jira issues plus changelogs and
// remote links through bounded Jira REST endpoints.
func (c HTTPClient) CollectWorkItemEvidence(
	ctx context.Context,
	target TargetConfig,
	window CollectionWindow,
) (CollectionResult, error) {
	stats := CollectionStats{}
	issues, err := c.searchIssues(ctx, target, window, &stats)
	if err != nil {
		return CollectionResult{}, err
	}
	stats.IssuesEmitted = len(issues)
	result := CollectionResult{
		Issues:        issues,
		Transitions:   make(map[string][]Transition, len(issues)),
		ExternalLinks: make(map[string][]ExternalLink, len(issues)),
		ObservedAt:    window.Until.UTC(),
		Stats:         stats,
	}
	if result.ObservedAt.IsZero() {
		result.ObservedAt = time.Now().UTC()
	}
	if err := c.collectMetadata(ctx, target, &result); err != nil {
		result.Stats.PartialFailures++
		return CollectionResult{}, PartialCollectionError{
			Stage: "metadata",
			Stats: result.Stats,
			Cause: err,
		}
	}
	for _, issue := range issues {
		transitions, err := c.issueChangelog(
			ctx,
			issue,
			normalizedLimit(target.ChangelogLimit, defaultChangelogLimit, maxChangelogLimit),
			&result.Stats,
		)
		if err != nil {
			result.Stats.PartialFailures++
			return CollectionResult{}, PartialCollectionError{
				Stage: "changelog",
				Stats: result.Stats,
				Cause: err,
			}
		}
		if len(transitions) > 0 {
			result.Transitions[issue.ID] = transitions
			result.Stats.ChangelogEventsEmitted += len(transitions)
		}
		links, err := c.issueRemoteLinks(
			ctx,
			issue,
			normalizedLimit(target.RemoteLinkLimit, defaultRemoteLinkLimit, maxRemoteLinkLimit),
			&result.Stats,
		)
		if err != nil {
			result.Stats.PartialFailures++
			return CollectionResult{}, PartialCollectionError{
				Stage: "remote_links",
				Stats: result.Stats,
				Cause: err,
			}
		}
		if len(links) > 0 {
			result.ExternalLinks[issue.ID] = links
			result.Stats.RemoteLinksEmitted += len(links)
		}
	}
	return result, nil
}

func (c HTTPClient) searchIssues(
	ctx context.Context,
	target TargetConfig,
	window CollectionWindow,
	stats *CollectionStats,
) ([]Issue, error) {
	limit := normalizedLimit(target.IssueLimit, defaultIssueLimit, maxIssueLimit)
	issues := make([]Issue, 0, limit)
	nextPageToken := ""
	for len(issues) < limit {
		body := searchRequest{
			JQL:           boundedJQL(target.JQL, window),
			MaxResults:    min(limit-len(issues), maxIssueLimit),
			Fields:        jiraSearchFields(),
			NextPageToken: nextPageToken,
		}
		var response searchResponse
		if err := c.doJSON(ctx, http.MethodPost, jiraSearchEndpoint, nil, body, &response); err != nil {
			return nil, err
		}
		stats.SearchPages++
		for _, raw := range response.Issues {
			if len(issues) >= limit {
				break
			}
			issues = append(issues, Issue{
				ID:         strings.TrimSpace(raw.ID),
				Key:        strings.TrimSpace(raw.Key),
				Summary:    strings.TrimSpace(raw.Fields.Summary),
				IssueType:  raw.Fields.IssueType,
				Status:     raw.Fields.Status,
				Project:    raw.Fields.Project,
				Assignee:   raw.Fields.Assignee,
				Reporter:   raw.Fields.Reporter,
				CreatedAt:  parseJiraTime(raw.Fields.Created),
				UpdatedAt:  parseJiraTime(raw.Fields.Updated),
				ResolvedAt: parseJiraTime(raw.Fields.ResolutionDate),
				Self:       sanitizeURL(raw.Self),
				BrowseURL:  c.browseURL(raw.Key),
			})
		}
		nextPageToken = strings.TrimSpace(response.NextPageToken)
		if response.IsLast || nextPageToken == "" || len(response.Issues) == 0 {
			break
		}
	}
	return issues, nil
}

func (c HTTPClient) issueChangelog(ctx context.Context, issue Issue, limit int, stats *CollectionStats) ([]Transition, error) {
	endpoint := path.Join(jiraIssueEndpointPrefix, issue.Key, "changelog")
	transitions := make([]Transition, 0, limit)
	startAt := 0
	for len(transitions) < limit {
		query := url.Values{}
		query.Set("maxResults", fmt.Sprintf("%d", min(limit-len(transitions), maxChangelogLimit)))
		query.Set("startAt", fmt.Sprintf("%d", startAt))
		var response changelogResponse
		if err := c.doJSON(ctx, http.MethodGet, endpoint, query, nil, &response); err != nil {
			return nil, err
		}
		stats.ChangelogPages++
		for _, entry := range response.Values {
			for _, item := range entry.Items {
				if len(transitions) >= limit {
					break
				}
				from, to, redacted := changelogValues(item)
				transitions = append(transitions, Transition{
					ID:             strings.TrimSpace(entry.ID),
					IssueID:        strings.TrimSpace(issue.ID),
					IssueKey:       strings.TrimSpace(issue.Key),
					Field:          strings.TrimSpace(item.Field),
					From:           from,
					To:             to,
					Author:         entry.Author,
					CreatedAt:      parseJiraTime(entry.CreatedAt),
					ValueRedacted:  redacted,
					AuthorRedacted: referencePresent(entry.Author),
				})
			}
		}
		if !changelogPaginationPresent(response) {
			break
		}
		if response.IsLast || len(response.Values) == 0 {
			break
		}
		pageSize := response.MaxResults
		if pageSize <= 0 {
			pageSize = len(response.Values)
		}
		startAt = response.StartAt + pageSize
		if response.Total > 0 && startAt >= response.Total {
			break
		}
	}
	return transitions, nil
}

func (c HTTPClient) issueRemoteLinks(ctx context.Context, issue Issue, limit int, stats *CollectionStats) ([]ExternalLink, error) {
	endpoint := path.Join(jiraIssueEndpointPrefix, issue.Key, "remotelink")
	var response []remoteLinkResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &response); err != nil {
		return nil, err
	}
	stats.RemoteLinkPages++
	if len(response) > limit {
		response = response[:limit]
	}
	links := make([]ExternalLink, 0, len(response))
	seen := make(map[string]struct{}, len(response))
	for _, raw := range response {
		id := anyString(raw.ID)
		sanitizedURL := sanitizeURL(raw.Object.URL)
		fingerprint := urlFingerprint(sanitizedURL)
		key := firstNonBlank(id, raw.GlobalID, fingerprint)
		if key == "" {
			stats.RemoteLinksRejected++
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		raw.Object.URL = sanitizedURL
		supportState := remoteLinkProviderSupportState(raw.Application, raw.Object.URL)
		if supportState == "unsupported_provider" {
			stats.UnsupportedProviderLinks++
		}
		links = append(links, ExternalLink{
			ID:                   id,
			IssueID:              strings.TrimSpace(issue.ID),
			IssueKey:             strings.TrimSpace(issue.Key),
			GlobalID:             strings.TrimSpace(raw.GlobalID),
			Application:          raw.Application,
			Relationship:         strings.TrimSpace(raw.Relationship),
			Object:               raw.Object,
			URLFingerprint:       fingerprint,
			URLRedacted:          strings.TrimSpace(raw.Object.URL) != "",
			ProviderSupportState: supportState,
		})
	}
	return links, nil
}

func (c HTTPClient) doJSON(
	ctx context.Context,
	method string,
	endpoint string,
	query url.Values,
	body any,
	out any,
) error {
	return sdk.DoJSON(ctx, sdk.JSONRequest{
		Provider:    "jira",
		Method:      method,
		BaseURL:     c.baseURL,
		Endpoint:    endpoint,
		Query:       query,
		Body:        body,
		Out:         out,
		Client:      c.httpClient,
		Headers:     c.setAuth,
		StatusError: jiraStatusError,
	})
}

func (c HTTPClient) setAuth(request *http.Request) {
	if c.email != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(c.email + ":" + c.token))
		request.Header.Set("Authorization", "Basic "+encoded)
		return
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
}

func (c HTTPClient) browseURL(issueKey string) string {
	if strings.TrimSpace(issueKey) == "" {
		return ""
	}
	browse := *c.baseURL
	browse.Path = path.Join(c.baseURL.Path, "browse", strings.TrimSpace(issueKey))
	browse.RawQuery = ""
	browse.Fragment = ""
	return browse.String()
}

func jiraStatusError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxJiraErrorBodyBytes))
	message := response.Status
	var decoded jiraErrorResponse
	if err := json.Unmarshal(body, &decoded); err == nil && len(decoded.ErrorMessages) > 0 {
		message = strings.Join(decoded.ErrorMessages, "; ")
	}
	if response.StatusCode == http.StatusGone {
		return fmt.Errorf("%w: %s", ErrArchivedIssue, message)
	}
	return JiraError{
		Provider:        "jira",
		StatusCode:      response.StatusCode,
		Message:         message,
		RetryAfter:      sdk.ParseRetryAfterHeader(response.Header.Get("Retry-After")),
		RateLimitReason: strings.TrimSpace(response.Header.Get("RateLimit-Reason")),
	}
}

func boundedJQL(raw string, window CollectionWindow) string {
	base := strings.TrimSpace(raw)
	since := window.Since.UTC().Format(jiraJQLTimeLayout)
	until := window.Until.UTC().Format(jiraJQLTimeLayout)
	windowClause := fmt.Sprintf(`updated >= "%s" AND updated <= "%s"`, since, until)
	if base == "" {
		return windowClause + " ORDER BY updated ASC"
	}
	order := ""
	lower := strings.ToLower(base)
	if idx := strings.LastIndex(lower, " order by "); idx >= 0 {
		order = strings.TrimSpace(base[idx:])
		base = strings.TrimSpace(base[:idx])
	}
	if order == "" {
		order = "ORDER BY updated ASC"
	}
	return fmt.Sprintf("(%s) AND %s %s", base, windowClause, order)
}

func parseJiraTime(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}
	for _, layout := range []string{jiraTimeLayout, time.RFC3339Nano, time.RFC3339} {
		value, err := time.Parse(layout, trimmed)
		if err == nil {
			return value.UTC()
		}
	}
	return time.Time{}
}

func normalizedLimit(value int, fallback int, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
