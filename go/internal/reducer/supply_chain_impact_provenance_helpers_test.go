// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// vulnerabilityCVEFactWithProvenance builds a CVE envelope including
// source-attributed severity and modification timestamp so reducer admission
// can preserve provenance.
func vulnerabilityCVEFactWithProvenance(
	factID string,
	cveID string,
	source string,
	advisoryID string,
	cvssScore float64,
	cvssVector string,
	severityLabel string,
	modifiedAt string,
) facts.Envelope {
	payload := map[string]any{
		"cve_id":         cveID,
		"advisory_id":    advisoryID,
		"source":         source,
		"cvss_score":     cvssScore,
		"cvss_vector":    cvssVector,
		"severity_label": severityLabel,
		"modified_at":    modifiedAt,
		"aliases":        []any{cveID, advisoryID},
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityCVEFactKind,
		Payload:  payload,
	}
}

func vulnerabilityCVEFactWithdrawn(
	factID string,
	cveID string,
	source string,
	advisoryID string,
	cvssScore float64,
	severityLabel string,
	modifiedAt string,
	withdrawnAt string,
) facts.Envelope {
	envelope := vulnerabilityCVEFactWithProvenance(factID, cveID, source, advisoryID, cvssScore, "", severityLabel, modifiedAt)
	envelope.Payload["withdrawn_at"] = withdrawnAt
	return envelope
}

func vulnerabilityAffectedPackageFactWithSource(
	factID string,
	cveID string,
	source string,
	advisoryID string,
	packageID string,
	ecosystem string,
	packageName string,
	affectedVersion string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":            cveID,
			"advisory_id":       advisoryID,
			"source":            source,
			"package_id":        packageID,
			"ecosystem":         ecosystem,
			"package_name":      packageName,
			"affected_versions": []any{affectedVersion},
			"fixed_versions":    []any{fixedVersion},
		},
	}
}

func vulnerabilityAffectedPackageMultiFixed(
	factID string,
	cveID string,
	source string,
	advisoryID string,
	packageID string,
	ecosystem string,
	packageName string,
	fixedVersions []string,
) facts.Envelope {
	fixed := make([]any, 0, len(fixedVersions))
	for _, value := range fixedVersions {
		fixed = append(fixed, value)
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"advisory_id":    advisoryID,
			"source":         source,
			"package_id":     packageID,
			"ecosystem":      ecosystem,
			"package_name":   packageName,
			"fixed_versions": fixed,
		},
	}
}

func findingHasAlternateSeverity(finding SupplyChainImpactFinding, source string, score float64) bool {
	for _, alt := range finding.AlternateSeverities {
		if alt.Source == source && alt.Score == score {
			return true
		}
	}
	return false
}

func findingHasAdvisorySource(finding SupplyChainImpactFinding, source string, advisoryID string, modifiedAt string) bool {
	for _, advisory := range finding.AdvisorySources {
		if advisory.Source != source {
			continue
		}
		if advisoryID != "" && advisory.AdvisoryID != advisoryID {
			continue
		}
		if modifiedAt != "" && advisory.SourceUpdatedAt != modifiedAt {
			continue
		}
		return true
	}
	return false
}

func findingHasFixedVersionBranch(finding SupplyChainImpactFinding, version string, source string) bool {
	for _, branch := range finding.FixedVersionBranches {
		if branch.Version == version && branch.Source == source {
			return true
		}
	}
	return false
}

func TestSupplyChainCVEGroupRepresentativeSelectsByPriorityAndSkipsWithdrawn(t *testing.T) {
	t.Parallel()

	nvd := supplyChainImpactCVE{factID: "nvd-cve", cveID: "CVE-2026-7777", source: "nvd", advisoryID: "CVE-2026-7777"}
	ghsa := supplyChainImpactCVE{factID: "ghsa-cve", cveID: "CVE-2026-7777", source: "osv", advisoryID: "GHSA-test"}
	withdrawn := supplyChainImpactCVE{factID: "ghsa-withdrawn", cveID: "CVE-2026-7777", source: "osv", advisoryID: "GHSA-withdrawn", withdrawnAt: "2026-05-22T08:00:00Z"}

	got := supplyChainCVEGroup{cveID: "CVE-2026-7777", observations: []supplyChainImpactCVE{nvd, withdrawn, ghsa}}.representative()
	if got.factID != "ghsa-cve" {
		t.Fatalf("representative.factID = %q, want ghsa-cve (highest-priority non-withdrawn observation)", got.factID)
	}

	onlyWithdrawn := supplyChainCVEGroup{cveID: "CVE-2026-7777", observations: []supplyChainImpactCVE{withdrawn}}.representative()
	if onlyWithdrawn.factID != "ghsa-withdrawn" {
		t.Fatalf("representative.factID = %q, want ghsa-withdrawn (must return withdrawn row when every observation is withdrawn)", onlyWithdrawn.factID)
	}

	empty := supplyChainCVEGroup{cveID: "CVE-2026-9999"}.representative()
	if empty.cveID != "CVE-2026-9999" || empty.factID != "" {
		t.Fatalf("representative for empty group = %#v, want stub with cveID only", empty)
	}
}

func TestPostgresSupplyChainImpactWriterSerializesProvenancePayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 16, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresSupplyChainImpactWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	_, err := writer.WriteSupplyChainImpactFindings(context.Background(), SupplyChainImpactWrite{
		IntentID:     "intent-provenance",
		ScopeID:      "vuln-intel://osv/npm/parse-server",
		GenerationID: "generation-provenance",
		SourceSystem: "vulnerability_intelligence",
		Cause:        "vulnerability evidence observed",
		Findings: []SupplyChainImpactFinding{
			{
				CVEID:              "CVE-2026-7777",
				PackageID:          "npm://registry.npmjs.org/parse-server",
				FixedVersion:       "8.6.77",
				FixedVersionSource: "ghsa",
				RangeSource:        "ghsa",
				CVSSScore:          9.8,
				SeveritySource:     "ghsa",
				SeverityVector:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
				SeverityLabel:      "CRITICAL",
				AlternateSeverities: []AlternateSeverity{
					{Source: "nvd", Score: 5.5, Vector: "CVSS:3.1/AV:N/AC:L/PR:L/UI:R/S:U/C:L/I:L/A:N", Label: "MEDIUM"},
				},
				FixedVersionBranches: []FixedVersionBranch{
					{Version: "8.6.77", Source: "ghsa"},
					{Version: "9.9.1-alpha.1", Source: "glad"},
				},
				AdvisorySources: []AdvisorySourceObservation{
					{Source: "ghsa", AdvisoryID: "GHSA-test-1", SourceUpdatedAt: "2026-05-20T12:00:00Z"},
					{Source: "nvd", AdvisoryID: "CVE-2026-7777", SourceUpdatedAt: "2026-05-18T09:00:00Z"},
					{Source: "glad", AdvisoryID: "GMS-2026-99", SourceUpdatedAt: "2026-05-24T08:00:00Z", WithdrawnAt: ""},
				},
				CanonicalWrites: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	rows := decodeBatchedVersionedFactCalls(t, db.execs)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("decoded rows = %d, want %d", got, want)
	}
	payload := unmarshalSupplyChainImpactPayload(t, rows[0].Payload)
	provenance, ok := payload["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing provenance block: %#v", payload)
	}
	if got, want := provenance["selected_severity_source"], "ghsa"; got != want {
		t.Fatalf("selected_severity_source = %#v, want %#v", got, want)
	}
	if got, want := provenance["selected_fixed_version_source"], "ghsa"; got != want {
		t.Fatalf("selected_fixed_version_source = %#v, want %#v", got, want)
	}
	alternates, ok := provenance["alternate_severities"].([]any)
	if !ok || len(alternates) != 1 {
		t.Fatalf("alternate_severities = %#v, want 1 entry", provenance["alternate_severities"])
	}
	branches, ok := provenance["fixed_version_branches"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("fixed_version_branches = %#v, want 2 entries", provenance["fixed_version_branches"])
	}
	advisories, ok := provenance["advisory_sources"].([]any)
	if !ok || len(advisories) != 3 {
		t.Fatalf("advisory_sources = %#v, want 3 entries", provenance["advisory_sources"])
	}
}
