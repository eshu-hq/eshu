// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func sbtManifestRow(name, cross, artifact string) facts.Envelope {
	metadata := map[string]any{
		"section":           "libraryDependencies",
		"sbt_cross_version": cross,
		"direct_dependency": true,
	}
	if artifact != "" {
		metadata["sbt_binary_artifact"] = artifact
	}
	return packageManifestDependencyFactWithMetadata(
		testImpactRepositoryID, "api", "build.sbt", name, "sbt", "2.10.0",
		time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), metadata,
	)
}

func sbtAffectedIndex(name string) *supplyChainImpactIndex {
	return &supplyChainImpactIndex{
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			"CVE-2026-0001": {{
				FactID:    "affected-" + name,
				CVEID:     "CVE-2026-0001",
				PackageID: "maven://repo1.maven.org/maven2/" + name,
				Ecosystem: "maven",
				Name:      name,
			}},
		},
		consumption: map[string][]supplychainmodel.PackageConsumption{},
	}
}

func TestManifestDependencySupplyChainConsumptionJoinsSBTArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		affected string
		row      facts.Envelope
		want     int
	}{
		{
			"suffixed advisory joins the binary row", "org.typelevel:cats-core_2.13",
			sbtManifestRow("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13"), 1,
		},
		{
			"unsuffixed advisory never joins the binary row", "org.typelevel:cats-core",
			sbtManifestRow("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13"), 0,
		},
		{
			"binary row without an artifact joins nothing", "org.typelevel:cats-core",
			sbtManifestRow("org.typelevel:cats-core", "binary", ""), 0,
		},
		{
			"percent row joins its exact coordinate", "org.slf4j:slf4j-api",
			sbtManifestRow("org.slf4j:slf4j-api", "none", ""), 1,
		},
		{
			"suffixed advisory never joins the percent row", "org.slf4j:slf4j-api_2.13",
			sbtManifestRow("org.slf4j:slf4j-api", "none", ""), 0,
		},
		{
			"platform row joins nothing", "org.scala-js:scalajs-dom_2.13",
			sbtManifestRow("org.scala-js:scalajs-dom", "scalajs", "org.scala-js:scalajs-dom_2.13"), 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			index := sbtAffectedIndex(test.affected)
			addManifestDependencySupplyChainConsumption(index, []facts.Envelope{test.row})
			got := 0
			for _, rows := range index.consumption {
				got += len(rows)
			}
			if got != test.want {
				t.Fatalf("consumption rows = %d, want %d: %#v", got, test.want, index.consumption)
			}
		})
	}
}

// An sbt dependency now joins an alert, but the join is identity evidence
// only: the sbt parser emits no package API packages, so a finding sourced from
// it never claims JVM package reachability, and one with no resolved version
// stays below exact with its gaps listed (issue #8961: the reachability tier
// for sbt does not move).
func TestSBTSourcedFindingNeverClaimsJVMReachability(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	canonicalRepoID := "repository:r_scala"
	alert := securityAlertEnvelope("alert-sbt", "security-alert:github:acme/scala", map[string]any{
		"provider":              "github_dependabot",
		"provider_alert_number": int64(7),
		"provider_state":        "open",
		"package_id":            "maven://repo1.maven.org/maven2/org.typelevel:cats-core_2.13",
		"ecosystem":             "maven",
		"package_name":          "org.typelevel:cats-core_2.13",
		"manifest_path":         "build.sbt",
		"ghsa_ids":              []string{"GHSA-sbt-0001"},
		"cve_ids":               []string{"CVE-2026-8961"},
		"vulnerable_range":      "< 2.12.0",
		"patched_version":       "2.12.0",
	})
	repository := packageSourceRepositoryFact(canonicalRepoID, "scala", "https://github.com/acme/scala", false, observedAt)
	for _, test := range []struct {
		name, version, state string
		wantExact            bool
	}{
		{"resolved literal version", "2.10.0", "resolved", true},
		{"unresolved version", "", "unresolved", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependency := packageManifestDependencyFactWithMetadata(
				canonicalRepoID, "", "build.sbt", "org.typelevel:cats-core", "sbt", test.version, observedAt.Add(time.Minute),
				map[string]any{
					"section":                     "libraryDependencies",
					"sbt_cross_version":           "binary",
					"sbt_binary_artifact":         "org.typelevel:cats-core_2.13",
					"dependency_resolution_state": test.state,
					"dependency_scope":            "compile",
				},
			)
			findings := BuildSupplyChainImpactFindings([]facts.Envelope{alert, repository, dependency})
			if len(findings) != 1 {
				t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
			}
			finding := findings[0]
			if finding.RuntimeReachability == jvmRuntimeReachabilityPackageAPIReachable {
				t.Fatalf("RuntimeReachability = %q, want sbt evidence below package-API reachable", finding.RuntimeReachability)
			}
			if gotExact := finding.Status == SupplyChainImpactAffectedExact; gotExact != test.wantExact {
				t.Fatalf("Status = %q, want exact=%v", finding.Status, test.wantExact)
			}
			if len(finding.MissingEvidence) == 0 {
				t.Fatalf("MissingEvidence is empty, want the open evidence gaps listed: %#v", finding)
			}
		})
	}
}
