// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

// decodeScannerWorkerAnalysis decodes one scanner_worker.analysis envelope
// into the typed scannerworkerv1.Analysis struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (analyzer, target_kind, target_locator_hash,
// analysis_status, coverage_status, result_count, fact_count,
// image_reference, image_digest, evidence_source, extraction_reason) or is
// otherwise malformed. It is the single decode site for the
// scanner_worker.analysis kind on the reducer side: every extractor that
// consumes scanner_worker.analysis facts decodes through here, and a missing
// required field is routed through partitionDecodeFailures so it
// dead-letters as a per-fact input_invalid quarantine rather than a silent
// empty-string identity or a whole-intent abort.
func decodeScannerWorkerAnalysis(env facts.Envelope) (scannerworkerv1.Analysis, error) {
	analysis, err := factschema.DecodeScannerWorkerAnalysis(factschemaEnvelope(env))
	if err != nil {
		return scannerworkerv1.Analysis{}, newFactDecodeError(factschema.FactKindScannerWorkerAnalysis, err)
	}
	return analysis, nil
}
