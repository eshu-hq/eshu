// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

const (
	// MetricDimensionAnalyzer labels scanner-worker metrics with a bounded
	// analyzer profile such as sbom_generation or secret_scan.
	MetricDimensionAnalyzer = "analyzer"
	// MetricDimensionTargetKind labels scanner-worker metrics with the bounded
	// target type, such as repository.
	MetricDimensionTargetKind = "target_kind"
	// MetricDimensionLimitKind labels scanner-worker metrics with the bounded
	// resource limit that caused an event.
	MetricDimensionLimitKind = "limit_kind"
)

const (
	// SpanScannerWorkerClaimProcess wraps one scanner-worker workflow claim
	// from claim validation through retry, dead-letter, or completion.
	SpanScannerWorkerClaimProcess = "scanner_worker.claim.process"
	// SpanScannerWorkerAnalyze wraps one bounded analyzer execution.
	SpanScannerWorkerAnalyze = "scanner_worker.analyze"
	// SpanScannerWorkerFactEmitBatch wraps one scanner-worker source fact batch.
	SpanScannerWorkerFactEmitBatch = "scanner_worker.fact.emit_batch"
)
