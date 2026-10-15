// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestIncidentContextAnswersFixedServerFailures is the #7674 regression for
// both failure steps of GET /api/v0/incidents/{incident_id}/context. The
// context read answered 500 with the store error text; the scoped-token
// authorization answered a fixed 500 but skipped the reader-fence 503 and
// answered a client cancel as a server fault. Both stay fail closed. Not
// parallel: it swaps incidentHandlerTracer.
func TestIncidentContextAnswersFixedServerFailures(t *testing.T) {
	const path = "/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&limit=10"
	failure.Run(t, failure.Suite{HandlerTracer: &incidentHandlerTracer, Routes: []failure.Route{
		{
			Name: "context read", Path: path, Message: incidentContextReadFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &IncidentHandler{Context: erroringIncidentContextStore{err: err}}
			},
		},
		{
			Name: "scoped authorization", Path: path, Message: incidentContextAuthorizationFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &IncidentHandler{
					Context:    &failingIncidentContextStore{},
					Authorizer: &recordingIncidentRepositoryAuthorizer{err: err},
				}
			},
			Scope: func(ctx context.Context) context.Context {
				return auth.ContextWithAuthContext(ctx, auth.AuthContext{
					Mode:                 auth.AuthModeScoped,
					TenantID:             "tenant-a",
					WorkspaceID:          "workspace-a",
					AllowedRepositoryIDs: []string{"repo-team-a"},
				})
			},
		},
	}})
}

// TestIncidentContextNotFoundAnswersFixedText proves a not-found that arrives
// wrapped answers the sentinel's fixed text, never the wrapper's (#7674). The
// scoped denial already answers the same text, so the two stay identical.
func TestIncidentContextNotFoundAnswersFixedText(t *testing.T) {
	err := fmt.Errorf("backend: %s: %w", failure.Canary, model.ErrIncidentContextNotFound)
	rec, _ := failure.Serve(failure.Suite{HandlerTracer: &incidentHandlerTracer},
		failure.Route{Path: "/api/v0/incidents/PABC123/context?limit=10"},
		&IncidentHandler{Context: erroringIncidentContextStore{err: err}}, false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := failure.Message(rec.Body.Bytes()), model.ErrIncidentContextNotFound.Error(); got != want {
		t.Fatalf("body message = %q, want the fixed %q; body = %s", got, want, rec.Body.String())
	}
}

// erroringIncidentContextStore fails the incident context read with err.
type erroringIncidentContextStore struct{ err error }

func (s erroringIncidentContextStore) ReadIncidentContext(
	context.Context, model.IncidentContextFilter,
) (model.IncidentContextSnapshot, error) {
	return model.IncidentContextSnapshot{}, s.err
}
