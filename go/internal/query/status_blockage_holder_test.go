// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/queue"
)

// TestStatusHandlerIndexStatusNamesBlockageHolderDomain is the #8954 wire
// check: a blocked security-alert reconciliation reports the domain that holds
// its fence, and a readiness-gated row reports an empty holder, both under the
// same key so a client never has to probe for it.
func TestStatusHandlerIndexStatusNamesBlockageHolderDomain(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
				QueueBlockages: []queue.Blockage{
					{
						Stage:          "reducer",
						Domain:         "security_alert_reconciliation",
						ConflictDomain: "scope",
						ConflictKey:    "scope-1",
						HolderDomain:   "supply_chain_impact",
						Blocked:        1,
					},
					{
						Stage:          "reducer",
						Domain:         "iam_can_perform_materialization",
						ConflictDomain: "readiness",
						ConflictKey:    "cloud_resource_uid:canonical_nodes_committed:scope-2",
						Blocked:        1,
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/index-status status = %d, want %d", got, want)
	}

	var payload struct {
		QueueBlockages []map[string]any `json:"queue_blockages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body=%s", err, rec.Body.String())
	}
	holders := map[string]any{}
	for _, row := range payload.QueueBlockages {
		holder, present := row["holder_domain"]
		if !present {
			t.Fatalf("queue_blockages row %v has no holder_domain key; body=%s", row, rec.Body.String())
		}
		holders[row["domain"].(string)] = holder
	}
	if got, want := holders["security_alert_reconciliation"], "supply_chain_impact"; got != want {
		t.Fatalf("security_alert_reconciliation holder_domain = %v, want %q", got, want)
	}
	if got := holders["iam_can_perform_materialization"]; got != "" {
		t.Fatalf("readiness-gated holder_domain = %v, want empty", got)
	}
}
