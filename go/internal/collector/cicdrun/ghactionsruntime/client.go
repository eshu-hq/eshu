// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// GitHubClient fetches bounded GitHub Actions metadata through GitHub's REST
// API.
type GitHubClient struct {
	HTTPClient *http.Client
}

// FetchRuns returns the configured repository's most recent runs (bounded by
// target.MaxRuns) plus bounded job and artifact metadata for each, and a
// truncation signal for whether GitHub reports additional runs beyond the
// fetched window. Every run in the window is fully populated so callers can
// emit one fact-set per run instead of discarding runs[1:] as the prior
// single-run FetchLatestRun did.
func (c GitHubClient) FetchRuns(ctx context.Context, target TargetConfig) (RunPage, error) {
	target, err := validateTarget(target)
	if err != nil {
		return RunPage{}, err
	}
	runs, totalCount, err := c.fetchRunPage(ctx, target)
	if err != nil {
		return RunPage{}, err
	}
	if len(runs) == 0 {
		return RunPage{}, fmt.Errorf("github actions repository %q returned no workflow runs", target.Repository)
	}
	if len(runs) > target.MaxRuns {
		runs = runs[:target.MaxRuns]
	}
	snapshots := make([]RunSnapshot, 0, len(runs))
	for _, run := range runs {
		runID, err := numericProviderID(run["id"])
		if err != nil {
			return RunPage{}, fmt.Errorf("github actions run.id: %w", err)
		}
		jobs, jobsPartial, err := c.fetchJobs(ctx, target, runID)
		if err != nil {
			return RunPage{}, err
		}
		artifacts, artifactsPartial, err := c.fetchArtifacts(ctx, target, runID)
		if err != nil {
			return RunPage{}, err
		}
		snapshots = append(snapshots, RunSnapshot{
			Workflow:         workflowMap(run),
			Run:              run,
			Jobs:             jobs,
			JobsPartial:      jobsPartial,
			Artifacts:        artifacts,
			ArtifactsPartial: artifactsPartial,
		})
	}
	return RunPage{
		Snapshots: snapshots,
		Truncated: runsPageTruncated(totalCount, len(runs), target.MaxRuns),
	}, nil
}

// runsPageTruncated reports whether more workflow runs exist beyond the
// fetched window. It prefers GitHub's exact total_count signal when the
// provider returned one (total_count > 0, which is always true whenever the
// repository has at least one run); otherwise it falls back to the
// full-page heuristic (the fetched page exactly filled the requested
// per_page/MaxRuns bound, so more runs may exist beyond it).
func runsPageTruncated(totalCount, fetchedLen, maxRuns int) bool {
	if totalCount > 0 {
		return totalCount > fetchedLen
	}
	return fetchedLen == maxRuns
}

func (c GitHubClient) fetchRunPage(ctx context.Context, target TargetConfig) ([]map[string]any, int, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs", target.Repository)
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxRuns),
	})
	if err != nil {
		return nil, 0, err
	}
	var decoded struct {
		TotalCount   int              `json:"total_count"`
		WorkflowRuns []map[string]any `json:"workflow_runs"`
	}
	if err := c.getJSON(ctx, target, endpoint, &decoded); err != nil {
		return nil, 0, fmt.Errorf("fetch github actions workflow runs: %w", err)
	}
	return decoded.WorkflowRuns, decoded.TotalCount, nil
}

func (c GitHubClient) fetchJobs(
	ctx context.Context,
	target TargetConfig,
	runID string,
) ([]map[string]any, bool, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%s/jobs", target.Repository, url.PathEscape(runID))
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxJobs),
	})
	if err != nil {
		return nil, false, err
	}
	var decoded struct {
		TotalCount int              `json:"total_count"`
		Jobs       []map[string]any `json:"jobs"`
	}
	if err := c.getJSON(ctx, target, endpoint, &decoded); err != nil {
		return nil, false, fmt.Errorf("fetch github actions jobs: %w", err)
	}
	return decoded.Jobs, decoded.TotalCount > len(decoded.Jobs), nil
}

func (c GitHubClient) fetchArtifacts(
	ctx context.Context,
	target TargetConfig,
	runID string,
) ([]map[string]any, bool, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%s/artifacts", target.Repository, url.PathEscape(runID))
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxArtifacts),
	})
	if err != nil {
		return nil, false, err
	}
	var decoded struct {
		TotalCount int              `json:"total_count"`
		Artifacts  []map[string]any `json:"artifacts"`
	}
	if err := c.getJSON(ctx, target, endpoint, &decoded); err != nil {
		return nil, false, fmt.Errorf("fetch github actions artifacts: %w", err)
	}
	return decoded.Artifacts, decoded.TotalCount > len(decoded.Artifacts), nil
}

func (c GitHubClient) getJSON(ctx context.Context, target TargetConfig, endpoint string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+target.Token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if rateLimit, ok := rateLimitErrorFromResponse(response, time.Now()); ok {
		return rateLimit
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return sdk.HTTPError{
			Provider:   "github_actions",
			StatusCode: response.StatusCode,
			Message:    http.StatusText(response.StatusCode),
			RetryAfter: sdk.ParseRetryAfterHeader(response.Header.Get("Retry-After")),
		}
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	return decoder.Decode(out)
}

func (c GitHubClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return sdk.DefaultHTTPClient(30 * time.Second)
}

func targetURL(target TargetConfig, path string, query map[string]string) (string, error) {
	base, err := sdk.ParseBaseURL("github actions", target.APIBaseURL)
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	relative, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	joined := base.ResolveReference(relative)
	values := joined.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	joined.RawQuery = values.Encode()
	return joined.String(), nil
}

func workflowMap(run map[string]any) map[string]any {
	workflow := make(map[string]any)
	if id, ok := run["workflow_id"]; ok {
		workflow["id"] = id
	}
	if name, ok := run["name"]; ok {
		workflow["name"] = name
	}
	if path, ok := run["path"]; ok {
		workflow["path"] = path
	}
	return workflow
}

func numericProviderID(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", errors.New("id is required")
	case json.Number:
		if strings.ContainsAny(typed.String(), ".eE") {
			return "", fmt.Errorf("id %q must be an integer", typed.String())
		}
		return typed.String(), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return "", fmt.Errorf("id %v must be an integer", typed)
		}
		return strconv.FormatInt(int64(typed), 10), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", errors.New("id is required")
		}
		return trimmed, nil
	default:
		return "", fmt.Errorf("unsupported id shape %T", value)
	}
}
