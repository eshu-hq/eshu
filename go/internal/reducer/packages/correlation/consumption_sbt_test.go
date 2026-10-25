// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

const sbtRepoID = "repo-scala"

var sbtObservedAt = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)

// sbtDependencyFact builds the content_entity row the sbt parser emits: the
// name is the unsuffixed organization:name, and the cross label and binary
// artifact ride in entity_metadata.
func sbtDependencyFact(name, cross, artifact string) facts.Envelope {
	metadata := map[string]any{
		"section":                     "libraryDependencies",
		"sbt_cross_version":           cross,
		"dependency_resolution_state": "resolved",
		"dependency_scope":            "compile",
		"direct_dependency":           true,
	}
	if artifact != "" {
		metadata["sbt_binary_artifact"] = artifact
	}
	envelope := packageManifestDependencyFactWithMetadata(
		sbtRepoID, "scala-app", "build.sbt", name, "sbt", "2.10.0", sbtObservedAt, metadata,
	)
	envelope.FactID = "sbt-dep:" + cross + ":" + name
	return envelope
}

func mavenRegistryFact(namespace, name string) facts.Envelope {
	return packageRegistryPackageFact("maven://repo/"+namespace+":"+name, "maven", name, namespace, sbtObservedAt)
}

func sbtConsumedPackageIDs(envelopes ...facts.Envelope) []string {
	var ids []string
	for _, decision := range BuildPackageConsumptionDecisions(envelopes) {
		ids = append(ids, decision.PackageID)
	}
	return ids
}

func TestSBTBinaryRowJoinsOnlyTheSuffixedMavenArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		artifact string
	}{
		{name: "Scala 2.13", artifact: "org.typelevel:cats-core_2.13"},
		{name: "Scala 3", artifact: "org.typelevel:cats-core_3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			suffixed := mavenRegistryFact("org.typelevel", test.artifact[len("org.typelevel:"):])
			got := sbtConsumedPackageIDs(
				mavenRegistryFact("org.typelevel", "cats-core"),
				suffixed,
				packageSourceRepositoryFact(sbtRepoID, "scala-app", "https://example.test/scala-app", false, sbtObservedAt),
				sbtDependencyFact("org.typelevel:cats-core", "binary", test.artifact),
			)
			want := []string{suffixed.Payload["package_id"].(string)}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("consumed packages = %v, want only the suffixed artifact %v", got, want)
			}
		})
	}
}

func TestSBTBinaryRowWithoutArtifactJoinsNothing(t *testing.T) {
	t.Parallel()

	got := sbtConsumedPackageIDs(
		mavenRegistryFact("org.typelevel", "cats-core"),
		mavenRegistryFact("org.typelevel", "cats-core_2.13"),
		sbtDependencyFact("org.typelevel:cats-core", "binary", ""),
	)
	if len(got) != 0 {
		t.Fatalf("a %%%% row with no known binary version joined %v, want nothing", got)
	}
}

func TestSBTUnsuffixedNameNeverMatchesMavenForBinaryRow(t *testing.T) {
	t.Parallel()

	// Only the unsuffixed coordinate exists in the registry: the row must not
	// reach it through its name even though the artifact is known.
	got := sbtConsumedPackageIDs(
		mavenRegistryFact("org.typelevel", "cats-core"),
		sbtDependencyFact("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13"),
	)
	if len(got) != 0 {
		t.Fatalf("a %%%% row joined the unsuffixed maven package %v", got)
	}
}

func TestSBTPercentRowJoinsTheExactMavenCoordinate(t *testing.T) {
	t.Parallel()

	registry := mavenRegistryFact("org.slf4j", "slf4j-api")
	got := sbtConsumedPackageIDs(
		registry,
		mavenRegistryFact("org.slf4j", "slf4j-api_2.13"),
		sbtDependencyFact("org.slf4j:slf4j-api", "none", ""),
	)
	want := []string{registry.Payload["package_id"].(string)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("consumed packages = %v, want %v", got, want)
	}
}

func TestSBTPlatformAndCustomCrossRowsJoinNothing(t *testing.T) {
	t.Parallel()

	for _, cross := range []string{"scalajs", "custom", ""} {
		got := sbtConsumedPackageIDs(
			mavenRegistryFact("org.scala-js", "scalajs-dom"),
			mavenRegistryFact("org.scala-js", "scalajs-dom_2.13"),
			sbtDependencyFact("org.scala-js:scalajs-dom", cross, "org.scala-js:scalajs-dom_2.13"),
		)
		if len(got) != 0 {
			t.Fatalf("cross %q joined %v, want nothing", cross, got)
		}
	}
}

func sbtAlert(packageID, name string) securityalert.ProviderSecurityAlert {
	return securityalert.ProviderSecurityAlert{
		SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
			RepositoryID: sbtRepoID,
			PackageID:    packageID,
			Ecosystem:    "maven",
			PackageName:  name,
		},
	}
}

func TestSBTSecurityAlertMatchUsesTheArtifactName(t *testing.T) {
	t.Parallel()

	binary := []facts.Envelope{sbtDependencyFact("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13")}
	percent := []facts.Envelope{sbtDependencyFact("org.typelevel:cats-core", "none", "")}
	suffixedAlert := sbtAlert("maven://repo/org.typelevel:cats-core_2.13", "org.typelevel:cats-core_2.13")
	unsuffixedAlert := sbtAlert("maven://repo/org.typelevel:cats-core", "org.typelevel:cats-core")

	tests := []struct {
		name      string
		alert     securityalert.ProviderSecurityAlert
		envelopes []facts.Envelope
		want      int
	}{
		{"suffixed alert matches the binary row", suffixedAlert, binary, 1},
		{"unsuffixed alert never matches the binary row", unsuffixedAlert, binary, 0},
		{"suffixed alert never matches the percent row", suffixedAlert, percent, 0},
		{"unsuffixed alert matches the percent row", unsuffixedAlert, percent, 1},
		{
			"binary row with no artifact matches neither alert", suffixedAlert,
			[]facts.Envelope{sbtDependencyFact("org.typelevel:cats-core", "binary", "")},
			0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractSecurityAlertManifestConsumptions(
				[]securityalert.ProviderSecurityAlert{test.alert}, test.envelopes,
			)
			if len(got) != test.want {
				t.Fatalf("alert consumptions = %d, want %d: %#v", len(got), test.want, got)
			}
		})
	}
}

func TestSBTDependencyNameMatchHonorsTheArtifact(t *testing.T) {
	t.Parallel()

	binary := ExtractPackageManifestDependencies(
		[]facts.Envelope{sbtDependencyFact("org.typelevel:cats-core", "binary", "org.typelevel:cats-core_2.13")},
	)
	if len(binary) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(binary))
	}
	if SecurityAlertPackageNameMatchesDependency(sbtAlert("", "org.typelevel:cats-core"), binary[0]) {
		t.Fatal("unsuffixed alert name matched a %% row")
	}
	if !SecurityAlertPackageNameMatchesDependency(sbtAlert("", "org.typelevel:cats-core_2.13"), binary[0]) {
		t.Fatal("suffixed alert name did not match the %% row artifact")
	}
}
