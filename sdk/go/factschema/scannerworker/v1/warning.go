// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Warning is the schema-version-1 typed payload for the
// "scanner_worker.warning" fact kind.
//
// Two scanner-worker producers emit this kind. The image analyzer emits it when
// image evidence is unsupported or cannot be scanned, carrying image identity
// and extraction evidence. WarningAnalyzer emits it when no concrete analyzer
// source is configured for the claimed target (for example
// "analyzer_not_configured" or "sbom_generator_source_not_configured"); that
// fallback only has the claim's target scope, so it has no image reference,
// image digest, evidence source, or extraction reason to report.
//
// The common core — analyzer, target identity, warning reason, and the bounded
// status fields — is therefore required because every warning carries it. Image
// identity, evidence source, and extraction reason are optional because the
// non-image fallback legitimately lacks them; requiring them would dead-letter
// every fallback warning as input_invalid. Distro/package metadata is optional
// because unsupported image targets often lack it. Consumers must treat the
// image-analysis fields as present only for image-analyzer warnings.
type Warning struct {
	Analyzer          string  `json:"analyzer"`
	TargetKind        string  `json:"target_kind"`
	TargetLocatorHash string  `json:"target_locator_hash"`
	Reason            string  `json:"reason"`
	WarningClass      string  `json:"warning_class"`
	AnalysisStatus    string  `json:"analysis_status"`
	CoverageStatus    string  `json:"coverage_status"`
	ImageReference    *string `json:"image_reference,omitempty"`
	ImageDigest       *string `json:"image_digest,omitempty"`
	EvidenceSource    *string `json:"evidence_source,omitempty"`
	ExtractionReason  *string `json:"extraction_reason,omitempty"`
	Distro            *string `json:"distro,omitempty"`
	DistroVersion     *string `json:"distro_version,omitempty"`
	PackageManager    *string `json:"package_manager,omitempty"`
}
