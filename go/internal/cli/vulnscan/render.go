// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"io"
	"strings"
)

// RenderSummary writes the human-readable `eshu vuln-scan repo` summary. It
// rebuilds the report from result rather than taking one, because the caller
// that renders text has no other reason to hold a report, and both paths must
// agree on the exit code and readiness lines they print.
//
// Every write error is returned rather than ignored: this output is the
// operator's only view when --json is off, so a truncated write must not be
// reported as a successful scan. Writes go through writef, which explains why.
func RenderSummary(w io.Writer, result Result) error {
	mode := result.ScopeMode
	if mode == "" {
		mode = ScopeModeScoped
	}
	report := BuildReport(result, Now())
	if err := writef(w, "Vulnerability scan (%s): %s\n", mode, result.ReadinessState); err != nil {
		return err
	}
	if err := writef(w, "Repository: %s\n", result.RepositoryID); err != nil {
		return err
	}
	if err := writef(w, "Findings: %d", result.Count); err != nil {
		return err
	}
	if result.Truncated {
		if err := writef(w, " (truncated)"); err != nil {
			return err
		}
	}
	if err := writef(w, "\n"); err != nil {
		return err
	}
	if err := writef(
		w,
		"Exit: code=%d reason=%s\n",
		report.Summary.ExitCode, report.Summary.ExitReason,
	); err != nil {
		return err
	}
	if err := writef(
		w,
		"Readiness: state=%s freshness=%s\n",
		report.Readiness.State, defaultString(report.Readiness.Freshness, "unknown"),
	); err != nil {
		return err
	}
	if len(report.Readiness.MissingEvidence) > 0 {
		if err := writef(w, "Missing evidence: %s\n", strings.Join(report.Readiness.MissingEvidence, ", ")); err != nil {
			return err
		}
	}
	if summaries := unsupportedTargetSummaries(report.Readiness.UnsupportedTargets); len(summaries) > 0 {
		if err := writef(w, "Unsupported targets: %s\n", strings.Join(summaries, ", ")); err != nil {
			return err
		}
	}
	if report.Summary.EvidenceFactsTotal > 0 {
		if err := writef(w, "Evidence facts: %d\n", report.Summary.EvidenceFactsTotal); err != nil {
			return err
		}
	}
	if plan := result.ScopePlan; plan != nil {
		if err := writef(
			w,
			"Scope: observed_dependency_facts=%d advisory_facts=%d package_registry_facts=%d freshness=%s\n",
			plan.ObservedDependencyFacts, plan.AdvisoryFacts, plan.PackageRegistryFacts, defaultString(plan.Freshness, "unknown"),
		); err != nil {
			return err
		}
	}
	if perf := result.Performance; perf != nil {
		if err := writef(
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
		if err := writef(
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
