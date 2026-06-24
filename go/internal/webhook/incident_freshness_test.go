// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeJiraIncidentFreshnessRejectsUnsupportedEvent(t *testing.T) {
	t.Parallel()

	_, err := NormalizeJiraIncidentFreshness(
		"jira:site:example",
		"delivery-project-1",
		[]byte(`{
			"webhookEvent": "project_deleted",
			"timestamp": 1780250400000,
			"project": {"id": "10000", "key": "OPS"}
		}`),
		time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("NormalizeJiraIncidentFreshness() error = nil, want unsupported event error")
	}
	if !errors.Is(err, ErrUnsupportedIncidentFreshnessEvent) {
		t.Fatalf("NormalizeJiraIncidentFreshness() error = %v, want unsupported event sentinel", err)
	}
}

func TestNormalizeJiraIncidentFreshnessAcceptsDelayedIssueDeletedEvent(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	trigger, err := NormalizeJiraIncidentFreshness(
		"jira:site:example",
		"delivery-delete-1",
		[]byte(`{
			"webhookEvent": "jira:issue_deleted",
			"timestamp": 1780248600000,
			"issue": {"id": "10001", "key": "OPS-123"}
		}`),
		receivedAt,
	)
	if err != nil {
		t.Fatalf("NormalizeJiraIncidentFreshness() error = %v, want nil", err)
	}
	if got, want := trigger.EventKind, "jira:issue_deleted"; got != want {
		t.Fatalf("EventKind = %q, want %q", got, want)
	}
	if got, want := trigger.ResourceID, "OPS-123"; got != want {
		t.Fatalf("ResourceID = %q, want %q", got, want)
	}
	if !trigger.ObservedAt.Before(receivedAt) {
		t.Fatalf("ObservedAt = %s, want delayed provider timestamp before %s", trigger.ObservedAt, receivedAt)
	}
}

func TestNormalizeJiraIncidentFreshnessFingerprintsSelfOnlyResource(t *testing.T) {
	t.Parallel()

	trigger, err := NormalizeJiraIncidentFreshness(
		"jira:site:example",
		"delivery-self-1",
		[]byte(`{
			"webhookEvent": "jira:issue_updated",
			"timestamp": 1780250400000,
			"issue": {"self": "https://example.atlassian.net/rest/api/3/issue/10001?token=secret"}
		}`),
		time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NormalizeJiraIncidentFreshness() error = %v, want nil", err)
	}
	if trigger.ResourceID == "" {
		t.Fatal("ResourceID = empty, want bounded fingerprint")
	}
	for _, sensitive := range []string{"https://", "example.atlassian.net", "token=secret"} {
		if strings.Contains(trigger.ResourceID, sensitive) {
			t.Fatalf("ResourceID = %q, must not contain %q", trigger.ResourceID, sensitive)
		}
	}
	if !strings.HasPrefix(trigger.ResourceID, "jira_self:") {
		t.Fatalf("ResourceID = %q, want jira_self fingerprint prefix", trigger.ResourceID)
	}
}

func TestNewStoredJiraIncidentFreshnessUsesProviderDeliveryAndScopeKeys(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC)
	stored, err := NewStoredIncidentFreshnessTrigger(IncidentFreshnessTrigger{
		Provider:   ProviderJira,
		EventKind:  "jira:issue_updated",
		EventID:    "delivery-jira-1",
		ScopeID:    "jira:site:example",
		ResourceID: "OPS-123",
		ObservedAt: observedAt,
	}, observedAt)
	if err != nil {
		t.Fatalf("NewStoredIncidentFreshnessTrigger() error = %v, want nil", err)
	}
	if got, want := stored.DeliveryKey, "jira_cloud:delivery-jira-1"; got != want {
		t.Fatalf("DeliveryKey = %q, want %q", got, want)
	}
	if got, want := stored.FreshnessKey, "jira_cloud:jira:site:example"; got != want {
		t.Fatalf("FreshnessKey = %q, want %q", got, want)
	}
}
