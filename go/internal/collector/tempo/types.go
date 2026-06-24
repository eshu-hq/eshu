// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// CollectorKind is the durable collector family identifier for live Tempo
	// trace-signal metadata.
	CollectorKind = "tempo"
	// ProviderTempo identifies Grafana Tempo API evidence.
	ProviderTempo = "tempo"
	// ScopeKindTraceSource identifies one configured Tempo API target.
	ScopeKindTraceSource = "trace_source"

	// SourceClassObserved marks live provider evidence.
	SourceClassObserved = "observed"
	// SourceKindTempo marks Tempo provider evidence.
	SourceKindTempo = "tempo"

	// RedactionVersion is the metadata-only Tempo redaction policy.
	RedactionVersion = "observability-live-tempo-v1"
)

// FailureClass is the bounded provider failure label stored in workflow status
// and telemetry.
type FailureClass = string

const (
	// FailureAuthDenied marks missing or unauthorized Tempo credentials.
	FailureAuthDenied FailureClass = string(sdk.FailureAuthDenied)
	// FailureRateLimited marks Tempo rate limiting as retryable.
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
	// ResourceClassTraceSignal marks a Tempo trace-signal observation.
	ResourceClassTraceSignal = "trace_signal"
	// SignalKindTagSet marks discovered Tempo tag names.
	SignalKindTagSet = "tag_set"
	// SignalKindTagValues marks bounded Tempo tag-value metadata.
	SignalKindTagValues = "tag_values"
	// SignalKindFreshnessProbe marks Tempo query-frontend readiness metadata.
	SignalKindFreshnessProbe = "freshness_probe"
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
	// WarningHighCardinality marks rejected high-cardinality tag values.
	WarningHighCardinality = "high_cardinality_rejected"
)

// EnvelopeContext carries durable fact-envelope identity for Tempo facts.
type EnvelopeContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceInstanceID    string
}

// SourceInstance is one bounded Tempo source observation.
type SourceInstance struct {
	Provider          string
	SourceInstanceID  string
	TenantPresent     bool
	TenantFingerprint string
	TenantRedacted    bool
}

// TraceSignal is one bounded live Tempo tag, tag-value, or freshness
// observation.
type TraceSignal struct {
	ProviderObjectID   string
	SignalKind         string
	TagScope           string
	TagName            string
	TagKeys            []string
	TagValueCount      int
	TagValueHashes     []string
	Query              string
	QueryRedacted      bool
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
	PagesFetched            int
	Signals                 int
	Warnings                int
	Redactions              int
	RateLimits              int
	Retries                 int
	Stale                   int
	HighCardinalityRejected int
	Partial                 bool
}

// CollectionResult is one bounded Tempo metadata snapshot.
type CollectionResult struct {
	Source     SourceInstance
	Signals    []TraceSignal
	Warnings   []Warning
	ObservedAt time.Time
	Stats      CollectionStats
}

// EvidenceClient fetches bounded Tempo metadata for one target.
type EvidenceClient interface {
	CollectObservedMetadata(context.Context, TargetConfig) (CollectionResult, error)
}

// ClientFactory builds a Tempo evidence client for a validated target.
type ClientFactory func(TargetConfig) (EvidenceClient, error)

// SourceConfig configures one claim-driven Tempo source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	ClientFactory       ClientFactory
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig describes one Tempo API target.
type TargetConfig struct {
	ScopeID              string
	InstanceID           string
	BaseURL              string
	PathPrefix           string
	Token                string
	TenantID             string
	ResourceLimit        int
	TagValueNames        []string
	MaxTagValuesPerTag   int
	StaleAfter           time.Duration
	Enabled              bool
	DeclaredIDs          map[string]struct{}
	ObservedOnlyHint     bool
	FreshnessProbeEnable bool
	Lookback             time.Duration
	Now                  func() time.Time
}

// HTTPClientConfig configures the bounded Tempo REST client.
type HTTPClientConfig struct {
	BaseURL string
	Client  HTTPDoer
}

// HTTPDoer is the subset of http.Client used by the Tempo client.
type HTTPDoer = sdk.HTTPDoer
