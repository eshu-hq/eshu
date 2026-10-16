// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestAdminAuditReadsAnswerFixedServerFailures is the #7674 regression for
// GET /api/v0/auth/admin/audit/events and GET /api/v0/auth/admin/audit/summary.
// Both read the governance audit store through the guarded PostgreSQL reader,
// but answered a stale reader with their fixed 500 instead of the retryable
// 503, and answered a client cancel as a server fault. The routes have no
// capability-matrix key (#8227), so the fence envelope carries no capability
// field.
func TestAdminAuditReadsAnswerFixedServerFailures(t *testing.T) {
	t.Parallel()

	sharedOperator := func(ctx context.Context) context.Context {
		return auth.ContextWithAuthContext(ctx, auth.AuthContext{Mode: auth.AuthModeShared, AllScopes: true})
	}
	routes := []failure.Route{
		{
			Name: "audit events", Path: "/api/v0/auth/admin/audit/events?limit=10",
			Message: auditEventsListFailedMessage, Scope: sharedOperator,
			Handler: func(err error) failure.Mounter {
				return &ReadHandler{Audit: failingAuditReader{err: err}}
			},
		},
		{
			Name: "audit summary", Path: "/api/v0/auth/admin/audit/summary",
			Message: auditSummaryFailedMessage, Scope: sharedOperator,
			Handler: func(err error) failure.Mounter {
				return &ReadHandler{Audit: failingAuditReader{err: err}}
			},
		},
	}
	suite := failure.Suite{Routes: routes}
	failure.Run(t, suite)

	for _, route := range routes {
		rec, _ := failure.Serve(suite, route, route.Handler(fmt.Errorf("%s: %w", failure.Canary, db.ErrReaderStale)), false)
		var body struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: decode fence body: %v; body = %s", route.Name, err, rec.Body.String())
		}
		if capability, present := body.Error["capability"]; present {
			t.Errorf("%s: fence envelope carries capability %v, want no field (#8227)", route.Name, capability)
		}
	}
}

// failingAuditReader fails every audit read with err, unconditionally.
type failingAuditReader struct{ err error }

func (f failingAuditReader) ListAuditEvents(context.Context, AuditQuery) ([]governanceaudit.Event, error) {
	return nil, f.err
}

func (f failingAuditReader) SummarizeAuditEvents(context.Context) (governanceaudit.Summary, error) {
	return governanceaudit.Summary{}, f.err
}

func (f failingAuditReader) SummarizeAuditEventsForTenant(context.Context, string) (governanceaudit.Summary, error) {
	return governanceaudit.Summary{}, f.err
}
