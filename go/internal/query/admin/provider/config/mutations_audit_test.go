// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// actorModeCases mirrors the root audit actor-class proof's per-mode table:
// the mode the request carries is the class the provider-mutation audit row
// must stamp, and the hash rule is the same one adminRecoveryActor applies.
func actorModeCases() []struct {
	name string
	mode queryauth.AuthMode
	want governanceaudit.ActorClass
} {
	return []struct {
		name string
		mode queryauth.AuthMode
		want governanceaudit.ActorClass
	}{
		{"browser session", queryauth.AuthModeBrowserSession, governanceaudit.ActorClassBrowserSession},
		{"scoped token", queryauth.AuthModeScoped, governanceaudit.ActorClassScopedToken},
		{"shared token", queryauth.AuthModeShared, governanceaudit.ActorClassSharedToken},
	}
}

// TestMutationAuditsStampActorClassByAuthMode pins the actor class the
// provider-mutation audit path stamps for every auth mode.
//
// Split out of the query root's audit actor-class proof (#6060, lane B S1):
// the root keeps the local-identity and sign-in-policy emitters, and this
// package owns its emitter's cases because the audit method is unexported.
// The per-mode cases and assertions match the root proof row for row.
func TestMutationAuditsStampActorClassByAuthMode(t *testing.T) {
	t.Parallel()

	for _, mode := range actorModeCases() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()

			recorder := &querytestutil.FakeGovernanceAuditAppender{}
			auth := queryauth.AuthContext{Mode: mode.mode, SubjectIDHash: "sha256:abcdef12", AllScopes: true}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", nil)
			req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), auth))

			h := &MutationHandler{Audit: recorder}
			h.audit(req, governanceaudit.EventTypeIDPConfigChange, governanceaudit.DecisionAllowed, "provider_config_changed", "")

			if len(recorder.Events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(recorder.Events))
			}
			event := recorder.Events[0]
			if got := event.ActorClass; got != mode.want {
				t.Errorf("provider mutation actor class = %q, want %q", got, mode.want)
			}
			if event.ActorIDHash != "sha256:abcdef12" {
				t.Errorf("provider mutation actor hash = %q, want the request subject hash", event.ActorIDHash)
			}
			if _, err := governanceaudit.NormalizeEvent(event); err != nil {
				t.Errorf("NormalizeEvent rejected the provider mutation event: %v", err)
			}
		})
	}
}

// TestMutationAuditsWithNoSubjectHash pins the provider-mutation audit row
// when there is no usable subject hash: the class stays browser_session with
// a blank hash (this emitter substitutes a hash only for shared_token), and
// NormalizeEvent rejects that row, so the store never persists it.
func TestMutationAuditsWithNoSubjectHash(t *testing.T) {
	t.Parallel()

	recorder := &querytestutil.FakeGovernanceAuditAppender{}
	auth := queryauth.AuthContext{Mode: queryauth.AuthModeBrowserSession, AllScopes: true}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", nil)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), auth))

	h := &MutationHandler{Audit: recorder}
	h.audit(req, governanceaudit.EventTypeIDPConfigChange, governanceaudit.DecisionAllowed, "provider_config_changed", "")

	if len(recorder.Events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(recorder.Events))
	}
	event := recorder.Events[0]
	if got, want := event.ActorClass, governanceaudit.ActorClassBrowserSession; got != want {
		t.Errorf("event.ActorClass = %q, want %q", got, want)
	}
	if event.ActorIDHash != "" {
		t.Errorf("event.ActorIDHash = %q, want blank: this emitter substitutes a hash only for shared_token", event.ActorIDHash)
	}
	if _, err := governanceaudit.NormalizeEvent(event); err == nil {
		t.Errorf("NormalizeEvent accepted a browser_session row with no actor identity; the store would persist it")
	}
}
