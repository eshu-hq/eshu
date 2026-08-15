// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnscan

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// VEXSchemaVersion identifies the VEX-style statement document this family
// writes. It is Eshu's own shape, not the CycloneDX or OpenVEX wire format.
const VEXSchemaVersion = "eshu.vex_statements.v1"

// VEXDocument is the whole `--export vex` output: the scope it covers, the
// readiness that backs it, the policy that decided which findings became
// statements, and the statements themselves.
type VEXDocument struct {
	SchemaVersion   string             `json:"schema_version"`
	GeneratedAt     string             `json:"generated_at"`
	DocumentName    string             `json:"document_name"`
	Scope           VEXScope           `json:"scope"`
	Readiness       ReportReadiness    `json:"readiness"`
	StatementPolicy VEXStatementPolicy `json:"statement_policy"`
	Statements      []VEXStatement     `json:"statements"`
}

// VEXScope names what the statements are about. Only repository scope exists
// today.
type VEXScope struct {
	Kind         string `json:"kind"`
	RepositoryID string `json:"repository_id,omitempty"`
}

// VEXStatementPolicy publishes the impact-status-to-VEX-status mapping the
// document was built with, so a consumer can audit the translation instead of
// inferring it. NonStatementReadiness names the readiness states under which
// an absent statement means "not established", never "not affected".
type VEXStatementPolicy struct {
	AffectedStatuses           []string `json:"affected_statuses"`
	NotAffectedStatuses        []string `json:"not_affected_statuses"`
	UnderInvestigationStatuses []string `json:"under_investigation_statuses"`
	NonStatementReadiness      []string `json:"non_statement_readiness"`
}

// VEXStatement is one finding's VEX-style verdict, carrying the impact status
// and evidence it was derived from alongside the translated status.
type VEXStatement struct {
	StatementID     string                `json:"statement_id"`
	FindingID       string                `json:"finding_id"`
	Status          string                `json:"status"`
	Justification   string                `json:"justification"`
	ImpactStatus    string                `json:"impact_status"`
	Confidence      string                `json:"confidence,omitempty"`
	Vulnerability   VEXVulnerability      `json:"vulnerability"`
	Product         VEXProduct            `json:"product"`
	SourceFreshness string                `json:"source_freshness,omitempty"`
	MissingEvidence []string              `json:"missing_evidence,omitempty"`
	EvidenceHandles []EvidenceHandle      `json:"evidence_handles,omitempty"`
	Remediation     map[string]any        `json:"remediation,omitempty"`
	Affected        ReportAffectedContext `json:"affected"`
}

// VEXVulnerability identifies the advisory a statement is about.
type VEXVulnerability struct {
	CVEID      string `json:"cve_id,omitempty"`
	AdvisoryID string `json:"advisory_id,omitempty"`
}

// VEXProduct identifies the product the statement applies to, at whichever
// granularity the finding carried: repository, image subject, or package.
type VEXProduct struct {
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
	ImageRef      string `json:"image_ref,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	PackageName   string `json:"package_name,omitempty"`
	Ecosystem     string `json:"ecosystem,omitempty"`
	PURL          string `json:"purl,omitempty"`
}

// WriteVEX renders result and report as a VEX-style statement document on w,
// indented for review. Like the SARIF writer it refuses rather than emitting a
// scopeless document.
func WriteVEX(w io.Writer, result Result, report Report) error {
	document, err := BuildVEXDocument(result, report)
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

// BuildVEXDocument assembles the document without writing it. It is exported
// so a test can assert the structure instead of parsing rendered JSON.
func BuildVEXDocument(result Result, report Report) (VEXDocument, error) {
	repositoryID := strings.TrimSpace(result.RepositoryID)
	if repositoryID == "" {
		repositoryID = strings.TrimSpace(report.RepositoryID)
	}
	if repositoryID == "" {
		return VEXDocument{}, fmt.Errorf("vuln-scan VEX export requires a resolved repository id")
	}
	generatedAt, err := time.Parse(time.RFC3339Nano, report.GeneratedAt)
	if err != nil {
		return VEXDocument{}, fmt.Errorf("parse report generated_at for VEX export: %w", err)
	}
	return VEXDocument{
		SchemaVersion: VEXSchemaVersion,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339Nano),
		DocumentName:  "Eshu VEX-style vulnerability statements",
		Scope: VEXScope{
			Kind:         "repository",
			RepositoryID: repositoryID,
		},
		Readiness:       report.Readiness,
		StatementPolicy: vexPolicy(),
		Statements:      vexStatements(report.Findings),
	}, nil
}

// vexPolicy is the published translation table. It is a function rather than a
// package variable so no caller can mutate the slices the document embeds.
func vexPolicy() VEXStatementPolicy {
	return VEXStatementPolicy{
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

// vexStatements translates the report's findings, skipping any impact status
// with no defined VEX meaning rather than guessing one. Output is sorted by
// statement id so two runs over the same findings produce identical bytes.
func vexStatements(findings []ReportFinding) []VEXStatement {
	statements := make([]VEXStatement, 0, len(findings))
	for _, finding := range findings {
		status, justification := vexStatus(finding.Affected.Status)
		if status == "" {
			continue
		}
		statement := VEXStatement{
			StatementID:     vexStatementID(finding),
			FindingID:       finding.FindingID,
			Status:          status,
			Justification:   justification,
			ImpactStatus:    finding.Affected.Status,
			Confidence:      finding.Affected.Confidence,
			Vulnerability:   VEXVulnerability{CVEID: finding.CVEID, AdvisoryID: finding.AdvisoryID},
			Product:         vexProductFromFinding(finding),
			SourceFreshness: finding.SourceFreshness,
			MissingEvidence: cloneAndSortStrings(finding.MissingEvidence),
			EvidenceHandles: sortedEvidenceHandles(finding.EvidenceHandles),
			Remediation:     RemediationForVEX(finding),
			Affected:        finding.Affected,
		}
		statements = append(statements, statement)
	}
	sort.SliceStable(statements, func(i, j int) bool {
		return statements[i].StatementID < statements[j].StatementID
	})
	return statements
}

// vexStatus maps a reducer impact status onto a VEX status and justification.
// An unrecognized status returns empty strings, which the caller reads as
// "no statement" — deliberately, since inventing `not_affected` from evidence
// the reducer did not give would be the worst possible wrong answer here.
func vexStatus(impactStatus string) (string, string) {
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

// vexStatementID derives a stable id, falling back through advisory, CVE, and
// package identity when the finding has no id of its own.
func vexStatementID(finding ReportFinding) string {
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

// vexProductFromFinding copies the identity fields a consumer matches a
// statement against.
func vexProductFromFinding(finding ReportFinding) VEXProduct {
	return VEXProduct{
		RepositoryID:  finding.Target.RepositoryID,
		SubjectDigest: finding.Target.SubjectDigest,
		ImageRef:      finding.Target.ImageRef,
		PackageID:     finding.Package.PackageID,
		PackageName:   finding.Package.PackageName,
		Ecosystem:     finding.Package.Ecosystem,
		PURL:          finding.Package.PURL,
	}
}

// sortedEvidenceHandles drops fully blank handles and orders the rest by kind
// then id, so the document is byte-stable.
func sortedEvidenceHandles(handles []EvidenceHandle) []EvidenceHandle {
	if len(handles) == 0 {
		return nil
	}
	out := make([]EvidenceHandle, 0, len(handles))
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

// RemediationForVEX re-reads the report finding's remediation map for the VEX
// document. It differs from RemediationFromFinding in two ways that matter:
// it reads an already-built report finding rather than the raw API map, and it
// carries `fixed_version` through from the map instead of lifting it off the
// finding. It is exported because its own regression test asserts the reducer
// envelope survives the second pass.
func RemediationForVEX(finding ReportFinding) map[string]any {
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
