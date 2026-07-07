// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

// DecodeScannerWorkerAnalysis decodes env.Payload into the latest
// scannerworkerv1.Analysis struct for the "scanner_worker.analysis" fact kind.
func DecodeScannerWorkerAnalysis(env Envelope) (scannerworkerv1.Analysis, error) {
	return decodeLatestMajor[scannerworkerv1.Analysis](FactKindScannerWorkerAnalysis, env)
}

// EncodeScannerWorkerAnalysis builds the map payload shape an Envelope carries
// for a scannerworkerv1.Analysis value.
func EncodeScannerWorkerAnalysis(analysis scannerworkerv1.Analysis) (map[string]any, error) {
	payload := map[string]any{
		"analyzer":            analysis.Analyzer,
		"target_kind":         analysis.TargetKind,
		"target_locator_hash": analysis.TargetLocatorHash,
		"analysis_status":     analysis.AnalysisStatus,
		"coverage_status":     analysis.CoverageStatus,
		"result_count":        analysis.ResultCount,
		"fact_count":          analysis.FactCount,
		"image_reference":     analysis.ImageReference,
		"image_digest":        analysis.ImageDigest,
		"evidence_source":     analysis.EvidenceSource,
		"extraction_reason":   analysis.ExtractionReason,
	}
	addOptionalString(payload, "distro", analysis.Distro)
	addOptionalString(payload, "distro_version", analysis.DistroVersion)
	addOptionalString(payload, "package_manager", analysis.PackageManager)
	addOptionalString(payload, "configured_image_reference", analysis.ConfiguredImageReference)
	return payload, nil
}

// DecodeScannerWorkerWarning decodes env.Payload into the latest
// scannerworkerv1.Warning struct for the "scanner_worker.warning" fact kind.
func DecodeScannerWorkerWarning(env Envelope) (scannerworkerv1.Warning, error) {
	return decodeLatestMajor[scannerworkerv1.Warning](FactKindScannerWorkerWarning, env)
}

// EncodeScannerWorkerWarning builds the map payload shape an Envelope carries
// for a scannerworkerv1.Warning value.
func EncodeScannerWorkerWarning(warning scannerworkerv1.Warning) (map[string]any, error) {
	payload := map[string]any{
		"analyzer":            warning.Analyzer,
		"target_kind":         warning.TargetKind,
		"target_locator_hash": warning.TargetLocatorHash,
		"reason":              warning.Reason,
		"warning_class":       warning.WarningClass,
		"analysis_status":     warning.AnalysisStatus,
		"coverage_status":     warning.CoverageStatus,
		"image_reference":     warning.ImageReference,
		"image_digest":        warning.ImageDigest,
		"evidence_source":     warning.EvidenceSource,
		"extraction_reason":   warning.ExtractionReason,
	}
	addOptionalString(payload, "distro", warning.Distro)
	addOptionalString(payload, "distro_version", warning.DistroVersion)
	addOptionalString(payload, "package_manager", warning.PackageManager)
	return payload, nil
}

func addOptionalString(payload map[string]any, key string, value *string) {
	if value == nil {
		return
	}
	payload[key] = *value
}
