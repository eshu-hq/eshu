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

// The shared test double rides the pinned admission shape, not a parallel
// signature: if the port re-signs RetireUnclaimedWorkflowItems, this file
// fails compilation the same way the frozen contract test fails. It proves
// every wire proof below exercises the pinned writer, not a lookalike. The
// handler still takes the wide Store, so the double is the full
// stubAdminStore asserted against the narrow admission port.
var _ RetireUnclaimedAdmission = (*stubAdminStore)(nil)

// TestRetireUnclaimedAdmissionWireNilStoreIs503WithoutAudit pins the
// fail-closed 503: with no admin store configured the endpoint answers
// "admin store is not configured" and emits no governance audit event.
func TestRetireUnclaimedAdmissionWireNilStoreIs503WithoutAudit(t *testing.T) {
	t.Parallel()

	audit := &testutil.FakeGovernanceAuditAppender{}
	h := &Handler{Store: nil, Audit: audit}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-gone",
	})

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "admin store not configured") {
		t.Errorf("503 body = %q, want it to name the unconfigured store", w.Body.String())
	}
	if len(audit.Events) != 0 {
		t.Errorf("audit events = %+v, want none on the 503 path", audit.Events)
	}
}

// TestRetireUnclaimedAdmissionWireStoreFailureIs500WithoutAudit pins the
// 500 path: an unexpected store fault answers the fixed
// retireUnclaimedFailedMessage (never err.Error()) and emits no governance
// audit event. A client cancel on the same step answers 499 through the
// shared writeMutationFailure instead.
func TestRetireUnclaimedAdmissionWireStoreFailureIs500WithoutAudit(t *testing.T) {
	t.Parallel()

	audit := &testutil.FakeGovernanceAuditAppender{}
	store := &stubAdminStore{retireUnclaimedErr: errors.New("boom")}
	h := &Handler{Store: store, Audit: audit}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-gone",
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "retire unclaimed workflow work items request failed") {
		t.Errorf("500 body = %q, want the fixed failure message", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "boom") {
		t.Errorf("500 body = %q, must not leak the store error", w.Body.String())
	}
	if len(audit.Events) != 0 {
		t.Errorf("audit events = %+v, want none on the 500 path", audit.Events)
	}
}

// TestRetireUnclaimedAdmissionWireValidationIs400WithoutAudit pins the 400
// path: a blank kind or instance is rejected before the store runs and
// emits no governance audit event.
func TestRetireUnclaimedAdmissionWireValidationIs400WithoutAudit(t *testing.T) {
	t.Parallel()

	for _, body := range []map[string]any{
		{"collector_kind": "   ", "collector_instance_id": "sbom-gone"},
		{"collector_kind": "sbom_attestation", "collector_instance_id": "  "},
	} {
		audit := &testutil.FakeGovernanceAuditAppender{}
		h := &Handler{Store: &stubAdminStore{}, Audit: audit}

		w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %v: status = %d, want %d", body, w.Code, http.StatusBadRequest)
		}
		if len(audit.Events) != 0 {
			t.Errorf("body %v: audit events = %+v, want none on the 400 path", body, audit.Events)
		}
	}
}

// TestRetireUnclaimedAdmissionWireUnknownInstanceNamesPair pins the 404
// wire shape: the sentinel arrives wrapped with the refused (kind,
// instance) pair, so errors.Is still matches while the body names the
// instance the operator typed.
func TestRetireUnclaimedAdmissionWireUnknownInstanceNamesPair(t *testing.T) {
	t.Parallel()

	audit := &testutil.FakeGovernanceAuditAppender{}
	store := &stubAdminStore{retireUnclaimedErr: fmt.Errorf("%w: %s/%s",
		ErrRetireUnclaimedUnknownInstance, "sbom_attestation", "never-existed")}
	h := &Handler{Store: store, Audit: audit}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "never-existed",
	})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "never-existed") {
		t.Errorf("404 body names the instance: %s", w.Body.String())
	}
	if len(audit.Events) != 1 || audit.Events[0].ReasonCode != "retire_unclaimed_refused_unknown_instance" {
		t.Fatalf("audit events = %+v, want one retire_unclaimed_refused_unknown_instance deny", audit.Events)
	}
}

// TestRetireUnclaimedAdmissionWireSuccessOrderAndCount pins the 200 wire
// shape: count equals the items served, and the items ride the port order
// (work_item_id order from the store) with exactly the work_item_id and
// scope_id keys — never scope ids as metric labels, only as response rows.
func TestRetireUnclaimedAdmissionWireSuccessOrderAndCount(t *testing.T) {
	t.Parallel()

	store := &stubAdminStore{
		retiredUnclaimed: []RetiredUnclaimedItem{
			{WorkItemID: "w-1", ScopeID: "scope-1"},
			{WorkItemID: "w-2", ScopeID: "scope-2"},
		},
	}
	h := &Handler{Store: store}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-gone",
		"operator_note":         "  dark scope cleanup  ",
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
	for i, want := range []string{"w-1", "w-2"} {
		item, ok := items[i].(map[string]any)
		if !ok {
			t.Fatalf("items[%d] = %#v, want an object", i, items[i])
		}
		if item["work_item_id"] != want {
			t.Errorf("items[%d].work_item_id = %v, want %q (port order)", i, item["work_item_id"], want)
		}
		if _, ok := item["scope_id"]; !ok {
			t.Errorf("items[%d] = %v, want a scope_id key", i, item)
		}
		if len(item) != 2 {
			t.Errorf("items[%d] = %v, want exactly work_item_id and scope_id", i, item)
		}
	}
	if store.retireUnclaimedKind != "sbom_attestation" || store.retireUnclaimedInstance != "sbom-gone" {
		t.Errorf("store got kind=%q instance=%q, want trimmed sbom_attestation/sbom-gone",
			store.retireUnclaimedKind, store.retireUnclaimedInstance)
	}
}

// TestRetireUnclaimedAdmissionWireEmptyIsNoop pins the empty wire shape: a
// call that transitions nothing answers 200 with count 0 and an empty items
// array, so a repeated call is a safe no-op.
func TestRetireUnclaimedAdmissionWireEmptyIsNoop(t *testing.T) {
	t.Parallel()

	h := &Handler{Store: &stubAdminStore{}}

	w := postJSON(newAdminMux(h), "/api/v0/admin/workflow-retire-unclaimed", map[string]any{
		"collector_kind":        "sbom_attestation",
		"collector_instance_id": "sbom-gone",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeBody(t, w)
	if int(got["count"].(float64)) != 0 {
		t.Errorf("count = %v, want 0", got["count"])
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 0 {
		t.Errorf("items = %v, want an empty array", got["items"])
	}
}
