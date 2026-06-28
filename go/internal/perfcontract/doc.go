// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package perfcontract encodes Eshu's published performance contracts as typed
// values and binds them to the documents that publish them, so the documented
// numbers and the in-code constants cannot silently drift.
//
// B-5 (#3798) of Epic B: the three performance docs
//
//	docs/public/reference/local-performance-envelope.md
//	docs/public/reference/reducer-claim-latency-gate.md
//	docs/public/reference/hybrid-retrieval-production-gate.md
//
// each state numeric thresholds. Before this package the only executable binding
// was the hybrid gate's TestProductionGateThresholdsAreDocumented; the envelope
// and claim-latency numbers were prose an edit could change without any code
// noticing. ContractThresholds returns every documented threshold, and
// TestPerformanceContractMatchesDocs (run by the standard go test ./... CI gate)
// fails if any threshold is missing from its doc or if the in-code value drifts
// from the documented token.
//
// Honesty boundary: this is a CONTRACT gate, not a runtime measurement. Whether a
// given build actually meets a threshold is a separate question. The hybrid
// local-deterministic accuracy/latency bars are measured by their own hermetic
// gate (go/internal/searchbench); the rest — cold start, reducer bulk-write, the
// reducer claim-latency budget, and the git-collector full-corpus envelope —
// require the operator/remote validation run on consistent hardware and are
// marked EnforcementOperatorGated. This package guarantees the numbers an
// operator measures against are real, present, and consistent with the code; it
// does not fabricate a measurement that hermetic CI cannot take.
package perfcontract
