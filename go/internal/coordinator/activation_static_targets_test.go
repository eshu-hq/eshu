// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
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
