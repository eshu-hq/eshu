// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type pagerDutyIncidentPayload struct {
	Event struct {
		ID         string          `json:"id"`
		EventType  string          `json:"event_type"`
		OccurredAt time.Time       `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	} `json:"event"`
}

type jiraIncidentPayload struct {
	WebhookEvent string           `json:"webhookEvent"`
	Timestamp    int64            `json:"timestamp"`
	Issue        jiraIssuePayload `json:"issue"`
}

type jiraIssuePayload struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

// ErrUnsupportedIncidentFreshnessEvent marks a verified provider webhook that
// is not allowed to wake an incident-source collector.
var ErrUnsupportedIncidentFreshnessEvent = errors.New("unsupported incident freshness event")

// NormalizePagerDutyIncidentFreshness maps a verified PagerDuty webhook into a
// scoped collector refresh trigger without treating the payload as fact truth.
func NormalizePagerDutyIncidentFreshness(
	scopeID string,
	deliveryID string,
	payload []byte,
	receivedAt time.Time,
) (IncidentFreshnessTrigger, error) {
	var decoded pagerDutyIncidentPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return IncidentFreshnessTrigger{}, fmt.Errorf("decode PagerDuty webhook event: %w", err)
	}
	eventID := firstNonEmpty(decoded.Event.ID, deliveryID)
	trigger := IncidentFreshnessTrigger{
		Provider:   ProviderPagerDuty,
		EventKind:  firstNonEmpty(decoded.Event.EventType, "pagerduty.webhook"),
		EventID:    eventID,
		ScopeID:    scopeID,
		ResourceID: pagerDutyResourceID(decoded.Event.Data),
		ObservedAt: firstTime(decoded.Event.OccurredAt, receivedAt),
	}
	return trigger.normalized(), trigger.Validate()
}

// NormalizeJiraIncidentFreshness maps a verified Jira Cloud webhook into a
// scoped collector refresh trigger without promoting issue payload fields to
// durable work-item truth.
func NormalizeJiraIncidentFreshness(
	scopeID string,
	deliveryID string,
	payload []byte,
	receivedAt time.Time,
) (IncidentFreshnessTrigger, error) {
	var decoded jiraIncidentPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return IncidentFreshnessTrigger{}, fmt.Errorf("decode Jira webhook event: %w", err)
	}
	observedAt := receivedAt
	if decoded.Timestamp > 0 {
		observedAt = time.UnixMilli(decoded.Timestamp).UTC()
	}
	eventKind := firstNonEmpty(decoded.WebhookEvent, "jira.webhook")
	if !supportedJiraIncidentFreshnessEvent(eventKind) {
		return IncidentFreshnessTrigger{}, fmt.Errorf("%w: jira webhook event %q", ErrUnsupportedIncidentFreshnessEvent, eventKind)
	}
	trigger := IncidentFreshnessTrigger{
		Provider:   ProviderJira,
		EventKind:  eventKind,
		EventID:    deliveryID,
		ScopeID:    scopeID,
		ResourceID: jiraIssueResourceID(decoded.Issue),
		ObservedAt: observedAt,
	}
	return trigger.normalized(), trigger.Validate()
}

// Validate checks that an incident freshness trigger is scoped to one
// configured source collector target.
func (t IncidentFreshnessTrigger) Validate() error {
	t = t.normalized()
	switch t.Provider {
	case ProviderPagerDuty, ProviderJira:
	default:
		return fmt.Errorf("unsupported incident freshness provider %q", t.Provider)
	}
	if t.EventKind == "" {
		return fmt.Errorf("incident freshness event_kind is required")
	}
	if t.EventID == "" {
		return fmt.Errorf("incident freshness event_id is required")
	}
	if t.ScopeID == "" {
		return fmt.Errorf("incident freshness scope_id is required")
	}
	if t.ObservedAt.IsZero() {
		return fmt.Errorf("incident freshness observed_at must not be zero")
	}
	return nil
}

// NewStoredIncidentFreshnessTrigger builds durable keys for a validated
// incident source refresh trigger.
func NewStoredIncidentFreshnessTrigger(
	trigger IncidentFreshnessTrigger,
	receivedAt time.Time,
) (StoredIncidentFreshnessTrigger, error) {
	if receivedAt.IsZero() {
		return StoredIncidentFreshnessTrigger{}, fmt.Errorf("received_at must not be zero")
	}
	trigger = trigger.normalized()
	if trigger.ObservedAt.IsZero() {
		trigger.ObservedAt = receivedAt.UTC()
	}
	if err := trigger.Validate(); err != nil {
		return StoredIncidentFreshnessTrigger{}, err
	}
	freshnessKey := trigger.FreshnessKey()
	return StoredIncidentFreshnessTrigger{
		IncidentFreshnessTrigger: trigger,
		TriggerID:                facts.StableID("IncidentFreshnessTrigger", map[string]any{"freshness_key": freshnessKey}),
		DeliveryKey:              trigger.DeliveryKey(),
		FreshnessKey:             freshnessKey,
		Status:                   TriggerStatusQueued,
		ReceivedAt:               receivedAt.UTC(),
		UpdatedAt:                receivedAt.UTC(),
	}, nil
}

// DeliveryKey returns the duplicate-delivery identity for this webhook event.
func (t IncidentFreshnessTrigger) DeliveryKey() string {
	t = t.normalized()
	return strings.Join([]string{string(t.Provider), t.EventID}, ":")
}

// FreshnessKey returns the coalescing identity for targeted incident refresh.
func (t IncidentFreshnessTrigger) FreshnessKey() string {
	t = t.normalized()
	return strings.Join([]string{string(t.Provider), t.ScopeID}, ":")
}

func (t IncidentFreshnessTrigger) normalized() IncidentFreshnessTrigger {
	t.Provider = Provider(strings.TrimSpace(string(t.Provider)))
	t.EventKind = strings.TrimSpace(t.EventKind)
	t.EventID = strings.TrimSpace(t.EventID)
	t.ScopeID = strings.TrimSpace(t.ScopeID)
	t.ResourceID = strings.TrimSpace(t.ResourceID)
	t.ObservedAt = t.ObservedAt.UTC()
	return t
}

func supportedJiraIncidentFreshnessEvent(eventKind string) bool {
	switch strings.TrimSpace(eventKind) {
	case "jira:issue_created", "jira:issue_updated", "jira:issue_deleted":
		return true
	default:
		return false
	}
}

func jiraIssueResourceID(issue jiraIssuePayload) string {
	if id := firstNonEmpty(issue.Key, issue.ID); id != "" {
		return id
	}
	self := strings.TrimSpace(issue.Self)
	if self == "" {
		return ""
	}
	return "jira_self:" + facts.StableID("JiraWebhookIssueSelf", map[string]any{"self": self})
}

func pagerDutyResourceID(raw json.RawMessage) string {
	var data map[string]any
	if len(raw) == 0 {
		return ""
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	return firstNonEmpty(
		stringValue(data, "id"),
		stringValue(data, "html_url"),
		stringValue(data, "self"),
	)
}

func stringValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}
