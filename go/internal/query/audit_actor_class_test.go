// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// actorClassModeCases lists every AuthMode member with a subject hash and the
// actor class the governance audit must stamp for it. It is shared by the
// tests below so one credential is pinned to one class at every emitter
// (#6566): a cookie session is browser_session, a scoped or OIDC bearer is
// scoped_token, and the legacy shared bearer is shared_token.
func actorClassModeCases() []struct {
	name string
	mode AuthMode
	want governanceaudit.ActorClass
} {
	return []struct {
		name string
		mode AuthMode
		want governanceaudit.ActorClass
	}{
		{name: "browser session", mode: AuthModeBrowserSession, want: governanceaudit.ActorClassBrowserSession},
		{name: "scoped bearer", mode: AuthModeScoped, want: governanceaudit.ActorClassScopedToken},
		{name: "shared bearer", mode: AuthModeShared, want: governanceaudit.ActorClassSharedToken},
	}
}

// TestActorClassForAuthMapsEveryAuthMode pins the shared mapping every audit
// emitter uses: each AuthMode member maps to the class named for its
// credential, and a context nobody authenticated maps to anonymous.
func TestActorClassForAuthMapsEveryAuthMode(t *testing.T) {
	t.Parallel()

	for _, mode := range actorClassModeCases() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			if got := actorClassForAuth(AuthContext{Mode: mode.mode}); got != mode.want {
				t.Errorf("actorClassForAuth(%q) = %q, want %q", mode.mode, got, mode.want)
			}
		})
	}
	t.Run("no auth context", func(t *testing.T) {
		t.Parallel()
		if got, want := actorClassForAuth(AuthContext{}), governanceaudit.ActorClassAnonymous; got != want {
			t.Errorf("actorClassForAuth(blank mode) = %q, want %q", got, want)
		}
	})
}

// identityMutationEmitters names every staying production emitter that stamps
// an actor class on an identity-mutation row, so the mapping is pinned at each
// one rather than at a representative. The admin identity-mutation and
// provider-config emitters moved to admin/identity/actor_test.go and
// admin/provider/config/actor_test.go with the families they pin (#6060,
// lane B S1); the recovery-actor mapping moved to admin/actor_test.go.
func identityMutationEmitters() []struct {
	name string
	emit func(r *http.Request, audit GovernanceAuditAppender)
} {
	return []struct {
		name string
		emit func(r *http.Request, audit GovernanceAuditAppender)
	}{
		{
			name: "local identity",
			emit: func(r *http.Request, audit GovernanceAuditAppender) {
				h := &LocalIdentityHandler{Audit: audit}
				h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionAllowed, "break_glass_enabled", "")
			},
		},
		{
			name: "sign-in policy mutation",
			emit: func(r *http.Request, audit GovernanceAuditAppender) {
				h := &SignInPolicyMutationHandler{Audit: audit}
				h.audit(r, governanceaudit.DecisionAllowed, "sign_in_policy_changed", "")
			},
		},
	}
}

// TestIdentityMutationAuditsStampActorClassByAuthMode drives each identity
// mutation emitter with every AuthMode member and checks the stamped class.
// Before #6566 the local-identity helper filed a cookie session as operator.
func TestIdentityMutationAuditsStampActorClassByAuthMode(t *testing.T) {
	t.Parallel()

	for _, emitter := range identityMutationEmitters() {
		for _, mode := range actorClassModeCases() {
			t.Run(emitter.name+"/"+mode.name, func(t *testing.T) {
				t.Parallel()
				audit := &querytestutil.FakeGovernanceAuditAppender{}
				auth := AuthContext{Mode: mode.mode, SubjectIDHash: "sha256:abcdef12", AllScopes: true}
				req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/anything", nil)
				req = req.WithContext(ContextWithAuthContext(req.Context(), auth))

				emitter.emit(req, audit)

				if len(audit.Events) != 1 {
					t.Fatalf("audit events = %d, want 1", len(audit.Events))
				}
				event := audit.Events[0]
				if event.ActorClass != mode.want {
					t.Errorf("event.ActorClass = %q, want %q", event.ActorClass, mode.want)
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
}

// TestRecordScopedReadAuthorizedActorClassFollowsAuthMode pins the allowed
// read helper to the same mapping as the denial helper, so an allowed and a
// denied read by one credential never carry two classes.
func TestRecordScopedReadAuthorizedActorClassFollowsAuthMode(t *testing.T) {
	t.Parallel()

	for _, mode := range actorClassModeCases() {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			audit := &fakeGovernanceAuditAppender{}
			auth := AuthContext{Mode: mode.mode, SubjectIDHash: "sha256:abcdef12", AllScopes: true}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/status", nil)

			recordScopedReadAuthorized(req, audit, auth)

			if len(audit.events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(audit.events))
			}
			if got := audit.events[0].ActorClass; got != mode.want {
				t.Errorf("event.ActorClass = %q, want %q", got, mode.want)
			}
		})
	}
	t.Run("no subject hash downgrades to anonymous", func(t *testing.T) {
		t.Parallel()
		audit := &fakeGovernanceAuditAppender{}
		req := httptest.NewRequest(http.MethodGet, "/api/v0/status", nil)

		recordScopedReadAuthorized(req, audit, AuthContext{Mode: AuthModeBrowserSession})

		if len(audit.events) != 1 {
			t.Fatalf("audit events = %d, want 1", len(audit.events))
		}
		if got, want := audit.events[0].ActorClass, governanceaudit.ActorClassAnonymous; got != want {
			t.Errorf("event.ActorClass = %q, want %q", got, want)
		}
	})
}

// TestIdentityMutationAuditsWithNoSubjectHash pins the hash rule each
// identity-mutation emitter kept through #6566 when a cookie session carries
// no subject hash. The local-identity helper substitutes localIdentityHash of
// the mode and the row is valid. The admin identity, provider config, and
// sign-in policy emitters keep browser_session with a blank hash, and
// NormalizeEvent, which the durable store runs at Append, rejects that row.
func TestIdentityMutationAuditsWithNoSubjectHash(t *testing.T) {
	t.Parallel()

	substitutesHash := map[string]bool{"local identity": true}
	for _, emitter := range identityMutationEmitters() {
		t.Run(emitter.name, func(t *testing.T) {
			t.Parallel()
			audit := &querytestutil.FakeGovernanceAuditAppender{}
			auth := AuthContext{Mode: AuthModeBrowserSession, AllScopes: true}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/anything", nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), auth))

			emitter.emit(req, audit)

			if len(audit.Events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(audit.Events))
			}
			event := audit.Events[0]
			if got, want := event.ActorClass, governanceaudit.ActorClassBrowserSession; got != want {
				t.Errorf("event.ActorClass = %q, want %q", got, want)
			}
			_, err := governanceaudit.NormalizeEvent(event)
			if substitutesHash[emitter.name] {
				if got, want := event.ActorIDHash, localIdentityHash(string(AuthModeBrowserSession)); got != want {
					t.Errorf("event.ActorIDHash = %q, want localIdentityHash(mode) %q", got, want)
				}
				if err != nil {
					t.Errorf("NormalizeEvent rejected the local-identity row: %v", err)
				}
				return
			}
			if event.ActorIDHash != "" {
				t.Errorf("event.ActorIDHash = %q, want blank: this emitter substitutes a hash only for shared_token", event.ActorIDHash)
			}
			if err == nil {
				t.Errorf("NormalizeEvent accepted a browser_session row with no actor identity; the store would persist it")
			}
		})
	}
}
