// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	analysisStatusCompleted   = "completed"
	analysisStatusNotScanned  = "not_scanned"
	coverageStatusScanned     = "scanned"
	coverageStatusUnsupported = "unsupported"
)

func newAnalysisFact(
	input scannerworker.ClaimInput,
	target TargetConfig,
	snapshot Snapshot,
	resultCount int,
	factCount int,
	observedAt time.Time,
) facts.Envelope {
	if observedAt.IsZero() {
		observedAt = input.ObservedAt.UTC()
	}
	stableKey := facts.StableID(facts.ScannerWorkerAnalysisFactKind, map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"generation_id":       input.GenerationID,
		"analysis_status":     analysisStatusCompleted,
		"image_digest":        strings.TrimSpace(snapshot.ImageDigest),
	})
	payload := map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"analysis_status":     analysisStatusCompleted,
		"coverage_status":     coverageStatusScanned,
		"result_count":        resultCount,
		"fact_count":          factCount,
		"image_reference":     strings.TrimSpace(snapshot.ImageReference),
		"image_digest":        strings.TrimSpace(snapshot.ImageDigest),
		"evidence_source":     string(snapshot.EvidenceSource),
		"extraction_reason":   snapshot.ExtractionReason,
	}
	addOptionalPayloadValue(payload, "distro", string(snapshot.Distro))
	addOptionalPayloadValue(payload, "distro_version", snapshot.DistroVersion)
	addOptionalPayloadValue(payload, "package_manager", string(snapshot.PackageManager))
	addOptionalPayloadValue(payload, "configured_image_reference", strings.TrimSpace(target.ImageReference))
	return facts.Envelope{
		FactID:           facts.ScannerWorkerAnalysisFactKind + ":" + stableKey,
		ScopeID:          input.Target.ScopeID,
		GenerationID:     input.GenerationID,
		FactKind:         facts.ScannerWorkerAnalysisFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ScannerWorkerSchemaVersionV1,
		CollectorKind:    string(scope.CollectorScannerWorker),
		FencingToken:     input.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: string(scope.CollectorScannerWorker),
			ScopeID:      input.Target.ScopeID,
			GenerationID: input.GenerationID,
			FactKey:      stableKey,
		},
	}
}
