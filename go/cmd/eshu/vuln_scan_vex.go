// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const vulnScanVEXSchemaVersion = "eshu.vex_statements.v1"

type vulnScanVEXDocument struct {
	SchemaVersion   string                     `json:"schema_version"`
	GeneratedAt     string                     `json:"generated_at"`
	DocumentName    string                     `json:"document_name"`
	Scope           vulnScanVEXScope           `json:"scope"`
	Readiness       vulnScanReportReadiness    `json:"readiness"`
	StatementPolicy vulnScanVEXStatementPolicy `json:"statement_policy"`
	Statements      []vulnScanVEXStatement     `json:"statements"`
}

type vulnScanVEXScope struct {
	Kind         string `json:"kind"`
	RepositoryID string `json:"repository_id,omitempty"`
}

type vulnScanVEXStatementPolicy struct {
	AffectedStatuses           []string `json:"affected_statuses"`
	NotAffectedStatuses        []string `json:"not_affected_statuses"`
	UnderInvestigationStatuses []string `json:"under_investigation_statuses"`
	NonStatementReadiness      []string `json:"non_statement_readiness"`
}

type vulnScanVEXStatement struct {
	StatementID     string                        `json:"statement_id"`
	FindingID       string                        `json:"finding_id"`
	Status          string                        `json:"status"`
	Justification   string                        `json:"justification"`
	ImpactStatus    string                        `json:"impact_status"`
	Confidence      string                        `json:"confidence,omitempty"`
	Vulnerability   vulnScanVEXVulnerability      `json:"vulnerability"`
	Product         vulnScanVEXProduct            `json:"product"`
	SourceFreshness string                        `json:"source_freshness,omitempty"`
	MissingEvidence []string                      `json:"missing_evidence,omitempty"`
	EvidenceHandles []vulnScanEvidenceHandle      `json:"evidence_handles,omitempty"`
	Remediation     map[string]any                `json:"remediation,omitempty"`
	Affected        vulnScanReportAffectedContext `json:"affected"`
}

type vulnScanVEXVulnerability struct {
	CVEID      string `json:"cve_id,omitempty"`
	AdvisoryID string `json:"advisory_id,omitempty"`
}

type vulnScanVEXProduct struct {
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
	ImageRef      string `json:"image_ref,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	PackageName   string `json:"package_name,omitempty"`
	Ecosystem     string `json:"ecosystem,omitempty"`
	PURL          string `json:"purl,omitempty"`
}

func writeVulnScanVEX(w io.Writer, result vulnScanRepoResult, report vulnScanReport) error {
	document, err := buildVulnScanVEXDocument(result, report)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return fmt.Errorf("vuln-scan VEX export: encode: %w", err)
	}
	return nil
}

func buildVulnScanVEXDocument(
	result vulnScanRepoResult,
	report vulnScanReport,
) (vulnScanVEXDocument, error) {
	repositoryID := strings.TrimSpace(result.RepositoryID)
	if repositoryID == "" {
		repositoryID = strings.TrimSpace(report.RepositoryID)
	}
	if repositoryID == "" {
		return vulnScanVEXDocument{}, fmt.Errorf("vuln-scan VEX export requires a resolved repository id")
	}
	generatedAt, err := time.Parse(time.RFC3339Nano, report.GeneratedAt)
	if err != nil {
		return vulnScanVEXDocument{}, fmt.Errorf("parse report generated_at for VEX export: %w", err)
	}
	return vulnScanVEXDocument{
		SchemaVersion: vulnScanVEXSchemaVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339Nano),
		DocumentName:  "Eshu VEX-style vulnerability statements",
		Scope: vulnScanVEXScope{
			Kind:         "repository",
			RepositoryID: repositoryID,
		},
		Readiness:       report.Readiness,
		StatementPolicy: vulnScanVEXPolicy(),
		Statements:      vulnScanVEXStatements(report.Findings),
	}, nil
}

func vulnScanVEXPolicy() vulnScanVEXStatementPolicy {
	return vulnScanVEXStatementPolicy{
		AffectedStatuses:           []string{"affected_derived", "affected_exact"},
		NotAffectedStatuses:        []string{"not_affected_known_fixed"},
		UnderInvestigationStatuses: []string{"possibly_affected", "unknown_impact"},
		NonStatementReadiness: []string{
			"evidence_incomplete",
			"readiness_unavailable",
			"target_incomplete",
			"unsupported",
		},
	}
}

func vulnScanVEXStatements(findings []vulnScanReportFinding) []vulnScanVEXStatement {
	statements := make([]vulnScanVEXStatement, 0, len(findings))
	for _, finding := range findings {
		status, justification := vulnScanVEXStatus(finding.Affected.Status)
		if status == "" {
			continue
		}
		statement := vulnScanVEXStatement{
			StatementID:     vulnScanVEXStatementID(finding),
			FindingID:       finding.FindingID,
			Status:          status,
			Justification:   justification,
			ImpactStatus:    finding.Affected.Status,
			Confidence:      finding.Affected.Confidence,
			Vulnerability:   vulnScanVEXVulnerability{CVEID: finding.CVEID, AdvisoryID: finding.AdvisoryID},
			Product:         vulnScanVEXProductFromFinding(finding),
			SourceFreshness: finding.SourceFreshness,
			MissingEvidence: cloneAndSortStrings(finding.MissingEvidence),
			EvidenceHandles: sortedEvidenceHandles(finding.EvidenceHandles),
			Remediation:     remediationForVEX(finding),
			Affected:        finding.Affected,
		}
		statements = append(statements, statement)
	}
	sort.SliceStable(statements, func(i, j int) bool {
		return statements[i].StatementID < statements[j].StatementID
	})
	return statements
}

func vulnScanVEXStatus(impactStatus string) (string, string) {
	switch strings.TrimSpace(impactStatus) {
	case "affected_exact", "affected_derived":
		return "affected", "reducer_evidence_supports_affected"
	case "not_affected_known_fixed":
		return "not_affected", "fixed_version_observed"
	case "possibly_affected", "unknown_impact":
		return "under_investigation", "evidence_incomplete"
	default:
		return "", ""
	}
}

func vulnScanVEXStatementID(finding vulnScanReportFinding) string {
	if finding.FindingID != "" {
		return "eshu-vex-" + finding.FindingID
	}
	for _, value := range []string{finding.AdvisoryID, finding.CVEID, finding.Package.PackageID} {
		if strings.TrimSpace(value) != "" {
			return "eshu-vex-" + strings.TrimSpace(value)
		}
	}
	return "eshu-vex-unknown"
}

func vulnScanVEXProductFromFinding(finding vulnScanReportFinding) vulnScanVEXProduct {
	return vulnScanVEXProduct{
		RepositoryID:  finding.Target.RepositoryID,
		SubjectDigest: finding.Target.SubjectDigest,
		ImageRef:      finding.Target.ImageRef,
		PackageID:     finding.Package.PackageID,
		PackageName:   finding.Package.PackageName,
		Ecosystem:     finding.Package.Ecosystem,
		PURL:          finding.Package.PURL,
	}
}

func sortedEvidenceHandles(handles []vulnScanEvidenceHandle) []vulnScanEvidenceHandle {
	if len(handles) == 0 {
		return nil
	}
	out := make([]vulnScanEvidenceHandle, 0, len(handles))
	for _, handle := range handles {
		if strings.TrimSpace(handle.ID) == "" && strings.TrimSpace(handle.Kind) == "" {
			continue
		}
		out = append(out, handle)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func remediationForVEX(finding vulnScanReportFinding) map[string]any {
	remediation := finding.Remediation
	if len(remediation) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{
		"ecosystem",
		"current_version",
		"vulnerable_range",
		"fixed_version_source",
		"match_reason",
		"first_patched_version",
		"fixed_version",
		"manifest_range",
		"manifest_allows_fix",
		"parent_package",
		"confidence",
		"reason",
	} {
		if value, ok := remediation[key].(string); ok && strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	if direct, ok := remediation["direct"].(bool); ok {
		out["direct"] = direct
	}
	if missing := stringSliceFromAny(remediation["missing_evidence"]); len(missing) > 0 {
		out["missing_evidence"] = cloneAndSortStrings(missing)
	}
	if branches := mapSliceFromAny(remediation["patched_version_branches"]); len(branches) > 0 {
		out["patched_version_branches"] = branches
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
