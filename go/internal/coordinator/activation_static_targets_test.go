// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestServiceRunPlansStandingCVEListScopeForDeriveDisabledActivation is the
// service-level #9880 regression: an activation with every derive section
// disabled but a standing cve_list target in its config block must still
// plan that scope beside the generic planner's host item. Before the fix
// the derived-scope hook returned at the disabled-derivation gate, so the
// validated cve_list target was never claimed. A disabled derivation still
// reads no owned-package evidence.
func TestServiceRunPlansStandingCVEListScopeForDeriveDisabledActivation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := testVulnerabilityActivationInstance(now, testVulnerabilityActivationConfiguration(
		`{"targets":[`+
			`{"source":"cisa_kev","scope_id":"vuln-intel://cisa/kev"},`+
			`{"source":"cve_list","scope_id":"vuln-intel://cve_list/cve","cve_ids":["CVE-2026-9101","CVE-2026-9102"]}`+
			`]}`,
		string(scope.CollectorVulnerabilityIntelligence),
		string(scope.CollectorVulnerabilityIntelligence),
	))
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := &fakeOwnedPackageTargetReader{}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	scopes := map[string]workflow.WorkItem{}
	for _, item := range store.enqueuedItems {
		scopes[item.ScopeID] = item
	}
	if _, ok := scopes[testVulnerabilityActivationHostScope]; !ok {
		t.Fatalf("enqueued scopes = %v, want the host KEV scope", keysOf(scopes))
	}
	static, ok := scopes["vuln-intel://cve_list/cve"]
	if !ok {
		t.Fatalf("enqueued scopes = %v, want the standing cve_list scope", keysOf(scopes))
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want %d (KEV once plus one cve_list scope)", got, want)
	}
	if got, want := static.CollectorInstanceID, instance.InstanceID; got != want {
		t.Fatalf("cve_list CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := static.SourceSystem, string(scope.CollectorVulnerabilityIntelligence); got != want {
		t.Fatalf("cve_list SourceSystem = %q, want %q", got, want)
	}
	if got := len(reader.requests); got != 0 {
		t.Fatalf("owned target reads = %d, want 0 for a disabled derivation", got)
	}
}

// TestServiceRunWarnsDroppedNVDAnchorWhileRollingDisabled pins the
// operator signal for the silent-drop review finding: an anchor-only
// activation with rolling windows disabled plans only the host scope, and
// the dropped anchor is named in a warning instead of reading as a
// silent success.
func TestServiceRunWarnsDroppedNVDAnchorWhileRollingDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := testVulnerabilityActivationInstance(now, testVulnerabilityActivationConfiguration(
		`{"targets":[`+
			`{"source":"cisa_kev","scope_id":"vuln-intel://cisa/kev"},`+
			`{"source":"nvd","scope_id":"vuln-intel://nvd/cve","modified_start":"2026-01-01T00:00:00Z","modified_end":"2026-01-08T00:00:00Z"}`+
			`]}`,
		string(scope.CollectorVulnerabilityIntelligence),
		string(scope.CollectorVulnerabilityIntelligence),
	))
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := &fakeOwnedPackageTargetReader{}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)
	var logs bytes.Buffer
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d (host KEV scope only)", got, want)
	}
	if got := logs.String(); !strings.Contains(got, "nvd_anchor_rolling_disabled") || !strings.Contains(got, "vuln-intel://nvd/cve") {
		t.Fatalf("logs = %q, want the dropped-anchor warning naming the scope", got)
	}
}
