// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// ctxAuditAppender fails an append on a canceled context, as the production
// governance audit store does, so a test sees the audit drop a client cancel
// causes.
type ctxAuditAppender struct{}

func (ctxAuditAppender) Append(ctx context.Context, _ []governanceaudit.Event) error {
	return ctx.Err()
}

// countRecords returns how many records sit at level with message.
func countRecords(records []slog.Record, level slog.Level, message string) int {
	n := 0
	for _, record := range records {
		if record.Level == level && record.Message == message {
			n++
		}
	}
	return n
}

// TestLoginRouteFailuresLogOnlyServerFaults proves the four routes that log
// a failed store step at ERROR log it once for a server fault and not at all
// for a client cancel (#7674). On the sign-in policy update, the failed
// step's audit event is appended on the request context, so a client cancel
// still logs `governance audit append failed` once while a server fault
// appends cleanly. Not parallel: it swaps the default slog logger.
func TestLoginRouteFailuresLogOnlyServerFaults(t *testing.T) {
	logger, records := newRecordingLogger()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	cases := []struct {
		route      failure.Route
		logMessage string
		// auditDropOnCancel is the number of `governance audit append
		// failed` lines a client cancel adds.
		auditDropOnCancel int
		handler           func(error) failure.Mounter
	}{
		{
			route:      failure.Route{Name: "session list", Path: "/api/v0/auth/sessions", Scope: browserSessionScope},
			logMessage: "list browser sessions failed",
			handler: func(err error) failure.Mounter {
				return &BrowserSessionListHandler{Store: &fakeBrowserSessionListStore{listErr: err}}
			},
		},
		{
			route:      failure.Route{Name: "profile", Path: "/api/v0/auth/profile", Scope: browserSessionScope},
			logMessage: "get local identity mfa status failed",
			handler: func(err error) failure.Mounter {
				return &ProfileHandler{LocalIdentityStore: &fakeLocalIdentityListStore{mfaErr: err}}
			},
		},
		{
			route:      failure.Route{Name: "sign-in policy read", Path: "/api/v0/auth/admin/sign-in-policy", Scope: allScopeTenantAdminScope},
			logMessage: "admin sign-in policy read failed",
			handler: func(err error) failure.Mounter {
				return &SignInPolicyReadHandler{Store: &fakeSignInPolicyReadStore{err: err}}
			},
		},
		{
			route: failure.Route{
				Name: "sign-in policy update", Method: http.MethodPatch, Path: "/api/v0/auth/admin/sign-in-policy",
				Body: `{"allow_local_user_creation":true}`, Scope: allScopeTenantAdminScope,
			},
			logMessage:        "admin sign-in policy update failed",
			auditDropOnCancel: 1,
			handler: func(err error) failure.Mounter {
				return &SignInPolicyMutationHandler{Store: &fakeSignInPolicyMutationStore{err: err}, Audit: ctxAuditAppender{}}
			},
		},
	}
	for _, tc := range cases {
		for _, kase := range []struct {
			name      string
			err       error
			cancel    bool
			wantLog   int
			wantAudit int
		}{
			{name: "server fault", err: errors.New("backend: " + failure.Canary), wantLog: 1},
			{
				name: "client cancel", err: fmt.Errorf("backend: %s: %w", failure.Canary, context.Canceled), cancel: true,
				wantAudit: tc.auditDropOnCancel,
			},
		} {
			t.Run(tc.route.Name+"/"+kase.name, func(t *testing.T) {
				*records = (*records)[:0]
				failure.Serve(failure.Suite{}, tc.route, tc.handler(kase.err), kase.cancel)
				if got := countRecords(*records, slog.LevelError, tc.logMessage); got != kase.wantLog {
					t.Errorf("ERROR %q records = %d, want %d", tc.logMessage, got, kase.wantLog)
				}
				if got := countRecords(*records, slog.LevelError, "governance audit append failed"); got != kase.wantAudit {
					t.Errorf("ERROR audit-drop records = %d, want %d", got, kase.wantAudit)
				}
			})
		}
	}
}
