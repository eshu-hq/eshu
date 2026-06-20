package conformance_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

func TestRunAcceptsDeclaredFixture(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
	})
	if got, want := report.Status, conformance.StatusPassed; got != want {
		t.Fatalf("Status = %q, want %q; findings=%#v", got, want, report.Findings)
	}
	if got, want := report.SchemaVersion, conformance.SchemaVersion; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := report.Summary.FixtureCount, 1; got != want {
		t.Fatalf("FixtureCount = %d, want %d", got, want)
	}
	if got, want := report.Summary.FactCount, 1; got != want {
		t.Fatalf("FactCount = %d, want %d", got, want)
	}
	if !report.Summary.IdempotentReemissionChecked {
		t.Fatal("IdempotentReemissionChecked = false, want true")
	}
}

func TestRunReportMarshalsToStableJSON(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
	})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v, want nil", err)
	}
	for _, want := range []string{
		`"schema_version":"eshu.extension.conformance.v1"`,
		`"status":"passed"`,
		`"fixture_count":1`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("report JSON missing %q: %s", want, raw)
		}
	}
}

func TestRunRejectsUndeclaredOrUnsafeFixture(t *testing.T) {
	t.Parallel()

	result := validResult()
	result.Facts[0].Kind = "dev.example.undeclared"
	result.Facts[0].Payload["api_key"] = "do-not-emit"

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Fixtures: []collector.Result{result},
		Mode:     conformance.ModeFixture,
	})
	if report.Status != conformance.StatusFailed {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	assertFinding(t, report, conformance.FindingFixtureContractFailed)
}

func TestRunReportsZeroFixtureIndexInJSON(t *testing.T) {
	t.Parallel()

	result := validResult()
	result.Facts[0].Kind = "dev.example.undeclared"

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Fixtures: []collector.Result{result},
		Mode:     conformance.ModeFixture,
	})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v, want nil", err)
	}
	if !strings.Contains(string(raw), `"fixture_index":0`) {
		t.Fatalf("report JSON missing fixture_index 0 for first-fixture finding: %s", raw)
	}
}

func TestRunRequiresFixtures(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Mode:     conformance.ModeCompose,
	})
	if report.Status != conformance.StatusFailed {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	if report.Mode != conformance.ModeCompose {
		t.Fatalf("Mode = %q, want %q", report.Mode, conformance.ModeCompose)
	}
	assertFinding(t, report, conformance.FindingFixtureRequired)
}

func TestRunBlocksUnsupportedMode(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.Mode("teleport"),
	})
	assertFinding(t, report, conformance.FindingUnsupportedMode)
}

func TestRunBlocksUnsupportedReducerConsumer(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.ConsumerContracts.Reducer.Phases = []string{"cloud_resource_uid:canonical_nodes_committed"}

	report := conformance.Run(conformance.Request{
		Manifest: manifest,
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
	})
	assertFinding(t, report, conformance.FindingMissingReducerConsumer)
}

func TestRunFailsOnUnversionedFactKind(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.EmittedFacts[0].SchemaVersions = nil

	report := conformance.Run(conformance.Request{
		Manifest: manifest,
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
	})
	assertFinding(t, report, conformance.FindingManifestInvalid)
}

func TestRunFailsOnUndigestedArtifact(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.Artifacts[0].Image = "ghcr.io/example/scorecard:latest"

	report := conformance.Run(conformance.Request{
		Manifest: manifest,
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
	})
	assertFinding(t, report, conformance.FindingManifestInvalid)
}

func TestRunFailsOnMalformedCompatibleCore(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.CompatibleCore = ">=0.0.5 nope"

	report := conformance.Run(conformance.Request{
		Manifest: manifest,
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
	})
	assertFinding(t, report, conformance.FindingManifestInvalid)
}

func TestRunAcceptsValidCompatibleCoreRanges(t *testing.T) {
	t.Parallel()

	for _, rangeExpr := range []string{">=0.0.5 <0.2.0", "1.0.0", "=1.2.3", ">=0.1"} {
		manifest := validManifest()
		manifest.Spec.CompatibleCore = rangeExpr
		report := conformance.Run(conformance.Request{
			Manifest: manifest,
			Fixtures: []collector.Result{validResult()},
			Mode:     conformance.ModeFixture,
		})
		if !report.OK() {
			t.Fatalf("compatibleCore %q: findings = %#v, want passed", rangeExpr, report.Findings)
		}
	}
}

func TestRunBlocksReservedFactKind(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest:          validManifest(),
		Fixtures:          []collector.Result{validResult()},
		Mode:              conformance.ModeFixture,
		ReservedFactKinds: []string{"dev.example.scorecard.snapshot"},
	})
	if report.OK() {
		t.Fatal("report OK = true, want failed for reserved fact kind")
	}
	assertFinding(t, report, conformance.FindingManifestInvalid)
}

func TestRunAllowsUnreservedFactKind(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest:          validManifest(),
		Fixtures:          []collector.Result{validResult()},
		Mode:              conformance.ModeFixture,
		ReservedFactKinds: []string{"some_core_kind", "another_core_kind"},
	})
	if !report.OK() {
		t.Fatalf("findings = %#v, want passed when no declared kind is reserved", report.Findings)
	}
}

func TestManifestContractDerivesDeclaredFacts(t *testing.T) {
	t.Parallel()

	contract := validManifest().Contract()
	if got, want := contract.ProtocolVersion, collector.ProtocolVersionV1Alpha1; got != want {
		t.Fatalf("ProtocolVersion = %q, want %q", got, want)
	}
	if got, want := len(contract.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	if got, want := contract.Facts[0].Kind, "dev.example.scorecard.snapshot"; got != want {
		t.Fatalf("Facts[0].Kind = %q, want %q", got, want)
	}
}

func assertFinding(t *testing.T, report conformance.Report, code conformance.FindingCode) {
	t.Helper()

	for _, finding := range report.Findings {
		if finding.Code != code {
			continue
		}
		if !finding.BlocksPublication || !finding.BlocksHostedActivation {
			t.Fatalf("%s blocks publication=%v hosted=%v, want both true", code, finding.BlocksPublication, finding.BlocksHostedActivation)
		}
		return
	}
	t.Fatalf("finding %q missing from %#v", code, report.Findings)
}

func validManifest() conformance.Manifest {
	return conformance.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: conformance.Metadata{
			ID:        "dev.example.collector.scorecard",
			Name:      "Scorecard collector",
			Publisher: "example",
			Version:   "0.1.0",
		},
		Spec: conformance.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  "collector",
			CollectorKinds: []string{"scorecard"},
			Runtime: conformance.RuntimeContract{
				SDKProtocol: collector.ProtocolVersionV1Alpha1,
				Adapter:     "oci",
			},
			Artifacts: []conformance.Artifact{{
				Platform: "linux/amd64",
				Image:    "ghcr.io/example/scorecard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []conformance.FactFamily{{
				Kind:             "dev.example.scorecard.snapshot",
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{"reported"},
			}},
			ConsumerContracts: conformance.ConsumerContracts{
				Reducer: conformance.ReducerContract{
					Phases: []string{"source_evidence_only:no_graph_truth"},
				},
			},
			Telemetry: conformance.Telemetry{MetricsPrefix: "eshu_dp_example_scorecard_"},
		},
	}
}

func validResult() collector.Result {
	observedAt := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	claim := collector.Claim{
		ComponentID:   "dev.example.collector.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: "scorecard",
		SourceSystem:  "dev.example.collector.scorecard",
		Scope:         collector.Scope{ID: "component:scorecard-primary", Kind: "component"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-1",
		FencingToken:  "fence-1",
		Attempt:       1,
		Deadline:      observedAt.Add(time.Hour),
		ConfigHandle:  "component-config:scorecard",
	}
	return collector.Result{
		ProtocolVersion: collector.ProtocolVersionV1Alpha1,
		State:           collector.ResultComplete,
		Claim:           claim,
		Generation:      collector.Generation{ID: "generation-1", ObservedAt: observedAt},
		Facts: []collector.Fact{{
			Kind:             "dev.example.scorecard.snapshot",
			SchemaVersion:    "1.0.0",
			StableKey:        "scorecard:snapshot:1",
			SourceConfidence: collector.SourceConfidenceReported,
			ObservedAt:       observedAt,
			SourceRef: collector.SourceRef{
				SourceSystem: "dev.example.collector.scorecard",
				ScopeID:      "component:scorecard-primary",
				GenerationID: "generation-1",
				FactKey:      "scorecard:snapshot:1",
				URI:          "component://scorecard/snapshot/1",
				RecordID:     "snapshot-1",
			},
			Payload: map[string]any{"score": float64(98)},
		}},
	}
}
