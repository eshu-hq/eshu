// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlabciruntime

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

// GitLabClient fetches bounded GitLab CI/CD pipeline metadata through
// GitLab's REST API (https://docs.gitlab.com/ee/api/pipelines.html,
// https://docs.gitlab.com/ee/api/jobs.html).
type GitLabClient struct {
	HTTPClient *http.Client
}

// FetchPipelines returns the configured project's most recent pipelines
// (bounded by target.MaxRuns) plus bounded job metadata for each (GitLab
// reports job artifacts inline on the job, so no separate artifact fetch is
// needed the way GitHub Actions requires), and a truncation signal for
// whether GitLab reports additional pipelines beyond the fetched window.
func (c GitLabClient) FetchPipelines(ctx context.Context, target TargetConfig) (PipelinePage, error) {
	target, err := validateTarget(target)
	if err != nil {
		return PipelinePage{}, err
	}
	pipelines, total, err := c.fetchPipelineListPage(ctx, target)
	if err != nil {
		return PipelinePage{}, err
	}
	if len(pipelines) == 0 {
		return PipelinePage{}, fmt.Errorf("gitlab ci project %q returned no pipelines", target.ProjectPath)
	}
	if len(pipelines) > target.MaxRuns {
		pipelines = pipelines[:target.MaxRuns]
	}
	snapshots := make([]PipelineSnapshot, 0, len(pipelines))
	for _, slim := range pipelines {
		pipelineID, err := numericProviderID(slim["id"])
		if err != nil {
			return PipelinePage{}, fmt.Errorf("gitlab ci pipeline.id: %w", err)
		}
		detail, err := c.fetchPipelineDetail(ctx, target, pipelineID)
		if err != nil {
			return PipelinePage{}, err
		}
		jobs, jobsPartial, err := c.fetchJobs(ctx, target, pipelineID)
		if err != nil {
			return PipelinePage{}, err
		}
		snapshots = append(snapshots, PipelineSnapshot{
			Pipeline:    detail,
			Jobs:        jobs,
			JobsPartial: jobsPartial,
		})
	}
	return PipelinePage{
		Snapshots: snapshots,
		Truncated: pipelinesPageTruncated(total, len(pipelines), target.MaxRuns),
	}, nil
}

// pipelinesPageTruncated reports whether more pipelines exist beyond the
// fetched window. It prefers GitLab's exact X-Total header signal when the
// provider returned one (total > 0); otherwise it falls back to the
// full-page heuristic (the fetched page exactly filled the requested
// per_page/MaxRuns bound, so more pipelines may exist beyond it). GitLab
// documents that some pagination headers (including X-Total) "may not be
// returned" for gitlab.com callers, so the fallback is required, not
// defensive-only -- see rate_limit.go's doc comment for the parallel GitLab
// vs. GitHub header-shape note.
func pipelinesPageTruncated(total, fetchedLen, maxRuns int) bool {
	if total > 0 {
		return total > fetchedLen
	}
	return fetchedLen == maxRuns
}

func (c GitLabClient) fetchPipelineListPage(ctx context.Context, target TargetConfig) ([]map[string]any, int, error) {
	path := fmt.Sprintf("/projects/%s/pipelines", url.PathEscape(target.ProjectPath))
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxRuns),
		"order_by": "id",
		"sort":     "desc",
	})
	if err != nil {
		return nil, 0, err
	}
	var decoded []map[string]any
	total, err := c.getJSONWithTotal(ctx, target, endpoint, &decoded)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch gitlab ci pipelines: %w", err)
	}
	return decoded, total, nil
}

func (c GitLabClient) fetchPipelineDetail(
	ctx context.Context,
	target TargetConfig,
	pipelineID string,
) (map[string]any, error) {
	path := fmt.Sprintf("/projects/%s/pipelines/%s", url.PathEscape(target.ProjectPath), url.PathEscape(pipelineID))
	endpoint, err := targetURL(target, path, nil)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if _, err := c.getJSONWithTotal(ctx, target, endpoint, &decoded); err != nil {
		return nil, fmt.Errorf("fetch gitlab ci pipeline %s: %w", pipelineID, err)
	}
	return decoded, nil
}

func (c GitLabClient) fetchJobs(
	ctx context.Context,
	target TargetConfig,
	pipelineID string,
) ([]map[string]any, bool, error) {
	path := fmt.Sprintf("/projects/%s/pipelines/%s/jobs", url.PathEscape(target.ProjectPath), url.PathEscape(pipelineID))
	endpoint, err := targetURL(target, path, map[string]string{
		"per_page": strconv.Itoa(target.MaxJobs),
	})
	if err != nil {
		return nil, false, err
	}
	var decoded []map[string]any
	total, err := c.getJSONWithTotal(ctx, target, endpoint, &decoded)
	if err != nil {
		return nil, false, fmt.Errorf("fetch gitlab ci jobs: %w", err)
	}
	return decoded, total > len(decoded), nil
}

// getJSONWithTotal issues one authenticated GET request and decodes the JSON
// body into out, returning GitLab's X-Total pagination header value (0 when
// absent or unparseable -- see pipelinesPageTruncated's doc comment).
func (c GitLabClient) getJSONWithTotal(ctx context.Context, target TargetConfig, endpoint string, out any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("PRIVATE-TOKEN", target.Token)
	response, err := c.httpClient().Do(request)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if rateLimit, ok := rateLimitErrorFromResponse(response, time.Now()); ok {
		return 0, rateLimit
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, sdk.HTTPError{
			Provider:   "gitlab_ci",
			StatusCode: response.StatusCode,
			Message:    http.StatusText(response.StatusCode),
			RetryAfter: sdk.ParseRetryAfterHeader(response.Header.Get("Retry-After")),
		}
	}
	total, _ := strconv.Atoi(strings.TrimSpace(response.Header.Get("X-Total")))
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return 0, err
	}
	return total, nil
}

func (c GitLabClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return sdk.DefaultHTTPClient(30 * time.Second)
}

func targetURL(target TargetConfig, path string, query map[string]string) (string, error) {
	base, err := sdk.ParseBaseURL("gitlab ci", target.APIBaseURL)
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
