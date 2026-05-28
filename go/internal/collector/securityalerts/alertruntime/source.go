package alertruntime

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// ProviderGitHubDependabot selects GitHub repository Dependabot alerts.
	ProviderGitHubDependabot = "github_dependabot"

	// FailureAuthDenied marks credential failures as terminal.
	FailureAuthDenied = "auth_denied"
	// FailureNotFound marks missing repositories or disabled alert surfaces.
	FailureNotFound = "not_found"
	// FailureRateLimited marks provider rate limiting as retryable.
	FailureRateLimited = "rate_limited"
	// FailureRetryable marks transient transport/provider failures.
	FailureRetryable = "retryable"
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal = "terminal"
)

const (
	securityAlertSourceFreshnessActive  = "active"
	securityAlertSourceFreshnessPartial = "partial"

	collectionCoverageComplete   = "complete"
	collectionCoverageIncomplete = "incomplete"

	collectionOpenStateFilter            = "open"
	collectionOpenPageLimitReachedReason = "provider_open_alert_page_limit_reached"
)

// RepositoryAlertClient fetches provider repository alerts for one target.
type RepositoryAlertClient interface {
	ListRepositoryAlertsPages(
		context.Context,
		string,
		int,
	) (securityalerts.GitHubDependabotAlertResult, error)
}

// ClientFactory builds one repository alert client for a validated target.
type ClientFactory func(TargetConfig) (RepositoryAlertClient, error)

// SourceConfig configures one claim-driven security-alert source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	ClientFactory       ClientFactory
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig describes one allowlisted provider repository target.
type TargetConfig struct {
	Provider             string
	ScopeID              string
	Repository           string
	Token                string
	AllowedRepositories  []string
	APIBaseURL           string
	RepositoryAlertLimit int
	MaxPages             int
	SourceURI            string
}

type targetRuntime struct {
	config TargetConfig
	client RepositoryAlertClient
}

type repositoryAlertCollectionCoverage struct {
	pagesFetched int
	truncated    bool
}

// ClaimedSource resolves security-alert workflow claims into source facts.
type ClaimedSource struct {
	collectorInstanceID string
	targets             map[string]targetRuntime
	now                 func() time.Time
	tracer              trace.Tracer
	instruments         *telemetry.Instruments
}

// NewClaimedSource validates configuration and builds a claim-driven source.
func NewClaimedSource(config SourceConfig) (*ClaimedSource, error) {
	collectorID := strings.TrimSpace(config.CollectorInstanceID)
	if collectorID == "" {
		return nil, fmt.Errorf("security alert collector instance ID is required")
	}
	factory := config.ClientFactory
	if factory == nil {
		factory = defaultClientFactory
	}
	targets := make(map[string]targetRuntime, len(config.Targets))
	for i, target := range config.Targets {
		validated, err := validateTarget(target)
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", i, err)
		}
		if _, exists := targets[validated.ScopeID]; exists {
			return nil, fmt.Errorf("duplicate security alert target scope_id %q", validated.ScopeID)
		}
		client, err := factory(validated)
		if err != nil {
			return nil, fmt.Errorf("target %d client: %w", i, err)
		}
		targets[validated.ScopeID] = targetRuntime{config: validated, client: client}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one security alert target is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &ClaimedSource{
		collectorInstanceID: collectorID,
		targets:             targets,
		now:                 now,
		tracer:              config.Tracer,
		instruments:         config.Instruments,
	}, nil
}

// NextClaimed collects the provider target named by item.ScopeID.
func (s *ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if strings.TrimSpace(item.CollectorInstanceID) != s.collectorInstanceID {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"security alert work item collector_instance_id %q does not match source %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if item.CollectorKind != "" && item.CollectorKind != scope.CollectorSecurityAlert {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"security alert source cannot collect %q work items",
			item.CollectorKind,
		)
	}
	if strings.TrimSpace(item.GenerationID) == "" {
		return collector.CollectedGeneration{}, false, fmt.Errorf("security alert work item generation_id is required")
	}
	target, ok := s.targets[strings.TrimSpace(item.ScopeID)]
	if !ok {
		return collector.CollectedGeneration{}, false, ProviderFailure{
			failureClass: FailureRetryable,
			cause:        fmt.Errorf("security alert target scope_id is not configured"),
		}
	}

	startedAt := time.Now()
	observeCtx, observeSpan := s.startObserve(ctx, target.config)
	defer observeSpan.End()
	fetchCtx, fetchSpan := s.startFetch(observeCtx)
	result, err := target.client.ListRepositoryAlertsPages(
		fetchCtx,
		target.config.Repository,
		target.config.MaxPages,
	)
	if err != nil {
		failure := classifiedProviderFailure(err)
		s.recordFetch(observeCtx, target.config, failure.FailureClass(), startedAt)
		s.recordRateLimit(observeCtx, target.config, failure)
		recordSpanError(fetchSpan, failure)
		recordSpanError(observeSpan, failure)
		fetchSpan.End()
		return collector.CollectedGeneration{}, false, failure
	}
	fetchSpan.End()

	observedAt := result.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = s.now().UTC()
	}
	coverage := repositoryAlertCollectionCoverageFromResult(result)
	envs, err := s.envelopes(item, target.config, result.Alerts, observedAt, coverage)
	if err != nil {
		recordSpanError(observeSpan, err)
		return collector.CollectedGeneration{}, false, err
	}
	s.recordFacts(observeCtx, target.config, envs)
	s.recordFetch(observeCtx, target.config, "success", startedAt)
	return collector.FactsFromSlice(
		ingestionScope(target.config),
		scope.ScopeGeneration{
			GenerationID: item.GenerationID,
			ScopeID:      target.config.ScopeID,
			ObservedAt:   observedAt,
			IngestedAt:   observedAt,
			Status:       scope.GenerationStatusPending,
			TriggerKind:  scope.TriggerKindSnapshot,
			FreshnessHint: securityAlertFreshnessHint(securityAlertFreshnessInput{
				alerts:       result.Alerts,
				pagesFetched: result.PagesFetched,
				truncated:    result.Truncated,
			}),
		},
		envs,
	), true, nil
}

func defaultClientFactory(target TargetConfig) (RepositoryAlertClient, error) {
	return securityalerts.NewGitHubDependabotClient(securityalerts.GitHubDependabotClientConfig{
		BaseURL:              target.APIBaseURL,
		Token:                target.Token,
		AllowedRepositories:  target.AllowedRepositories,
		RepositoryAlertLimit: target.RepositoryAlertLimit,
	}), nil
}

func (s *ClaimedSource) envelopes(
	item workflow.WorkItem,
	target TargetConfig,
	alerts []securityalerts.GitHubDependabotAlert,
	observedAt time.Time,
	coverage repositoryAlertCollectionCoverage,
) ([]facts.Envelope, error) {
	envs := make([]facts.Envelope, 0, len(alerts))
	for _, alert := range alerts {
		env, err := securityalerts.NewGitHubDependabotAlertEnvelope(
			securityalerts.EnvelopeContext{
				ScopeID:             target.ScopeID,
				GenerationID:        item.GenerationID,
				CollectorInstanceID: s.collectorInstanceID,
				FencingToken:        item.CurrentFencingToken,
				ObservedAt:          observedAt,
				SourceURI:           safeSourceURI(firstNonBlank(target.SourceURI, alert.HTMLURL)),
			},
			alert,
		)
		if err != nil {
			return nil, err
		}
		if env.FactKind != facts.SecurityAlertRepositoryAlertFactKind {
			return nil, fmt.Errorf("security alert runtime refusing unsupported fact_kind %q", env.FactKind)
		}
		annotateRepositoryAlertCollectionCoverage(env.Payload, coverage)
		envs = append(envs, env)
	}
	return envs, nil
}

func repositoryAlertCollectionCoverageFromResult(
	result securityalerts.GitHubDependabotAlertResult,
) repositoryAlertCollectionCoverage {
	pagesFetched := result.PagesFetched
	if pagesFetched <= 0 && len(result.Alerts) > 0 {
		pagesFetched = 1
	}
	return repositoryAlertCollectionCoverage{
		pagesFetched: pagesFetched,
		truncated:    result.Truncated,
	}
}

func annotateRepositoryAlertCollectionCoverage(
	payload map[string]any,
	coverage repositoryAlertCollectionCoverage,
) {
	payload["source_freshness"] = coverage.sourceFreshness()
	payload["collection_coverage_state"] = coverage.coverageState()
	payload["collection_truncated"] = coverage.truncated
	payload["collection_pages_fetched"] = int64(coverage.pagesFetched)
	payload["collection_state_filter"] = collectionOpenStateFilter
	if reasons := coverage.incompleteReasons(); len(reasons) > 0 {
		payload["collection_incomplete_reasons"] = reasons
	}
}

func (c repositoryAlertCollectionCoverage) sourceFreshness() string {
	if c.truncated {
		return securityAlertSourceFreshnessPartial
	}
	return securityAlertSourceFreshnessActive
}

func (c repositoryAlertCollectionCoverage) coverageState() string {
	if c.truncated {
		return collectionCoverageIncomplete
	}
	return collectionCoverageComplete
}

func (c repositoryAlertCollectionCoverage) incompleteReasons() []string {
	if !c.truncated {
		return nil
	}
	return []string{collectionOpenPageLimitReachedReason}
}

func ingestionScope(target TargetConfig) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       target.ScopeID,
		SourceSystem:  string(scope.CollectorSecurityAlert),
		ScopeKind:     scope.KindSecurityAlert,
		CollectorKind: scope.CollectorSecurityAlert,
		PartitionKey:  target.Provider,
		Metadata: map[string]string{
			"provider": target.Provider,
		},
	}
}

func (s *ClaimedSource) startObserve(ctx context.Context, target TargetConfig) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanSecurityAlertObserve, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionProvider, target.Provider),
	))
}

func (s *ClaimedSource) startFetch(ctx context.Context) (context.Context, trace.Span) {
	if s.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return s.tracer.Start(ctx, telemetry.SpanSecurityAlertFetch)
}

func recordSpanError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func safeSourceURI(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func sensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"key", "password", "passwd", "secret", "sig", "signature", "token":
		return true
	default:
		return false
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
