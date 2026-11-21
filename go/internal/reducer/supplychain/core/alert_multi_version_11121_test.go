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
