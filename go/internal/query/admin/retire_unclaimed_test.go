// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestAdminHandler_RetireUnclaimedWorkflowItems mirrors the skip contract:
// a successful call returns the retired count and items.
func TestAdminHandler_RetireUnclaimedWorkflowItems(t *testing.T) {
	store := &stubAdminStore{
		retiredUnclaimed: []RetiredUnclaimedItem{
			{WorkItemID: "w-1", ScopeID: "scope-1"},
			{WorkItemID: "w-2", ScopeID: "scope-2"},
		},
	}
	h := &Handler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-gone",
		"operator_note":         "dark scope cleanup",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 2 {
		t.Errorf("count = %v, want 2", got["count"])
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %v, want 2 retired items", got["items"])
	}
	if store.retireUnclaimedKind != "sbom_attestation" || store.retireUnclaimedInstance != "sbom-gone" {
		t.Errorf("store got kind=%q instance=%q, want sbom_attestation/sbom-gone",
			store.retireUnclaimedKind, store.retireUnclaimedInstance)
	}
}

// TestAdminHandler_RetireUnclaimedWorkflowItems_RequiresIdentity pins the
// 400s: kind and instance are both required.
func TestAdminHandler_RetireUnclaimedWorkflowItems_RequiresIdentity(t *testing.T) {
	for _, body := range []map[string]any{
		{"collector_instance_id": "sbom-gone"},
		{"collector_kind": "sbom_attestation"},
		{},
	} {
		h := &Handler{Store: &stubAdminStore{}}
		w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %v: status = %d, want %d", body, w.Code, http.StatusBadRequest)
		}
	}
}

// TestAdminHandler_RetireUnclaimedWorkflowItems_UnknownInstance pins the
// fail-closed 404: an instance no collector_instances row names is a typo,
// never a silent no-op.
func TestAdminHandler_RetireUnclaimedWorkflowItems_UnknownInstance(t *testing.T) {
	store := &stubAdminStore{retireUnclaimedErr: ErrRetireUnclaimedUnknownInstance}
	h := &Handler{Store: store}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "never-existed",
	})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestAdminHandler_RetireUnclaimedWorkflowItems_ActiveInstance pins the
// fail-closed 409 with a deny audit event: a still-configured instance may
// still have a live collector claiming its pending rows, so the
// disposition is not valid yet.
func TestAdminHandler_RetireUnclaimedWorkflowItems_ActiveInstance(t *testing.T) {
	audit := &testutil.FakeGovernanceAuditAppender{}
	// The wrapper wraps the sentinel with the refused (kind, instance)
	// pair, so errors.Is still matches while the 409 names the instance.
	store := &stubAdminStore{retireUnclaimedErr: fmt.Errorf("%w: %s/%s",
		ErrRetireUnclaimedInstanceActive, "sbom_attestation", "sbom-live")}
	h := &Handler{Store: store, Audit: audit}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-live",
	})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "sbom-live") {
		t.Errorf("409 body names the instance: %s", w.Body.String())
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_unclaimed_refused_instance_active" {
		t.Fatalf("audit events = %+v, want one retire_unclaimed_refused_instance_active deny", audit.Events)
	}
}

// TestAdminHandler_RetireUnclaimedWorkflowItems_StoreFailure pins the 500
// path for an unexpected store fault.
func TestAdminHandler_RetireUnclaimedWorkflowItems_StoreFailure(t *testing.T) {
	store := &stubAdminStore{retireUnclaimedErr: errors.New("boom")}
	h := &Handler{Store: store}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-gone",
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}
