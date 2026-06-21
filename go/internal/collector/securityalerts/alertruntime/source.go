package alertruntime

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	// ProviderGitHubDependabot selects GitHub repository Dependabot alerts.
	ProviderGitHubDependabot = "github_dependabot"

	// TargetScopeRepository polls one repository via the per-repository
	// Dependabot alerts endpoint. It is the default when scope is unset.
	TargetScopeRepository = "repository"
	// TargetScopeOrganization polls one organization via the org-wide
	// Dependabot alerts endpoint and fans the result out into per-repository
	// facts so reducer reconciliation is identical to the per-repository path.
	TargetScopeOrganization = "org"

	// FailureAuthDenied marks credential failures as terminal.
	FailureAuthDenied = string(sdk.FailureAuthDenied)
	// FailureNotFound marks missing repositories or disabled alert surfaces.
	FailureNotFound = string(sdk.FailureNotFound)
	// FailureRateLimited marks provider rate limiting as retryable.
	FailureRateLimited = string(sdk.FailureRateLimited)
	// FailureRetryable marks transient transport/provider failures.
	FailureRetryable = string(sdk.FailureRetryable)
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal = string(sdk.FailureTerminal)
)

const (
	securityAlertSourceFreshnessActive  = "active"
	securityAlertSourceFreshnessPartial = "partial"

	collectionCoverageComplete   = "complete"
	collectionCoverageIncomplete = "incomplete"

	collectionOpenStateFilter            = "open"
	collectionOpenPageLimitReachedReason = "provider_open_alert_page_limit_reached"
	providerAccessPreflightMaxPages      = 1
)

// RepositoryAlertClient fetches provider alerts for one target. It serves both
// the per-repository endpoint (one repo per request) and the organization-wide
// endpoint (one request fanned out into per-repository facts).
type RepositoryAlertClient interface {
	ListRepositoryAlertsPages(
		context.Context,
		string,
		int,
	) (securityalerts.GitHubDependabotAlertResult, error)
	ListOrganizationAlertsPages(
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

// TargetConfig describes one provider security-alert target. A repository
// target (Scope=="repository", the default) polls a single allowlisted
// repository. An organization target (Scope=="org") polls one organization and
// fans the result out into per-repository facts.
type TargetConfig struct {
	Provider             string
	Scope                string
	ScopeID              string
	Repository           string
	Organization         string
	Token                string
	AllowedRepositories  []string
	APIBaseURL           string
	RepositoryAlertLimit int
	MaxPages             int
	SourceURI            string
}

// IsOrganizationScope reports whether the target polls the organization-wide
// Dependabot alerts endpoint.
func (t TargetConfig) IsOrganizationScope() bool {
	return t.Scope == TargetScopeOrganization
}

type targetRuntime struct {
	config TargetConfig
	client RepositoryAlertClient
}

type repositoryAlertCollectionCoverage struct {
	pagesFetched int
	truncated    bool
}

// PreflightResult summarizes a bounded provider-access preflight without
// exposing provider target identifiers.
type PreflightResult struct {
	TargetCount int
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

// PreflightProviderAccess verifies each configured provider target with one
// bounded request before workflow claims are processed.
func (s *ClaimedSource) PreflightProviderAccess(ctx context.Context) (PreflightResult, error) {
	result := PreflightResult{TargetCount: len(s.targets)}
	targets := s.sortedTargets()
	for _, target := range targets {
		startedAt := time.Now()
		observeCtx, observeSpan := s.startObserve(ctx, target.config)
		fetchCtx, fetchSpan := s.startFetch(observeCtx)
		_, err := fetchAlerts(fetchCtx, target, providerAccessPreflightMaxPages)
		if err != nil {
			failure := classifiedProviderFailure(err)
			s.recordFetch(observeCtx, target.config, failure.FailureClass(), startedAt)
			s.recordRateLimit(observeCtx, target.config, failure)
			recordSpanError(fetchSpan, failure)
			recordSpanError(observeSpan, failure)
			fetchSpan.End()
			observeSpan.End()
			return result, failure
		}
		fetchSpan.End()
		s.recordFetch(observeCtx, target.config, "success", startedAt)
		observeSpan.End()
	}
	return result, nil
}

func (s *ClaimedSource) sortedTargets() []targetRuntime {
	scopeIDs := make([]string, 0, len(s.targets))
	for scopeID := range s.targets {
		scopeIDs = append(scopeIDs, scopeID)
	}
	sort.Strings(scopeIDs)
	targets := make([]targetRuntime, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		targets = append(targets, s.targets[scopeID])
	}
	return targets
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
	result, err := fetchAlerts(fetchCtx, target, target.config.MaxPages)
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

// fetchAlerts requests provider alerts for one target using the endpoint that
// matches the target scope. Organization targets use the org-wide endpoint;
// repository targets use the per-repository endpoint.
func fetchAlerts(
	ctx context.Context,
	target targetRuntime,
	maxPages int,
) (securityalerts.GitHubDependabotAlertResult, error) {
	if target.config.IsOrganizationScope() {
		return target.client.ListOrganizationAlertsPages(ctx, target.config.Organization, maxPages)
	}
	return target.client.ListRepositoryAlertsPages(ctx, target.config.Repository, maxPages)
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
		scopeID := target.ScopeID
		if target.IsOrganizationScope() {
			// One org request fans out into per-repository facts. Each fact is
			// scoped to the repository that owns the alert so reducer
			// reconciliation keys on the same repository_id as the
			// per-repository path. Alerts whose repository cannot be resolved
			// are skipped rather than misattributed to the org scope.
			scopeID = organizationAlertScopeID(alert)
			if scopeID == "" {
				continue
			}
		}
		env, err := securityalerts.NewGitHubDependabotAlertEnvelope(
			securityalerts.EnvelopeContext{
				ScopeID:             scopeID,
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
		if target.IsOrganizationScope() {
			if name := organizationAlertRepositoryName(alert); name != "" {
				env.Payload["repository_name"] = name
			}
		}
		annotateRepositoryAlertCollectionCoverage(env.Payload, coverage)
		envs = append(envs, env)
	}
	return envs, nil
}

// organizationAlertScopeID derives the canonical per-repository security-alert
// scope ID (security-alert:github:<owner>/<repo>) from an organization alert's
// repository object. It returns "" when the repository cannot be resolved.
func organizationAlertScopeID(alert securityalerts.GitHubDependabotAlert) string {
	fullName := normalizeOrganizationAlertRepositoryFullName(alert)
	if fullName == "" {
		return ""
	}
	return "security-alert:github:" + fullName
}

// organizationAlertRepositoryName returns the short repository name for an
// organization alert, used to populate the repository_name payload field that
// the per-repository path leaves for the reducer to derive.
func organizationAlertRepositoryName(alert securityalerts.GitHubDependabotAlert) string {
	if name := strings.TrimSpace(alert.Repository.Name); name != "" {
		return name
	}
	fullName := normalizeOrganizationAlertRepositoryFullName(alert)
	if slash := strings.LastIndex(fullName, "/"); slash >= 0 && slash+1 < len(fullName) {
		return fullName[slash+1:]
	}
	return ""
}

func normalizeOrganizationAlertRepositoryFullName(alert securityalerts.GitHubDependabotAlert) string {
	fullName := strings.ToLower(strings.Trim(strings.TrimSpace(alert.Repository.FullName), "/"))
	if fullName != "" {
		if strings.Count(fullName, "/") == 1 {
			return fullName
		}
		return ""
	}
	owner := strings.ToLower(strings.TrimSpace(alert.Repository.Owner.Login))
	name := strings.ToLower(strings.TrimSpace(alert.Repository.Name))
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
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
		attribute.String(telemetry.AttrSecurityAlertTargetScope, targetScopeLabel(target)),
	))
}

func targetScopeLabel(target TargetConfig) string {
	if target.IsOrganizationScope() {
		return TargetScopeOrganization
	}
	return TargetScopeRepository
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
