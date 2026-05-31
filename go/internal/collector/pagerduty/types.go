package pagerduty

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// ProviderPagerDuty identifies PagerDuty source evidence.
	ProviderPagerDuty = "pagerduty"
)

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
	ObservedAt          time.Time
	PagesFetched        int
	Truncated           bool
}

// EvidenceClient fetches PagerDuty incident evidence for one target and time
// window.
type EvidenceClient interface {
	CollectIncidentEvidence(context.Context, TargetConfig, CollectionWindow) (CollectionResult, error)
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
	Provider          string
	ScopeID           string
	AccountID         string
	Token             string
	APIBaseURL        string
	SourceURI         string
	IncidentLimit     int
	IncidentLookback  time.Duration
	LogEntryLimit     int
	ChangeEventLimit  int
	AllowedServiceIDs []string
}
