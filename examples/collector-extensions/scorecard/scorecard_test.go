// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scorecard

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"gopkg.in/yaml.v3"
)

func TestCollectCompleteResultConformsToSDKContract(t *testing.T) {
	t.Parallel()

	report := readReportFixture(t, "complete.json")
	claim := testClaim()
	result, err := Collect(claim, report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://api.securityscorecards.dev/projects/github.com/example/widgets",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	validation, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want %q", result.State, sdk.ResultComplete)
	}
	if got, want := validation.FactCount, 4; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	assertFactKinds(t, result.Facts, []string{
		FactKindSnapshot,
		FactKindCheck,
		FactKindCheck,
		FactKindWarning,
	})
	assertNoCredentialLikePayloadKeys(t, result.Facts)
}

func TestCollectIsDeterministicAndDeduplicatesChecks(t *testing.T) {
	t.Parallel()

	report := readReportFixture(t, "duplicate-checks.json")
	claim := testClaim()
	options := CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://api.securityscorecards.dev/projects/github.com/example/widgets",
	}

	first, err := Collect(claim, report, options)
	if err != nil {
		t.Fatalf("first Collect() error = %v, want nil", err)
	}
	second, err := Collect(claim, report, options)
	if err != nil {
		t.Fatalf("second Collect() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Collect() is not deterministic\nfirst: %#v\nsecond: %#v", first, second)
	}
	checkFacts := 0
	for _, fact := range first.Facts {
		if fact.Kind == FactKindCheck {
			checkFacts++
		}
	}
	if got, want := checkFacts, 1; got != want {
		t.Fatalf("check fact count = %d, want %d", got, want)
	}
}

func TestCollectReturnsUnchangedWhenDigestMatches(t *testing.T) {
	t.Parallel()

	report := readReportFixture(t, "complete.json")
	digest, err := Digest(report)
	if err != nil {
		t.Fatalf("Digest() error = %v, want nil", err)
	}
	result, err := Collect(testClaim(), report, CollectOptions{
		ObservedAt:     testObservedAt(),
		SourceURI:      "https://api.securityscorecards.dev/projects/github.com/example/widgets",
		PreviousDigest: digest,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	validation, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if result.State != sdk.ResultUnchanged {
		t.Fatalf("State = %q, want %q", result.State, sdk.ResultUnchanged)
	}
	if validation.FactCount != 0 {
		t.Fatalf("FactCount = %d, want 0", validation.FactCount)
	}
}

func TestCollectReturnsPartialForEmptyCheckSet(t *testing.T) {
	t.Parallel()

	report := readReportFixture(t, "empty-checks.json")
	result, err := Collect(testClaim(), report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://api.securityscorecards.dev/projects/github.com/example/widgets",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	validation, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if result.State != sdk.ResultPartial {
		t.Fatalf("State = %q, want %q", result.State, sdk.ResultPartial)
	}
	if got, want := validation.FactCount, 1; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	if len(result.Statuses) == 0 || !result.Statuses[0].Partial {
		t.Fatalf("Statuses = %#v, want partial warning status", result.Statuses)
	}
}

func TestCollectRejectsReportOutsideClaimScope(t *testing.T) {
	t.Parallel()

	report := readReportFixture(t, "complete.json")
	report.Repository.Name = "github.com/example/other"
	_, err := Collect(testClaim(), report, CollectOptions{
		ObservedAt: testObservedAt(),
		SourceURI:  "https://api.securityscorecards.dev/projects/github.com/example/other",
	})
	if err == nil {
		t.Fatal("Collect() error = nil, want scope mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match claim scope") {
		t.Fatalf("Collect() error = %v, want scope mismatch error", err)
	}
}

func TestLoadReportRejectsTrailingJSONValue(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "complete.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(complete.json) error = %v, want nil", err)
	}
	_, err = LoadReport(bytes.NewReader(append(raw, []byte("\n{}")...)))
	if err == nil {
		t.Fatal("LoadReport() error = nil, want trailing JSON rejection")
	}
	if !strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("LoadReport() error = %v, want trailing JSON rejection", err)
	}
}

func TestRetryableSourceFailureResultConforms(t *testing.T) {
	t.Parallel()

	result := RetryableSourceFailureResult(testClaim(), testObservedAt(), "rate_limited", 60)
	validation, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if result.State != sdk.ResultRetryable {
		t.Fatalf("State = %q, want %q", result.State, sdk.ResultRetryable)
	}
	if validation.FactCount != 0 {
		t.Fatalf("FactCount = %d, want 0", validation.FactCount)
	}
}

func TestRetryableSourceFailureResultDefaultsRemainSDKValid(t *testing.T) {
	t.Parallel()

	result := RetryableSourceFailureResult(testClaim(), testObservedAt(), "", 0)
	validation, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if validation.StatusCount != 1 {
		t.Fatalf("StatusCount = %d, want 1", validation.StatusCount)
	}
}

func TestManifestMatchesPackageContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("os.ReadFile(manifest.yaml) error = %v, want nil", err)
	}
	var manifest componentManifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("yaml.Unmarshal(manifest.yaml) error = %v, want nil", err)
	}

	if got, want := manifest.Metadata.ID, ComponentID; got != want {
		t.Fatalf("metadata.id = %q, want %q", got, want)
	}
	if got, want := manifest.Spec.Runtime.SDKProtocol, sdk.ProtocolVersionV1Alpha1; got != want {
		t.Fatalf("spec.runtime.sdkProtocol = %q, want %q", got, want)
	}
	if got, want := manifest.Spec.Runtime.Adapter, "process"; got != want {
		t.Fatalf("spec.runtime.adapter = %q, want %q", got, want)
	}
	if got, want := manifest.Spec.Telemetry.MetricsPrefix, MetricsPrefix; got != want {
		t.Fatalf("spec.telemetry.metricsPrefix = %q, want %q", got, want)
	}
	if !strings.Contains(manifest.Spec.Artifacts[0].Image, "@sha256:") {
		t.Fatalf("artifact image %q is not digest pinned", manifest.Spec.Artifacts[0].Image)
	}

	manifestFacts := map[string][]string{}
	for _, fact := range manifest.Spec.EmittedFacts {
		manifestFacts[fact.Kind] = fact.SourceConfidence
	}
	for _, declaration := range Contract().Facts {
		confidences, ok := manifestFacts[declaration.Kind]
		if !ok {
			t.Fatalf("manifest missing emitted fact %q", declaration.Kind)
		}
		if !slices.Contains(confidences, string(sdk.SourceConfidenceReported)) {
			t.Fatalf("manifest fact %q sourceConfidence = %v, want reported", declaration.Kind, confidences)
		}
	}
}

func TestExampleDoesNotImportEshuCoreInternals(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch path {
			case "testdata", ".eshu-components":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, `"`)
			if importPath == "github.com/eshu-hq/eshu/go/internal" ||
				strings.Contains(importPath, "github.com/eshu-hq/eshu/go/internal/") {
				t.Fatalf("example imports Eshu internal package %q", importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.WalkDir() error = %v, want nil", err)
	}
}

func readReportFixture(t *testing.T, name string) Report {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", name, err)
	}
	report, err := LoadReport(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadReport(%s) error = %v, want nil", name, err)
	}
	return report
}

func assertFactKinds(t *testing.T, facts []sdk.Fact, want []string) {
	t.Helper()

	got := make([]string, 0, len(facts))
	for _, fact := range facts {
		got = append(got, fact.Kind)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("fact kinds = %v, want %v", got, want)
	}
}

func assertNoCredentialLikePayloadKeys(t *testing.T, facts []sdk.Fact) {
	t.Helper()

	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatalf("json.Marshal(facts) error = %v, want nil", err)
	}
	for _, blocked := range []string{"token", "secret", "password", "credential", "api_key"} {
		if strings.Contains(strings.ToLower(string(raw)), blocked) {
			t.Fatalf("facts contain credential-like payload text %q: %s", blocked, raw)
		}
	}
}

func testClaim() sdk.Claim {
	return sdk.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "scorecard-local",
		CollectorKind: CollectorKind,
		SourceSystem:  SourceSystem,
		Scope: sdk.Scope{
			ID:   "github.com/example/widgets",
			Kind: "repository",
		},
		SourceRunID:  "run-2026-06-09",
		GenerationID: "generation-2026-06-09",
		WorkItemID:   "work-scorecard-widgets",
		FencingToken: "fence-scorecard-widgets",
		Attempt:      1,
		Deadline:     testObservedAt().Add(5 * time.Minute),
		ConfigHandle: "config://examples/scorecard/local",
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
}

type componentManifest struct {
	Metadata struct {
		ID string `yaml:"id"`
	} `yaml:"metadata"`
	Spec struct {
		Runtime struct {
			SDKProtocol string `yaml:"sdkProtocol"`
			Adapter     string `yaml:"adapter"`
		} `yaml:"runtime"`
		Artifacts []struct {
			Image string `yaml:"image"`
		} `yaml:"artifacts"`
		EmittedFacts []struct {
			Kind             string   `yaml:"kind"`
			SourceConfidence []string `yaml:"sourceConfidence"`
		} `yaml:"emittedFacts"`
		Telemetry struct {
			MetricsPrefix string `yaml:"metricsPrefix"`
		} `yaml:"telemetry"`
	} `yaml:"spec"`
}
