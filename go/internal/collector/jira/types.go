// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package jira collects Jira work-item source evidence.
package jira

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// CollectorKind is the durable collector family name for Jira facts.
	CollectorKind = "jira"
	// ProviderJiraCloud selects Jira Cloud REST API collection.
	ProviderJiraCloud = "jira_cloud"
)

// FailureClass is a bounded workflow retry class for Jira provider failures.
type FailureClass = string

const (
	// FailurePermissionHidden marks Jira permission or issue-security denials.
	FailurePermissionHidden FailureClass = string(sdk.FailurePermissionHidden)
	// FailureDeleted marks missing or deleted Jira issues and sites.
	FailureDeleted FailureClass = string(sdk.FailureDeleted)
	// FailureArchived marks archived Jira projects or issues.
	FailureArchived FailureClass = string(sdk.FailureArchived)
	// FailureRateLimited marks Jira rate limiting as retryable.
	FailureRateLimited FailureClass = string(sdk.FailureRateLimited)
	// FailureRetryable marks transient transport or provider failures.
	FailureRetryable FailureClass = string(sdk.FailureRetryable)
	// FailureTerminal marks malformed or otherwise non-retryable failures.
	FailureTerminal FailureClass = string(sdk.FailureTerminal)
)

// JiraError is a bounded provider error. It deliberately omits tokens, URLs,
// and raw response bodies from Error.
type JiraError = sdk.HTTPError

// ProviderFailure is a bounded Jira provider failure returned to claim
// handling.
type ProviderFailure = sdk.ProviderFailure

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
	ID             string
	IssueID        string
	IssueKey       string
	Field          string
	From           string
	To             string
	Author         Reference
	CreatedAt      time.Time
	ValueRedacted  bool
	AuthorRedacted bool
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
	ID                   string
	IssueID              string
	IssueKey             string
	GlobalID             string
	Application          LinkApplication
	Relationship         string
	Object               LinkObject
	URLFingerprint       string
	URLRedacted          bool
	ProviderSupportState string
}

// MetadataScope identifies whether Jira metadata is global or scoped to a
// project.
type MetadataScope struct {
	Type      string
	ProjectID string
}

// ProjectMetadata is one Jira project definition normalized for source
// evidence without private project names or URLs.
type ProjectMetadata struct {
	ID              string
	Key             string
	Name            string
	TypeKey         string
	CategoryID      string
	CategoryName    string
	Style           string
	Archived        bool
	Deleted         bool
	Self            string
	LastIssueUpdate time.Time
	IssueCount      int
}

// IssueTypeMetadata is one Jira issue-type definition normalized for work-item
// context.
type IssueTypeMetadata struct {
	ID             string
	Name           string
	Description    string
	EntityID       string
	ProjectID      string
	Scope          MetadataScope
	HierarchyLevel int
	Subtask        bool
	Self           string
}

// StatusMetadata is one Jira workflow status definition normalized for
// work-item context.
type StatusMetadata struct {
	ID                string
	Name              string
	Description       string
	StatusCategory    string
	StatusCategoryKey string
	ProjectID         string
	Scope             MetadataScope
	Self              string
}

// WorkflowVersion identifies one provider workflow version.
type WorkflowVersion struct {
	ID     string
	Number int
}

// WorkflowStatusMetadata identifies one status reference inside a workflow
// definition.
type WorkflowStatusMetadata struct {
	StatusReference string
	StatusID        string
	Deprecated      bool
}

// WorkflowTransitionMetadata identifies one sanitized transition shape inside
// a workflow definition.
type WorkflowTransitionMetadata struct {
	ID                   string
	Name                 string
	Type                 string
	FromStatusReferences []string
	ToStatusReference    string
	HasValidators        bool
	HasTriggers          bool
	HasActions           bool
}

// WorkflowMetadata is one Jira workflow definition normalized for transition
// context without raw workflow or transition names.
type WorkflowMetadata struct {
	ID          string
	Name        string
	Description string
	Scope       MetadataScope
	Version     WorkflowVersion
	Statuses    []WorkflowStatusMetadata
	Transitions []WorkflowTransitionMetadata
}

// FieldSchema is the safe subset of a Jira field schema definition. CustomID
// records provider input presence only and is not emitted as a raw payload
// value.
type FieldSchema struct {
	Type     string
	Items    string
	System   string
	Custom   string
	CustomID string
}

// FieldMetadata is one Jira field definition normalized for work-item context.
type FieldMetadata struct {
	ID          string
	Name        string
	Description string
	Schema      FieldSchema
	Self        string
}

// MetadataWarning records a metadata collection state that must remain visible
// to readers instead of being confused with empty metadata.
type MetadataWarning struct {
	MetadataType string
	Reason       string
	FailureClass string
	ProviderID   string
}

// CollectionWindow bounds one Jira updated-window collection.
type CollectionWindow struct {
	Since time.Time
	Until time.Time
}

// CollectionResult is one bounded Jira work-item evidence fetch.
type CollectionResult struct {
	Issues           []Issue
	Transitions      map[string][]Transition
	ExternalLinks    map[string][]ExternalLink
	Projects         []ProjectMetadata
	IssueTypes       []IssueTypeMetadata
	Statuses         []StatusMetadata
	Workflows        []WorkflowMetadata
	Fields           []FieldMetadata
	MetadataWarnings []MetadataWarning
	ObservedAt       time.Time
	Stats            CollectionStats
}

// CollectionStats carries bounded collection counters for telemetry and tests.
type CollectionStats struct {
	SearchPages              int
	ChangelogPages           int
	RemoteLinkPages          int
	MetadataPages            int
	IssuesEmitted            int
	ChangelogEventsEmitted   int
	RemoteLinksEmitted       int
	RemoteLinksRejected      int
	MetadataObjectsScanned   int
	MetadataObjectsEmitted   int
	UnsupportedMetadata      int
	PermissionHiddenMetadata int
	StaleMetadata            int
	MetadataRedactions       int
	PartialFailures          int
	RateLimits               int
	RetryAfterSeconds        int
	StaleWindows             int
	UnsupportedProviderLinks int
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
	MetadataLimit   int
}
