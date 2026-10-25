// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

// securityAlertFreshnessInput is the provider truth a freshness digest covers.
// fanoutOutcomes, set only after an org fan-out (#8805), maps each fan-out
// repository to its outcome so a changed per-repository outcome moves the
// digest; it is omitted otherwise. The alert snapshot also carries each alert's
// repository (#8805), so org-scope digests, fan-out or not, differ from those
// computed before that field existed: each org target takes one extra
// generation after an upgrade, then the digest is stable again. Repository-scope
// digests were unchanged by that field. targetSetHash (#8838) moves every
// target's digest, repository and org, once after its upgrade.
type securityAlertFreshnessInput struct {
	alerts         []securityalerts.GitHubDependabotAlert
	pagesFetched   int
	truncated      bool
	fanoutOutcomes map[string]string
	// orgReadState marks an org-scope claim that records its read state
	// (#8943). It moves the digest of every org target once, so a target whose
	// provider truth is unchanged still commits one generation that carries the
	// record instead of keeping an older generation without it.
	orgReadState bool
	// targetSetHash is the hash of the repository set the target collects
	// (#8838). An allowlist change, or a reused scope_id naming another
	// repository, leaves the provider answer unchanged when the added
	// repository has no alerts; the hash moves the digest so the ingestion
	// store commits a generation that records the new set instead of skipping
	// it as unchanged. It moves every target's digest once after the upgrade.
	targetSetHash string
}

type securityAlertFreshnessSnapshot struct {
	Repository             string                              `json:"repository,omitempty"`
	Number                 int                                 `json:"number"`
	State                  string                              `json:"state"`
	Ecosystem              string                              `json:"ecosystem"`
	PackageName            string                              `json:"package_name"`
	ManifestPath           string                              `json:"manifest_path"`
	DependencyScope        string                              `json:"dependency_scope"`
	Relationship           string                              `json:"relationship"`
	GHSAIDs                []string                            `json:"ghsa_ids"`
	CVEIDs                 []string                            `json:"cve_ids"`
	VulnerableVersionRange string                              `json:"vulnerable_version_range"`
	FirstPatchedVersion    string                              `json:"first_patched_version"`
	Severity               string                              `json:"severity"`
	CVSSVector             string                              `json:"cvss_vector"`
	CVSSScore              float64                             `json:"cvss_score"`
	EPSSPercentage         string                              `json:"epss_percentage"`
	EPSSPercentile         string                              `json:"epss_percentile"`
	CWEs                   []securityAlertFreshnessCWESnapshot `json:"cwes"`
	CreatedAt              string                              `json:"created_at"`
	UpdatedAt              string                              `json:"updated_at"`
	FixedAt                string                              `json:"fixed_at"`
	DismissedAt            string                              `json:"dismissed_at"`
}

type securityAlertFreshnessCWESnapshot struct {
	CWEID string `json:"cwe_id"`
	Name  string `json:"name"`
}

func securityAlertFreshnessHint(input securityAlertFreshnessInput) string {
	snapshots := make([]securityAlertFreshnessSnapshot, 0, len(input.alerts))
	for _, alert := range input.alerts {
		snapshots = append(snapshots, securityAlertSnapshot(alert))
	}
	slices.SortFunc(snapshots, func(a, b securityAlertFreshnessSnapshot) int {
		if cmp := strings.Compare(a.Repository, b.Repository); cmp != 0 {
			return cmp
		}
		if a.Number != b.Number {
			return cmp.Compare(a.Number, b.Number)
		}
		if cmp := strings.Compare(a.PackageName, b.PackageName); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ManifestPath, b.ManifestPath)
	})
	digest := map[string]any{
		"alerts":        snapshots,
		"pages_fetched": input.pagesFetched,
		"truncated":     input.truncated,
	}
	if len(input.fanoutOutcomes) > 0 {
		digest["org_fanout"] = input.fanoutOutcomes
	}
	if input.orgReadState {
		digest["org_read_state"] = 1
	}
	if input.targetSetHash != "" {
		digest["target_set"] = input.targetSetHash
	}
	raw, err := json.Marshal(digest)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func securityAlertSnapshot(alert securityalerts.GitHubDependabotAlert) securityAlertFreshnessSnapshot {
	pkg := alert.Dependency.Package
	if strings.TrimSpace(pkg.Name) == "" {
		pkg = alert.SecurityVulnerability.Package
	}
	return securityAlertFreshnessSnapshot{
		Repository:             normalizeOrganizationAlertRepositoryFullName(alert),
		Number:                 alert.Number,
		State:                  strings.TrimSpace(alert.State),
		Ecosystem:              strings.TrimSpace(pkg.Ecosystem),
		PackageName:            strings.TrimSpace(pkg.Name),
		ManifestPath:           strings.TrimSpace(alert.Dependency.ManifestPath),
		DependencyScope:        strings.TrimSpace(alert.Dependency.Scope),
		Relationship:           strings.TrimSpace(alert.Dependency.Relationship),
		GHSAIDs:                securityAlertIdentifierValues(alert.SecurityAdvisory, "GHSA"),
		CVEIDs:                 securityAlertIdentifierValues(alert.SecurityAdvisory, "CVE"),
		VulnerableVersionRange: strings.TrimSpace(alert.SecurityVulnerability.VulnerableVersionRange),
		FirstPatchedVersion:    strings.TrimSpace(alert.SecurityVulnerability.FirstPatchedVersion.Identifier),
		Severity:               strings.ToLower(strings.TrimSpace(alert.SecurityAdvisory.Severity)),
		CVSSVector:             strings.TrimSpace(alert.SecurityAdvisory.CVSS.Vector),
		CVSSScore:              alert.SecurityAdvisory.CVSS.Score,
		EPSSPercentage:         securityAlertAnyString(alert.SecurityAdvisory.EPSS.Percentage),
		EPSSPercentile:         securityAlertAnyString(alert.SecurityAdvisory.EPSS.Percentile),
		CWEs:                   securityAlertCWESnapshot(alert.SecurityAdvisory.CWEs),
		CreatedAt:              strings.TrimSpace(alert.CreatedAt),
		UpdatedAt:              strings.TrimSpace(alert.UpdatedAt),
		FixedAt:                strings.TrimSpace(alert.FixedAt),
		DismissedAt:            strings.TrimSpace(alert.DismissedAt),
	}
}

func securityAlertIdentifierValues(
	advisory securityalerts.GitHubDependabotSecurityAdvisory,
	identifierType string,
) []string {
	values := make([]string, 0, 1+len(advisory.Identifiers))
	switch strings.ToUpper(strings.TrimSpace(identifierType)) {
	case "GHSA":
		values = append(values, advisory.GHSAID)
	case "CVE":
		values = append(values, advisory.CVEID)
	}
	for _, identifier := range advisory.Identifiers {
		if strings.EqualFold(identifier.Type, identifierType) {
			values = append(values, identifier.Value)
		}
	}
	return cleanFreshnessStrings(values)
}

func securityAlertCWESnapshot(cwes []securityalerts.GitHubDependabotCWE) []securityAlertFreshnessCWESnapshot {
	snapshots := make([]securityAlertFreshnessCWESnapshot, 0, len(cwes))
	seen := make(map[string]struct{}, len(cwes))
	for _, cwe := range cwes {
		cweID := strings.TrimSpace(cwe.CWEID)
		if cweID == "" {
			continue
		}
		if _, ok := seen[cweID]; ok {
			continue
		}
		seen[cweID] = struct{}{}
		snapshots = append(snapshots, securityAlertFreshnessCWESnapshot{
			CWEID: cweID,
			Name:  strings.TrimSpace(cwe.Name),
		})
	}
	slices.SortFunc(snapshots, func(a, b securityAlertFreshnessCWESnapshot) int {
		return strings.Compare(a.CWEID, b.CWEID)
	})
	return snapshots
}

func cleanFreshnessStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	slices.Sort(cleaned)
	return cleaned
}

func securityAlertAnyString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
