// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	alertMultiVersionRepo     = "repository:r_11121"
	alertMultiVersionPackage  = "npm://registry.npmjs.org/fast-uri"
	alertMultiVersionCVE      = "CVE-2026-11121"
	alertMultiVersionAffected = "3.0.3"
	alertMultiVersionFixed    = "3.1.0"
)

var alertMultiVersionObservedAt = time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC)

// alertMultiVersionEnvelopes builds an open provider alert on fast-uri
// "< 3.1.0" and one package-lock.json holding an affected and a fixed row.
// The alert path breaks a tie between rows of one lockfile by the lower fact
// id, so affectedID and fixedID decide which row it used to pick.
func alertMultiVersionEnvelopes(affectedID, fixedID string) []facts.Envelope {
	alert := securityAlertEnvelope("alert-11121", "security-alert:github:acme/api", map[string]any{
		"provider": "github_dependabot", "provider_alert_number": int64(11121), "provider_state": "open",
		"package_id": alertMultiVersionPackage, "ecosystem": "npm", "package_name": "fast-uri",
		"manifest_path": "package-lock.json", "ghsa_ids": []string{"GHSA-1112-1abc-defg"},
		"cve_ids": []string{alertMultiVersionCVE}, "vulnerable_range": "< 3.1.0", "patched_version": "3.1.0",
	})
	repository := packageSourceRepositoryFact(alertMultiVersionRepo, "api", "https://github.com/acme/api", false, alertMultiVersionObservedAt)
	row := func(id, version string) facts.Envelope {
		envelope := packageManifestDependencyFactWithMetadata(alertMultiVersionRepo, "", "package-lock.json", "fast-uri", "npm", version,
			alertMultiVersionObservedAt.Add(time.Minute), map[string]any{"section": "package-lock", "lockfile": true, "dependency_scope": "runtime"})
		envelope.FactID, envelope.StableFactKey = id, id
		return envelope
	}
	return []facts.Envelope{alert, repository, row(affectedID, alertMultiVersionAffected), row(fixedID, alertMultiVersionFixed)}
}

type alertMultiVersionKey struct {
	repositoryID string
	version      string
	status       SupplyChainImpactStatus
}

func alertMultiVersionKeys(findings []SupplyChainImpactFinding) []alertMultiVersionKey {
	var keys []alertMultiVersionKey
	for _, finding := range findings {
		if finding.CVEID != alertMultiVersionCVE {
			continue
		}
		keys = append(keys, alertMultiVersionKey{repositoryID: finding.RepositoryID, version: finding.ObservedVersion, status: finding.Status})
	}
	slices.SortFunc(keys, func(a, b alertMultiVersionKey) int { return cmp.Compare(a.version, b.version) })
	return keys
}

// TestSecurityAlertFindingsClassifyEachInstalledVersion11121 pins issue #11121:
// an open alert whose lockfile holds an affected and a fixed version used to
// yield one finding judged on the row with the lower fact id, so the fixed row
// could hide the affected one. Each installed version now classifies once, in
// either fact-id order.
func TestSecurityAlertFindingsClassifyEachInstalledVersion11121(t *testing.T) {
	t.Parallel()
	want := []alertMultiVersionKey{
		{repositoryID: alertMultiVersionRepo, version: alertMultiVersionAffected, status: SupplyChainImpactAffectedExact},
		{repositoryID: alertMultiVersionRepo, version: alertMultiVersionFixed, status: SupplyChainImpactNotAffectedKnownFixed},
	}
	for _, tc := range []struct{ name, affectedID, fixedID string }{
		{name: "fixed row has the lower fact id", affectedID: "manifest-dep:11121:z-affected", fixedID: "manifest-dep:11121:a-fixed"},
		{name: "affected row has the lower fact id", affectedID: "manifest-dep:11121:a-affected", fixedID: "manifest-dep:11121:z-fixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := alertMultiVersionKeys(BuildSupplyChainImpactFindings(alertMultiVersionEnvelopes(tc.affectedID, tc.fixedID)))
			if !slices.Equal(got, want) {
				t.Fatalf("findings = %+v, want %+v: each installed version must classify once", got, want)
			}
		})
	}
}

// TestSecurityAlertExistingFindingSuppressesOnlyItsVersion11121: a finding
// that already exists for one version of the package (from the advisory path)
// keeps the alert from duplicating that version, and only that version. The
// alert still reports the other installed version.
func TestSecurityAlertExistingFindingSuppressesOnlyItsVersion11121(t *testing.T) {
	t.Parallel()
	envelopes := alertMultiVersionEnvelopes("manifest-dep:11121:z-affected", "manifest-dep:11121:a-fixed")
	index, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	existing := SupplyChainImpactFinding{
		CVEID: alertMultiVersionCVE, AdvisoryID: alertMultiVersionCVE, PackageID: alertMultiVersionPackage,
		RepositoryID: alertMultiVersionRepo, ObservedVersion: alertMultiVersionFixed, Status: SupplyChainImpactNotAffectedKnownFixed,
	}
	findings, _, err := appendSecurityAlertImpactFindings([]SupplyChainImpactFinding{existing}, envelopes, index)
	if err != nil {
		t.Fatalf("append alert findings: %v", err)
	}
	added := alertMultiVersionKeys(findings[1:])
	want := []alertMultiVersionKey{
		{repositoryID: alertMultiVersionRepo, version: alertMultiVersionAffected, status: SupplyChainImpactAffectedExact},
	}
	if !slices.Equal(added, want) {
		t.Fatalf("alert findings added = %+v, want %+v: an existing finding for %s must not suppress %s", added, want, alertMultiVersionFixed, alertMultiVersionAffected)
	}
}

// TestSecurityAlertVersionlessRowKeepsOldSuppression11121: an alert row that
// proves no version keeps the pre-#11121 rule, so any existing finding for the
// advisory suppresses it and no blank-version duplicate appears.
func TestSecurityAlertVersionlessRowKeepsOldSuppression11121(t *testing.T) {
	t.Parallel()
	envelopes := alertMultiVersionEnvelopes("manifest-dep:11121:z-affected", "manifest-dep:11121:a-fixed")
	envelopes = envelopes[:2]
	rangeRow := packageManifestDependencyFactWithMetadata(alertMultiVersionRepo, "", "package-lock.json", "fast-uri", "npm", "^3.0.0",
		alertMultiVersionObservedAt.Add(time.Minute), map[string]any{"dependency_scope": "runtime"})
	envelopes = append(envelopes, rangeRow)
	index, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	existing := SupplyChainImpactFinding{
		CVEID: alertMultiVersionCVE, AdvisoryID: alertMultiVersionCVE, PackageID: alertMultiVersionPackage,
		RepositoryID: alertMultiVersionRepo, ObservedVersion: alertMultiVersionFixed,
	}
	findings, _, err := appendSecurityAlertImpactFindings([]SupplyChainImpactFinding{existing}, envelopes, index)
	if err != nil {
		t.Fatalf("append alert findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want only the existing finding: a versionless alert row is suppressed by any finding for the advisory", alertMultiVersionKeys(findings))
	}
}

// TestSecurityAlertMultiVersionCountsInSubSignal11121: the pass's
// multi_version_repository_groups count covers alert-path findings too.
func TestSecurityAlertMultiVersionCountsInSubSignal11121(t *testing.T) {
	t.Parallel()
	_, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(alertMultiVersionEnvelopes("manifest-dep:11121:z-affected", "manifest-dep:11121:a-fixed"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if stats.multiVersionRepositoryGroups != 1 {
		t.Fatalf("multiVersionRepositoryGroups = %d, want 1", stats.multiVersionRepositoryGroups)
	}
}

// TestSecurityAlertAmbiguousRepositoriesWithDifferentVersions11121 pins review
// F2 of #11121: an alert whose repository scope matches two repositories is
// ambiguous and yields no finding, even when the two repositories hold
// different versions. The per-version split must not turn one ambiguous
// candidate set into one unambiguous group per repository.
func TestSecurityAlertAmbiguousRepositoriesWithDifferentVersions11121(t *testing.T) {
	t.Parallel()
	packageID := alertMultiVersionPackage
	alert := securityAlertEnvelope("alert-11121-ambiguous", "security-alert:github:acme/api", map[string]any{
		"provider": "github_dependabot", "provider_alert_number": int64(11123), "provider_state": "open",
		"package_id": packageID, "ecosystem": "npm", "package_name": "fast-uri",
		"manifest_path": "packages/client/package-lock.json", "ghsa_ids": []string{"GHSA-1112-1abc-defg"},
		"vulnerable_range": "< 3.1.0", "patched_version": "3.1.0",
	})
	row := func(id, repo, version string) facts.Envelope {
		envelope := packageConsumptionCorrelationEnvelope(id, repo, packageID, "packages/client/package-lock.json")
		envelope.Payload["repository_name"] = "api"
		envelope.Payload["dependency_range"] = version
		return envelope
	}
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		alert,
		row("consume-11121-ambiguous-1", "repository:r_api_1", alertMultiVersionAffected),
		row("consume-11121-ambiguous-2", "repository:r_api_2", alertMultiVersionFixed),
	})
	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want none for an ambiguous repository scope", len(findings))
	}
}

// TestSecurityAlertInstalledVersionRowIsVersioned11121 pins review F4 of
// #11121: an alert row whose only version evidence is installed_version must
// read that version on the impact path, as reconciliation and the manifest
// path already do (exactConsumptionDependencyVersion reads InstalledVersion
// first). Otherwise the impact path judges the row range-only while
// reconciliation reports 3.0.3.
func TestSecurityAlertInstalledVersionRowIsVersioned11121(t *testing.T) {
	t.Parallel()
	alert := securityAlertEnvelope("alert-11121-installed", "security-alert:github:acme/api", map[string]any{
		"provider": "github_dependabot", "provider_alert_number": int64(11124), "provider_state": "open",
		"package_id": alertMultiVersionPackage, "ecosystem": "npm", "package_name": "fast-uri",
		"manifest_path": "package-lock.json", "cve_ids": []string{alertMultiVersionCVE},
		"vulnerable_range": "< 3.1.0", "patched_version": "3.1.0",
	})
	row := packageConsumptionCorrelationEnvelope("consume-11121-installed", alertMultiVersionRepo, alertMultiVersionPackage, "package-lock.json")
	row.Payload["dependency_range"] = "^3.0.0"
	row.Payload["installed_version"] = alertMultiVersionAffected
	row.Payload["repository_name"] = "api"
	got := alertMultiVersionKeys(BuildSupplyChainImpactFindings([]facts.Envelope{alert, row}))
	want := []alertMultiVersionKey{
		{repositoryID: alertMultiVersionRepo, version: alertMultiVersionAffected, status: SupplyChainImpactAffectedExact},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("findings = %+v, want %+v: installed_version is the row's version", got, want)
	}
}
