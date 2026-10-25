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
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// scriptedAcknowledger records calls and replays canned results.
type scriptedAcknowledger struct {
	mu          sync.Mutex
	resolves    int
	acknowledge int
	resolveErr  error
	ackErr      error
	result      pgstorage.TerminalFailureAckResult
	records     []pgstorage.TerminalFailureAckRecord
	lastRequest pgstorage.TerminalFailureAckRequest
	listErr     error
	lastFilter  pgstorage.TerminalFailureAckListFilter
	listPage    pgstorage.TerminalFailureListPage
	listFailErr error
	lastList    pgstorage.TerminalFailureListFilter
}

func (s *scriptedAcknowledger) ResolveTarget(_ context.Context, req pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolves++
	return s.result.TerminalFailureAckTarget, s.resolveErr
}

func (s *scriptedAcknowledger) Acknowledge(_ context.Context, req pgstorage.TerminalFailureAckRequest) (pgstorage.TerminalFailureAckResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acknowledge++
	s.lastRequest = req
	return s.result, s.ackErr
}

func (s *scriptedAcknowledger) ListTerminalFailures(_ context.Context, filter pgstorage.TerminalFailureListFilter) (pgstorage.TerminalFailureListPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastList = filter
	return s.listPage, s.listFailErr
}

func (s *scriptedAcknowledger) ListTerminalFailureAcks(_ context.Context, filter pgstorage.TerminalFailureAckListFilter) ([]pgstorage.TerminalFailureAckRecord, error) {
	s.lastFilter = filter
	return s.records, s.listErr
}

// memoryLedger is an in-memory replay idempotency ledger with the real claim
// semantics: the first claim of a key wins, a later claim sees the stored
// fingerprint and status, and Complete records the outcome.
type memoryLedger struct {
	mu          sync.Mutex
	rows        map[string]*ledgerRow
	completeErr error
}

type ledgerRow struct {
	fingerprint string
	status      string
	ids         []string
}

func newLedger() *memoryLedger { return &memoryLedger{rows: map[string]*ledgerRow{}} }

func (l *memoryLedger) ClaimReplayIdempotency(_ context.Context, key, fingerprint string, _ time.Time) (admin.ReplayIdempotencyClaim, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if row, ok := l.rows[key]; ok {
		return admin.ReplayIdempotencyClaim{Status: row.status, Fingerprint: row.fingerprint, ReplayedCount: len(row.ids), WorkItemIDs: row.ids}, nil
	}
	l.rows[key] = &ledgerRow{fingerprint: fingerprint, status: admin.ReplayRequestStatusInProgress}
	return admin.ReplayIdempotencyClaim{Claimed: true}, nil
}

func (l *memoryLedger) CompleteReplayIdempotency(_ context.Context, key string, _ int, ids []string, _ time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.completeErr != nil {
		return l.completeErr
	}
	l.rows[key].status = admin.ReplayRequestStatusCompleted
	l.rows[key].ids = ids
	return nil
}

// recordingAppender keeps the events the handler appends, after running each
// through the real normalizer the durable store runs at Append, so a stubbed
// appender cannot hide a rejected event.
type recordingAppender struct {
	t      *testing.T
	mu     sync.Mutex
	events []governanceaudit.Event
}

func (a *recordingAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, event := range events {
		if _, err := governanceaudit.NormalizeEvent(event); err != nil {
			a.t.Errorf("NormalizeEvent rejected the audit event %+v: %v", event, err)
		}
		a.events = append(a.events, event)
	}
	return nil
}

func adminAuth() auth.AuthContext { return auth.AuthContext{Mode: "shared", AllScopes: true} }

type fixtureParts struct {
	handler  *Handler
	store    *scriptedAcknowledger
	ledger   *memoryLedger
	appender *recordingAppender
}

func fixture(t *testing.T) fixtureParts {
	t.Helper()
	failureAt := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	store := &scriptedAcknowledger{result: pgstorage.TerminalFailureAckResult{
		TerminalFailureAckTarget: pgstorage.TerminalFailureAckTarget{
			CollectorKind: "security_alert", InstanceID: "alerts", ScopeID: "org:example", WorkItemID: "work-1",
			FailureClass: "auth_denied", FailureOperation: "list_org_alerts", FailureAt: failureAt,
		},
		AcknowledgedAt: failureAt.Add(time.Minute),
	}}
	ledger := newLedger()
	appender := &recordingAppender{t: t}
	return fixtureParts{
		handler:  &Handler{Acknowledger: store, Ledger: ledger, Audit: appender, Clock: func() time.Time { return failureAt }},
		store:    store,
		ledger:   ledger,
		appender: appender,
	}
}

const goodBody = `{"collector_kind":"security_alert","instance_id":"alerts","scope_id":"org:example","reason":"org left the program","idempotency_key":"key-1"}`

func post(handler http.HandlerFunc, path, body string, authCtx auth.AuthContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), authCtx))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// lastAuditEvent returns the newest recorded audit event.
func lastAuditEvent(t *testing.T, appender *recordingAppender) governanceaudit.Event {
	t.Helper()
	appender.mu.Lock()
	defer appender.mu.Unlock()
	if len(appender.events) == 0 {
		t.Fatal("no audit event recorded, want one per decision")
	}
	return appender.events[len(appender.events)-1]
}

func lastAudit(t *testing.T, appender *recordingAppender) (governanceaudit.Decision, string) {
	t.Helper()
	appender.mu.Lock()
	defer appender.mu.Unlock()
	if len(appender.events) == 0 {
		t.Fatal("no audit event recorded, want one per decision")
	}
	event := appender.events[len(appender.events)-1]
	if event.Type != governanceaudit.EventTypeAdminRecoveryAction {
		t.Fatalf("audit event type = %q, want the admin recovery action", event.Type)
	}
	return event.Decision, event.ReasonCode
}

func TestAcknowledgeRecordsAuditsAndNeverEchoesTheReason(t *testing.T) {
	p := fixture(t)
	rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if p.store.acknowledge != 1 || p.store.lastRequest.Reason != "org left the program" ||
		p.store.lastRequest.CollectorKind != "security_alert" || p.store.lastRequest.ActorClass == "" {
		t.Fatalf("store request = %+v after %d writes, want one write carrying the request and the audit actor class", p.store.lastRequest, p.store.acknowledge)
	}
	var body struct {
		Status          string `json:"status"`
		IdempotencyKey  string `json:"idempotency_key"`
		Acknowledgement struct {
			CollectorKind    string `json:"collector_kind"`
			InstanceID       string `json:"instance_id"`
			ScopeID          string `json:"scope_id"`
			WorkItemID       string `json:"work_item_id"`
			FailureClass     string `json:"failure_class"`
			FailureOperation string `json:"failure_operation"`
			FailedAt         string `json:"failed_at"`
			AcknowledgedAt   string `json:"acknowledged_at"`
		} `json:"acknowledgement"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "acknowledged" || body.IdempotencyKey != "key-1" || body.Acknowledgement.WorkItemID != "work-1" ||
		body.Acknowledgement.FailureClass != "auth_denied" || body.Acknowledgement.FailureOperation != "list_org_alerts" ||
		body.Acknowledgement.FailedAt == "" || body.Acknowledgement.AcknowledgedAt == "" {
		t.Fatalf("body = %s, want an acknowledged record with the bounded failure", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "org left the program") || strings.Contains(rec.Body.String(), "reason") {
		t.Fatalf("response echoes the operator's reason: %s", rec.Body.String())
	}
	if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionAllowed || reason != "ack_recorded" {
		t.Fatalf("audit = %s/%s, want allowed/ack_recorded", decision, reason)
	}
	// The audit event ties to the acknowledged target by hash and never carries
	// the target or the reason.
	event := lastAuditEvent(t, p.appender)
	if event.ScopeIDHash != ackScopeHash("security_alert", "alerts", "org:example") || !strings.HasPrefix(event.ScopeIDHash, "sha256:") {
		t.Fatalf("audit ScopeIDHash = %q, want the sha256 hash of the acknowledged target", event.ScopeIDHash)
	}
	if encoded, _ := json.Marshal(event); strings.Contains(string(encoded), "org:example") || strings.Contains(string(encoded), "org left the program") {
		t.Fatalf("audit event carries the scope or the reason: %s", encoded)
	}
}

func TestAcknowledgeDuplicateKeyReplaysTheStoredOutcome(t *testing.T) {
	p := fixture(t)
	first := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
	again := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
	if first.Code != http.StatusOK || again.Code != http.StatusOK {
		t.Fatalf("codes = %d, %d, want 200 and 200", first.Code, again.Code)
	}
	if p.store.acknowledge != 1 {
		t.Fatalf("store writes = %d, want 1 (the ledger answers the repeat)", p.store.acknowledge)
	}
	var replay struct {
		Status          string `json:"status"`
		Acknowledgement struct {
			WorkItemID string `json:"work_item_id"`
		} `json:"acknowledgement"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &replay); err != nil || replay.Status != "duplicate" || replay.Acknowledgement.WorkItemID != "work-1" {
		t.Fatalf("replay = %s (%v), want status duplicate with the stored work item", again.Body.String(), err)
	}
	if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionAllowed || reason != "ack_idempotent_replay" {
		t.Fatalf("audit = %s/%s, want allowed/ack_idempotent_replay", decision, reason)
	}
}

func TestAcknowledgeSameKeyDifferentTargetConflicts(t *testing.T) {
	p := fixture(t)
	post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
	other := strings.Replace(goodBody, `"scope_id":"org:example"`, `"scope_id":"org:other"`, 1)
	rec := post(p.handler.acknowledge, acknowledgeRoute, other, adminAuth())
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if p.store.acknowledge != 1 {
		t.Fatalf("store writes = %d, want the conflicting call to write nothing", p.store.acknowledge)
	}
	if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionDenied || reason != "ack_idempotency_key_reused" {
		t.Fatalf("audit = %s/%s, want denied/ack_idempotency_key_reused", decision, reason)
	}
}

// TestAcknowledgeConcurrentSameKeyWritesOnce proves the ledger claim serializes
// two requests with one key: exactly one reaches the store.
func TestAcknowledgeConcurrentSameKeyWritesOnce(t *testing.T) {
	p := fixture(t)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth()).Code
		}()
	}
	wg.Wait()
	close(codes)
	if p.store.acknowledge != 1 {
		t.Fatalf("store writes = %d across 8 concurrent calls with one key, want 1", p.store.acknowledge)
	}
	for code := range codes {
		if code != http.StatusOK && code != http.StatusConflict {
			t.Fatalf("concurrent call answered %d, want 200 (done) or 409 (in progress)", code)
		}
	}
}

func TestAcknowledgeRefusals(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		auth         auth.AuthContext
		resolveErr   error
		wantCode     int
		wantReason   string
		wantResolves int
	}{
		{"missing reason", `{"collector_kind":"k","instance_id":"a","scope_id":"s","reason":"  ","idempotency_key":"k1"}`, adminAuth(), nil, 400, "ack_refused_missing_reason", 0},
		{"missing key", `{"collector_kind":"k","instance_id":"a","scope_id":"s","reason":"r"}`, adminAuth(), nil, 400, "ack_refused_missing_idempotency_key", 0},
		{"missing kind", `{"instance_id":"a","scope_id":"s","reason":"r","idempotency_key":"k1"}`, adminAuth(), nil, 400, "ack_refused_invalid_request", 0},
		{"missing instance", `{"collector_kind":"k","scope_id":"s","reason":"r","idempotency_key":"k1"}`, adminAuth(), nil, 400, "ack_refused_invalid_request", 0},
		{"missing scope", `{"collector_kind":"k","instance_id":"a","reason":"r","idempotency_key":"k1"}`, adminAuth(), nil, 400, "ack_refused_invalid_request", 0},
		{"reason too long", `{"collector_kind":"k","instance_id":"a","scope_id":"s","reason":"` + strings.Repeat("x", 513) + `","idempotency_key":"k1"}`, adminAuth(), nil, 400, "ack_refused_invalid_request", 0},
		{"malformed body", `{`, adminAuth(), nil, 400, "", 0},
		{"scoped token", goodBody, auth.AuthContext{Mode: "scoped", AllScopes: false}, nil, 403, "ack_refused_unauthorized", 0},
		{"unknown instance", goodBody, adminAuth(), pgstorage.ErrTerminalFailureAckInstanceNotFound, 404, "ack_refused_instance_not_found", 1},
		{"nothing to acknowledge", goodBody, adminAuth(), pgstorage.ErrTerminalFailureAckNoFailure, 422, "ack_refused_no_terminal_failure", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture(t)
			p.store.resolveErr = tc.resolveErr
			rec := post(p.handler.acknowledge, acknowledgeRoute, tc.body, tc.auth)
			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if p.store.resolves != tc.wantResolves || p.store.acknowledge != 0 {
				t.Fatalf("store calls = %d resolves and %d writes, want %d and 0", p.store.resolves, p.store.acknowledge, tc.wantResolves)
			}
			if len(p.ledger.rows) != 0 {
				t.Fatalf("ledger rows = %d, want a refusal to leave the idempotency key unconsumed", len(p.ledger.rows))
			}
			if tc.wantReason == "" {
				return
			}
			if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionDenied || reason != tc.wantReason {
				t.Fatalf("audit = %s/%s, want denied/%s", decision, reason, tc.wantReason)
			}
		})
	}
}

// TestAcknowledgeNoFailureAnswerIsTruthfulPerPath pins the 422 detail for both
// places the same refusal can arrive. Before the claim the key is unconsumed.
// From the re-resolve inside the acknowledgement transaction (a resolution
// landed after the pre-claim resolve) the key is consumed: the ledger row stays
// in_progress and a retry with the same key answers 409 until a fresh key is used.
func TestAcknowledgeNoFailureAnswerIsTruthfulPerPath(t *testing.T) {
	t.Run("before the claim", func(t *testing.T) {
		p := fixture(t)
		p.store.resolveErr = pgstorage.ErrTerminalFailureAckNoFailure
		rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if rec.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(rec.Body.String(), "nothing was acknowledged and the idempotency key was not consumed") {
			t.Fatalf("response = %d %s, want 422 saying the key was not consumed", rec.Code, rec.Body.String())
		}
		if len(p.ledger.rows) != 0 {
			t.Fatalf("ledger rows = %d, want the key unconsumed", len(p.ledger.rows))
		}
	})
	t.Run("inside the acknowledgement transaction", func(t *testing.T) {
		p := fixture(t)
		p.store.ackErr = pgstorage.ErrTerminalFailureAckNoFailure
		rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("code = %d, want 422: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if strings.Contains(body, "was not consumed") ||
			!strings.Contains(body, "the idempotency key is now consumed") ||
			!strings.Contains(body, "new idempotency_key") {
			t.Fatalf("body = %s, want the key reported consumed and a fresh key requested", body)
		}
		if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionDenied || reason != "ack_refused_no_terminal_failure" {
			t.Fatalf("audit = %s/%s, want denied/ack_refused_no_terminal_failure", decision, reason)
		}
		retry := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if retry.Code != http.StatusConflict {
			t.Fatalf("same-key retry = %d %s, want 409 until a fresh key is used", retry.Code, retry.Body.String())
		}
	})
}

// TestAcknowledgeStoreFaultLeaksNothing pins the #7674 rule: a store fault
// answers the fixed message, never the backend text, and audits the failure.
func TestAcknowledgeStoreFaultLeaksNothing(t *testing.T) {
	p := fixture(t)
	p.store.ackErr = errors.New("pq: password authentication failed for user eshu at 10.0.0.7")
	rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "10.0.0.7") ||
		!strings.Contains(rec.Body.String(), acknowledgeFailedMessage) {
		t.Fatalf("body = %s, want only the fixed message", rec.Body.String())
	}
	if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionDenied || reason != "ack_failed" {
		t.Fatalf("audit = %s/%s, want denied/ack_failed", decision, reason)
	}
}

func TestAcknowledgeLedgerFinalizeFaultLeaksNothing(t *testing.T) {
	p := fixture(t)
	p.ledger.completeErr = errors.New("pq: connection refused at 10.0.0.7")
	rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "10.0.0.7") ||
		!strings.Contains(rec.Body.String(), recordFailedMessage) {
		t.Fatalf("response = %d %s, want 500 with only the fixed finalize message", rec.Code, rec.Body.String())
	}
}

func TestAcknowledgeNotConfigured(t *testing.T) {
	for name, handler := range map[string]*Handler{"empty": {}, "no ledger": {Acknowledger: &scriptedAcknowledger{}}} {
		rec := post(handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: code = %d, want 503", name, rec.Code)
		}
	}
}

// TestQueryAcknowledgementsReturnsTheReasonOnlyToAdmins pins the admin-only read: the reason
// appears here and nowhere else, and a scoped token is refused.
func TestQueryAcknowledgementsReturnsTheReasonOnlyToAdmins(t *testing.T) {
	p := fixture(t)
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	p.store.records = []pgstorage.TerminalFailureAckRecord{{
		CollectorKind: "security_alert", InstanceID: "alerts", ScopeID: "org:example", FailureClass: "auth_denied",
		FailureAt: at, WorkItemID: "work-1", Reason: "org left the program", ActorClass: "shared_token", CorrelationID: "corr-1", AcknowledgedAt: at,
	}}
	rec := post(p.handler.queryAcknowledgements, queryRoute, `{"instance_id":"alerts","limit":5}`, adminAuth())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reason":"org left the program"`) {
		t.Fatalf("admin query = %d %s, want 200 with the reason", rec.Code, rec.Body.String())
	}
	if p.store.lastFilter.InstanceID != "alerts" || p.store.lastFilter.Limit != 5 {
		t.Fatalf("filter = %+v, want the request filter passed through", p.store.lastFilter)
	}
	scoped := post(p.handler.queryAcknowledgements, queryRoute, `{}`, auth.AuthContext{Mode: "scoped"})
	if scoped.Code != http.StatusForbidden || strings.Contains(scoped.Body.String(), "org left the program") {
		t.Fatalf("scoped query = %d %s, want 403 with no reason", scoped.Code, scoped.Body.String())
	}
	p.store.listErr = errors.New("pq: boom at 10.0.0.7")
	failed := post(p.handler.queryAcknowledgements, queryRoute, `{}`, adminAuth())
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "10.0.0.7") {
		t.Fatalf("failed query = %d %s, want 500 with the fixed message", failed.Code, failed.Body.String())
	}
}

func TestMountRegistersBothRoutes(t *testing.T) {
	p := fixture(t)
	mux := http.NewServeMux()
	p.handler.Mount(mux)
	for _, path := range []string{acknowledgeRoute, queryRoute} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(goodBody))
		req = req.WithContext(auth.ContextWithAuthContext(req.Context(), adminAuth()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Fatalf("%s is not mounted (%d)", path, rec.Code)
		}
	}
}

func TestOutcomeRoundTripsThroughTheLedgerEncoding(t *testing.T) {
	p := fixture(t)
	encoded := encodeOutcome(p.store.result)
	decoded, ok := decodeOutcome(encoded)
	if !ok || decoded.WorkItemID != "work-1" || decoded.FailureClass != "auth_denied" ||
		!decoded.FailureAt.Equal(p.store.result.FailureAt) || !decoded.AcknowledgedAt.Equal(p.store.result.AcknowledgedAt) {
		t.Fatalf("decode(encode(result)) = %+v, %v; want the same outcome", decoded, ok)
	}
	if _, ok := decodeOutcome([]string{"only", "two"}); ok {
		t.Fatal("decodeOutcome accepted a foreign ledger row")
	}
}
