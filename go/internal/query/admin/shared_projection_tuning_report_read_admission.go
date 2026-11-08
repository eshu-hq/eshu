// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

// SharedProjectionTuningReportContractVersion versions the #9669 Gap
// 02 admission-contract shape for the static shared-projection tuning
// report (GET /api/v0/admin/shared-projection/tuning-report).
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api reproduces this static body byte-identical
// ahead of relay from these pinned constants. A breaking change to the
// report shape or values fails the frozen contract test until that
// shape is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen shape is the
// enforcement) so dest consumers can pin the shape.
const SharedProjectionTuningReportContractVersion = "v0"

// SharedProjectionTuningReportStatus is the pinned status value of the
// static tuning report: shared-write tuning is not an operator
// surface, so the report is always not_applicable, never an error.
const SharedProjectionTuningReportStatus = "not_applicable"

// SharedProjectionTuningReportDetail is the pinned detail text of the
// static tuning report: tuning is managed internally by the Go
// projector pipeline.
const SharedProjectionTuningReportDetail = "Shared-projection tuning is managed internally by the Go projector pipeline."

// D-wave consumer: D3 (admin reads, per the arbiter D-wave ruling on
// #9669: Decision 3 / census Q4 — admin reads go native only after the
// pin). The satellite records this pin in a follow-up; a satellite PR
// consuming an unminted pin is invalid.
//
// There is deliberately no reader interface here: the report has no
// reader — no graph read, no store read, no port to freeze. The
// contract is the exact served shape and its fixed values, served from
// the constants above by the handler. Pinning a synthetic reader would
// invent a seam the route never admits through; the minting rule bars
// that kind of blanket shape.
//
// Rollback: revert this pin and the handler's use of the constants.
// The seam is unwired — no route or caller depends on the constants
// yet — so rollback moves no traffic and changes no data.
