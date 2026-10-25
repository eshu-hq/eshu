// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/sbt"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// parsedSBTDependencyFacts runs the real sbt parser over source and wraps each
// row the way the git collector does (name -> entity_name, the rest ->
// entity_metadata), so the join is tested against the parser's actual output
// and not a hand-built copy of its field names.
func parsedSBTDependencyFacts(t *testing.T, source string) []facts.Envelope {
	t.Helper()
	path := filepath.Join(t.TempDir(), "build.sbt")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write build.sbt: %v", err)
	}
	payload, err := sbt.Parse(filepath.Dir(path), path, false, shared.Options{})
	if err != nil {
		t.Fatalf("sbt.Parse(): %v", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	envelopes := make([]facts.Envelope, 0, len(rows))
	for _, row := range rows {
		metadata := make(map[string]any, len(row))
		for key, value := range row {
			metadata[key] = value
		}
		name, _ := row["name"].(string)
		delete(metadata, "name")
		value, _ := row["value"].(string)
		envelope := packageManifestDependencyFactWithMetadata(
			sbtRepoID, "scala-app", "build.sbt", name, "sbt", value, sbtObservedAt, metadata,
		)
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func TestParsedSBTRowsJoinTheSuffixedArtifactPerScalaVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scalaVersion string
		registryName string
		wantJoin     bool
	}{
		{"Scala 2.13", `scalaVersion := "2.13.12"`, "cats-core_2.13", true},
		{"Scala 3", `scalaVersion := "3.3.1"`, "cats-core_3", true},
		{"pre-release", `scalaVersion := "3.0.0-RC1"`, "cats-core_3", false},
		{"no scalaVersion", ``, "cats-core_2.13", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			envelopes := parsedSBTDependencyFacts(t, test.scalaVersion+"\n"+`libraryDependencies += "org.typelevel" %% "cats-core" % "2.10.0"`+"\n")
			registry := mavenRegistryFact("org.typelevel", test.registryName)
			got := sbtConsumedPackageIDs(append([]facts.Envelope{
				mavenRegistryFact("org.typelevel", "cats-core"),
				registry,
			}, envelopes...)...)
			var want []string
			if test.wantJoin {
				want = []string{registry.Payload["package_id"].(string)}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("consumed packages = %v, want %v", got, want)
			}
		})
	}
}

func TestParsedSBTPercentAndPlatformRows(t *testing.T) {
	t.Parallel()

	envelopes := parsedSBTDependencyFacts(t, "scalaVersion := \"2.13.12\"\n"+
		`libraryDependencies += "org.slf4j" % "slf4j-api" % "2.0.9"`+"\n"+
		`libraryDependencies += "org.scala-js" %%% "scalajs-dom" % "2.8.0"`+"\n"+
		`libraryDependencies += "com.lihaoyi" % "ammonite" % "3.0.0" cross CrossVersion.full`+"\n")
	slf4j := mavenRegistryFact("org.slf4j", "slf4j-api")
	got := sbtConsumedPackageIDs(append([]facts.Envelope{
		slf4j,
		mavenRegistryFact("org.scala-js", "scalajs-dom"),
		mavenRegistryFact("org.scala-js", "scalajs-dom_2.13"),
		mavenRegistryFact("com.lihaoyi", "ammonite"),
		mavenRegistryFact("com.lihaoyi", "ammonite_2.13.12"),
	}, envelopes...)...)
	want := []string{slf4j.Payload["package_id"].(string)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("consumed packages = %v, want only the plain percent row %v", got, want)
	}
}
