// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Warning is the schema-version-1 typed payload for the
// "scanner_worker.warning" fact kind.
//
// The scanner-worker image analyzer emits this fact when image evidence is
// unsupported or cannot be scanned. Analyzer, target identity, warning reason,
// bounded status fields, image identity, evidence source, and extraction reason
// are required because every emitted warning carries them and query/readiness
// code uses them as explicit negative coverage evidence. Distro/package
// metadata is optional because unsupported image targets often lack it.
type Warning struct {
	Analyzer          string  `json:"analyzer"`
	TargetKind        string  `json:"target_kind"`
	TargetLocatorHash string  `json:"target_locator_hash"`
	Reason            string  `json:"reason"`
	WarningClass      string  `json:"warning_class"`
	AnalysisStatus    string  `json:"analysis_status"`
	CoverageStatus    string  `json:"coverage_status"`
	ImageReference    string  `json:"image_reference"`
	ImageDigest       string  `json:"image_digest"`
	EvidenceSource    string  `json:"evidence_source"`
	ExtractionReason  string  `json:"extraction_reason"`
	Distro            *string `json:"distro,omitempty"`
	DistroVersion     *string `json:"distro_version,omitempty"`
	PackageManager    *string `json:"package_manager,omitempty"`
}
