// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
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

// sbtAffectedIndex builds an affected-package index entry in the shape the OSV
// collector emits for a group:artifact coordinate: the bare artifact id as the
// name, a slash purl, and a package ID whose path starts with the registry's
// maven2 segment (issue #9286). A fixture that names the package by its
// coordinate hides a join that only works on coordinate-shaped names.
func sbtAffectedIndex(name string) *supplyChainImpactIndex {
	group, artifact, _ := strings.Cut(name, ":")
	return &supplyChainImpactIndex{
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{
			"CVE-2026-0001": {{
				FactID:    "affected-" + name,
				CVEID:     "CVE-2026-0001",
				PackageID: "maven://repo1.maven.org/maven2/" + name,
				PURL:      "pkg:maven/" + group + "/" + artifact,
				Ecosystem: "maven",
				Name:      artifact,
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

// TestManifestDependencyKeySetIsSharedByBothSupplyChainPaths pins the one key
// function the consumption join and the parser-file repository scope use. The
// scope path admits only npm advisories while sbt keys are Maven, so the sbt
// case is equivalent to "no repository" today; this is a consistency pin, not
// a behavior change, and it fails if either path computes its own keys again.
func TestManifestDependencyKeySetIsSharedByBothSupplyChainPaths(t *testing.T) {
	t.Parallel()

	binary := correlationDependency(t, sbtManifestRow("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13"))
	if keys := manifestDependencyKeySet(binary); len(keys) == 0 {
		t.Fatal("binary row key set is empty, want the Maven artifact keys")
	} else {
		for key := range keys {
			if strings.HasSuffix(key, "cats-core") {
				t.Fatalf("binary row key set holds the unsuffixed key %q", key)
			}
		}
	}

	npmAdvisory := vulnerabilityAffectedPackageFact(
		"affected-npm", "CVE-2026-8961", "pkg:npm/cats-core", "npm", "org.typelevel:cats-core", "1.0.0", "2.0.0",
	)
	npmRow := packageManifestDependencyFactWithMetadata(
		testImpactRepositoryID, "api", "package-lock.json", "org.typelevel:cats-core", "npm", "1.0.0",
		time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC), map[string]any{"lockfile": true},
	)
	sbtRow := sbtManifestRow("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13")
	if got := supplyChainImpactParserFileRepositoryIDs([]facts.Envelope{npmAdvisory, npmRow}); len(got) != 1 || got[0] != testImpactRepositoryID {
		t.Fatalf("npm row repository scope = %v, want [%s]", got, testImpactRepositoryID)
	}
	if got := supplyChainImpactParserFileRepositoryIDs([]facts.Envelope{npmAdvisory, sbtRow}); len(got) != 0 {
		t.Fatalf("sbt row repository scope = %v, want none: its keys are Maven, not npm", got)
	}
}

func correlationDependency(t *testing.T, envelope facts.Envelope) correlation.PackageManifestDependency {
	t.Helper()
	dependencies := correlation.ExtractPackageManifestDependencies([]facts.Envelope{envelope})
	if len(dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(dependencies))
	}
	return dependencies[0]
}

// The sbt and Maven fixtures carry the OSV shape (bare artifact name), so the
// manifest read filter built from them must still request the stored
// group:artifact coordinate. Before issue #9286 it did not, and the fixtures'
// coordinate-shaped names hid that.
func TestSBTAffectedFixtureFilterRequestsStoredCoordinate(t *testing.T) {
	t.Parallel()

	for _, coordinate := range []string{"org.slf4j:slf4j-api", "org.typelevel:cats-core_2.13"} {
		t.Run(coordinate, func(t *testing.T) {
			t.Parallel()
			index := sbtAffectedIndex(coordinate)
			var names []string
			for _, pkg := range index.affectedPackages["CVE-2026-0001"] {
				if pkg.Name == coordinate {
					t.Fatalf("fixture Name = %q, want the bare artifact id as OSV emits", pkg.Name)
				}
				for _, name := range supplyChainAffectedPackageNameCandidates(pkg) {
					names = append(names, correlation.PackageConsumptionNameCandidates("maven", name)...)
				}
			}
			if !slices.Contains(names, coordinate) {
				t.Fatalf("requested names = %q, want to contain %q", names, coordinate)
			}
		})
	}
}
