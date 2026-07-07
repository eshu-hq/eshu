// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Analysis is the schema-version-1 typed payload for the
// "scanner_worker.analysis" fact kind.
//
// The scanner-worker image analyzer emits this fact only for a completed
// supported image analysis. Analyzer, target identity, bounded status fields,
// result/fact counts, image identity, evidence source, and extraction reason
// are required because every emitted analysis fact carries them and downstream
// supply-chain readiness logic relies on their presence to distinguish scanned
// images from unsupported or missing evidence. Distro/package metadata is
// optional because image evidence can be validly present before a package
// database resolves those attributes.
type Analysis struct {
	Analyzer                 string  `json:"analyzer"`
	TargetKind               string  `json:"target_kind"`
	TargetLocatorHash        string  `json:"target_locator_hash"`
	AnalysisStatus           string  `json:"analysis_status"`
	CoverageStatus           string  `json:"coverage_status"`
	ResultCount              int     `json:"result_count"`
	FactCount                int     `json:"fact_count"`
	ImageReference           string  `json:"image_reference"`
	ImageDigest              string  `json:"image_digest"`
	EvidenceSource           string  `json:"evidence_source"`
	ExtractionReason         string  `json:"extraction_reason"`
	Distro                   *string `json:"distro,omitempty"`
	DistroVersion            *string `json:"distro_version,omitempty"`
	PackageManager           *string `json:"package_manager,omitempty"`
	ConfiguredImageReference *string `json:"configured_image_reference,omitempty"`
}
