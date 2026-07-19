// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// ProviderPagerDuty identifies PagerDuty source evidence.
	ProviderPagerDuty = "pagerduty"

	// ConfigSourceClassObserved marks live PagerDuty configuration evidence.
	ConfigSourceClassObserved = "observed"
	// ConfigSourceKindPagerDutyAPI marks PagerDuty REST API config evidence.
	ConfigSourceKindPagerDutyAPI = "pagerduty_api"

	// ConfigResourceClassService marks a PagerDuty service resource.
	ConfigResourceClassService = "service"
	// ConfigResourceClassServiceIntegration marks a PagerDuty integration.
	ConfigResourceClassServiceIntegration = "service_integration"
	// ConfigResourceClassRelatedChangeEvent marks related change-event
	// enrichment coverage for a PagerDuty incident.
	ConfigResourceClassRelatedChangeEvent = "related_change_event"
	// ConfigResourceClassIncident marks the top-level incident list resource.
	ConfigResourceClassIncident = "incident"
	// ConfigResourceClassLogEntry marks incident lifecycle log-entry coverage.
	ConfigResourceClassLogEntry = "log_entry"

	// ConfigMatchStateNotCompared means reducer comparison has not run.
	ConfigMatchStateNotCompared = "not_compared"

	// ConfigWarningMissing marks a configured resource missing from live state.
	ConfigWarningMissing = "missing"
	// ConfigWarningPermissionHidden marks a resource hidden by permissions.
	ConfigWarningPermissionHidden = "permission_hidden"
	// ConfigWarningUnsupported marks unsupported live configuration collection.
	ConfigWarningUnsupported = "unsupported"
	// ConfigWarningPartial marks an incomplete live configuration read.
	ConfigWarningPartial = "partial"
	// ConfigWarningTruncated marks a resource list collection that stopped at
	// a configured page/record pagination bound while the provider still had
	// more pages available (its "more" field was true). It is only ever
	// emitted when the bound was actually hit, never when pagination
	// naturally exhausted "more".
	ConfigWarningTruncated = "truncated"
)

const (
	// FailureAuthDenied marks PagerDuty credential or permission failures as terminal.
	FailureAuthDenied = string(sdk.FailureAuthDenied)
	// FailureNotFound marks missing PagerDuty resources as terminal.
	FailureNotFound = string(sdk.FailureNotFound)
	// FailureRateLimited marks PagerDuty rate limiting as retryable.
	FailureRateLimited = string(sdk.FailureRateLimited)
	// FailureRetryable marks transient PagerDuty transport/provider failures.
	FailureRetryable = string(sdk.FailureRetryable)
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal = string(sdk.FailureTerminal)
)

// PagerDutyError carries bounded PagerDuty HTTP failure details.
type PagerDutyError = sdk.HTTPError

// ProviderFailure is a bounded PagerDuty failure returned to claim handling.
type ProviderFailure = sdk.ProviderFailure

// EnvelopeContext carries durable fact-envelope identity for PagerDuty facts.
type EnvelopeContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// Reference is the bounded PagerDuty reference shape kept in source facts.
type Reference struct {
	ID      string
	Type    string
	Summary string
	HTMLURL string
}

// Link is a provider link attached to a PagerDuty change event.
type Link struct {
	Href string
	Text string
}

// Incident is one PagerDuty incident normalized before envelope emission.
type Incident struct {
	ID             string
	IncidentNumber int64
	Title          string
	Status         string
	Urgency        string
	Priority       Reference
	Service        Reference
	Escalation     Reference
	Teams          []Reference
	Assignments    []Reference
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ResolvedAt     time.Time
	HTMLURL        string
}

// LifecycleEvent is one PagerDuty incident log entry normalized as timeline
// evidence.
type LifecycleEvent struct {
	ID         string
	IncidentID string
	Type       string
	Actor      Reference
	Channel    string
	Summary    string
	CreatedAt  time.Time
	HTMLURL    string
}

// ChangeEvent is one PagerDuty related change event normalized as provider
// change evidence.
type ChangeEvent struct {
	ID        string
	Summary   string
	Source    string
	Services  []Reference
	Links     []Link
	Timestamp time.Time
	HTMLURL   string
}

// ConfigService is one live PagerDuty service normalized before envelope
// emission.
type ConfigService struct {
	ID              string
	Summary         string
	Status          string
	AlertCreation   string
	Escalation      Reference
	Teams           []Reference
	CreatedAt       time.Time
	UpdatedAt       time.Time
	HTMLURL         string
	Disabled        bool
	Deleted         bool
	MatchState      string
	ManuallyCreated bool
	DriftReason     string
}

// ConfigIntegration is one live PagerDuty service integration normalized
// before envelope emission.
type ConfigIntegration struct {
	ID                 string
	ServiceID          string
	Summary            string
	Type               string
	VendorID           string
	HTMLURL            string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Disabled           bool
	Deleted            bool
	MatchState         string
	ManuallyCreated    bool
	DriftReason        string
	RoutingKey         string
	RoutingKeyRedacted bool
}

// ConfigWarning is one bounded PagerDuty coverage warning.
type ConfigWarning struct {
	ResourceClass string
	ResourceID    string
	Reason        string
}

// CollectionWindow bounds one PagerDuty provider read.
type CollectionWindow struct {
	Since time.Time
	Until time.Time
}

// CollectionResult is one PagerDuty evidence snapshot for a target.
type CollectionResult struct {
	Incidents           []Incident
	LifecycleEvents     map[string][]LifecycleEvent
	RelatedChangeEvents map[string][]ChangeEvent
	Warnings            []ConfigWarning
	ObservedAt          time.Time
	PagesFetched        int
	Truncated           bool
}

// ConfigCollectionResult is one optional live PagerDuty configuration
// observation for a target.
type ConfigCollectionResult struct {
	Services     []ConfigService
	Integrations []ConfigIntegration
	Warnings     []ConfigWarning
	ObservedAt   time.Time
	PagesFetched int
	Partial      bool
	Redactions   int
	// Truncated is true only when a configured pagination bound (max pages or
	// max records) stopped a service or integration list fetch while the
	// provider still had more pages available. It stays false when
	// pagination exhausted the provider's "more" signal naturally.
	Truncated bool
}

// EvidenceClient fetches PagerDuty incident evidence for one target and time
// window.
type EvidenceClient interface {
	CollectIncidentEvidence(context.Context, TargetConfig, CollectionWindow) (CollectionResult, error)
}

// ConfigEvidenceClient fetches optional live PagerDuty configuration evidence
// for one target.
type ConfigEvidenceClient interface {
	CollectConfigEvidence(context.Context, TargetConfig) (ConfigCollectionResult, error)
}

// ClientFactory builds a PagerDuty evidence client for a validated target.
type ClientFactory func(TargetConfig) (EvidenceClient, error)

// SourceConfig configures one claim-driven PagerDuty source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	ClientFactory       ClientFactory
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig describes one PagerDuty account or service allowlist target.
type TargetConfig struct {
	Provider                string
	ScopeID                 string
	AccountID               string
	Token                   string
	APIBaseURL              string
	SourceURI               string
	IncidentLimit           int
	IncidentLookback        time.Duration
	LogEntryLimit           int
	ChangeEventLimit        int
	AllowedServiceIDs       []string
	ConfigValidationEnabled bool
	ConfigResourceLimit     int
	PaginationMaxPages      int
	PaginationMaxRecords    int
}
