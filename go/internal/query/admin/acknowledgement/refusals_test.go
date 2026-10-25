// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

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

// TestAcknowledgeInstanceNotFoundAnswerIsTruthfulPerPath pins the 404 for both
// places an unknown instance can arrive. Before the claim the key stays
// unconsumed and the body is the plain unknown-instance answer. From the
// re-resolve inside the acknowledgement transaction (the instance was disabled
// or deactivated after the first check) the key is consumed, so the body keeps
// the same message and also says the key is consumed.
func TestAcknowledgeInstanceNotFoundAnswerIsTruthfulPerPath(t *testing.T) {
	const message = "collector instance is not registered or not active for that collector_kind"
	t.Run("before the claim", func(t *testing.T) {
		p := fixture(t)
		p.store.resolveErr = pgstorage.ErrTerminalFailureAckInstanceNotFound
		rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), message) {
			t.Fatalf("response = %d %s, want 404 with the unknown-instance message", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "consumed") {
			t.Fatalf("body = %s, want no consumed-key detail before the claim", rec.Body.String())
		}
		if len(p.ledger.rows) != 0 {
			t.Fatalf("ledger rows = %d, want the key unconsumed", len(p.ledger.rows))
		}
	})
	t.Run("inside the acknowledgement transaction", func(t *testing.T) {
		p := fixture(t)
		p.store.ackErr = pgstorage.ErrTerminalFailureAckInstanceNotFound
		rec := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body = %s, not JSON: %v", rec.Body.String(), err)
		}
		if body["error"] != "Not Found" || body["detail"] != message {
			t.Fatalf("body = %v, want the existing error and detail kept", body)
		}
		if body["key_consumed"] != true {
			t.Fatalf("body = %v, want key_consumed true", body)
		}
		keyDetail, _ := body["key_detail"].(string)
		if !strings.Contains(keyDetail, "nothing was acknowledged") ||
			!strings.Contains(keyDetail, "the idempotency key is now consumed") ||
			!strings.Contains(keyDetail, "new idempotency_key") {
			t.Fatalf("key_detail = %q, want the key reported consumed and a fresh key requested", keyDetail)
		}
		if decision, reason := lastAudit(t, p.appender); decision != governanceaudit.DecisionDenied || reason != "ack_refused_instance_not_found" {
			t.Fatalf("audit = %s/%s, want denied/ack_refused_instance_not_found", decision, reason)
		}
		retry := post(p.handler.acknowledge, acknowledgeRoute, goodBody, adminAuth())
		if retry.Code != http.StatusConflict {
			t.Fatalf("same-key retry = %d %s, want 409 until a fresh key is used", retry.Code, retry.Body.String())
		}
	})
}
