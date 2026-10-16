// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Closed failure classes of every error this package returns. The class
// rides on Error.Class so the caller can log it as failure_class; any error
// fails the collector cycle with no partial listing.
const (
	// FailureClassAuthFailed means the token was rejected (401).
	FailureClassAuthFailed = "auth_failed"
	// FailureClassForbidden means the token lacks read_api (403).
	FailureClassForbidden = "forbidden"
	// FailureClassGroupNotFound means the group does not exist (404).
	FailureClassGroupNotFound = "group_not_found"
	// FailureClassConfigInvalid means an exact rule could never match the
	// resolved group prefix.
	FailureClassConfigInvalid = "config_invalid"
	// FailureClassRateLimited means 429s exhausted the retry budget.
	FailureClassRateLimited = "rate_limited"
	// FailureClassUpstream means another 5xx failed it.
	FailureClassUpstream = "upstream_error"
	// FailureClassHTTPError means a final non-401/403/404 status failed
	// it: another 4xx, or any 5xx outside the retry set.
	FailureClassHTTPError = "http_error"
	// FailureClassTransport means the network failed it.
	FailureClassTransport = "transport_error"
	// FailureClassDecode means the response body did not decode.
	FailureClassDecode = "decode_error"
)

// Error is a classified GitLab listing failure. Class is one of the
// FailureClass constants. A canceled context rides as the cause so
// errors.Is still finds it.
type Error struct {
	Class string
	msg   string
	cause error
}

// Error returns the classified failure message.
func (e *Error) Error() string { return e.msg }

// Unwrap returns the context error when cancellation ended the listing.
func (e *Error) Unwrap() error { return e.cause }

// ClassOf returns the failure class of err, or "" when err is not a
// classified listing failure.
func ClassOf(err error) string {
	var glErr *Error
	if errors.As(err, &glErr) {
		return glErr.Class
	}
	return ""
}

func classified(class, format string, args ...any) *Error {
	return &Error{Class: class, msg: "gitlab " + fmt.Sprintf(format, args...)}
}

// canceled returns a transport-classified error caused by ctxErr.
func canceled(operation string, ctxErr error) *Error {
	return &Error{Class: FailureClassTransport, msg: "gitlab " + operation + ": " + ctxErr.Error(), cause: ctxErr}
}

// Group is one resolved GitLab group: its numeric ID and its canonical
// full path in the server's case.
type Group struct {
	ID       int64
	FullPath string
}

// Project is one listed GitLab project: its numeric ID, its full path
// with namespace in the server's case, and its archived flag.
type Project struct {
	ID                int64
	PathWithNamespace string
	Archived          bool
}

// Listing is one group project listing. Complete is true only when an
// empty page with no rel="next" ended it below the limit, mirroring
// listGitHubOrgRepositoriesFrom. Pages counts the page requests, and
// OutsideGroup counts response projects dropped outside the group prefix.
type Listing struct {
	Projects     []Project
	Complete     bool
	Pages        int
	OutsideGroup int
}

// Request constants: per_page maxes at 100 and stays fixed for the whole
// listing, ordered by ascending id so creations never shift earlier pages.
// archived is omitted so both archived and active projects come back for
// local classification, and min_access_level stays unset so a role
// downgrade cannot read as a mass departure.
const (
	listingPageSize = 100
	// maxPageAttempts is one initial attempt plus three retries.
	maxPageAttempts = 4
	// requestTimeout bounds one listing request, as for GitHub.
	requestTimeout = 15 * time.Second
	// retryWaitMin and retryWaitMax clamp every backoff wait.
	retryWaitMin = time.Second
	retryWaitMax = 60 * time.Second
)

// defaultRetryWaits is the wait when the response carries no usable
// Retry-After, by retry index.
var defaultRetryWaits = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// collectorKind is the collector_kind label every signal here emits; only
// the git collector lists GitLab groups.
const collectorKind = "git"

// provider is the provider label every listing signal carries.
const provider = "gitlab"

// Client lists one GitLab group's projects. APIBase is
// "https://<host>/api/v4" and Token a token with read_api scope; both are
// fixed per process. Sleep waits out backoff (a timer select in production,
// a recorder in tests) and ends early when the context ends.
type Client struct {
	httpClient  *http.Client
	apiBase     string
	token       string
	instruments *telemetry.Instruments
	logger      *slog.Logger
	// Sleep waits out a backoff wait, returning early when the context ends.
	// Tests replace it to assert the schedule without waiting.
	Sleep func(ctx context.Context, d time.Duration) error
}

// sleepWithTimer waits with a timer: either it fires or the context ends.
func sleepWithTimer(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("backoff wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// NewClient returns a listing client for apiBase ("https://<host>/api/v4")
// with token. A nil httpClient gets a 15s-timeout client. The base must
// carry a scheme and host and no credentials.
func NewClient(httpClient *http.Client, apiBase, token string, inst *telemetry.Instruments, logger *slog.Logger) (*Client, error) {
	if _, err := sdk.ParseBaseURL("gitlab", apiBase); err != nil {
		return nil, classified(FailureClassTransport, "invalid api base: %v", err)
	}
	if httpClient == nil {
		httpClient = sdk.DefaultHTTPClient(requestTimeout)
	}
	return &Client{httpClient: httpClient, apiBase: strings.TrimRight(strings.TrimSpace(apiBase), "/"), token: token, instruments: inst, logger: logger, Sleep: sleepWithTimer}, nil
}

// ValidateConfig checks the gitlabGroup environment contract at config
// load: the group is required, the auth method must be token (githubApp
// mints a GitHub token, and SSH would need a second API credential), and
// the token must come from ESHU_GIT_TOKEN, never the GITHUB_TOKEN fallback,
// which would send a GitHub credential to the GitLab host. A path-form
// group also gets the exact-rule prefix check now; a numeric group has no
// known prefix until ResolveGroup, so discovery checks it instead.
func ValidateConfig(authMethod, group, gitToken, githubToken string, exactRuleIDs []string) error {
	group = strings.TrimSpace(group)
	if group == "" {
		return errors.New("gitlabGroup source mode requires ESHU_GITLAB_GROUP (a group full path or numeric ID)")
	}
	if !strings.EqualFold(strings.TrimSpace(authMethod), "token") {
		return fmt.Errorf("gitlabGroup source mode requires ESHU_GIT_AUTH_METHOD=token, got %q", strings.TrimSpace(authMethod))
	}
	if strings.TrimSpace(gitToken) == "" {
		if strings.TrimSpace(githubToken) != "" {
			return errors.New("gitlabGroup source mode requires ESHU_GIT_TOKEN: GITHUB_TOKEN is not accepted as a fallback in this mode")
		}
		return errors.New("gitlabGroup source mode requires ESHU_GIT_TOKEN")
	}
	if !isNumericGroup(group) {
		// Lowercase for this check only; discovery re-checks the canonical
		// server case, so fail-closed is preserved.
		if err := CheckExactRules(strings.ToLower(group), exactRuleIDs); err != nil {
			return err
		}
	}
	return nil
}

// isNumericGroup reports whether group is a numeric GitLab group ID, which
// has no known path prefix until it resolves.
func isNumericGroup(group string) bool {
	if group == "" {
		return false
	}
	for _, r := range group {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CheckExactRules fails with config_invalid unless every exact rule ID
// lives under the resolved group prefix ("gitlab/<fullPath>/"), compared
// case-sensitively like every exact rule. A rule outside the prefix could
// never match, so every project would silently read rule_excluded.
func CheckExactRules(fullPath string, exactRuleIDs []string) error {
	prefix := "gitlab/" + fullPath + "/"
	for _, id := range exactRuleIDs {
		if !strings.HasPrefix(id, prefix) {
			return classified(FailureClassConfigInvalid, "invalid exact rule %q: gitlabGroup %q selects only %q*", id, fullPath, prefix)
		}
	}
	return nil
}

type groupRecord struct {
	ID       int64  `json:"id"`
	FullPath string `json:"full_path"`
}

// ResolveGroup resolves group (a full path or numeric ID) to its numeric
// ID and canonical full path with GET /groups/:id?with_projects=false. A
// 404 is group_not_found.
func (c *Client) ResolveGroup(ctx context.Context, group string) (Group, error) {
	endpoint := c.apiBase + "/groups/" + url.PathEscape(strings.TrimSpace(group)) + "?with_projects=false"
	fetched, err := c.get(ctx, endpoint, "resolve gitlab group")
	if err != nil {
		return Group{}, err
	}
	return decodeGroup(fetched.body, endpoint)
}

// decodeGroup decodes a group resolve body.
func decodeGroup(body []byte, endpoint string) (Group, error) {
	var record groupRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return Group{}, classified(FailureClassDecode, "resolve gitlab group: decode %s: %v", endpoint, err)
	}
	if record.ID == 0 || strings.TrimSpace(record.FullPath) == "" {
		return Group{}, classified(FailureClassDecode, "resolve gitlab group: response names no group: %s", endpoint)
	}
	return Group(record), nil
}

type projectRecord struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	Archived          bool   `json:"archived"`
}

// ListGroupProjects lists every project under fullPath, including
// subgroups, at listingPageSize with order_by=id&sort=asc. Only an empty
// page ends the listing; it is complete only when that page has no
// rel="next" and the count is below limit. Reaching limit is always
// incomplete and trims to it, so the observer writes nothing. Projects
// outside the group prefix are dropped and counted in OutsideGroup.
func (c *Client) ListGroupProjects(ctx context.Context, fullPath string, limit int) (Listing, error) {
	start := time.Now()
	var listing Listing
	prefix := strings.ToLower(fullPath) + "/"
	for page := 1; len(listing.Projects) < limit; page++ {
		endpoint := fmt.Sprintf("%s/groups/%s/projects?include_subgroups=true&with_shared=false&order_by=id&sort=asc&per_page=%d&page=%d",
			c.apiBase, url.PathEscape(fullPath), listingPageSize, page)
		fetched, err := c.get(ctx, endpoint, "list gitlab group projects")
		if err != nil {
			return Listing{}, err
		}
		listing.Pages++
		var records []projectRecord
		if err := json.Unmarshal(fetched.body, &records); err != nil {
			c.add(ctx, telemetry.RepositoryListingOutcomeDecodeError)
			return Listing{}, classified(FailureClassDecode, "list gitlab group projects: decode %s: %v", endpoint, err)
		}
		if len(records) == 0 {
			listing.Complete = !HasNextLink(fetched.link)
			break
		}
		for _, record := range records {
			if record.ID == 0 || strings.TrimSpace(record.PathWithNamespace) == "" {
				c.add(ctx, telemetry.RepositoryListingOutcomeDecodeError)
				return Listing{}, classified(FailureClassDecode, "list gitlab group projects: project names no path: %s", endpoint)
			}
			if !strings.HasPrefix(strings.ToLower(record.PathWithNamespace), prefix) {
				listing.OutsideGroup++
				continue
			}
			listing.Projects = append(listing.Projects, Project(record))
		}
	}
	if len(listing.Projects) >= limit {
		listing.Complete = false
	}
	if len(listing.Projects) > limit {
		listing.Projects = listing.Projects[:limit]
	}
	c.logCompleted(ctx, fullPath, listing, time.Since(start))
	return listing, nil
}

// fetchedPage is one decoded listing response with the headers the
// completeness check reads.
type fetchedPage struct {
	body []byte
	link []string
}

// get issues one GET with PRIVATE-TOKEN under the page retry budget: 429,
// 502, 503 and 504 retry up to maxPageAttempts with a Retry-After (or
// 1s/2s/4s, clamped to [1s, 60s]) wait between attempts. Any error fails
// with no partial result.
func (c *Client) get(ctx context.Context, endpoint, operation string) (fetchedPage, error) {
	sleep := c.Sleep
	if sleep == nil {
		sleep = sleepWithTimer
	}
	for attempt := 1; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fetchedPage{}, classified(FailureClassTransport, "%s: build request: %v", operation, err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("PRIVATE-TOKEN", c.token)
		request.Header.Set("User-Agent", "eshu-go")
		response, err := c.httpClient.Do(request)
		if err != nil {
			c.add(ctx, telemetry.RepositoryListingOutcomeTransportError)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fetchedPage{}, canceled(operation, ctxErr)
			}
			return fetchedPage{}, classified(FailureClassTransport, "%s: %v", operation, err)
		}
		body, link, status := readResponse(response)
		if status == http.StatusOK {
			c.add(ctx, telemetry.RepositoryListingOutcomeSuccess)
			return fetchedPage{body: body, link: link}, nil
		}
		class, outcome := classifyStatus(status)
		if !retryableStatus(status) || attempt >= maxPageAttempts {
			c.add(ctx, outcome)
			return fetchedPage{}, classified(class, "%s: HTTP %d", operation, status)
		}
		wait := backoffWait(response.Header.Get("Retry-After"), attempt)
		c.add(ctx, telemetry.RepositoryListingOutcomeRetried)
		c.logRetry(ctx, status, attempt, wait)
		if err := waitContext(ctx, operation, sleep, wait); err != nil {
			return fetchedPage{}, err
		}
	}
}

// readResponse drains response and reports its body, Link headers, and
// status. The body stays bounded by the client timeout and the
// per_page=100 page shape; a body cut short fails closed at decode time.
func readResponse(response *http.Response) (body []byte, link []string, status int) {
	defer func() { _ = response.Body.Close() }()
	body, _ = io.ReadAll(response.Body)
	return body, response.Header.Values("Link"), response.StatusCode
}

// retryableStatus reports whether status retries: 429 and 502/503/504
// only. Any other 4xx or 5xx is final, so a wrong credential or a missing
// group surfaces immediately instead of waiting out a backoff.
func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// classifyStatus maps a terminal status to its failure class and counter
// outcome: 401/403/404 name their class, every other status shares the
// http_error outcome (other 5xx with the upstream_error class).
func classifyStatus(status int) (class, outcome string) {
	switch status {
	case http.StatusUnauthorized:
		return FailureClassAuthFailed, telemetry.RepositoryListingOutcomeHTTPError
	case http.StatusForbidden:
		return FailureClassForbidden, telemetry.RepositoryListingOutcomeHTTPError
	case http.StatusNotFound:
		return FailureClassGroupNotFound, telemetry.RepositoryListingOutcomeHTTPError
	case http.StatusTooManyRequests:
		return FailureClassRateLimited, telemetry.RepositoryListingOutcomeRateLimited
	default:
		if status >= 500 {
			return FailureClassUpstream, telemetry.RepositoryListingOutcomeHTTPError
		}
		return FailureClassHTTPError, telemetry.RepositoryListingOutcomeHTTPError
	}
}

// backoffWait returns the wait before the next attempt: the response's
// Retry-After (seconds or HTTP-date) when usable, else 1s/2s/4s by retry
// index, clamped to [1s, 60s].
func backoffWait(retryAfter string, attempt int) time.Duration {
	wait := sdk.ParseRetryAfter(retryAfter, time.Now())
	if wait <= 0 {
		index := attempt - 1
		if index < 0 {
			index = 0
		}
		if index >= len(defaultRetryWaits) {
			index = len(defaultRetryWaits) - 1
		}
		wait = defaultRetryWaits[index]
	}
	return min(max(wait, retryWaitMin), retryWaitMax)
}

// waitContext waits wait via sleep, mapping an early end to a classified
// error. The production sleep watches the context, so no goroutine parks.
func waitContext(ctx context.Context, operation string, sleep func(context.Context, time.Duration) error, wait time.Duration) error {
	if err := sleep(ctx, wait); err != nil {
		return canceled(operation+" backoff wait", err)
	}
	return nil
}

// HasNextLink reports whether any RFC 8288 Link header value names a
// rel="next" target. Both the GitHub org listing and the GitLab group
// listing share it, so the two completeness state machines stay in
// lockstep.
func HasNextLink(values []string) bool {
	for _, value := range values {
		for _, link := range strings.Split(value, ",") {
			for _, param := range strings.Split(link, ";")[1:] {
				name, rel, ok := strings.Cut(strings.TrimSpace(param), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
					continue
				}
				for _, relType := range strings.Fields(strings.Trim(strings.TrimSpace(rel), `"`)) {
					if strings.EqualFold(relType, "next") {
						return true
					}
				}
			}
		}
	}
	return false
}

func (c *Client) add(ctx context.Context, outcome string) {
	if c.instruments == nil {
		return
	}
	c.instruments.RepositoryListingRequests.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrCollectorKind(collectorKind),
		telemetry.AttrProvider(provider),
		telemetry.AttrOutcome(outcome),
	))
}

func (c *Client) logCompleted(ctx context.Context, groupPath string, listing Listing, elapsed time.Duration) {
	if c.logger == nil {
		return
	}
	c.logger.InfoContext(ctx, "git_gitlab_group_listing_completed",
		slog.String("group_path", groupPath),
		slog.Int("pages", listing.Pages),
		slog.Int("listed_count", len(listing.Projects)),
		slog.Int("outside_group_count", listing.OutsideGroup),
		slog.Bool("listing_complete", listing.Complete),
		slog.Float64("duration_seconds", elapsed.Seconds()),
	)
}

func (c *Client) logRetry(ctx context.Context, status, attempt int, wait time.Duration) {
	if c.logger == nil {
		return
	}
	c.logger.WarnContext(ctx, "git_gitlab_group_listing_retry",
		slog.Int("status_code", status),
		slog.Int("attempt", attempt),
		slog.Float64("retry_after_seconds", wait.Seconds()),
	)
}
