// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imageanalyzer

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func newUnsupportedWarning(
	input scannerworker.ClaimInput,
	target TargetConfig,
	snapshot Snapshot,
	observedAt time.Time,
) facts.Envelope {
	stableKey := facts.StableID(facts.ScannerWorkerWarningFactKind, map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"generation_id":       input.GenerationID,
		"reason":              warningReasonUnsupported,
	})
	payload := map[string]any{
		"analyzer":            string(input.Analyzer),
		"target_kind":         string(input.Target.Kind),
		"target_locator_hash": input.Target.LocatorHash,
		"reason":              warningReasonUnsupported,
		"warning_class":       "scanner_worker_warning",
		"analysis_status":     analysisStatusNotScanned,
		"coverage_status":     coverageStatusUnsupported,
		"image_reference":     strings.TrimSpace(target.ImageReference),
		"image_digest":        strings.TrimSpace(target.ImageDigest),
		"evidence_source":     evidenceSourceForWarning(target, snapshot),
		"extraction_reason":   firstNonBlank(snapshot.ExtractionReason, extractionUnsupported),
	}
	addOptionalPayloadValue(payload, "distro", string(firstNonBlankDistro(snapshot.Distro, target.Distro)))
	addOptionalPayloadValue(payload, "distro_version", firstNonBlank(snapshot.DistroVersion, target.DistroVersion))
	addOptionalPayloadValue(payload, "package_manager", string(firstNonBlankPackageManager(snapshot.PackageManager, target.PackageManager)))
	return facts.Envelope{
		FactID:           facts.ScannerWorkerWarningFactKind + ":" + stableKey,
		ScopeID:          input.Target.ScopeID,
		GenerationID:     input.GenerationID,
		FactKind:         facts.ScannerWorkerWarningFactKind,
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

func evidenceSourceForWarning(target TargetConfig, snapshot Snapshot) string {
	if snapshot.EvidenceSource != "" {
		return string(snapshot.EvidenceSource)
	}
	if strings.TrimSpace(target.RootFSPath) != "" {
		return string(EvidenceSourceRootFS)
	}
	return string(EvidenceSourceLayer)
}

func addOptionalPayloadValue(payload map[string]any, key string, value string) {
	if value == "" {
		return
	}
	payload[key] = value
}

func firstNonBlankPackageManager(
	values ...ospackagevulnerability.PackageManager,
) ospackagevulnerability.PackageManager {
	for _, value := range values {
		if strings.TrimSpace(string(value)) != "" {
			return value
		}
	}
	return ""
}
