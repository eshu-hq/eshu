// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// CollectorKind is the durable collector family identifier for live
	// Prometheus-compatible metric metadata.
	CollectorKind = "prometheus_mimir"
	// ProviderPrometheus identifies Prometheus API evidence.
	ProviderPrometheus = "prometheus"
	// ProviderMimir identifies Grafana Mimir API evidence.
	ProviderMimir = "mimir"
	// ScopeKindMetricSource identifies one configured metric API target.
	ScopeKindMetricSource = "metric_source"

	// SourceClassObserved marks live provider evidence.
	SourceClassObserved = "observed"
	// SourceKindPrometheus marks Prometheus provider evidence.
	SourceKindPrometheus = "prometheus"
	// SourceKindMimir marks Mimir provider evidence.
	SourceKindMimir = "mimir"

	// RedactionVersion is the metadata-only Prometheus/Mimir redaction policy.
	RedactionVersion = "observability-live-prometheus-mimir-v1"
)

// FailureClass is the bounded provider failure label stored in workflow status
// and telemetry.
type FailureClass = string

const (
	// FailureAuthDenied marks missing or unauthorized metric credentials.
	FailureAuthDenied FailureClass = string(sdk.FailureAuthDenied)
	// FailureRateLimited marks provider throttling as retryable.
	FailureRateLimited FailureClass = string(sdk.FailureRateLimited)
	// FailureRetryable marks transport or server-side failures as retryable.
	FailureRetryable FailureClass = string(sdk.FailureRetryable)
	// FailureTerminal marks malformed provider responses or unsupported setup.
	FailureTerminal FailureClass = string(sdk.FailureTerminal)
)

// ProviderFailure carries a bounded failure class without provider response
// bodies or credential-bearing request details.
type ProviderFailure = sdk.ProviderFailure

// ProviderHTTPError describes a bounded non-success HTTP response.
type ProviderHTTPError = sdk.HTTPError

const (
	// ResourceClassTarget marks a Prometheus scrape target observation.
	ResourceClassTarget = "target"
	// ResourceClassRule marks a Prometheus-compatible rule observation.
	ResourceClassRule = "rule"
	// RuleTypeAlerting marks an alerting rule.
	RuleTypeAlerting = "alerting"
	// RuleTypeRecording marks a recording rule.
	RuleTypeRecording = "recording"
)

const (
	// FreshnessCurrent marks a live response observed during the current scan.
	FreshnessCurrent = "current"
	// FreshnessUnknown marks a source-local freshness gap.
	FreshnessUnknown = "unknown"
	// FreshnessStale marks live evidence older than the configured freshness window.
	FreshnessStale = "stale"
	// FreshnessPermissionHidden marks provider data hidden by credentials.
	FreshnessPermissionHidden = "permission_hidden"
)

const (
	// OutcomeObserved marks live provider evidence that was accepted.
	OutcomeObserved = "observed"
	// OutcomePartial marks an incomplete source-local read.
	OutcomePartial = "partial"
	// OutcomePermissionHidden marks evidence hidden by provider permissions.
	OutcomePermissionHidden = "permission_hidden"
	// OutcomeUnsupported marks a provider endpoint outside the supported API.
	OutcomeUnsupported = "unsupported"
	// OutcomeRejected marks malformed or unsafe source data.
	OutcomeRejected = "rejected"
	// OutcomeStale marks accepted evidence outside the configured freshness window.
	OutcomeStale = "stale"
)

const (
	// MatchStateNotCompared means reducer comparison has not run.
	MatchStateNotCompared = "not_compared"
	// MatchStateMatchedDeclared means a live identity has a declared counterpart.
	MatchStateMatchedDeclared = "matched_declared"
)

const (
	// WarningManualProviderResource marks manual provider-only resources.
	WarningManualProviderResource = "manual_provider_resource"
	// WarningPermissionHidden marks permission-hidden provider data.
	WarningPermissionHidden = "permission_hidden"
	// WarningRateLimited marks provider rate limiting.
	WarningRateLimited = "rate_limited"
	// WarningUnsupported marks an unsupported provider endpoint.
	WarningUnsupported = "unsupported"
	// WarningPartial marks a partial read.
	WarningPartial = "partial"
	// WarningStale marks provider evidence older than the freshness window.
	WarningStale = "stale"
)

// EnvelopeContext carries durable fact-envelope identity for metric facts.
type EnvelopeContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceInstanceID    string
	SourceKind          string
}

// SourceInstance is one bounded Prometheus-compatible source observation.
type SourceInstance struct {
	Provider          string
	SourceInstanceID  string
	TenantPresent     bool
	TenantFingerprint string
	TenantRedacted    bool
}

// Target is one bounded live Prometheus scrape target observation.
type Target struct {
	ProviderObjectID   string
	ScrapePool         string
	Health             string
	ScrapeURL          string
	ScrapeURLRedacted  bool
	LabelKeys          []string
	DiscoveredKeys     []string
	LastScrapeAt       time.Time
	LastErrorRedacted  bool
	DeclaredMatchState string
	FreshnessState     string
	Outcome            string
	ManuallyCreated    bool
}

// Rule is one bounded live Prometheus-compatible rule observation.
type Rule struct {
	ProviderObjectID   string
	GroupName          string
	RuleName           string
	RuleType           string
	Health             string
	Query              string
	QueryRedacted      bool
	LabelKeys          []string
	AnnotationKeys     []string
	LastEvaluationAt   time.Time
	DeclaredMatchState string
	FreshnessState     string
	Outcome            string
	ManuallyCreated    bool
}

// Warning is one bounded coverage or redaction warning.
type Warning struct {
	ResourceClass string
	ResourceID    string
	Reason        string
}

// CollectionStats carries bounded counters for telemetry and tests.
type CollectionStats struct {
	PagesFetched int
	Targets      int
	Rules        int
	Warnings     int
	Redactions   int
	RateLimits   int
	Retries      int
	Stale        int
	Partial      bool
}

// CollectionResult is one bounded Prometheus-compatible metadata snapshot.
type CollectionResult struct {
	Source     SourceInstance
	Targets    []Target
	Rules      []Rule
	Warnings   []Warning
	ObservedAt time.Time
	Stats      CollectionStats
}

// EvidenceClient fetches bounded metric metadata for one target.
type EvidenceClient interface {
	CollectObservedMetadata(context.Context, TargetConfig) (CollectionResult, error)
}

// ClientFactory builds a metric evidence client for a validated target.
type ClientFactory func(TargetConfig) (EvidenceClient, error)

// SourceConfig configures one claim-driven metric source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	ClientFactory       ClientFactory
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig describes one Prometheus-compatible API target.
type TargetConfig struct {
	Provider         string
	ScopeID          string
	InstanceID       string
	BaseURL          string
	PathPrefix       string
	Token            string
	TenantID         string
	ResourceLimit    int
	StaleAfter       time.Duration
	Enabled          bool
	DeclaredIDs      map[string]struct{}
	ObservedOnlyHint bool
}

// HTTPClientConfig configures the bounded Prometheus-compatible REST client.
type HTTPClientConfig struct {
	BaseURL string
	Client  HTTPDoer
}

// HTTPDoer is the subset of *http.Client used by HTTPClient.
type HTTPDoer = sdk.HTTPDoer
