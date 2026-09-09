// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// TestRecoveryActorMapsEveryAuthMode pins the recovery audit actor mapping
// for every auth mode with and without a subject hash. Before #6566 a cookie
// session was filed as shared_token here while the same session was
// browser_session on a route denial.
//
// Moved from the query root's audit actor-class proof (#6060, lane B S1)
// with only the package repoints (audit.RecoveryActor,
// audit.SharedActorIDHash, queryauth); the cases and assertions are unchanged.
func TestRecoveryActorMapsEveryAuthMode(t *testing.T) {
	t.Parallel()

	const hash = "sha256:abcdef12"
	cases := []struct {
		name      string
		auth      queryauth.AuthContext
		wantClass governanceaudit.ActorClass
		wantHash  string
	}{
		{
			name:      "browser session with a subject hash is browser_session",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeBrowserSession, SubjectIDHash: hash},
			wantClass: governanceaudit.ActorClassBrowserSession,
			wantHash:  hash,
		},
		{
			name:      "browser session with no subject hash downgrades to anonymous",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeBrowserSession},
			wantClass: governanceaudit.ActorClassAnonymous,
		},
		{
			name:      "scoped Bearer [REDACTED] a subject hash is scoped_token",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeScoped, SubjectIDHash: hash},
			wantClass: governanceaudit.ActorClassScopedToken,
			wantHash:  hash,
		},
		{
			name:      "scoped Bearer [REDACTED] no subject hash downgrades to anonymous",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeScoped},
			wantClass: governanceaudit.ActorClassAnonymous,
		},
		{
			name:      "shared Bearer [REDACTED] a subject hash is shared_token",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeShared, SubjectIDHash: hash},
			wantClass: governanceaudit.ActorClassSharedToken,
			wantHash:  hash,
		},
		{
			name:      "shared Bearer [REDACTED] no subject hash keeps the synthetic identity",
			auth:      queryauth.AuthContext{Mode: queryauth.AuthModeShared},
			wantClass: governanceaudit.ActorClassSharedToken,
			wantHash:  audit.SharedActorIDHash,
		},
		{
			name:      "no auth context is anonymous",
			auth:      queryauth.AuthContext{},
			wantClass: governanceaudit.ActorClassAnonymous,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotClass, gotHash := audit.RecoveryActor(tc.auth)
			if gotClass != tc.wantClass {
				t.Errorf("RecoveryActor class = %q, want %q", gotClass, tc.wantClass)
			}
			if gotHash != tc.wantHash {
				t.Errorf("RecoveryActor hash = %q, want %q", gotHash, tc.wantHash)
			}
			event := governanceaudit.Event{
				Type:        governanceaudit.EventTypeAdminRecoveryAction,
				ActorClass:  gotClass,
				ActorIDHash: gotHash,
				ScopeClass:  governanceaudit.ScopeClassAdmin,
				Decision:    governanceaudit.DecisionDenied,
				ReasonCode:  "replay_refused_unauthorized",
				OccurredAt:  time.Now(),
			}
			if _, err := governanceaudit.NormalizeEvent(event); err != nil {
				t.Errorf("NormalizeEvent rejected the recovery event: %v", err)
			}
		})
	}
}
