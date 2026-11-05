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
//
// RepositoryID and RelativePath scope a warning to the part of a repository
// whose analysis did not finish. Both are optional: older warnings and the
// image and SBOM warnings carry neither. A warning with a RelativePath and no
// RepositoryID joins nothing.
//
// A Go reachability producer (analyzer "go_reachability") that cannot finish
// the scan of a go.mod:
//
//   - MUST set target_kind "repository".
//   - MUST put the bounded failure class in Reason (for example
//     "toolchain_unsupported"). WarningClass stays the constant
//     "scanner_worker_warning" for every producer of this kind; a reader does
//     not use it to tell failures apart.
//   - MUST set RepositoryID to the repository id that the same claim writes on
//     its vulnerability.go_module_evidence rows.
//   - MUST set RelativePath, when one go.mod failed, to the slash-separated
//     path of that go.mod FILE relative to the repository root ("go.mod",
//     "tools/go.mod"), byte-equal to the relative_path the claim writes on that
//     go.mod's go_module_evidence rows. It MUST leave RelativePath absent or
//     blank when the failure covers the whole repository.
//   - MUST NOT write a vulnerability.go_call_reachability fact from the failed
//     scan of a go.mod it warns about, because a failed run has no trustworthy
//     reachability result. A sibling go.mod's finished scan still writes its
//     facts, even for the same dependency.
//
// RelativePath names the go.mod file, not the vulnerable dependency, because a
// finding is keyed by the dependency while a scan is keyed by the file.
//
// A reader applies this rule. It joins a warning to a finding when the
// warning's analyzer is "go_reachability", its RepositoryID equals the finding's
// repository, and its RelativePath is blank or is one of the go.mod paths of
// the go_module_evidence rows behind that finding (all go.mod files that
// require the dependency, not only the one the finding reports). Then:
//
//	vulnerability.go_call_reachability fact | joined warning | outcome
//	reachable (symbol or import level)      | any            | reachable; the fact wins
//	not_called, module, or unknown level    | one or more    | analysis_failed: missing evidence
//	no fact                                 | one or more    | analysis_failed: missing evidence
//	not_called level                        | none           | not_called
//	module or unknown level                 | none           | the fact's own state: module_only or unknown (unchanged)
//	no fact                                 | none           | module_only, unknown (go_module_evidence rows exist)
//	(no go_module_evidence rows)            | any            | no Go finding
//
// A module-level fact with no joined warning stays module_only and an
// unknown-level one stays unknown, each with the reason text the reader
// already attaches to that level; this contract does not fix that text. The
// reader MUST NOT add analysis_failed to either without a joined warning.
//
// An analysis_failed outcome names each distinct warning as
// "govulncheck analysis failed: <Reason>" for a whole-repository warning and
// "govulncheck analysis failed: <Reason> at <RelativePath>" for a go.mod
// warning, plus the "govulncheck call-graph evidence missing" entry. A module with neither a fact nor a warning has no evidence either
// way and stays unknown.
//
// The core reducer does not read this kind yet (eshu-hq/eshu#10043); until that
// lands a failed scan stays unknown.
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
	// RepositoryID is the canonical repository id of the scan target, the same
	// value the Go reachability facts carry. Absent when the producer does not
	// scope the warning to a repository; a warning without it joins nothing.
	RepositoryID *string `json:"repository_id,omitempty"`
	// RelativePath is the slash-separated path of the go.mod file, relative to
	// the repository root, whose scan did not finish. Blank or absent means the
	// failure covers the whole repository. It is byte-equal to the relative_path
	// of that file's go_module_evidence rows.
	RelativePath *string `json:"relative_path,omitempty"`
}
