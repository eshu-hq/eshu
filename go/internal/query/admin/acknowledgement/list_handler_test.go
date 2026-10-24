// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const listBody = `{"collector_kind":"security_alert","instance_id":"alerts","limit":2,"cursor":"scope-a"}`

func seedListPage(p fixtureParts) {
	at := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	p.store.listPage = pgstorage.TerminalFailureListPage{
		Items: []pgstorage.TerminalFailureListItem{
			{ScopeID: "scope-b", FailureClass: "rate_limited", FailureOperation: "list_org_alerts", FailedAt: at, WorkItemID: "work-b"},
			{
				ScopeID: "scope-c", FailureClass: "auth_denied", FailureOperation: "list_org_alerts", FailedAt: at, WorkItemID: "work-c",
				Acknowledged: true, AcknowledgedAt: at.Add(time.Minute),
			},
		},
		Truncated:  true,
		NextCursor: "scope-c",
	}
}

func TestQueryCollectorFailuresListsScopesForAnAdmin(t *testing.T) {
	p := fixture(t)
	seedListPage(p)
	rec := post(p.handler.queryCollectorFailures, listRoute, listBody, adminAuth())
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if p.store.lastList.CollectorKind != "security_alert" || p.store.lastList.InstanceID != "alerts" ||
		p.store.lastList.Limit != 2 || p.store.lastList.Cursor != "scope-a" {
		t.Fatalf("store filter = %+v, want the request passed through", p.store.lastList)
	}
	var body struct {
		Count      int    `json:"count"`
		Truncated  bool   `json:"truncated"`
		NextCursor string `json:"next_cursor"`
		Items      []struct {
			ScopeID          string `json:"scope_id"`
			FailureClass     string `json:"failure_class"`
			FailureOperation string `json:"failure_operation"`
			FailedAt         string `json:"failed_at"`
			WorkItemID       string `json:"work_item_id"`
			Acknowledged     bool   `json:"acknowledged"`
			AcknowledgedAt   string `json:"acknowledged_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Count != 2 || !body.Truncated || body.NextCursor != "scope-c" || len(body.Items) != 2 {
		t.Fatalf("body = %s, want two items, truncated, next_cursor scope-c", rec.Body.String())
	}
	if first := body.Items[0]; first.ScopeID != "scope-b" || first.Acknowledged || first.AcknowledgedAt != "" || first.WorkItemID != "work-b" {
		t.Fatalf("first item = %+v, want unacknowledged scope-b with no acknowledgement time", first)
	}
	if second := body.Items[1]; second.ScopeID != "scope-c" || !second.Acknowledged || second.AcknowledgedAt == "" {
		t.Fatalf("second item = %+v, want acknowledged scope-c with a time", second)
	}
	// This read is not an audit-worthy decision, carries no reason, and has no ledger.
	if len(p.appender.events) != 0 || len(p.ledger.rows) != 0 {
		t.Fatalf("list wrote %d audit events and %d ledger rows, want none", len(p.appender.events), len(p.ledger.rows))
	}
	for _, leaked := range []string{"reason", "last_failure_message", "provider text"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Fatalf("list response mentions %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestQueryCollectorFailuresOmitsNextCursorOnTheLastPage(t *testing.T) {
	p := fixture(t)
	p.store.listPage = pgstorage.TerminalFailureListPage{}
	rec := post(p.handler.queryCollectorFailures, listRoute, listBody, adminAuth())
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "next_cursor") ||
		!strings.Contains(rec.Body.String(), `"truncated":false`) || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("response = %d %s, want 200 with truncated false, no next_cursor, and an empty items array", rec.Code, rec.Body.String())
	}
}

func TestQueryCollectorFailuresGates(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		auth     auth.AuthContext
		wantCode int
	}{
		{"scoped token", listBody, auth.AuthContext{Mode: "scoped", AllScopes: false}, 403},
		{"browser session without all scopes", listBody, auth.AuthContext{Mode: "browser_session", AllScopes: false}, 403},
		{"shared key", listBody, adminAuth(), 200},
		{"all-scopes browser session", listBody, auth.AuthContext{Mode: "browser_session", AllScopes: true}, 200},
		{"unauthenticated dev mode", listBody, auth.AuthContext{}, 200},
		{"missing kind", `{"instance_id":"alerts"}`, adminAuth(), 400},
		{"missing instance", `{"collector_kind":"security_alert"}`, adminAuth(), 400},
		{"malformed body", `{`, adminAuth(), 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture(t)
			rec := post(p.handler.queryCollectorFailures, listRoute, tc.body, tc.auth)
			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode == 403 && p.store.lastList.InstanceID != "" {
				t.Fatalf("a refused caller reached the store: %+v", p.store.lastList)
			}
		})
	}
}

func TestQueryCollectorFailuresStoreFaultLeaksNothing(t *testing.T) {
	p := fixture(t)
	p.store.listFailErr = errors.New("pq: connection refused at 10.0.0.7")
	rec := post(p.handler.queryCollectorFailures, listRoute, listBody, adminAuth())
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "10.0.0.7") ||
		!strings.Contains(rec.Body.String(), listFailedMessage) {
		t.Fatalf("response = %d %s, want 500 with only the fixed message", rec.Code, rec.Body.String())
	}
}

func TestQueryCollectorFailuresNotConfigured(t *testing.T) {
	rec := post((&Handler{}).queryCollectorFailures, listRoute, listBody, adminAuth())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}
