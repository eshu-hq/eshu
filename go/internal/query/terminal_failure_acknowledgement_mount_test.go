// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type fakeTerminalFailureAcknowledger struct{ calls int }

func (f *fakeTerminalFailureAcknowledger) Acknowledge(context.Context, pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckResult, error) {
	f.calls++
	return pgstorage.TerminalFailureAckResult{CollectorKind: "security_alert", InstanceID: "alerts", ScopeID: "org:example", FailureClass: "auth_denied"}, nil
}

// TestAPIRouterMountsTheTerminalFailureAcknowledgement pins that the root
// router serves the acknowledgement route through the leaf (#8830), and that a
// router without the handler does not mount it.
func TestAPIRouterMountsTheTerminalFailureAcknowledgement(t *testing.T) {
	t.Parallel()
	const path = "/api/v0/admin/collector-terminal-failure-acknowledgements"
	body := `{"instance_id":"alerts","scope_id":"org:example","failure_class":"auth_denied","reason":"expected"}`

	fake := &fakeTerminalFailureAcknowledger{}
	mounted := http.NewServeMux()
	(&APIRouter{AdminTerminalFailure: &AdminTerminalFailureHandler{Acknowledger: fake}}).Mount(mounted)
	rec := httptest.NewRecorder()
	mounted.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if rec.Code != http.StatusOK || fake.calls != 1 {
		t.Fatalf("mounted route = %d with %d calls, want 200 with one call: %s", rec.Code, fake.calls, rec.Body.String())
	}

	bare := http.NewServeMux()
	(&APIRouter{}).Mount(bare)
	rec = httptest.NewRecorder()
	bare.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unwired route = %d, want 404", rec.Code)
	}
}
