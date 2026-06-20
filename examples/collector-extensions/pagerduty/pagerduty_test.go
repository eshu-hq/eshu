package pagerduty

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"gopkg.in/yaml.v3"
)

func TestCollectCompleteResultConformsToSDKContract(t *testing.T) {
	t.Parallel()

	observation := readObservationFixture(t, "complete.json")
	result, err := Collect(testClaim(), observation)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	validation, err := sdk.NewValidator(Contract()).ValidateResult(result)
	if err != nil {
		t.Fatalf("ValidateResult() error = %v, want nil", err)
	}
	if got, want := validation.FactCount, 6; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	if got, want := factKinds(result.Facts), []string{
		FactKindIncident,
		FactKindLifecycleEvent,
		FactKindChange,
		FactKindObservedService,
		FactKindObservedIntegration,
		FactKindCoverageWarning,
	}; !slices.Equal(got, want) {
		t.Fatalf("fact kinds = %v, want %v", got, want)
	}
	assertNoSensitiveFixtureText(t, result.Facts)
}

func TestCollectRejectsWrongClaimFamily(t *testing.T) {
	t.Parallel()

	claim := testClaim()
	claim.SourceSystem = "other"
	_, err := Collect(claim, readObservationFixture(t, "complete.json"))
	if err == nil {
		t.Fatal("Collect() error = nil, want source_system mismatch")
	}
	if !strings.Contains(err.Error(), "source_system") {
		t.Fatalf("Collect() error = %v, want source_system mismatch", err)
	}
}

func TestLoadObservationRejectsTrailingJSONValue(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "complete.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(complete.json) error = %v, want nil", err)
	}
	_, err = LoadObservation(bytes.NewReader(append(raw, []byte("\n{}")...)))
	if err == nil {
		t.Fatal("LoadObservation() error = nil, want trailing JSON rejection")
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

func TestDraftIndexMatchesPackageContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("community-extension-index.draft.yaml")
	if err != nil {
		t.Fatalf("os.ReadFile(community-extension-index.draft.yaml) error = %v, want nil", err)
	}
	var index communityExtensionIndex
	if err := yaml.Unmarshal(raw, &index); err != nil {
		t.Fatalf("yaml.Unmarshal(community-extension-index.draft.yaml) error = %v, want nil", err)
	}
	if len(index.Entries) != 1 {
		t.Fatalf("index entries = %d, want 1", len(index.Entries))
	}
	entry := index.Entries[0]
	if got, want := entry.ComponentID, ComponentID; got != want {
		t.Fatalf("entry.componentId = %q, want %q", got, want)
	}
	indexFacts := map[string][]string{}
	for _, fact := range entry.EmittedFacts {
		indexFacts[fact.Kind] = fact.SourceConfidence
	}
	for _, declaration := range Contract().Facts {
		confidences, ok := indexFacts[declaration.Kind]
		if !ok {
			t.Fatalf("index missing emitted fact %q", declaration.Kind)
		}
		if !slices.Equal(confidences, []string{string(sdk.SourceConfidenceReported)}) {
			t.Fatalf("index fact %q sourceConfidence = %v, want [reported]", declaration.Kind, confidences)
		}
	}
}

func TestReferenceDoesNotImportEshuCoreInternals(t *testing.T) {
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
				t.Fatalf("reference package imports Eshu internal package %q", importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.WalkDir() error = %v, want nil", err)
	}
}

func readObservationFixture(t *testing.T, name string) Observation {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", name, err)
	}
	observation, err := LoadObservation(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadObservation(%s) error = %v, want nil", name, err)
	}
	return observation
}

func factKinds(facts []sdk.Fact) []string {
	kinds := make([]string, 0, len(facts))
	for _, fact := range facts {
		kinds = append(kinds, fact.Kind)
	}
	return kinds
}

func assertNoSensitiveFixtureText(t *testing.T, facts []sdk.Fact) {
	t.Helper()

	payloads := make([]map[string]any, 0, len(facts))
	for _, fact := range facts {
		payloads = append(payloads, fact.Payload)
	}
	raw, err := json.Marshal(payloads)
	if err != nil {
		t.Fatalf("json.Marshal(facts) error = %v, want nil", err)
	}
	for _, blocked := range []string{"token", "secret", "password", "credential", "api_key"} {
		if strings.Contains(strings.ToLower(string(raw)), blocked) {
			t.Fatalf("facts contain sensitive marker %q: %s", blocked, raw)
		}
	}
}

func testClaim() sdk.Claim {
	observedAt := time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
	return sdk.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "pagerduty-reference-local",
		CollectorKind: CollectorKind,
		SourceSystem:  SourceSystem,
		Scope: sdk.Scope{
			ID:   "pagerduty:account:synthetic-reference",
			Kind: "pagerduty_account",
		},
		SourceRunID:  "pagerduty-reference-run-2026-06-14",
		GenerationID: "pagerduty-reference-generation-2026-06-14",
		WorkItemID:   "pagerduty-reference-work",
		FencingToken: "pagerduty-reference-fence",
		Attempt:      1,
		Deadline:     observedAt.Add(5 * time.Minute),
		ConfigHandle: "config://examples/pagerduty/reference",
	}
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
		EmittedFacts []struct {
			Kind             string   `yaml:"kind"`
			SourceConfidence []string `yaml:"sourceConfidence"`
		} `yaml:"emittedFacts"`
		Telemetry struct {
			MetricsPrefix string `yaml:"metricsPrefix"`
		} `yaml:"telemetry"`
	} `yaml:"spec"`
}

type communityExtensionIndex struct {
	Entries []struct {
		ComponentID  string `yaml:"componentId"`
		EmittedFacts []struct {
			Kind             string   `yaml:"kind"`
			SourceConfidence []string `yaml:"sourceConfidence"`
		} `yaml:"emittedFacts"`
	} `yaml:"entries"`
}
