// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlabciruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	maxPipelinePages = 100
	maxJobPages      = 500
	// defaultMaxRuns bounds a target's pipeline window with the DEFAULT,
	// mirroring ghactionsruntime's defaultMaxRuns rationale: an omitted/zero
	// max_runs resolves to this value rather than requiring every target to
	// spell out a limit.
	defaultMaxRuns = 10
)

// ErrRateLimited marks provider throttling that should remain distinguishable
// from malformed target or claim errors.
var ErrRateLimited = errors.New("gitlab ci provider rate limited")

// Client fetches one bounded window of GitLab CI/CD pipelines for a
// configured target.
type Client interface {
	FetchPipelines(context.Context, TargetConfig) (PipelinePage, error)
}

// SourceConfig configures one claim-aware GitLab CI/CD runtime source.
type SourceConfig struct {
	CollectorInstanceID string
	Client              Client
	Targets             []TargetConfig
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig bounds one GitLab CI/CD project target.
type TargetConfig struct {
	ScopeID     string
	ProjectPath string
	Token       string
	// AllowedProjectPaths bounds which normalized project paths this target
	// may resolve to, mirroring ghactionsruntime.TargetConfig.AllowedRepositories.
	AllowedProjectPaths []string
	APIBaseURL          string
	SourceURI           string
	MaxRuns             int
	MaxJobs             int
}

// PipelineSnapshot carries one pipeline's raw provider-shaped fields, keyed
// exactly as cicdrun.GitLabCIFixtureEnvelopes expects them
// ("pipeline"/"jobs"/"jobs_partial" — see gitlab_ci_fixture.go's
// gitlabCIFixture).
type PipelineSnapshot struct {
	Pipeline    map[string]any
	Jobs        []map[string]any
	JobsPartial bool
}

// PipelinePage carries the bounded window of pipelines one claim fetched
// (newest first, as GitLab returns them by default), plus whether GitLab's
// pipeline listing indicated additional pipelines exist beyond the window.
// Each snapshot's normalized facts are keyed by pipeline ID at the cicdrun
// envelope layer, so re-fetching the same window on a later claim cycle
// re-emits the same StableFactKey set per pipeline (an idempotent upsert at
// projection).
type PipelinePage struct {
	Snapshots []PipelineSnapshot
	Truncated bool
}

// ClaimedSource resolves CI/CD run workflow claims into fact generations for
// GitLab CI/CD targets.
type ClaimedSource struct {
	collectorInstanceID string
	client              Client
	targets             map[string]TargetConfig
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates source configuration and returns a claim-aware
// GitLab CI/CD runtime source.
func NewClaimedSource(config SourceConfig) (ClaimedSource, error) {
	if strings.TrimSpace(config.CollectorInstanceID) == "" {
		return ClaimedSource{}, fmt.Errorf("collector_instance_id is required")
	}
	if config.Client == nil {
		return ClaimedSource{}, fmt.Errorf("client is required")
	}
	if len(config.Targets) == 0 {
		return ClaimedSource{}, fmt.Errorf("targets are required")
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	targets := make(map[string]TargetConfig, len(config.Targets))
	for i, target := range config.Targets {
		normalized, err := validateTarget(target)
		if err != nil {
			return ClaimedSource{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		if _, ok := targets[normalized.ScopeID]; ok {
			return ClaimedSource{}, fmt.Errorf("duplicate target scope_id %q", normalized.ScopeID)
		}
		targets[normalized.ScopeID] = normalized
	}
	return ClaimedSource{
		collectorInstanceID: strings.TrimSpace(config.CollectorInstanceID),
		client:              config.Client,
		targets:             targets,
		now:                 now,
		tracer:              config.Tracer,
		instruments:         config.Instruments,
	}, nil
}

// NextClaimed implements collector.ClaimedSource for GitLab CI/CD run work.
func (s ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if err := s.validateClaim(item); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		return collector.CollectedGeneration{}, false, fmt.Errorf("ci/cd run target %q is not configured", item.ScopeID)
	}
	startedAt := time.Now()
	observeCtx, observeSpan := s.startObserve(ctx)
	defer observeSpan.End()
	fetchCtx, fetchSpan := s.startFetch(observeCtx)
	page, err := s.client.FetchPipelines(fetchCtx, target)
	if err != nil {
		statusClass := classifyProviderStatus(err)
		s.recordFetch(observeCtx, statusClass, startedAt)
		s.recordRateLimit(observeCtx, statusClass)
		recordSpanError(fetchSpan, err)
		recordSpanError(observeSpan, err)
		fetchSpan.End()
		return collector.CollectedGeneration{}, false, err
	}
	fetchSpan.End()
	observedAt := s.now().UTC()
	envelopes, err := s.buildPipelineEnvelopes(observeSpan, item, target, page, observedAt)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       item.ScopeID,
		SourceSystem:  string(scope.CollectorCICDRun),
		ScopeKind:     scope.KindCICDRun,
		CollectorKind: scope.CollectorCICDRun,
		PartitionKey:  target.ProjectPath,
		Metadata: map[string]string{
			"provider":     string(cicdrun.ProviderGitLabCI),
			"project_path": target.ProjectPath,
		},
	}
	generationValue := scope.ScopeGeneration{
		ScopeID:      item.ScopeID,
		GenerationID: item.GenerationID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		TriggerKind:  scope.TriggerKindSnapshot,
		Status:       scope.GenerationStatusCompleted,
	}
	if err := scopeValue.Validate(); err != nil {
		recordSpanError(observeSpan, err)
		return collector.CollectedGeneration{}, false, err
	}
	if err := generationValue.ValidateForScope(scopeValue); err != nil {
		recordSpanError(observeSpan, err)
		return collector.CollectedGeneration{}, false, err
	}
	s.recordFacts(observeCtx, envelopes)
	s.recordPartialGeneration(observeCtx, page)
	s.recordFetch(observeCtx, "success", startedAt)
	return collector.FactsFromSlice(scopeValue, generationValue, envelopes), true, nil
}

// buildPipelineEnvelopes normalizes one fetched pipeline window into facts,
// emitting one independently keyed fact-set per pipeline (via the shared
// cicdrun normalizer's pipeline-ID-scoped StableFactKey). A truncated page
// attaches a pipelines_truncated warning to the newest (first) pipeline's job
// list is unaffected -- the fixture normalizer itself has no top-level
// "warnings" input for GitLab (unlike GitHub Actions' RunSnapshot.Warnings),
// so a truncated-window signal here is recorded only via telemetry
// (recordPartialGeneration), not as a synthesized ci.warning fact.
func (s ClaimedSource) buildPipelineEnvelopes(
	observeSpan trace.Span,
	item workflow.WorkItem,
	target TargetConfig,
	page PipelinePage,
	observedAt time.Time,
) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(page.Snapshots))
	for _, snapshot := range page.Snapshots {
		raw, err := json.Marshal(map[string]any{
			"pipeline":     snapshot.Pipeline,
			"jobs":         snapshot.Jobs,
			"jobs_partial": snapshot.JobsPartial,
		})
		if err != nil {
			recordSpanError(observeSpan, err)
			return nil, fmt.Errorf("marshal gitlab ci snapshot: %w", err)
		}
		pipelineEnvelopes, err := cicdrun.GitLabCIFixtureEnvelopes(raw, cicdrun.FixtureContext{
			ScopeID:             item.ScopeID,
			GenerationID:        item.GenerationID,
			CollectorInstanceID: s.collectorInstanceID,
			FencingToken:        item.CurrentFencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			recordSpanError(observeSpan, err)
			return nil, fmt.Errorf("normalize gitlab ci snapshot: %w", err)
		}
		envelopes = append(envelopes, pipelineEnvelopes...)
	}
	return envelopes, nil
}

func (s ClaimedSource) validateClaim(item workflow.WorkItem) error {
	if strings.TrimSpace(s.collectorInstanceID) == "" {
		return fmt.Errorf("collector_instance_id is required")
	}
	if item.CollectorKind != scope.CollectorCICDRun {
		return fmt.Errorf("claimed collector_kind %q must be %q", item.CollectorKind, scope.CollectorCICDRun)
	}
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return fmt.Errorf("claimed collector_instance_id %q must be %q", item.CollectorInstanceID, s.collectorInstanceID)
	}
	if strings.TrimSpace(item.ScopeID) == "" {
		return fmt.Errorf("claimed scope_id is required")
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return fmt.Errorf("claimed generation_id is required")
	}
	if item.CurrentFencingToken <= 0 {
		return fmt.Errorf("claimed current_fencing_token must be positive")
	}
	return nil
}

func validateTarget(target TargetConfig) (TargetConfig, error) {
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.ProjectPath = normalizeProjectPath(target.ProjectPath)
	target.Token = strings.TrimSpace(target.Token)
	target.APIBaseURL = strings.TrimSpace(target.APIBaseURL)
	target.SourceURI = strings.TrimSpace(target.SourceURI)
	if target.APIBaseURL == "" {
		target.APIBaseURL = "https://gitlab.com/api/v4"
	}
	if target.SourceURI == "" && target.ProjectPath != "" {
		target.SourceURI = "https://gitlab.com/" + target.ProjectPath
	}
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if target.ProjectPath == "" {
		return TargetConfig{}, fmt.Errorf("project_path must be namespace/project")
	}
	if target.Token == "" {
		return TargetConfig{}, fmt.Errorf("token is required")
	}
	if !projectPathAllowed(target.ProjectPath, target.AllowedProjectPaths) {
		return TargetConfig{}, fmt.Errorf("project_path must be listed in allowed_project_paths")
	}
	if target.MaxRuns == 0 {
		target.MaxRuns = defaultMaxRuns
	}
	if target.MaxRuns < 0 || target.MaxRuns > maxPipelinePages {
		return TargetConfig{}, fmt.Errorf("max_runs must be between 0 and %d (0 uses the default of %d)", maxPipelinePages, defaultMaxRuns)
	}
	if target.MaxJobs <= 0 || target.MaxJobs > maxJobPages {
		return TargetConfig{}, fmt.Errorf("max_jobs must be between 1 and %d", maxJobPages)
	}
	if err := validateTargetURL("api_base_url", target.APIBaseURL, true); err != nil {
		return TargetConfig{}, err
	}
	if err := validateTargetURL("source_uri", target.SourceURI, false); err != nil {
		return TargetConfig{}, err
	}
	return target, nil
}

func projectPathAllowed(projectPath string, allowed []string) bool {
	for _, candidate := range allowed {
		if normalizeProjectPath(candidate) == projectPath {
			return true
		}
	}
	return false
}

// normalizeProjectPath lower-cases and trims a "namespace/project" (or
// "group/subgroup/project") GitLab project path. Unlike GitHub's
// owner/repo (always exactly two segments), GitLab project paths may nest
// under any number of subgroups, so this only trims surrounding slashes and
// a trailing ".git" rather than requiring exactly two segments.
func normalizeProjectPath(projectPath string) string {
	trimmed := strings.ToLower(strings.Trim(strings.TrimSpace(projectPath), "/"))
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if trimmed == "" || !strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}

func validateTargetURL(field, raw string, requireHTTPS bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", field)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	return nil
}
