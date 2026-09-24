// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestAuditLocalIdentityStampsActorClassByAuthMode moved from root's
// audit_actor_class_test.go (#6642): once this family moved to package
// local, auditLocalIdentity became an unexported method of a different
// package and root could no longer call it directly. Root's file keeps
// pinning the same mapping for its own staying emitters (sign-in policy
// mutation); this test pins it for auditLocalIdentity specifically, driven
// with every AuthMode member the same way root's shared table did before the
// split. Before #6566 the local-identity helper filed a cookie session as
// operator.
func TestAuditLocalIdentityStampsActorClassByAuthMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mode auth.AuthMode
		want governanceaudit.ActorClass
	}{
		{name: "browser session", mode: auth.AuthModeBrowserSession, want: governanceaudit.ActorClassBrowserSession},
		{name: "scoped bearer", mode: auth.AuthModeScoped, want: governanceaudit.ActorClassScopedToken},
		{name: "shared bearer", mode: auth.AuthModeShared, want: governanceaudit.ActorClassSharedToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			audit := &querytestutil.FakeGovernanceAuditAppender{}
			authCtx := auth.AuthContext{Mode: tc.mode, SubjectIDHash: "sha256:abcdef12", AllScopes: true}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/anything", nil)
			req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))

			h := &IdentityHandler{Audit: audit}
			h.auditLocalIdentity(req, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionAllowed, "break_glass_enabled", "")

			if len(audit.Events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(audit.Events))
			}
			event := audit.Events[0]
			if got := event.ActorClass; got != tc.want {
				t.Errorf("auditLocalIdentity(%q) actor class = %q, want %q", tc.mode, got, tc.want)
			}
			if event.ActorIDHash == "" {
				t.Errorf("event.ActorIDHash is blank for an identity-bearing class")
			}
			if _, err := governanceaudit.NormalizeEvent(event); err != nil {
				t.Errorf("NormalizeEvent rejected the mutation event: %v", err)
			}
		})
	}
}
