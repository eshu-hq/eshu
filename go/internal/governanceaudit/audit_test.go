// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func TestNormalizeEventAcceptsSafeDecision(t *testing.T) {
	t.Parallel()

	event, err := governanceaudit.NormalizeEvent(governanceaudit.Event{
		Type:               governanceaudit.EventTypeSemanticPolicyDecision,
		ActorClass:         governanceaudit.ActorClassServicePrincipal,
		ActorIDHash:        "sha256:abcdef1234567890",
		ServicePrincipalID: "svc:workflow-coordinator",
		ScopeClass:         governanceaudit.ScopeClassSourceClass,
		ScopeIDHash:        "sha256:0123456789abcdef",
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         "egress_policy_missing",
		CorrelationID:      "corr:semantic-denied-1",
		PolicyRevisionHash: "sha256:1111222233334444",
		OccurredAt:         time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NormalizeEvent() error = %v, want nil", err)
	}
	if got, want := event.ActorClass, governanceaudit.ActorClassServicePrincipal; got != want {
		t.Fatalf("ActorClass = %q, want %q", got, want)
	}
	if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
		t.Fatalf("Decision = %q, want %q", got, want)
	}
}

func TestNormalizeEventAcceptsRequiredAuthAuditFamilies(t *testing.T) {
	t.Parallel()

	eventTypes := []governanceaudit.EventType{
		governanceaudit.EventTypeAPIMCPAuthentication,
		governanceaudit.EventTypeIdentityAuthentication,
		governanceaudit.EventTypeMFALifecycle,
		governanceaudit.EventTypeSessionLifecycle,
		governanceaudit.EventTypeTokenLifecycle,
		governanceaudit.EventTypeIDPConfigChange,
		governanceaudit.EventTypeRoleGrantChange,
		governanceaudit.EventTypeReadAuthorization,
		governanceaudit.EventTypeTenantSwitch,
		governanceaudit.EventTypeSensitiveDataAccess,
		governanceaudit.EventTypeAskSearchRun,
		governanceaudit.EventTypeExport,
		governanceaudit.EventTypeBootstrap,
		governanceaudit.EventTypeBreakGlass,
		governanceaudit.EventTypeAuditRead,
	}

	for _, eventType := range eventTypes {
		eventType := eventType
		t.Run(string(eventType), func(t *testing.T) {
			t.Parallel()

			_, err := governanceaudit.NormalizeEvent(governanceaudit.Event{
				Type:               eventType,
				ActorClass:         governanceaudit.ActorClassScopedToken,
				ActorIDHash:        "sha256:abcdef1234567890",
				ScopeClass:         governanceaudit.ScopeClassAdmin,
				Decision:           governanceaudit.DecisionAllowed,
				ReasonCode:         "allowed",
				CorrelationID:      "corr:auth-audit-coverage",
				PolicyRevisionHash: "sha256:1111222233334444",
				OccurredAt:         time.Date(2026, 6, 21, 23, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("NormalizeEvent(%q) error = %v, want nil", eventType, err)
			}
		})
	}
}

func TestNormalizeEventRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	base := governanceaudit.Event{
		Type:       governanceaudit.EventTypeReadAuthorization,
		ActorClass: governanceaudit.ActorClassScopedToken,
		ScopeClass: governanceaudit.ScopeClassRepository,
		Decision:   governanceaudit.DecisionDenied,
		ReasonCode: "subject_scope_missing",
		OccurredAt: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name   string
		mutate func(*governanceaudit.Event)
	}{
		{
			name: "raw token actor hash",
			mutate: func(event *governanceaudit.Event) {
				event.ActorIDHash = "Bearer unsafe-token"
			},
		},
		{
			name: "private URL service principal",
			mutate: func(event *governanceaudit.Event) {
				event.ServicePrincipalID = "https://service.example.invalid/private"
			},
		},
		{
			name: "repository path scope",
			mutate: func(event *governanceaudit.Event) {
				event.ScopeIDHash = "repo://private/source"
			},
		},
		{
			name: "direct personal identifier correlation",
			mutate: func(event *governanceaudit.Event) {
				event.CorrelationID = "operator.person@example.invalid"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			event := base
			tt.mutate(&event)
			_, err := governanceaudit.NormalizeEvent(event)
			if err == nil {
				t.Fatal("NormalizeEvent() error = nil, want unsafe value rejection")
			}
			for _, forbidden := range []string{
				"unsafe-token",
				"service.example.invalid",
				"repo://private/source",
				"operator.person@example.invalid",
			} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("error %q exposed unsafe value %q", err, forbidden)
				}
			}
		})
	}
}

func TestAggregateCountsSafeClasses(t *testing.T) {
	t.Parallel()

	events := []governanceaudit.Event{
		{
			Type:        governanceaudit.EventTypeReadAuthorization,
			ActorClass:  governanceaudit.ActorClassScopedToken,
			ActorIDHash: "sha256:aaaaaaaaaaaaaaaa",
			ScopeClass:  governanceaudit.ScopeClassRepository,
			Decision:    governanceaudit.DecisionAllowed,
			ReasonCode:  "allowed",
			OccurredAt:  time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC),
		},
		{
			Type:               governanceaudit.EventTypeSemanticPolicyDecision,
			ActorClass:         governanceaudit.ActorClassServicePrincipal,
			ServicePrincipalID: "svc:semantic-worker",
			ScopeClass:         governanceaudit.ScopeClassSourceClass,
			Decision:           governanceaudit.DecisionDenied,
			ReasonCode:         "egress_policy_missing",
			OccurredAt:         time.Date(2026, 6, 9, 12, 1, 0, 0, time.UTC),
		},
		{
			Type:               governanceaudit.EventTypeSemanticPolicyDecision,
			ActorClass:         governanceaudit.ActorClassServicePrincipal,
			ServicePrincipalID: "svc:semantic-worker",
			ScopeClass:         governanceaudit.ScopeClassSourceClass,
			Decision:           governanceaudit.DecisionDenied,
			ReasonCode:         "egress_policy_missing",
			OccurredAt:         time.Date(2026, 6, 9, 12, 2, 0, 0, time.UTC),
		},
	}

	summary, err := governanceaudit.Aggregate(events)
	if err != nil {
		t.Fatalf("Aggregate() error = %v, want nil", err)
	}
	if got, want := summary.Total, 3; got != want {
		t.Fatalf("Total = %d, want %d", got, want)
	}
	if got, want := summary.Denied, 2; got != want {
		t.Fatalf("Denied = %d, want %d", got, want)
	}
	requireCount(t, summary.DecisionCounts, string(governanceaudit.DecisionDenied), 2)
	requireCount(t, summary.EventTypeCounts, string(governanceaudit.EventTypeSemanticPolicyDecision), 2)
	requireCount(t, summary.ReasonCounts, "egress_policy_missing", 2)
	requireCount(t, summary.ActorClassCounts, string(governanceaudit.ActorClassServicePrincipal), 2)
	requireCount(t, summary.ScopeClassCounts, string(governanceaudit.ScopeClassSourceClass), 2)
}

func requireCount(t *testing.T, rows []governanceaudit.Count, name string, want int) {
	t.Helper()
	for _, row := range rows {
		if row.Name == name {
			if row.Count != want {
				t.Fatalf("count %q = %d, want %d", name, row.Count, want)
			}
			return
		}
	}
	t.Fatalf("missing count %q in %#v", name, rows)
}
