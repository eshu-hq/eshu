// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"testing"
)

// TestSharedProjectionTuningReportContractIsVersioned pins the #9669
// Gap 02 admission-contract version for the static shared-projection
// tuning report (GET /api/v0/admin/shared-projection/tuning-report).
// The report has no reader: tuning is managed internally by the Go
// projector pipeline, so the contract is the exact served shape and
// its fixed values, served from the pinned constants. A renamed key,
// an added key, or a changed value fails here until the version bumps
// in the same diff (manually — the bump is convention, the frozen
// shape is the enforcement) so dest consumers can reproduce the body
// byte-identical ahead of relay.
//
// D-wave consumer: D3 (admin reads, per the arbiter D-wave ruling on
// #9669: Decision 3 / census Q4 — admin reads go native only after the
// pin).
func TestSharedProjectionTuningReportContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SharedProjectionTuningReportContractVersion != "v0" {
		t.Errorf("SharedProjectionTuningReportContractVersion = %q, want %q: bump it deliberately with the shape change", SharedProjectionTuningReportContractVersion, "v0")
	}

	h := &Handler{}
	mux := newAdminMux(h)

	w := getJSON(mux, "/api/v0/admin/shared-projection/tuning-report")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	got := decodeBody(t, w)
	if len(got) != 2 {
		t.Fatalf("tuning report keys = %v, want exactly [detail status]: bump %s with the shape change", got, SharedProjectionTuningReportContractVersion)
	}
	if got["status"] != SharedProjectionTuningReportStatus {
		t.Errorf("status = %q, want pinned %q: bump %s with the shape change", got["status"], SharedProjectionTuningReportStatus, SharedProjectionTuningReportContractVersion)
	}
	if got["detail"] != SharedProjectionTuningReportDetail {
		t.Errorf("detail = %q, want pinned %q: bump %s with the shape change", got["detail"], SharedProjectionTuningReportDetail, SharedProjectionTuningReportContractVersion)
	}
}
