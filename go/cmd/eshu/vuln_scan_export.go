// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	exportspkg "github.com/eshu-hq/eshu/go/internal/exports"
)

const (
	vulnScanExportFormatSARIF = "sarif"
	vulnScanExportFormatVEX   = "vex"
)

func writeVulnScanSARIF(w io.Writer, result vulnScanRepoResult, report vulnScanReport) error {
	snapshot, err := vulnScanSARIFSnapshot(result, report)
	if err != nil {
		return err
	}
	return exportspkg.NewRegistry().Export(w, exportspkg.FormatSARIF, snapshot, exportspkg.Options{
		Tool: exportspkg.Tool{
			Name:    "eshu",
			Version: buildinfo.AppVersion(),
			URI:     "https://eshu.dev",
		},
	})
}

func vulnScanSARIFSnapshot(result vulnScanRepoResult, report vulnScanReport) (exportspkg.Snapshot, error) {
	repositoryID := strings.TrimSpace(result.RepositoryID)
	if repositoryID == "" {
		repositoryID = strings.TrimSpace(report.RepositoryID)
	}
	if repositoryID == "" {
		return exportspkg.Snapshot{}, fmt.Errorf("vuln-scan SARIF export requires a resolved repository id")
	}
	generatedAt, err := time.Parse(time.RFC3339Nano, report.GeneratedAt)
	if err != nil {
		return exportspkg.Snapshot{}, fmt.Errorf("parse report generated_at for SARIF export: %w", err)
	}
	return exportspkg.Snapshot{
		Scope: exportspkg.Scope{
			Kind:         exportspkg.ScopeKindRepository,
			RepositoryID: repositoryID,
		},
		GeneratedAt: generatedAt,
		Findings:    vulnScanSARIFFindings(result.Findings),
		Status:      vulnScanSARIFStatus(report, result.ScopeMode),
	}, nil
}

func vulnScanSARIFStatus(report vulnScanReport, scopeMode string) exportspkg.SnapshotStatus {
	missingEvidence := report.Readiness.MissingEvidence
	if report.ScopePlan != nil {
		missingEvidence = append(missingEvidence, report.ScopePlan.MissingEvidence...)
	}
	return exportspkg.SnapshotStatus{
		ReportSchemaVersion: report.SchemaVersion,
		ReadinessState:      report.Readiness.State,
		ReadinessFreshness:  report.Readiness.Freshness,
		ExitCode:            report.Summary.ExitCode,
		ExitReason:          report.Summary.ExitReason,
		ScopeMode:           defaultString(scopeMode, vulnScanScopeModeScoped),
		MissingEvidence:     cloneUniqueSortedStrings(missingEvidence),
		IncompleteReasons:   cloneAndSortStrings(report.Readiness.IncompleteReasons),
		UnsupportedTargets:  vulnScanSARIFUnsupportedTargets(report.Readiness.UnsupportedTargets),
	}
}

func vulnScanSARIFFindings(findings []map[string]any) []exportspkg.Finding {
	out := make([]exportspkg.Finding, 0, len(findings))
	for _, finding := range findings {
		provenance := mapFromAny(finding["provenance"])
		cvssScore := firstPositiveFloat(
			floatFromAny(finding["cvss_score"]),
			floatFromAny(provenance["selected_severity_score"]),
		)
		cvssVector := firstNonEmpty(
			stringFromMap(finding, "cvss_vector"),
			stringFromMap(provenance, "selected_severity_vector"),
		)
		out = append(out, exportspkg.Finding{
			FindingID:           stringFromMap(finding, "finding_id"),
			CVEID:               stringFromMap(finding, "cve_id"),
			AdvisoryID:          stringFromMap(finding, "advisory_id"),
			PackageID:           stringFromMap(finding, "package_id"),
			Ecosystem:           stringFromMap(finding, "ecosystem"),
			PackageName:         stringFromMap(finding, "package_name"),
			PURL:                stringFromMap(finding, "purl"),
			ObservedVersion:     stringFromMap(finding, "observed_version"),
			RequestedRange:      stringFromMap(finding, "requested_range"),
			VulnerableRange:     stringFromMap(finding, "vulnerable_range"),
			FixedVersion:        stringFromMap(finding, "fixed_version"),
			MatchReason:         stringFromMap(finding, "match_reason"),
			Summary:             stringFromMap(finding, "summary"),
			Description:         stringFromMap(finding, "description"),
			Severity:            vulnScanSARIFSeverity(finding, provenance, cvssScore),
			CVSSScore:           cvssScore,
			CVSSVector:          cvssVector,
			KnownExploited:      boolFromAny(finding["known_exploited"]),
			EPSSProbability:     stringFromMap(finding, "epss_probability"),
			RepositoryID:        stringFromMap(finding, "repository_id"),
			SubjectDigest:       stringFromMap(finding, "subject_digest"),
			ImageRef:            stringFromMap(finding, "image_ref"),
			RuntimeReachability: stringFromMap(finding, "runtime_reachability"),
			Reachability:        vulnScanSARIFReachability(finding),
			ImpactStatus:        stringFromMap(finding, "impact_status"),
			Confidence:          stringFromMap(finding, "confidence"),
			WorkloadIDs:         cloneAndSortStrings(stringSliceFromAny(finding["workload_ids"])),
			ServiceIDs:          cloneAndSortStrings(stringSliceFromAny(finding["service_ids"])),
			Environments:        cloneAndSortStrings(stringSliceFromAny(finding["environments"])),
			DependencyScope:     stringFromMap(finding, "dependency_scope"),
			DependencyPath:      stringSliceFromAny(finding["dependency_path"]),
			DirectDependency:    boolPtrFromAny(finding["direct_dependency"]),
			MissingEvidence:     cloneAndSortStrings(stringSliceFromAny(finding["missing_evidence"])),
			EvidenceFactIDs:     cloneAndSortStrings(stringSliceFromAny(finding["evidence_fact_ids"])),
			SourceFreshness:     stringFromMap(finding, "source_freshness"),
			Remediation:         vulnScanSARIFRemediation(finding),
			Locations:           vulnScanSARIFLocations(finding),
			AdvisorySources:     vulnScanSARIFAdvisorySources(finding, provenance),
			HelpURI:             vulnScanSARIFHelpURI(finding, provenance),
		})
	}
	return out
}

func vulnScanSARIFSeverity(
	finding map[string]any,
	provenance map[string]any,
	cvssScore float64,
) exportspkg.Severity {
	for _, raw := range []string{
		stringFromMap(provenance, "selected_severity_label"),
		stringFromMap(finding, "selected_severity_label"),
		stringFromMap(finding, "severity"),
		stringFromMap(finding, "priority_bucket"),
	} {
		if severity := exportspkg.NormalizeSeverity(raw); severity != exportspkg.SeverityUnknown {
			return severity
		}
	}
	return severityFromCVSS(cvssScore)
}

func severityFromCVSS(score float64) exportspkg.Severity {
	switch {
	case score >= 9:
		return exportspkg.SeverityCritical
	case score >= 7:
		return exportspkg.SeverityHigh
	case score >= 4:
		return exportspkg.SeverityMedium
	case score > 0:
		return exportspkg.SeverityLow
	default:
		return exportspkg.SeverityUnknown
	}
}

func vulnScanSARIFLocations(finding map[string]any) []exportspkg.Location {
	locations := make([]exportspkg.Location, 0, 2)
	seen := map[string]struct{}{}
	appendLocation := func(source map[string]any) {
		path := firstNonEmpty(
			stringFromMap(source, "manifest_path"),
			stringFromMap(source, "source_path"),
			stringFromMap(source, "relative_path"),
			stringFromMap(source, "path"),
			stringFromMap(source, "uri"),
		)
		if path == "" {
			return
		}
		location := exportspkg.Location{
			ManifestPath: path,
			StartLine:    firstPositiveInt(intFromAny(source["start_line"]), intFromAny(source["line"])),
			EndLine:      intFromAny(source["end_line"]),
		}
		key := fmt.Sprintf("%s:%d:%d", location.ManifestPath, location.StartLine, location.EndLine)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		locations = append(locations, location)
	}
	appendLocation(finding)
	if sourceLocation := mapFromAny(finding["source_location"]); len(sourceLocation) > 0 {
		appendLocation(sourceLocation)
	}
	for _, location := range mapSliceFromAny(finding["locations"]) {
		appendLocation(location)
	}
	return locations
}

func vulnScanSARIFAdvisorySources(
	finding map[string]any,
	provenance map[string]any,
) []exportspkg.AdvisorySource {
	rawSources := mapSliceFromAny(provenance["advisory_sources"])
	if len(rawSources) == 0 {
		rawSources = mapSliceFromAny(finding["advisory_sources"])
	}
	sources := make([]exportspkg.AdvisorySource, 0, len(rawSources))
	for _, raw := range rawSources {
		source := exportspkg.AdvisorySource{
			Source:     stringFromMap(raw, "source"),
			AdvisoryID: firstNonEmpty(stringFromMap(raw, "advisory_id"), stringFromMap(raw, "advisoryId")),
			URL:        firstNonEmpty(stringFromMap(raw, "url"), stringFromMap(raw, "source_url")),
		}
		if source.Source == "" && source.AdvisoryID == "" && source.URL == "" {
			continue
		}
		sources = append(sources, source)
	}
	return sources
}

func vulnScanSARIFHelpURI(finding map[string]any, provenance map[string]any) string {
	if uri := firstNonEmpty(stringFromMap(finding, "help_uri"), stringFromMap(finding, "advisory_url")); uri != "" {
		return uri
	}
	for _, source := range vulnScanSARIFAdvisorySources(finding, provenance) {
		if source.URL != "" {
			return source.URL
		}
	}
	return ""
}

func vulnScanSARIFRemediation(finding map[string]any) *exportspkg.Remediation {
	remediation := remediationFromFinding(finding)
	if len(remediation) == 0 {
		return nil
	}
	out := &exportspkg.Remediation{
		CurrentVersion:      stringFromMap(remediation, "current_version"),
		VulnerableRange:     stringFromMap(remediation, "vulnerable_range"),
		FixedVersionSource:  stringFromMap(remediation, "fixed_version_source"),
		MatchReason:         stringFromMap(remediation, "match_reason"),
		FirstPatchedVersion: stringFromMap(remediation, "first_patched_version"),
		ManifestRange:       stringFromMap(remediation, "manifest_range"),
		ManifestAllowsFix:   stringFromMap(remediation, "manifest_allows_fix"),
		Confidence:          stringFromMap(remediation, "confidence"),
		Reason:              stringFromMap(remediation, "reason"),
		Direct:              boolPtrFromAny(remediation["direct"]),
		MissingEvidence:     cloneAndSortStrings(stringSliceFromAny(remediation["missing_evidence"])),
	}
	if out.CurrentVersion == "" && out.VulnerableRange == "" &&
		out.FixedVersionSource == "" && out.MatchReason == "" &&
		out.FirstPatchedVersion == "" && out.ManifestRange == "" &&
		out.ManifestAllowsFix == "" && out.Confidence == "" &&
		out.Reason == "" && out.Direct == nil &&
		len(out.MissingEvidence) == 0 {
		return nil
	}
	return out
}

func vulnScanSARIFUnsupportedTargets(targets []map[string]any) []exportspkg.UnsupportedTarget {
	out := make([]exportspkg.UnsupportedTarget, 0, len(targets))
	for _, target := range targets {
		unsupported := exportspkg.UnsupportedTarget{
			TargetKind:  stringFromMap(target, "target_kind"),
			Reason:      stringFromMap(target, "reason"),
			Ecosystem:   stringFromMap(target, "ecosystem"),
			PackageID:   stringFromMap(target, "package_id"),
			PackageName: stringFromMap(target, "package_name"),
			Count:       intFromAny(target["count"]),
		}
		if unsupported.TargetKind == "" && unsupported.Reason == "" &&
			unsupported.Ecosystem == "" && unsupported.PackageID == "" &&
			unsupported.PackageName == "" && unsupported.Count == 0 {
			continue
		}
		out = append(out, unsupported)
	}
	return out
}

func mapFromAny(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func floatFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func boolFromAny(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func cloneAndSortStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

func cloneUniqueSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
