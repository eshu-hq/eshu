// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scorecard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// TestPublicConformanceHarnessRunsOutOfTree proves the public collector
// conformance harness validates this out-of-tree package using only the public
// SDK module — no Eshu core internal packages and no eshu binary. It is the
// reference example for an external collector running conformance in its own CI.
func TestPublicConformanceHarnessRunsOutOfTree(t *testing.T) {
	t.Parallel()

	manifest := loadConformanceManifest(t)
	report := loadConformanceReport(t, "complete.json")
	result, err := Collect(testClaim(), report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://api.securityscorecards.dev/projects/github.com/example/widgets",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	conformanceReport := conformance.Run(conformance.Request{
		Manifest: manifest,
		Fixtures: []sdk.Result{result},
		Mode:     conformance.ModeFixture,
	})
	if !conformanceReport.OK() {
		t.Fatalf("conformance report = %#v, want passed", conformanceReport)
	}
	if got, want := conformanceReport.Summary.FixtureCount, 1; got != want {
		t.Fatalf("FixtureCount = %d, want %d", got, want)
	}
	if got, want := conformanceReport.Summary.FactCount, 4; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	if !conformanceReport.Summary.IdempotentReemissionChecked {
		t.Fatal("IdempotentReemissionChecked = false, want true")
	}

	raw, err := json.Marshal(conformanceReport)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v, want nil", err)
	}
	for _, want := range []string{
		`"schema_version":"eshu.extension.conformance.v1"`,
		`"status":"passed"`,
		`"component_id":"` + ComponentID + `"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("conformance report JSON missing %q: %s", want, raw)
		}
	}
}

// TestPublicConformanceHarnessFailsOnUnversionedFactKind proves the out-of-tree
// harness fails closed when a fact family drops its schema version, satisfying
// the conformance contract that unversioned fact kinds are rejected.
func TestPublicConformanceHarnessFailsOnUnversionedFactKind(t *testing.T) {
	t.Parallel()

	manifest := loadConformanceManifest(t)
	manifest.Spec.EmittedFacts[0].SchemaVersions = nil

	report := conformance.Run(conformance.Request{
		Manifest: manifest,
		Fixtures: []sdk.Result{mustCollectComplete(t)},
		Mode:     conformance.ModeFixture,
	})
	if report.OK() {
		t.Fatal("conformance report OK = true, want failed for unversioned fact kind")
	}
	if !hasFinding(report, conformance.FindingManifestInvalid) {
		t.Fatalf("findings = %#v, want %q", report.Findings, conformance.FindingManifestInvalid)
	}
}

func mustCollectComplete(t *testing.T) sdk.Result {
	t.Helper()

	result, err := Collect(testClaim(), loadConformanceReport(t, "complete.json"), CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://api.securityscorecards.dev/projects/github.com/example/widgets",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return result
}

func loadConformanceManifest(t *testing.T) conformance.Manifest {
	t.Helper()

	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.yaml) error = %v, want nil", err)
	}
	var manifest conformance.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal(manifest.yaml) error = %v, want nil", err)
	}
	return manifest
}

func loadConformanceReport(t *testing.T, name string) Report {
	t.Helper()

	return readReportFixture(t, name)
}

func hasFinding(report conformance.Report, code conformance.FindingCode) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
