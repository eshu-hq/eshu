// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const vulnScanReportSchemaVersion = "eshu.vulnerability_report.v1"

type vulnScanReport struct {
	SchemaVersion string                  `json:"schema_version"`
	GeneratedAt   string                  `json:"generated_at"`
	Command       string                  `json:"command"`
	Target        scanTarget              `json:"target"`
	RepositoryID  string                  `json:"repository_id,omitempty"`
	Summary       vulnScanReportSummary   `json:"summary"`
	Readiness     vulnScanReportReadiness `json:"readiness"`
	Findings      []vulnScanReportFinding `json:"findings"`
	ScopePlan     *vulnScanScopePlan      `json:"scope_plan,omitempty"`
	Performance   *vulnScanPerformance    `json:"scan_performance,omitempty"`
	Evidence      vulnScanRepoEvidence    `json:"evidence"`
}

type vulnScanReportSummary struct {
	TotalFindings      int            `json:"total_findings"`
	Truncated          bool           `json:"truncated"`
	FindingsByStatus   map[string]int `json:"findings_by_status,omitempty"`
	HighestPriority    string         `json:"highest_priority,omitempty"`
	ExitCode           int            `json:"exit_code"`
	ExitReason         string         `json:"exit_reason"`
	ReadinessState     string         `json:"readiness_state"`
	EvidenceFactsTotal int            `json:"evidence_facts_total,omitempty"`
}

type vulnScanReportReadiness struct {
	State              string           `json:"state"`
	Freshness          string           `json:"freshness,omitempty"`
	MissingEvidence    []string         `json:"missing_evidence,omitempty"`
	UnsupportedTargets []map[string]any `json:"unsupported_targets,omitempty"`
	IncompleteReasons  []string         `json:"incomplete_reasons,omitempty"`
	EvidenceSources    []map[string]any `json:"evidence_sources,omitempty"`
	SourceSnapshots    []map[string]any `json:"source_snapshots,omitempty"`
	Counts             map[string]any   `json:"counts,omitempty"`
}

type vulnScanReportFinding struct {
	FindingID       string                         `json:"finding_id"`
	CVEID           string                         `json:"cve_id,omitempty"`
	AdvisoryID      string                         `json:"advisory_id,omitempty"`
	Target          vulnScanReportFindingTarget    `json:"target"`
	Package         vulnScanReportPackageContext   `json:"package"`
	Affected        vulnScanReportAffectedContext  `json:"affected"`
	Priority        *vulnScanReportPriorityContext `json:"priority,omitempty"`
	Reachability    *vulnScanReportReachability    `json:"reachability,omitempty"`
	Remediation     map[string]any                 `json:"remediation,omitempty"`
	MissingEvidence []string                       `json:"missing_evidence,omitempty"`
	EvidenceHandles []vulnScanEvidenceHandle       `json:"evidence_handles,omitempty"`
	SourceFreshness string                         `json:"source_freshness,omitempty"`
}

type vulnScanReportFindingTarget struct {
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

type vulnScanReportPackageContext struct {
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

type vulnScanReportAffectedContext struct {
	Status          string `json:"status"`
	Confidence      string `json:"confidence,omitempty"`
	ObservedVersion string `json:"observed_version,omitempty"`
	RequestedRange  string `json:"requested_range,omitempty"`
	VulnerableRange string `json:"vulnerable_range,omitempty"`
	FixedVersion    string `json:"fixed_version,omitempty"`
	MatchReason     string `json:"match_reason,omitempty"`
}

type vulnScanReportPriorityContext struct {
	Bucket      string   `json:"bucket,omitempty"`
	Score       int      `json:"score,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	ReasonCodes []string `json:"reason_codes,omitempty"`
}

type vulnScanReportReachability struct {
	State            string   `json:"state"`
	Confidence       string   `json:"confidence,omitempty"`
	Source           string   `json:"source,omitempty"`
	Evidence         string   `json:"evidence,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	LanguageMaturity string   `json:"language_maturity,omitempty"`
	MissingEvidence  []string `json:"missing_evidence,omitempty"`
}

type vulnScanEvidenceHandle struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func buildVulnScanReport(result vulnScanRepoResult, generatedAt time.Time) vulnScanReport {
	code, reason := vulnScanExitClassification(result.ReadinessState, result.Count)
	readiness := buildVulnScanReportReadiness(result.Readiness, result.ReadinessState)
	if result.ScopePlan != nil {
		readiness.MissingEvidence = mergeStringLists(readiness.MissingEvidence, result.ScopePlan.MissingEvidence)
		readiness.IncompleteReasons = mergeStringLists(readiness.IncompleteReasons, result.ScopePlan.IncompleteReasons)
	}
	findings := buildVulnScanReportFindings(result.Findings)
	return vulnScanReport{
		SchemaVersion: vulnScanReportSchemaVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339Nano),
		Command:       result.Command,
		Target:        result.Target,
		RepositoryID:  result.RepositoryID,
		Summary: vulnScanReportSummary{
			TotalFindings:      result.Count,
			Truncated:          result.Truncated,
			FindingsByStatus:   vulnScanFindingsByStatus(result.Findings),
			HighestPriority:    highestVulnScanPriority(result.Findings),
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

func renderVulnScanRepoSummary(w io.Writer, result vulnScanRepoResult) error {
	mode := result.ScopeMode
	if mode == "" {
		mode = vulnScanScopeModeScoped
	}
	report := buildVulnScanReport(result, vulnScanNow())
	if _, err := fmt.Fprintf(w, "Vulnerability scan (%s): %s\n", mode, result.ReadinessState); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Repository: %s\n", result.RepositoryID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Findings: %d", result.Count); err != nil {
		return err
	}
	if result.Truncated {
		if _, err := fmt.Fprint(w, " (truncated)"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"Exit: code=%d reason=%s\n",
		report.Summary.ExitCode, report.Summary.ExitReason,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"Readiness: state=%s freshness=%s\n",
		report.Readiness.State, defaultString(report.Readiness.Freshness, "unknown"),
	); err != nil {
		return err
	}
	if len(report.Readiness.MissingEvidence) > 0 {
		if _, err := fmt.Fprintf(w, "Missing evidence: %s\n", strings.Join(report.Readiness.MissingEvidence, ", ")); err != nil {
			return err
		}
	}
	if summaries := unsupportedTargetSummaries(report.Readiness.UnsupportedTargets); len(summaries) > 0 {
		if _, err := fmt.Fprintf(w, "Unsupported targets: %s\n", strings.Join(summaries, ", ")); err != nil {
			return err
		}
	}
	if report.Summary.EvidenceFactsTotal > 0 {
		if _, err := fmt.Fprintf(w, "Evidence facts: %d\n", report.Summary.EvidenceFactsTotal); err != nil {
			return err
		}
	}
	if plan := result.ScopePlan; plan != nil {
		if _, err := fmt.Fprintf(
			w,
			"Scope: observed_dependency_facts=%d advisory_facts=%d package_registry_facts=%d freshness=%s\n",
			plan.ObservedDependencyFacts, plan.AdvisoryFacts, plan.PackageRegistryFacts, defaultString(plan.Freshness, "unknown"),
		); err != nil {
			return err
		}
	}
	if perf := result.Performance; perf != nil {
		if _, err := fmt.Fprintf(
			w,
			"Performance: wall_time_ms=%d repo_files=%d repo_bytes=%d stop=%s\n",
			perf.WallTimeMS, perf.RepositoryFileCount, perf.RepositorySizeBytes, perf.StopThreshold,
		); err != nil {
			return err
		}
	}
	for _, finding := range report.Findings {
		packageLabel := defaultString(
			finding.Package.PackageName,
			defaultString(finding.Package.PackageID, "-"),
		)
		if _, err := fmt.Fprintf(
			w,
			"- %s %s %s %s fixed=%s evidence=%s\n",
			defaultString(finding.FindingID, "-"),
			defaultString(finding.CVEID, "-"),
			packageLabel,
			defaultString(finding.Affected.Status, "-"),
			defaultString(finding.Affected.FixedVersion, "unknown"),
			strings.Join(evidenceHandleIDs(finding.EvidenceHandles), ","),
		); err != nil {
			return err
		}
	}
	return nil
}

func buildVulnScanReportReadiness(readiness map[string]any, state string) vulnScanReportReadiness {
	report := vulnScanReportReadiness{State: strings.TrimSpace(state)}
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

func buildVulnScanReportFindings(findings []map[string]any) []vulnScanReportFinding {
	reportFindings := make([]vulnScanReportFinding, 0, len(findings))
	for _, finding := range findings {
		reportFinding := vulnScanReportFinding{
			FindingID:  stringFromMap(finding, "finding_id"),
			CVEID:      stringFromMap(finding, "cve_id"),
			AdvisoryID: stringFromMap(finding, "advisory_id"),
			Target: vulnScanReportFindingTarget{
				RepositoryID:        stringFromMap(finding, "repository_id"),
				SourcePath:          vulnScanFindingSourcePath(finding),
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
			Package: vulnScanReportPackageContext{
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
			Affected: vulnScanReportAffectedContext{
				Status:          stringFromMap(finding, "impact_status"),
				Confidence:      stringFromMap(finding, "confidence"),
				ObservedVersion: stringFromMap(finding, "observed_version"),
				RequestedRange:  stringFromMap(finding, "requested_range"),
				VulnerableRange: stringFromMap(finding, "vulnerable_range"),
				FixedVersion:    stringFromMap(finding, "fixed_version"),
				MatchReason:     stringFromMap(finding, "match_reason"),
			},
			Priority:        priorityFromFinding(finding),
			Reachability:    reachabilityFromFinding(finding),
			MissingEvidence: stringSliceFromAny(finding["missing_evidence"]),
			EvidenceHandles: evidenceHandlesFromFinding(finding),
			SourceFreshness: stringFromMap(finding, "source_freshness"),
		}
		reportFinding.Remediation = remediationFromFinding(finding)
		reportFindings = append(reportFindings, reportFinding)
	}
	return reportFindings
}

func vulnScanFindingSourcePath(finding map[string]any) string {
	if sourcePath := stringFromMap(finding, "source_path"); sourcePath != "" {
		return sourcePath
	}
	return stringFromMap(finding, "manifest_path")
}
