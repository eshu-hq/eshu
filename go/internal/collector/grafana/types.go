// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// CollectorKind is the durable collector family identifier for live
	// Grafana metadata facts.
	CollectorKind = "grafana"
	// ProviderGrafana identifies Grafana API evidence.
	ProviderGrafana = "grafana"
	// ScopeKindGrafanaInstance identifies one configured Grafana API target.
	ScopeKindGrafanaInstance = "grafana_instance"

	// SourceClassObserved marks live provider evidence.
	SourceClassObserved = "observed"
	// SourceKindGrafana marks Grafana provider evidence.
	SourceKindGrafana = "grafana"

	// RedactionVersion is the metadata-only live Grafana redaction policy.
	RedactionVersion = "observability-live-grafana-v1"
)

const (
	// FailureRetryable marks transient provider or transport failures.
	FailureRetryable = string(sdk.FailureRetryable)
	// FailureRateLimited marks provider throttling as retryable.
	FailureRateLimited = string(sdk.FailureRateLimited)
	// FailureAuthDenied marks authentication or authorization failures.
	FailureAuthDenied = string(sdk.FailureAuthDenied)
	// FailureTerminal marks malformed or unsupported terminal failures.
	FailureTerminal = string(sdk.FailureTerminal)
)

// GrafanaError carries a bounded HTTP provider failure.
type GrafanaError = sdk.HTTPError

// ProviderFailure wraps a Grafana failure with a bounded failure class.
type ProviderFailure = sdk.ProviderFailure

const (
	// ResourceClassFolder marks a Grafana folder resource.
	ResourceClassFolder = "folder"
	// ResourceClassDashboard marks a Grafana dashboard resource.
	ResourceClassDashboard = "dashboard"
	// ResourceClassDatasource marks a Grafana datasource resource.
	ResourceClassDatasource = "datasource"
	// ResourceClassAlertRule marks a Grafana alert rule resource.
	ResourceClassAlertRule = "alert_rule"
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
	// MatchStateMatchedDeclared means a live UID has a declared counterpart.
	MatchStateMatchedDeclared = "matched_declared"
)

const (
	// DriftReasonManualProviderResource marks resources likely created in the UI.
	DriftReasonManualProviderResource = "manual_provider_resource"
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

// EnvelopeContext carries durable fact-envelope identity for Grafana facts.
type EnvelopeContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
	SourceInstanceID    string
}

// Resource is one bounded live Grafana folder, dashboard, or datasource.
type Resource struct {
	Class              string
	ID                 int64
	UID                string
	Title              string
	Name               string
	FolderUID          string
	DatasourceType     string
	URL                string
	URLRedacted        bool
	UpdatedAt          time.Time
	ManuallyCreated    bool
	DriftReason        string
	DeclaredMatchState string
	FreshnessState     string
	Outcome            string
}

// AlertRule is one bounded live Grafana alert-rule observation.
type AlertRule struct {
	UID                     string
	Title                   string
	RuleGroup               string
	FolderUID               string
	DatasourceUID           string
	Condition               string
	Model                   map[string]any
	UpdatedAt               time.Time
	For                     string
	NoDataState             string
	ExecErrState            string
	ContactPoint            string
	NotificationURL         string
	QueryModelRedacted      bool
	ContactPointRedacted    bool
	NotificationURLRedacted bool
	DeclaredMatchState      string
	FreshnessState          string
	Outcome                 string
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
	Resources    int
	Rules        int
	Warnings     int
	Redactions   int
	RateLimits   int
	Retries      int
	Partial      bool
}

// CollectionResult is one bounded Grafana metadata snapshot.
type CollectionResult struct {
	Resources  []Resource
	Rules      []AlertRule
	Warnings   []Warning
	ObservedAt time.Time
	Stats      CollectionStats
}

// EvidenceClient fetches bounded Grafana metadata for one target.
type EvidenceClient interface {
	CollectObservedMetadata(context.Context, TargetConfig) (CollectionResult, error)
}

// ClientFactory builds a Grafana evidence client for a validated target.
type ClientFactory func(TargetConfig) (EvidenceClient, error)

// SourceConfig configures one claim-driven Grafana source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	ClientFactory       ClientFactory
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig describes one Grafana API target.
type TargetConfig struct {
	Provider         string
	ScopeID          string
	InstanceID       string
	BaseURL          string
	Token            string
	ResourceLimit    int
	StaleAfter       time.Duration
	Enabled          bool
	DeclaredUIDs     map[string]struct{}
	ObservedOnlyHint bool
}

// HTTPClientConfig configures the bounded Grafana REST client.
type HTTPClientConfig struct {
	BaseURL string
	Token   string
	Client  HTTPDoer
}

// HTTPDoer is the subset of *http.Client used by HTTPClient.
type HTTPDoer = sdk.HTTPDoer
