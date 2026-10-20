// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudtrail

import "context"

// Client is the CloudTrail read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned metadata records.
//
// The interface intentionally excludes CloudTrail event extraction
// (`LookupEvents`) and Lake query (`StartQuery` / `GetQueryResults`) APIs
// because audit event payloads are the protected data class for this
// service. The interface also excludes mutation APIs (CreateTrail,
// UpdateTrail, DeleteTrail, StartLogging, StopLogging, PutEventSelectors,
// PutInsightSelectors, Create/Update/Delete EventDataStore/Channel/Dashboard)
// because the collector is metadata-only.
type Client interface {
	// ListTrails returns trail configuration snapshots for the claimed region.
	ListTrails(context.Context) ([]Trail, error)
	// ListEventDataStores returns CloudTrail Lake event data store
	// configuration snapshots for the claimed region. Selector bodies and Lake
	// query results are intentionally outside this contract.
	ListEventDataStores(context.Context) ([]EventDataStore, error)
	// ListChannels returns CloudTrail channel configuration snapshots for the
	// claimed region.
	ListChannels(context.Context) ([]Channel, error)
	// ListDashboards returns CloudTrail Lake dashboard configuration
	// snapshots for the claimed region.
	ListDashboards(context.Context) ([]Dashboard, error)
}

// Trail is the scanner-owned representation of one CloudTrail trail.
// The struct intentionally omits event payloads, raw event selector bodies
// (only the resource-type count summary is kept), and any field that would
// require a mutation or event-extraction API to populate.
type Trail struct {
	ARN                        string
	Name                       string
	HomeRegion                 string
	S3BucketName               string
	S3KeyPrefix                string
	SNSTopicARN                string
	CloudWatchLogsLogGroupARN  string
	CloudWatchLogsRoleARN      string
	KMSKeyID                   string
	IncludeGlobalServiceEvents bool
	IsMultiRegionTrail         bool
	IsOrganizationTrail        bool
	LogFileValidationEnabled   bool
	HasCustomEventSelectors    bool
	HasInsightSelectors        bool
	LoggingEnabled             bool
	LatestDeliveryError        string
	LatestNotificationError    string
	EventSelectorSummary       EventSelectorSummary
	InsightSelectors           []string
	Tags                       map[string]string
}

// EventSelectorSummary holds bounded counts derived from a trail's event
// selectors and advanced event selectors. The summary persists counts only;
// selector bodies, field lists, and string equality matchers are never part
// of the contract because they may reveal event payload classification rules.
type EventSelectorSummary struct {
	EventSelectorCount         int
	AdvancedEventSelectorCount int
	ResourceTypeCounts         map[string]int
}

// EventDataStore is the scanner-owned representation of one CloudTrail Lake
// event data store. Advanced selector bodies, Lake query strings, and result
// rows are out of scope; only identity, retention, and selector-count summary
// are persisted.
type EventDataStore struct {
	ARN                          string
	Name                         string
	Status                       string
	RetentionPeriod              int32
	MultiRegionEnabled           bool
	OrganizationEnabled          bool
	TerminationProtectionEnabled bool
	BillingMode                  string
	KMSKeyID                     string
	CreatedTimestamp             string
	UpdatedTimestamp             string
	AdvancedEventSelectorCount   int
	Tags                         map[string]string
}

// Channel is the scanner-owned representation of one CloudTrail channel.
type Channel struct {
	ARN             string
	Name            string
	Source          string
	DestinationType string
	DestinationARN  string
	Tags            map[string]string
}

// Dashboard is the scanner-owned representation of one CloudTrail Lake
// dashboard configuration. Widget query bodies and result rows are not part of
// the contract.
type Dashboard struct {
	ARN              string
	Name             string
	Status           string
	Type             string
	RefreshSchedule  string
	WidgetCount      int
	CreatedTimestamp string
	UpdatedTimestamp string
	Tags             map[string]string
}
