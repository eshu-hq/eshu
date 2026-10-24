// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// scriptedAcknowledger records calls and replays a canned result.
type scriptedAcknowledger struct {
	calls  []pgstorage.TerminalFailureAckRequest
	result pgstorage.TerminalFailureAckResult
	err    error
}

func (s *scriptedAcknowledger) Acknowledge(_ context.Context, req pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckResult, error) {
	s.calls = append(s.calls, req)
	return s.result, s.err
}

// recordingAppender keeps the events the handler appends, after running each
// through the real normalizer the durable store runs at Append, so a stubbed
// appender cannot hide a rejected event.
type recordingAppender struct {
	t      *testing.T
	events []governanceaudit.Event
}

func (a *recordingAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	for _, event := range events {
		if _, err := governanceaudit.NormalizeEvent(event); err != nil {
			a.t.Errorf("NormalizeEvent rejected the audit event %+v: %v", event, err)
		}
		a.events = append(a.events, event)
	}
	return nil
}

func adminAuth() auth.AuthContext { return auth.AuthContext{Mode: "shared", AllScopes: true} }

func fixture(t *testing.T) (*Handler, *scriptedAcknowledger, *recordingAppender) {
	t.Helper()
	failureAt := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	ack := &scriptedAcknowledger{result: pgstorage.TerminalFailureAckResult{
		CollectorKind: "security_alert", InstanceID: "alerts", ScopeID: "org:example",
		FailureClass: "auth_denied", FailureAt: failureAt, AcknowledgedAt: failureAt.Add(time.Minute),
		Reason: "org left the program",
	}}
	appender := &recordingAppender{t: t}
	return &Handler{Acknowledger: ack, Audit: appender, Clock: func() time.Time { return failureAt }}, ack, appender
}

const goodBody = `{"instance_id":"alerts","scope_id":"org:example","failure_class":"auth_denied","reason":"org left the program"}`

func post(handler *Handler, body string, authCtx auth.AuthContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
	rec := httptest.NewRecorder()
	handler.acknowledge(rec, req)
	return rec
}

func lastReason(t *testing.T, appender *recordingAppender) (governanceaudit.Decision, string) {
	t.Helper()
	if len(appender.events) == 0 {
		t.Fatal("no audit event recorded, want one per decision")
	}
	event := appender.events[len(appender.events)-1]
	if event.Type != governanceaudit.EventTypeAdminRecoveryAction {
		t.Fatalf("audit event type = %q, want the admin recovery action", event.Type)
	}
	return event.Decision, event.ReasonCode
}

func TestAcknowledgeRecordsAndAudits(t *testing.T) {
	handler, ack, appender := fixture(t)
	rec := post(handler, goodBody, adminAuth())
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(ack.calls) != 1 || ack.calls[0].InstanceID != "alerts" || ack.calls[0].ScopeID != "org:example" ||
		ack.calls[0].FailureClass != "auth_denied" || ack.calls[0].Reason != "org left the program" {
		t.Fatalf("calls = %+v, want one call carrying the request", ack.calls)
	}
	if ack.calls[0].ActorClass == "" {
		t.Fatalf("actor class is empty, want the audit actor class passed to the store: %+v", ack.calls[0])
	}
	var body struct {
		Status          string `json:"status"`
		Duplicate       bool   `json:"duplicate"`
		Acknowledgement struct {
			CollectorKind  string `json:"collector_kind"`
			InstanceID     string `json:"instance_id"`
			ScopeID        string `json:"scope_id"`
			FailureClass   string `json:"failure_class"`
			FailureAt      string `json:"failure_at"`
			AcknowledgedAt string `json:"acknowledged_at"`
			Reason         string `json:"reason"`
		} `json:"acknowledgement"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "acknowledged" || body.Duplicate || body.Acknowledgement.CollectorKind != "security_alert" ||
		body.Acknowledgement.FailureClass != "auth_denied" || body.Acknowledgement.FailureAt == "" ||
		body.Acknowledgement.AcknowledgedAt == "" || body.Acknowledgement.Reason != "org left the program" {
		t.Fatalf("body = %s, want an acknowledged record", rec.Body.String())
	}
	if decision, reason := lastReason(t, appender); decision != governanceaudit.DecisionAllowed || reason != "collector_failure_ack_recorded" {
		t.Fatalf("audit = %s/%s, want allowed/collector_failure_ack_recorded", decision, reason)
	}
}

func TestAcknowledgeRepeatIsUnchanged(t *testing.T) {
	handler, ack, appender := fixture(t)
	ack.result.Unchanged = true
	rec := post(handler, goodBody, adminAuth())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"duplicate":true`) ||
		!strings.Contains(rec.Body.String(), `"status":"unchanged"`) {
		t.Fatalf("response = %d %s, want 200 unchanged duplicate", rec.Code, rec.Body.String())
	}
	if decision, reason := lastReason(t, appender); decision != governanceaudit.DecisionAllowed || reason != "collector_failure_ack_unchanged" {
		t.Fatalf("audit = %s/%s, want allowed/collector_failure_ack_unchanged", decision, reason)
	}
}

func TestAcknowledgeRefusals(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		auth       auth.AuthContext
		storeErr   error
		wantCode   int
		wantReason string
		wantCalls  int
	}{
		{"missing reason", `{"instance_id":"a","scope_id":"s","failure_class":"auth_denied","reason":"  "}`, adminAuth(), nil, 400, "collector_failure_ack_refused_missing_reason", 0},
		{"missing instance", `{"scope_id":"s","failure_class":"auth_denied","reason":"r"}`, adminAuth(), nil, 400, "collector_failure_ack_refused_invalid_request", 0},
		{"missing scope", `{"instance_id":"a","failure_class":"auth_denied","reason":"r"}`, adminAuth(), nil, 400, "collector_failure_ack_refused_invalid_request", 0},
		{"missing class", `{"instance_id":"a","scope_id":"s","reason":"r"}`, adminAuth(), nil, 400, "collector_failure_ack_refused_invalid_request", 0},
		{"reason too long", `{"instance_id":"a","scope_id":"s","failure_class":"auth_denied","reason":"` + strings.Repeat("x", 513) + `"}`, adminAuth(), nil, 400, "collector_failure_ack_refused_invalid_request", 0},
		{"malformed body", `{`, adminAuth(), nil, 400, "", 0},
		{"scoped token", goodBody, auth.AuthContext{Mode: "scoped", AllScopes: false}, nil, 403, "collector_failure_ack_refused_unauthorized", 0},
		{"unknown instance", goodBody, adminAuth(), pgstorage.ErrTerminalFailureAckInstanceNotFound, 404, "collector_failure_ack_refused_instance_not_found", 1},
		{"no failure", goodBody, adminAuth(), pgstorage.ErrTerminalFailureAckNoFailure, 404, "collector_failure_ack_refused_no_failure", 1},
		{"class changed", goodBody, adminAuth(), pgstorage.ErrTerminalFailureAckClassChanged, 409, "collector_failure_ack_refused_class_changed", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, ack, appender := fixture(t)
			ack.err = tc.storeErr
			rec := post(handler, tc.body, tc.auth)
			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if len(ack.calls) != tc.wantCalls {
				t.Fatalf("store calls = %d, want %d", len(ack.calls), tc.wantCalls)
			}
			if tc.wantReason == "" {
				return
			}
			if decision, reason := lastReason(t, appender); decision != governanceaudit.DecisionDenied || reason != tc.wantReason {
				t.Fatalf("audit = %s/%s, want denied/%s", decision, reason, tc.wantReason)
			}
		})
	}
}

// TestAcknowledgeStoreFaultLeaksNothing pins the #7674 rule: a store fault
// answers the fixed message, never the backend text, and audits the failure.
func TestAcknowledgeStoreFaultLeaksNothing(t *testing.T) {
	handler, ack, appender := fixture(t)
	ack.err = errors.New("pq: password authentication failed for user eshu at 10.0.0.7")
	rec := post(handler, goodBody, adminAuth())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "10.0.0.7") ||
		!strings.Contains(rec.Body.String(), acknowledgeFailedMessage) {
		t.Fatalf("body = %s, want only the fixed message", rec.Body.String())
	}
	if decision, reason := lastReason(t, appender); decision != governanceaudit.DecisionDenied || reason != "collector_failure_ack_failed" {
		t.Fatalf("audit = %s/%s, want denied/collector_failure_ack_failed", decision, reason)
	}
}

func TestAcknowledgeNotConfigured(t *testing.T) {
	rec := post(&Handler{}, goodBody, adminAuth())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503 when no acknowledger is wired", rec.Code)
	}
}

func TestMountRegistersTheRoute(t *testing.T) {
	handler, ack, _ := fixture(t)
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(goodBody))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), adminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(ack.calls) != 1 {
		t.Fatalf("mounted route = %d with %d calls, want 200 with one call", rec.Code, len(ack.calls))
	}
}
