// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type fakeTerminalFailureAcknowledger struct{ calls int }

func (f *fakeTerminalFailureAcknowledger) ResolveTarget(context.Context, pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckTarget, error) {
	return pgstorage.TerminalFailureAckTarget{}, nil
}

func (f *fakeTerminalFailureAcknowledger) Acknowledge(context.Context, pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckResult, error) {
	f.calls++
	return pgstorage.TerminalFailureAckResult{TerminalFailureAckTarget: pgstorage.TerminalFailureAckTarget{
		CollectorKind: "security_alert", InstanceID: "alerts", ScopeID: "org:example", FailureClass: "auth_denied",
	}}, nil
}

func (f *fakeTerminalFailureAcknowledger) ListTerminalFailureAcks(context.Context, pgstorage.TerminalFailureAckListFilter) ([]pgstorage.TerminalFailureAckRecord, error) {
	return nil, nil
}

type fakeTerminalFailureLedger struct{}

func (fakeTerminalFailureLedger) ClaimReplayIdempotency(context.Context, string, string, time.Time) (admin.ReplayIdempotencyClaim, error) {
	return admin.ReplayIdempotencyClaim{Claimed: true}, nil
}

func (fakeTerminalFailureLedger) CompleteReplayIdempotency(context.Context, string, int, []string, time.Time) error {
	return nil
}

// TestAPIRouterMountsTheTerminalFailureAcknowledgement pins that the root
// router serves both acknowledgement routes through the leaf (#8830), and that a
// router without the handler does not mount them.
func TestAPIRouterMountsTheTerminalFailureAcknowledgement(t *testing.T) {
	t.Parallel()
	const writePath = "/api/v0/admin/collector-failures/acknowledge"
	const readPath = "/api/v0/admin/collector-failures/acknowledgements/query"
	body := `{"collector_kind":"security_alert","instance_id":"alerts","scope_id":"org:example","reason":"expected","idempotency_key":"k1"}`

	fake := &fakeTerminalFailureAcknowledger{}
	mounted := http.NewServeMux()
	(&APIRouter{AdminTerminalFailure: &AdminTerminalFailureHandler{Acknowledger: fake, Ledger: fakeTerminalFailureLedger{}}}).Mount(mounted)
	rec := httptest.NewRecorder()
	mounted.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, writePath, strings.NewReader(body)))
	if rec.Code != http.StatusOK || fake.calls != 1 {
		t.Fatalf("mounted write route = %d with %d calls, want 200 with one call: %s", rec.Code, fake.calls, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mounted.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, readPath, strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("mounted query route = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	bare := http.NewServeMux()
	(&APIRouter{}).Mount(bare)
	for _, path := range []string{writePath, readPath} {
		rec = httptest.NewRecorder()
		bare.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unwired %s = %d, want 404", path, rec.Code)
		}
	}
}
