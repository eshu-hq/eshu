// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTuningReportConstants(t *testing.T) {
	if TuningReportContractVersion != "v0" {
		t.Fatalf("tuning contract version: want v0, got %q", TuningReportContractVersion)
	}
	if TuningReportStatus != "not_applicable" {
		t.Fatalf("tuning status: want not_applicable, got %q", TuningReportStatus)
	}
	if TuningReportDetail != "Shared-projection tuning is managed internally by the Go projector pipeline." {
		t.Fatalf("tuning detail drifted: %q", TuningReportDetail)
	}
}

func TestWriteTuningReportBodyIsPinned(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteTuningReport(rec, httptest.NewRequest(http.MethodGet, "/api/v0/admin/shared-projection/tuning-report", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("tuning report: want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("tuning report: want JSON content type, got %q", ct)
	}
	// Pinned byte-identical body: map keys marshal sorted (detail, status)
	// with HTML escaping off, plus the encoder newline. Core serves the
	// same bytes from the same constants.
	want := "{\"detail\":\"Shared-projection tuning is managed internally by the Go projector pipeline.\",\"status\":\"not_applicable\"}\n"
	if rec.Body.String() != want {
		t.Fatalf("tuning report body drifted:\n got %q\nwant %q", rec.Body.String(), want)
	}
}
