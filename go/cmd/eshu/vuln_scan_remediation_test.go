// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

func TestRemediationFromFindingPreservesReducerEnvelopeTruth(t *testing.T) {
	t.Parallel()

	finding := map[string]any{
		"fixed_version": "3.9.9",
		"remediation": map[string]any{
			"ecosystem":                "maven",
			"current_version":          "3.9.8",
			"vulnerable_range":         "[3.8.0,3.9.9)",
			"first_patched_version":    "3.9.9",
			"fixed_version_source":     "ghsa",
			"match_reason":             "maven_range_match",
			"manifest_range":           "[3.8.0,4.0.0)",
			"manifest_allows_fix":      "allowed",
			"confidence":               "exact",
			"reason":                   "direct_upgrade_allowed",
			"patched_version_branches": []any{map[string]any{"version": "3.9.9", "source": "ghsa"}},
		},
	}

	remediation := remediationFromFinding(finding)
	if remediation["match_reason"] != "maven_range_match" {
		t.Fatalf("match_reason = %#v, want maven_range_match", remediation["match_reason"])
	}
	if remediation["fixed_version_source"] != "ghsa" {
		t.Fatalf("fixed_version_source = %#v, want ghsa", remediation["fixed_version_source"])
	}
	if remediation["fixed_version"] != "3.9.9" {
		t.Fatalf("fixed_version = %#v, want 3.9.9", remediation["fixed_version"])
	}
}

func TestVulnScanSARIFRemediationPreservesReducerEnvelopeTruth(t *testing.T) {
	t.Parallel()

	finding := remediationEnvelopeFinding()

	remediation := vulnScanSARIFRemediation(finding)
	if remediation == nil {
		t.Fatal("vulnScanSARIFRemediation() = nil, want remediation")
	}
	if remediation.MatchReason != "maven_range_match" {
		t.Fatalf("MatchReason = %q, want maven_range_match", remediation.MatchReason)
	}
	if remediation.FixedVersionSource != "ghsa" {
		t.Fatalf("FixedVersionSource = %q, want ghsa", remediation.FixedVersionSource)
	}
}

func TestRemediationForVEXPreservesReducerEnvelopeTruth(t *testing.T) {
	t.Parallel()

	finding := vulnScanReportFinding{
		Remediation: remediationFromFinding(remediationEnvelopeFinding()),
	}

	remediation := remediationForVEX(finding)
	if remediation["match_reason"] != "maven_range_match" {
		t.Fatalf("match_reason = %#v, want maven_range_match", remediation["match_reason"])
	}
	if remediation["fixed_version_source"] != "ghsa" {
		t.Fatalf("fixed_version_source = %#v, want ghsa", remediation["fixed_version_source"])
	}
}

func remediationEnvelopeFinding() map[string]any {
	return map[string]any{
		"fixed_version": "3.9.9",
		"remediation": map[string]any{
			"ecosystem":             "maven",
			"current_version":       "3.9.8",
			"vulnerable_range":      "[3.8.0,3.9.9)",
			"first_patched_version": "3.9.9",
			"fixed_version_source":  "ghsa",
			"match_reason":          "maven_range_match",
			"manifest_range":        "[3.8.0,4.0.0)",
			"manifest_allows_fix":   "allowed",
			"confidence":            "exact",
			"reason":                "direct_upgrade_allowed",
			"patched_version_branches": []any{
				map[string]any{"version": "3.9.9", "source": "ghsa"},
			},
		},
	}
}
