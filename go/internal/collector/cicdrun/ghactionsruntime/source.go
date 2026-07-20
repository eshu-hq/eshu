// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

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
	maxRunPages      = 100
	maxJobPages      = 500
	maxArtifactPages = 500
	// defaultMaxRuns bounds a target's run window with the DEFAULT, not the
	// collection mechanism: an omitted/zero max_runs resolves to this value
	// rather than requiring every target to spell out a limit. Steady-state
	// per-repo request volume tracks the actual new-run rate (unchanged runs
	// re-emit idempotently at projection), so 10 is a small, safe default;
	// the hard cap (maxRunPages) stays 100 for targets that opt into a wider
	// window explicitly.
	defaultMaxRuns = 10
)

// ErrRateLimited marks provider throttling that should remain distinguishable
// from malformed target or claim errors.
var ErrRateLimited = errors.New("github actions provider rate limited")

// Client fetches one bounded window of GitHub Actions runs for a configured
// target.
type Client interface {
	FetchRuns(context.Context, TargetConfig) (RunPage, error)
}

// SourceConfig configures one claim-aware GitHub Actions runtime source.
type SourceConfig struct {
	CollectorInstanceID string
	Client              Client
	Targets             []TargetConfig
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig bounds one GitHub Actions repository target.
type TargetConfig struct {
	ScopeID             string
	Repository          string
	Token               string
	AllowedRepositories []string
	APIBaseURL          string
	SourceURI           string
	MaxRuns             int
	MaxJobs             int
	MaxArtifacts        int
}

// RunSnapshot carries raw provider-shaped fields consumed by the shared
// cicdrun normalizer.
type RunSnapshot struct {
	Workflow    map[string]any
	Run         map[string]any
	Jobs        []map[string]any
	JobsPartial bool
	Artifacts   []map[string]any
	Warnings    []map[string]any
}

// RunPage carries the bounded window of runs one claim fetched (newest first,
// as GitHub returns them), plus whether the provider's runs listing indicated
// additional runs exist beyond the window. Each snapshot's normalized facts
// are keyed by run ID at the cicdrun envelope layer, so re-fetching the same
// window on a later claim cycle re-emits the same StableFactKey set per run
// (an idempotent upsert at projection) rather than requiring a persistent
// watermark/cursor here.
type RunPage struct {
	Snapshots []RunSnapshot
	Truncated bool
}

// ClaimedSource resolves CI/CD run workflow claims into fact generations.
type ClaimedSource struct {
	collectorInstanceID string
	client              Client
	targets             map[string]TargetConfig
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates source configuration and returns a claim-aware
// GitHub Actions runtime source.
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

// NextClaimed implements collector.ClaimedSource for CI/CD run work.
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
	page, err := s.client.FetchRuns(fetchCtx, target)
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
	envelopes, err := s.buildRunEnvelopes(observeSpan, item, target, page, observedAt)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       item.ScopeID,
		SourceSystem:  string(scope.CollectorCICDRun),
		ScopeKind:     scope.KindCICDRun,
		CollectorKind: scope.CollectorCICDRun,
		PartitionKey:  target.Repository,
		Metadata: map[string]string{
			"provider":   string(cicdrun.ProviderGitHubActions),
			"repository": target.Repository,
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

// buildRunEnvelopes normalizes one fetched run window into facts, emitting
// one independently keyed fact-set per run (via the shared cicdrun fixture
// normalizer's run-ID-scoped StableFactKey) instead of the single-run shape
// the prior FetchLatestRun path produced. A truncated page attaches a
// runs_truncated warning to the newest (first) run in the window so it
// reaches the graph as a ci.warning fact through the existing
// fixture.Warnings pipeline, mirroring the jobs_partial pattern.
func (s ClaimedSource) buildRunEnvelopes(
	observeSpan trace.Span,
	item workflow.WorkItem,
	target TargetConfig,
	page RunPage,
	observedAt time.Time,
) ([]facts.Envelope, error) {
	snapshots := attachRunsTruncatedWarning(page)
	envelopes := make([]facts.Envelope, 0, len(snapshots))
	for _, snapshot := range snapshots {
		raw, err := json.Marshal(map[string]any{
			"workflow":     snapshot.Workflow,
			"run":          snapshot.Run,
			"jobs":         snapshot.Jobs,
			"jobs_partial": snapshot.JobsPartial,
			"artifacts":    sanitizeArtifacts(snapshot.Artifacts),
			"warnings":     snapshot.Warnings,
		})
		if err != nil {
			recordSpanError(observeSpan, err)
			return nil, fmt.Errorf("marshal github actions snapshot: %w", err)
		}
		runEnvelopes, err := cicdrun.GitHubActionsFixtureEnvelopes(raw, cicdrun.FixtureContext{
			ScopeID:             item.ScopeID,
			GenerationID:        item.GenerationID,
			CollectorInstanceID: s.collectorInstanceID,
			FencingToken:        item.CurrentFencingToken,
			ObservedAt:          observedAt,
			SourceURI:           target.SourceURI,
		})
		if err != nil {
			recordSpanError(observeSpan, err)
			return nil, fmt.Errorf("normalize github actions snapshot: %w", err)
		}
		envelopes = append(envelopes, runEnvelopes...)
	}
	// Collapse workflow-identity-keyed facts across the run window. The fixture
	// normalizer already dedups within a single run, but a WORKFLOW-level fact
	// (ci.pipeline_definition, whose FactID is keyed by
	// provider+repository+workflow identity, not run id) is emitted once per
	// run, so two runs of the SAME workflow in the window produce the identical
	// FactID twice. Postgres masks this on FactID, but the emitted-fact
	// count/metric would be inflated and a non-Postgres committer would receive
	// duplicate envelopes. Deduping the combined slice by FactID (first
	// occurrence wins, order preserved) collapses those workflow-identity facts
	// to one while leaving every run-level fact (keyed by run_id:run_attempt)
	// untouched.
	return dedupeEnvelopesByFactID(envelopes), nil
}

// dedupeEnvelopesByFactID returns envelopes with duplicate FactID values
// removed, keeping the first occurrence and preserving order. It is the
// cross-run analogue of the fixture normalizer's own within-run dedup: it
// collapses the workflow-identity-keyed facts (ci.pipeline_definition) that
// repeat across runs of the same workflow in one claim window, so the source's
// emitted-fact count and any non-Postgres committer see each unique fact once.
func dedupeEnvelopesByFactID(envelopes []facts.Envelope) []facts.Envelope {
	seen := make(map[string]struct{}, len(envelopes))
	out := envelopes[:0]
	for _, envelope := range envelopes {
		if _, ok := seen[envelope.FactID]; ok {
			continue
		}
		seen[envelope.FactID] = struct{}{}
		out = append(out, envelope)
	}
	return out
}

// attachRunsTruncatedWarning returns page.Snapshots unchanged when the page
// was not truncated. When truncated, it returns a copy of the snapshots with
// a runs_truncated warning appended to the newest (first, since GitHub
// returns runs newest-first) run's Warnings, without mutating page.Snapshots
// itself (recordPartialGeneration still reads the untouched page for its own
// telemetry accounting).
func attachRunsTruncatedWarning(page RunPage) []RunSnapshot {
	if !page.Truncated || len(page.Snapshots) == 0 {
		return page.Snapshots
	}
	snapshots := append([]RunSnapshot(nil), page.Snapshots...)
	latest := snapshots[0]
	latest.Warnings = append(append([]map[string]any(nil), latest.Warnings...), map[string]any{
		"reason": "runs_truncated",
		"message": "additional workflow runs exist beyond the collected window; " +
			"increase max_runs or rely on idempotent re-collection to catch up",
	})
	snapshots[0] = latest
	return snapshots
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
	target.Repository = normalizeRepository(target.Repository)
	target.Token = strings.TrimSpace(target.Token)
	target.APIBaseURL = strings.TrimSpace(target.APIBaseURL)
	target.SourceURI = strings.TrimSpace(target.SourceURI)
	if target.APIBaseURL == "" {
		target.APIBaseURL = "https://api.github.com"
	}
	if target.SourceURI == "" && target.Repository != "" {
		target.SourceURI = "https://github.com/" + target.Repository
	}
	if target.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if target.Repository == "" {
		return TargetConfig{}, fmt.Errorf("repository must be owner/name")
	}
	if target.Token == "" {
		return TargetConfig{}, fmt.Errorf("token is required")
	}
	if !repositoryAllowed(target.Repository, target.AllowedRepositories) {
		return TargetConfig{}, fmt.Errorf("repository must be listed in allowed_repositories")
	}
	if target.MaxRuns == 0 {
		target.MaxRuns = defaultMaxRuns
	}
	if target.MaxRuns < 0 || target.MaxRuns > maxRunPages {
		return TargetConfig{}, fmt.Errorf("max_runs must be between 1 and %d", maxRunPages)
	}
	if target.MaxJobs <= 0 || target.MaxJobs > maxJobPages {
		return TargetConfig{}, fmt.Errorf("max_jobs must be between 1 and %d", maxJobPages)
	}
	if target.MaxArtifacts <= 0 || target.MaxArtifacts > maxArtifactPages {
		return TargetConfig{}, fmt.Errorf("max_artifacts must be between 1 and %d", maxArtifactPages)
	}
	if err := validateTargetURL("api_base_url", target.APIBaseURL, true); err != nil {
		return TargetConfig{}, err
	}
	if err := validateTargetURL("source_uri", target.SourceURI, false); err != nil {
		return TargetConfig{}, err
	}
	return target, nil
}

func sanitizeArtifacts(artifacts []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		next := make(map[string]any, len(artifact))
		for key, value := range artifact {
			if key == "archive_download_url" {
				if raw, ok := value.(string); ok {
					next[key] = stripURLQuery(raw)
					continue
				}
			}
			next[key] = value
		}
		out = append(out, next)
	}
	return out
}

func stripURLQuery(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func repositoryAllowed(repository string, allowed []string) bool {
	for _, candidate := range allowed {
		if normalizeRepository(candidate) == repository {
			return true
		}
	}
	return false
}

func normalizeRepository(repository string) string {
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
