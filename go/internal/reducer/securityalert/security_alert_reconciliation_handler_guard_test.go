// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"context"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestHandleRejectsNilManifestConsumptionExtractor covers the fail-closed guard
// in Handle directly, by constructing the handler with a nil
// ExtractManifestConsumptions rather than reading the registered production
// handler's field.
//
// The distinction is the whole point of this test. The reducer root's
// TestSecurityAlertReconciliationRegistrationCarriesTheManifestConsumptionSeam
// guards the WIRING -- that the registered handler carries a non-nil seam -- and
// so it only ever calls Handle with the field already populated. Deleting the
// guard leaves that test green, because the branch it protects is never taken.
// A guard whose removal no test notices is not covered.
//
// Both assertions matter. The error proves the call is rejected; the untouched
// writer proves it is rejected BEFORE anything durable is written, which is the
// property "fail closed" actually names. A nil seam is a forgotten wire, not an
// absence of manifest evidence: without the guard every lockfile-only alert
// reconciles as provider_only and commits that wrong decision as truth.
func TestHandleRejectsNilManifestConsumptionExtractor(t *testing.T) {
	writer := &recordingSecurityAlertReconciliationWriter{}
	handler := SecurityAlertReconciliationHandler{
		FactLoader:                  &recordingSecurityAlertReconciliationFactLoader{},
		Writer:                      writer,
		ExtractManifestConsumptions: nil,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-nil-extractor",
		ScopeID:      "repo-guard",
		GenerationID: "generation-1",
		SourceSystem: "security_alert",
		Domain:       reducercontract.DomainSecurityAlertReconciliation,
	})

	if err == nil {
		t.Fatalf("Handle accepted a nil ExtractManifestConsumptions; want the fail-closed error. result=%+v", result)
	}
	if !strings.Contains(err.Error(), "manifest consumption extractor is required") {
		t.Fatalf("Handle error = %q, want it to name the missing manifest consumption extractor", err.Error())
	}
	if result.Status != "" {
		t.Fatalf("Handle returned status %q on the fail-closed path; want the zero Result", result.Status)
	}
	if writer.calls != 0 {
		t.Fatalf("Handle wrote %d batch(es) before failing closed; want 0 -- the guard must reject before any durable write", writer.calls)
	}
}
