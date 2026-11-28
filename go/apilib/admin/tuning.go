// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"encoding/json"
	"net/http"
)

// TuningReportContractVersion versions the admission-contract shape for the
// static shared-projection tuning report
// (GET /api/v0/admin/shared-projection/tuning-report). A breaking change to
// the report shape or values fails the pinned body test until that shape is
// deliberately updated; bump this version alongside so dest consumers can
// pin the shape.
const TuningReportContractVersion = "v0"

// TuningReportStatus is the pinned status value of the static tuning report:
// shared-write tuning is not an operator surface, so the report is always
// not_applicable, never an error.
const TuningReportStatus = "not_applicable"

// TuningReportDetail is the pinned detail text of the static tuning report:
// tuning is managed internally by the Go projector pipeline.
const TuningReportDetail = "Shared-projection tuning is managed internally by the Go projector pipeline."

// WriteTuningReport serves the static tuning report: shared-write tuning is
// managed by the projector pipeline internally, so the endpoint returns the
// pinned static body. The served bytes are byte-identical to the core
// handler's body: same map keys (sorted by the encoder), same HTML-escaping
// setting, same trailing newline. r is accepted for handler-shape parity and
// is otherwise unused.
func WriteTuningReport(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{
		"status": TuningReportStatus,
		"detail": TuningReportDetail,
	})
}
