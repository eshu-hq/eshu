// Package jira collects Jira work-item source evidence.
package jira

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// CollectorKind is the durable collector family name for Jira facts.
	CollectorKind = "jira"
	// ProviderJiraCloud selects Jira Cloud REST API collection.
	ProviderJiraCloud = "jira_cloud"
)

// EnvelopeContext carries Eshu fact boundary fields for one Jira observation.
type EnvelopeContext struct {
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// Reference is a bounded Jira identity/name object used in issue and user
// fields.
type Reference struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

// Issue is the Jira issue subset normalized by Eshu.
type Issue struct {
	ID         string
	Key        string
	Summary    string
	IssueType  Reference
	Status     Reference
	Project    Reference
	Assignee   Reference
	Reporter   Reference
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt time.Time
	Self       string
	BrowseURL  string
}

// Transition is one Jira changelog item normalized as lifecycle evidence.
type Transition struct {
	ID        string
	IssueID   string
	IssueKey  string
	Field     string
	From      string
	To        string
	Author    Reference
	CreatedAt time.Time
}

// LinkApplication identifies the external system attached to a Jira issue.
type LinkApplication struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// LinkObject identifies the remote object attached to a Jira issue.
type LinkObject struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// ExternalLink is one Jira remote issue link normalized as cross-system
// evidence.
type ExternalLink struct {
	ID           string
	IssueID      string
	IssueKey     string
	GlobalID     string
	Application  LinkApplication
	Relationship string
	Object       LinkObject
}

// CollectionWindow bounds one Jira updated-window collection.
type CollectionWindow struct {
	Since time.Time
	Until time.Time
}

// CollectionResult is one bounded Jira work-item evidence fetch.
type CollectionResult struct {
	Issues        []Issue
	Transitions   map[string][]Transition
	ExternalLinks map[string][]ExternalLink
	ObservedAt    time.Time
}

// EvidenceClient fetches Jira work-item evidence for one target and time
// window.
type EvidenceClient interface {
	CollectWorkItemEvidence(context.Context, TargetConfig, CollectionWindow) (CollectionResult, error)
}

// ClientFactory builds one Jira evidence client for a validated target.
type ClientFactory func(TargetConfig) (EvidenceClient, error)

// SourceConfig configures one claim-driven Jira source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	ClientFactory       ClientFactory
	Now                 func() time.Time
	Tracer              trace.Tracer
	Instruments         *telemetry.Instruments
}

// TargetConfig describes one Jira Cloud site target.
type TargetConfig struct {
	Provider        string
	ScopeID         string
	SiteID          string
	BaseURL         string
	Email           string
	Token           string
	JQL             string
	IssueLimit      int
	UpdatedLookback time.Duration
	ChangelogLimit  int
	RemoteLinkLimit int
}
