// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"strings"
	"time"
)

// ReportSchemaVersion identifies the report shape embedded in the JSON
// envelope and re-read by the SARIF and VEX writers. Consumers pin it, so a
// shape change needs a new version rather than an edit here.
const ReportSchemaVersion = "eshu.vulnerability_report.v1"

// Report is the scanner-style vulnerability report the vuln-scan envelope
// carries and the SARIF and VEX exports are derived from. It is a projection
// of Result: everything here is either copied from the result or computed from
// its findings and readiness envelope.
type Report struct {
	SchemaVersion string          `json:"schema_version"`
	GeneratedAt   string          `json:"generated_at"`
	Command       string          `json:"command"`
	Target        Target          `json:"target"`
	RepositoryID  string          `json:"repository_id,omitempty"`
	Summary       ReportSummary   `json:"summary"`
	Readiness     ReportReadiness `json:"readiness"`
	Findings      []ReportFinding `json:"findings"`
	ScopePlan     *ScopePlan      `json:"scope_plan,omitempty"`
	Performance   *Performance    `json:"scan_performance,omitempty"`
	Evidence      RepoEvidence    `json:"evidence"`
}

// ReportSummary is the one-glance verdict: how many findings, the exit code
// and reason the run will end with, and the readiness state it stopped at.
type ReportSummary struct {
	TotalFindings      int            `json:"total_findings"`
	Truncated          bool           `json:"truncated"`
	FindingsByStatus   map[string]int `json:"findings_by_status,omitempty"`
	HighestPriority    string         `json:"highest_priority,omitempty"`
	ExitCode           int            `json:"exit_code"`
	ExitReason         string         `json:"exit_reason"`
	ReadinessState     string         `json:"readiness_state"`
	EvidenceFactsTotal int            `json:"evidence_facts_total,omitempty"`
}

// ReportReadiness is the server's readiness envelope as the report presents
// it, with the CLI-side scope plan's missing evidence and incomplete reasons
// merged in.
type ReportReadiness struct {
	State              string           `json:"state"`
	Freshness          string           `json:"freshness,omitempty"`
	MissingEvidence    []string         `json:"missing_evidence,omitempty"`
	UnsupportedTargets []map[string]any `json:"unsupported_targets,omitempty"`
	IncompleteReasons  []string         `json:"incomplete_reasons,omitempty"`
	EvidenceSources    []map[string]any `json:"evidence_sources,omitempty"`
	SourceSnapshots    []map[string]any `json:"source_snapshots,omitempty"`
	Counts             map[string]any   `json:"counts,omitempty"`
}

// ReportFinding is one vulnerability impact finding, read out of the generic
// map the API returned into the named shape the report publishes.
type ReportFinding struct {
	FindingID       string                 `json:"finding_id"`
	CVEID           string                 `json:"cve_id,omitempty"`
	AdvisoryID      string                 `json:"advisory_id,omitempty"`
	Target          ReportFindingTarget    `json:"target"`
	Package         ReportPackageContext   `json:"package"`
	Affected        ReportAffectedContext  `json:"affected"`
	Priority        *ReportPriorityContext `json:"priority,omitempty"`
	Reachability    *ReportReachability    `json:"reachability,omitempty"`
	Remediation     map[string]any         `json:"remediation,omitempty"`
	MissingEvidence []string               `json:"missing_evidence,omitempty"`
	EvidenceHandles []EvidenceHandle       `json:"evidence_handles,omitempty"`
	SourceFreshness string                 `json:"source_freshness,omitempty"`
}

// ReportFindingTarget locates a finding: where in the repository it was
// observed, and which deployed subjects carry it.
type ReportFindingTarget struct {
	RepositoryID        string   `json:"repository_id,omitempty"`
	SourcePath          string   `json:"source_path,omitempty"`
	ManifestPath        string   `json:"manifest_path,omitempty"`
	StartLine           int      `json:"start_line,omitempty"`
	EndLine             int      `json:"end_line,omitempty"`
	SubjectDigest       string   `json:"subject_digest,omitempty"`
	ImageRef            string   `json:"image_ref,omitempty"`
	RuntimeReachability string   `json:"runtime_reachability,omitempty"`
	WorkloadIDs         []string `json:"workload_ids,omitempty"`
	ServiceIDs          []string `json:"service_ids,omitempty"`
	Environments        []string `json:"environments,omitempty"`
}

// ReportPackageContext identifies the package the advisory matched and how the
// repository depends on it.
type ReportPackageContext struct {
	PackageID        string   `json:"package_id,omitempty"`
	PackageName      string   `json:"package_name,omitempty"`
	Ecosystem        string   `json:"ecosystem,omitempty"`
	PURL             string   `json:"purl,omitempty"`
	ProductCriteria  string   `json:"product_criteria,omitempty"`
	DependencyScope  string   `json:"dependency_scope,omitempty"`
	DependencyPath   []string `json:"dependency_path,omitempty"`
	DependencyDepth  int      `json:"dependency_depth,omitempty"`
	DirectDependency *bool    `json:"direct_dependency,omitempty"`
}

// ReportAffectedContext is the reducer's impact verdict for the finding, with
// the version evidence the verdict rests on.
type ReportAffectedContext struct {
	Status          string `json:"status"`
	Confidence      string `json:"confidence,omitempty"`
	ObservedVersion string `json:"observed_version,omitempty"`
	RequestedRange  string `json:"requested_range,omitempty"`
	VulnerableRange string `json:"vulnerable_range,omitempty"`
	FixedVersion    string `json:"fixed_version,omitempty"`
	MatchReason     string `json:"match_reason,omitempty"`
}

// ReportPriorityContext is the server-assigned priority. It is a pointer on
// ReportFinding so a finding with no priority evidence omits the block instead
// of publishing a zero bucket.
type ReportPriorityContext struct {
	Bucket      string   `json:"bucket,omitempty"`
	Score       int      `json:"score,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	ReasonCodes []string `json:"reason_codes,omitempty"`
}

// ReportReachability is the reachability verdict for the finding, including
// what evidence is missing when the verdict is not conclusive.
type ReportReachability struct {
	State            string   `json:"state"`
	Confidence       string   `json:"confidence,omitempty"`
	Source           string   `json:"source,omitempty"`
	Evidence         string   `json:"evidence,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	LanguageMaturity string   `json:"language_maturity,omitempty"`
	MissingEvidence  []string `json:"missing_evidence,omitempty"`
}

// EvidenceHandle points at a stored fact behind a finding, so an operator can
// fetch the raw evidence rather than trusting the summary.
type EvidenceHandle struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// BuildReport projects a finished Result into the published report. The
// scope plan's missing evidence and incomplete reasons are merged into the
// readiness block, so a CLI-side fail-closed reason appears alongside the
// server's own.
func BuildReport(result Result, generatedAt time.Time) Report {
	code, reason := ExitClassification(result.ReadinessState, result.Count)
	readiness := buildReportReadiness(result.Readiness, result.ReadinessState)
	if result.ScopePlan != nil {
		readiness.MissingEvidence = mergeStringLists(readiness.MissingEvidence, result.ScopePlan.MissingEvidence)
		readiness.IncompleteReasons = mergeStringLists(readiness.IncompleteReasons, result.ScopePlan.IncompleteReasons)
	}
	findings := buildReportFindings(result.Findings)
	return Report{
		SchemaVersion: ReportSchemaVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339Nano),
		Command:       result.Command,
		Target:        result.Target,
		RepositoryID:  result.RepositoryID,
		Summary: ReportSummary{
			TotalFindings:      result.Count,
			Truncated:          result.Truncated,
			FindingsByStatus:   findingsByStatus(result.Findings),
			HighestPriority:    highestPriority(result.Findings),
			ExitCode:           code,
			ExitReason:         reason,
			ReadinessState:     result.ReadinessState,
			EvidenceFactsTotal: evidenceFactsTotal(readiness.Counts),
		},
		Readiness:   readiness,
		Findings:    findings,
		ScopePlan:   result.ScopePlan,
		Performance: result.Performance,
		Evidence:    result.Evidence,
	}
}

// buildReportReadiness reads the server's readiness map into the report's
// named shape. A blank state is reported as `readiness_unavailable` rather
// than empty, so the report never implies a verdict the server did not give.
func buildReportReadiness(readiness map[string]any, state string) ReportReadiness {
	report := ReportReadiness{State: strings.TrimSpace(state)}
	if report.State == "" {
		report.State = "readiness_unavailable"
	}
	if readiness == nil {
		return report
	}
	if freshness, ok := readiness["freshness"].(string); ok {
		report.Freshness = strings.TrimSpace(freshness)
	}
	report.MissingEvidence = stringSliceFromAny(readiness["missing_evidence"])
	report.IncompleteReasons = stringSliceFromAny(readiness["incomplete_reasons"])
	report.UnsupportedTargets = mapSliceFromAny(readiness["unsupported_targets"])
	report.EvidenceSources = mapSliceFromAny(readiness["evidence_sources"])
	report.SourceSnapshots = mapSliceFromAny(readiness["source_snapshots"])
	if counts, ok := readiness["counts"].(map[string]any); ok {
		report.Counts = counts
	}
	return report
}

// buildReportFindings reads each raw finding map into the report's named
// finding shape.
func buildReportFindings(findings []map[string]any) []ReportFinding {
	reportFindings := make([]ReportFinding, 0, len(findings))
	for _, finding := range findings {
		reportFinding := ReportFinding{
			FindingID:  stringFromMap(finding, "finding_id"),
			CVEID:      stringFromMap(finding, "cve_id"),
			AdvisoryID: stringFromMap(finding, "advisory_id"),
			Target: ReportFindingTarget{
				RepositoryID:        stringFromMap(finding, "repository_id"),
				SourcePath:          findingSourcePath(finding),
				ManifestPath:        stringFromMap(finding, "manifest_path"),
				StartLine:           intFromAny(finding["start_line"]),
				EndLine:             intFromAny(finding["end_line"]),
				SubjectDigest:       stringFromMap(finding, "subject_digest"),
				ImageRef:            stringFromMap(finding, "image_ref"),
				RuntimeReachability: stringFromMap(finding, "runtime_reachability"),
				WorkloadIDs:         stringSliceFromAny(finding["workload_ids"]),
				ServiceIDs:          stringSliceFromAny(finding["service_ids"]),
				Environments:        stringSliceFromAny(finding["environments"]),
			},
			Package: ReportPackageContext{
				PackageID:        stringFromMap(finding, "package_id"),
				PackageName:      stringFromMap(finding, "package_name"),
				Ecosystem:        stringFromMap(finding, "ecosystem"),
				PURL:             stringFromMap(finding, "purl"),
				ProductCriteria:  stringFromMap(finding, "product_criteria"),
				DependencyScope:  stringFromMap(finding, "dependency_scope"),
				DependencyPath:   stringSliceFromAny(finding["dependency_path"]),
				DependencyDepth:  intFromAny(finding["dependency_depth"]),
				DirectDependency: boolPtrFromAny(finding["direct_dependency"]),
			},
			Affected: ReportAffectedContext{
				Status:          stringFromMap(finding, "impact_status"),
				Confidence:      stringFromMap(finding, "confidence"),
				ObservedVersion: stringFromMap(finding, "observed_version"),
				RequestedRange:  stringFromMap(finding, "requested_range"),
				VulnerableRange: stringFromMap(finding, "vulnerable_range"),
				FixedVersion:    stringFromMap(finding, "fixed_version"),
				MatchReason:     stringFromMap(finding, "match_reason"),
			},
			Priority:        priorityFromFinding(finding),
			Reachability:    ReachabilityFromFinding(finding),
			MissingEvidence: stringSliceFromAny(finding["missing_evidence"]),
			EvidenceHandles: evidenceHandlesFromFinding(finding),
			SourceFreshness: stringFromMap(finding, "source_freshness"),
		}
		reportFinding.Remediation = RemediationFromFinding(finding)
		reportFindings = append(reportFindings, reportFinding)
	}
	return reportFindings
}

// findingSourcePath prefers the finding's own source path and falls back to
// the manifest path, so a finding located only by manifest still reports a
// path rather than an empty string.
func findingSourcePath(finding map[string]any) string {
	if sourcePath := stringFromMap(finding, "source_path"); sourcePath != "" {
		return sourcePath
	}
	return stringFromMap(finding, "manifest_path")
}
