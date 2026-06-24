// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import "time"

const (
	// ComponentID is the manifest identity used for local allowlist policy.
	ComponentID = "dev.eshu.examples.pagerduty"
	// CollectorKind is the collector family declared by the component manifest.
	CollectorKind = "pagerduty"
	// SourceSystem identifies PagerDuty as the source evidence family.
	SourceSystem = "pagerduty"
	// MetricsPrefix is the component-owned metric prefix declared by the manifest.
	MetricsPrefix = "eshu_dp_example_pagerduty_"
)

const (
	// FactKindIncident mirrors the core PagerDuty incident source fact payload.
	FactKindIncident = "dev.eshu.examples.pagerduty.incident"
	// FactKindLifecycleEvent mirrors the core PagerDuty lifecycle event payload.
	FactKindLifecycleEvent = "dev.eshu.examples.pagerduty.lifecycle_event"
	// FactKindChange mirrors the core PagerDuty related change-event payload.
	FactKindChange = "dev.eshu.examples.pagerduty.change"
	// FactKindObservedService mirrors the core live service config payload.
	FactKindObservedService = "dev.eshu.examples.pagerduty.observed_service"
	// FactKindObservedIntegration mirrors the core live integration config payload.
	FactKindObservedIntegration = "dev.eshu.examples.pagerduty.observed_integration"
	// FactKindCoverageWarning mirrors the core PagerDuty coverage warning payload.
	FactKindCoverageWarning = "dev.eshu.examples.pagerduty.coverage_warning"
)

const (
	coreFactKindIncident            = "incident.record"
	coreFactKindLifecycleEvent      = "incident.lifecycle_event"
	coreFactKindChange              = "change.record"
	coreFactKindObservedService     = "incident_routing.observed_pagerduty_service"
	coreFactKindObservedIntegration = "incident_routing.observed_pagerduty_integration"
	coreFactKindCoverageWarning     = "incident_routing.coverage_warning"
)

// Observation is one redacted synthetic PagerDuty fixture.
type Observation struct {
	ObservedAt      time.Time        `json:"observed_at"`
	SourceURI       string           `json:"source_uri"`
	Incidents       []Incident       `json:"incidents"`
	LifecycleEvents []LifecycleEvent `json:"lifecycle_events"`
	Changes         []ChangeEvent    `json:"changes"`
	Services        []ConfigService  `json:"services"`
	Integrations    []Integration    `json:"integrations"`
	Warnings        []ConfigWarning  `json:"warnings"`
}

// Reference is a bounded PagerDuty reference without private names.
type Reference struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Summary string `json:"summary,omitempty"`
	HTMLURL string `json:"html_url,omitempty"`
}

// Link is a redacted provider link attached to a change event.
type Link struct {
	Href string `json:"href,omitempty"`
	Text string `json:"text,omitempty"`
}

// Incident is one synthetic PagerDuty incident.
type Incident struct {
	ID             string      `json:"id"`
	IncidentNumber int64       `json:"incident_number"`
	Title          string      `json:"title,omitempty"`
	Status         string      `json:"status,omitempty"`
	Urgency        string      `json:"urgency,omitempty"`
	Priority       Reference   `json:"priority,omitempty"`
	Service        Reference   `json:"service,omitempty"`
	Escalation     Reference   `json:"escalation,omitempty"`
	Teams          []Reference `json:"teams,omitempty"`
	Assignments    []Reference `json:"assignments,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	ResolvedAt     time.Time   `json:"resolved_at,omitempty"`
	HTMLURL        string      `json:"html_url,omitempty"`
}

// LifecycleEvent is one synthetic PagerDuty timeline entry.
type LifecycleEvent struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incident_id"`
	Type       string    `json:"type,omitempty"`
	Actor      Reference `json:"actor,omitempty"`
	Channel    string    `json:"channel,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	HTMLURL    string    `json:"html_url,omitempty"`
}

// ChangeEvent is one synthetic related change event.
type ChangeEvent struct {
	ID        string      `json:"id"`
	Summary   string      `json:"summary,omitempty"`
	Source    string      `json:"source,omitempty"`
	Services  []Reference `json:"services,omitempty"`
	Links     []Link      `json:"links,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	HTMLURL   string      `json:"html_url,omitempty"`
}

// ConfigService is one synthetic live PagerDuty service observation.
type ConfigService struct {
	ID              string      `json:"id"`
	Summary         string      `json:"summary,omitempty"`
	Status          string      `json:"status,omitempty"`
	AlertCreation   string      `json:"alert_creation,omitempty"`
	Escalation      Reference   `json:"escalation,omitempty"`
	Teams           []Reference `json:"teams,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	HTMLURL         string      `json:"html_url,omitempty"`
	Disabled        bool        `json:"disabled,omitempty"`
	Deleted         bool        `json:"deleted,omitempty"`
	MatchState      string      `json:"match_state,omitempty"`
	ManuallyCreated bool        `json:"manually_created,omitempty"`
	DriftReason     string      `json:"drift_reason,omitempty"`
}

// Integration is one synthetic live PagerDuty integration observation.
type Integration struct {
	ID                 string    `json:"id"`
	ServiceID          string    `json:"service_id,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	Type               string    `json:"type,omitempty"`
	VendorID           string    `json:"vendor_id,omitempty"`
	HTMLURL            string    `json:"html_url,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Disabled           bool      `json:"disabled,omitempty"`
	Deleted            bool      `json:"deleted,omitempty"`
	MatchState         string    `json:"match_state,omitempty"`
	ManuallyCreated    bool      `json:"manually_created,omitempty"`
	DriftReason        string    `json:"drift_reason,omitempty"`
	RoutingKeyRedacted bool      `json:"routing_key_redacted,omitempty"`
}

// ConfigWarning is one bounded PagerDuty fixture coverage warning.
type ConfigWarning struct {
	ResourceClass string `json:"resource_class"`
	ResourceID    string `json:"resource_id"`
	Reason        string `json:"reason"`
}
