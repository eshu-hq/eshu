// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestGovernanceStatusReadsPrivateAuditSinkAggregates(t *testing.T) {
	t.Parallel()

	envelope, body := governanceStatusEnvelope(t, &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 9, 18, 0, 0, 0, time.UTC),
			},
		},
		GovernanceAudit: fakeGovernanceAuditSummaryReader{
			summary: governanceaudit.Summary{
				Total:       3,
				Allowed:     1,
				Denied:      1,
				Unavailable: 1,
				EventTypeCounts: []governanceaudit.Count{
					{Name: string(governanceaudit.EventTypeReadAuthorization), Count: 2},
				},
				ActorClassCounts: []governanceaudit.Count{
					{Name: string(governanceaudit.ActorClassScopedToken), Count: 2},
				},
				ScopeClassCounts: []governanceaudit.Count{
					{Name: string(governanceaudit.ScopeClassRepository), Count: 2},
				},
				ReasonCounts: []governanceaudit.Count{
					{Name: "subject_scope_missing", Count: 1},
				},
			},
		},
		Profile: ProfileProduction,
		Governance: GovernanceStatusConfig{
			Mode:       "hosted_single_tenant",
			State:      "enforcing",
			AuthMode:   "scoped_token",
			TenantMode: "single_tenant",
			AuditState: "configured",
		},
	})

	for _, forbidden := range []string{
		"sha256:aaaaaaaaaaaaaaaa",
		"corr:read-denied-1",
		"svc:semantic-worker",
		"actor_id_hash",
		"scope_id_hash",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("governance audit status leaked detailed field %q: %s", forbidden, body)
		}
	}
	data := envelope.Data.(map[string]any)
	audit := data["audit"].(map[string]any)
	if got, want := audit["event_count"], float64(3); got != want {
		t.Fatalf("audit.event_count = %#v, want %#v", got, want)
	}
	if got, want := audit["denied_decision_count"], float64(1); got != want {
		t.Fatalf("audit.denied_decision_count = %#v, want %#v", got, want)
	}
	if got, want := audit["unavailable_decision_count"], float64(1); got != want {
		t.Fatalf("audit.unavailable_decision_count = %#v, want %#v", got, want)
	}
}

type fakeGovernanceAuditSummaryReader struct {
	summary governanceaudit.Summary
	err     error
}

func (f fakeGovernanceAuditSummaryReader) Summary(context.Context) (governanceaudit.Summary, error) {
	if f.err != nil {
		return governanceaudit.Summary{}, f.err
	}
	return f.summary, nil
}
