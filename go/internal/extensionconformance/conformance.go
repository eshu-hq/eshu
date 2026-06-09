package extensionconformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/component"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const (
	// SchemaVersion is the stable JSON report schema emitted by the
	// conformance harness.
	SchemaVersion = "eshu.extension.conformance.v1"
)

const (
	// ModeFixture validates local SDK result fixtures only.
	ModeFixture Mode = "fixture"
	// ModeCompose reserves a Compose-backed proof mode while preserving the
	// requested mode in reports.
	ModeCompose Mode = "compose"
)

const (
	// StatusPassed reports that no blocking conformance findings were emitted.
	StatusPassed Status = "passed"
	// StatusFailed reports that at least one blocking conformance finding was
	// emitted or the request could not be evaluated.
	StatusFailed Status = "failed"
)

const (
	// FindingFixtureRequired means the request did not include result fixtures.
	FindingFixtureRequired FindingCode = "fixture_required"
	// FindingManifestInvalid means the component manifest could not be loaded
	// through the component package contract.
	FindingManifestInvalid FindingCode = "manifest_invalid"
	// FindingFixtureReadFailed means a fixture file could not be read or
	// decoded as a collector SDK result.
	FindingFixtureReadFailed FindingCode = "fixture_read_failed"
	// FindingFixtureContractFailed means a fixture violates the manifest-derived
	// collector SDK contract.
	FindingFixtureContractFailed FindingCode = "fixture_contract_failed"
	// FindingMissingReducerConsumer means the manifest declares reducer truth
	// phases that core does not consume for optional component facts yet.
	FindingMissingReducerConsumer FindingCode = "missing_reducer_consumer"
	// FindingUnsupportedMode means the request named a conformance mode this
	// package does not support.
	FindingUnsupportedMode FindingCode = "unsupported_mode"
)

const sourceEvidenceOnlyReducerPhase = "source_evidence_only:no_graph_truth"

// Mode selects the conformance proof mode.
type Mode string

// Status is the overall conformance result.
type Status string

// FindingCode identifies one stable conformance failure class.
type FindingCode string

// Request describes one conformance run.
type Request struct {
	ManifestPath  string
	FixturePaths  []string
	Mode          Mode
	ComponentHome string
}

// Report is the stable conformance result returned to CLIs and automation.
type Report struct {
	SchemaVersion    string    `json:"schema_version"`
	Mode             Mode      `json:"mode"`
	Status           Status    `json:"status"`
	ComponentID      string    `json:"component_id,omitempty"`
	ComponentVersion string    `json:"component_version,omitempty"`
	Findings         []Finding `json:"findings,omitempty"`
	Summary          Summary   `json:"summary"`
}

// Finding describes one conformance failure or blocker.
type Finding struct {
	Code                   FindingCode `json:"code"`
	Message                string      `json:"message"`
	FixturePath            string      `json:"fixture_path,omitempty"`
	BlocksPublication      bool        `json:"blocks_publication"`
	BlocksHostedActivation bool        `json:"blocks_hosted_activation"`
}

// Summary aggregates accepted fixture evidence.
type Summary struct {
	FixtureCount                int  `json:"fixture_count"`
	FactCount                   int  `json:"fact_count"`
	DuplicateCount              int  `json:"duplicate_count"`
	RedactionCount              int  `json:"redaction_count"`
	TombstoneCount              int  `json:"tombstone_count"`
	StatusCount                 int  `json:"status_count"`
	IdempotentReemissionChecked bool `json:"idempotent_reemission_checked"`
}

// Run executes one read-only extension conformance check.
func Run(ctx context.Context, req Request) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mode := normalizeMode(req.Mode)
	report := Report{
		SchemaVersion: SchemaVersion,
		Mode:          mode,
		Status:        StatusFailed,
	}

	if mode != ModeFixture && mode != ModeCompose {
		addBlockingFinding(&report, FindingUnsupportedMode, fmt.Sprintf("conformance mode %q is unsupported", req.Mode), "")
		return report, conformanceError(report)
	}

	manifest, ok := loadManifest(&report, req.ManifestPath)
	if !ok {
		return report, conformanceError(report)
	}
	report.ComponentID = manifest.Metadata.ID
	report.ComponentVersion = manifest.Metadata.Version

	addReducerConsumerFindings(&report, manifest)
	if len(req.FixturePaths) == 0 {
		addBlockingFinding(&report, FindingFixtureRequired, "at least one collector SDK result fixture is required", "")
		return report, conformanceError(report)
	}

	validator := sdkcollector.NewValidator(contractFromManifest(manifest))
	for _, fixturePath := range req.FixturePaths {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		validateFixture(&report, validator, fixturePath)
	}

	if hasBlockingFindings(report) {
		return report, conformanceError(report)
	}
	report.Status = StatusPassed
	return report, nil
}

func normalizeMode(mode Mode) Mode {
	if strings.TrimSpace(string(mode)) == "" {
		return ModeFixture
	}
	return Mode(strings.TrimSpace(string(mode)))
}

func loadManifest(report *Report, path string) (component.Manifest, bool) {
	manifest, err := component.LoadManifest(path)
	if err != nil {
		addBlockingFinding(report, FindingManifestInvalid, err.Error(), "")
		return component.Manifest{}, false
	}
	return manifest, true
}

func addReducerConsumerFindings(report *Report, manifest component.Manifest) {
	for _, phase := range manifest.Spec.ConsumerContracts.Reducer.Phases {
		if strings.TrimSpace(phase) == sourceEvidenceOnlyReducerPhase {
			continue
		}
		addBlockingFinding(
			report,
			FindingMissingReducerConsumer,
			fmt.Sprintf("reducer phase %q is not available for optional component facts", phase),
			"",
		)
	}
}

func contractFromManifest(manifest component.Manifest) sdkcollector.Contract {
	facts := make([]sdkcollector.FactDeclaration, 0, len(manifest.Spec.EmittedFacts))
	for _, declared := range manifest.Spec.EmittedFacts {
		sourceConfidence := make([]sdkcollector.SourceConfidence, 0, len(declared.SourceConfidence))
		for _, confidence := range declared.SourceConfidence {
			sourceConfidence = append(sourceConfidence, sdkcollector.SourceConfidence(confidence))
		}
		facts = append(facts, sdkcollector.FactDeclaration{
			Kind:             declared.Kind,
			SchemaVersions:   append([]string(nil), declared.SchemaVersions...),
			SourceConfidence: sourceConfidence,
		})
	}
	return sdkcollector.Contract{
		ProtocolVersion: manifest.Spec.Runtime.SDKProtocol,
		Facts:           facts,
	}
}

func validateFixture(report *Report, validator sdkcollector.Validator, fixturePath string) {
	result, ok := readFixture(report, fixturePath)
	if !ok {
		return
	}
	report.Summary.FixtureCount++

	validationReport, err := validator.ValidateResult(result)
	if err != nil {
		addBlockingFinding(report, FindingFixtureContractFailed, err.Error(), fixturePath)
		return
	}
	if _, err := validator.ValidateResult(result); err != nil {
		addBlockingFinding(report, FindingFixtureContractFailed, fmt.Sprintf("idempotent re-emission failed: %v", err), fixturePath)
		return
	}
	report.Summary.IdempotentReemissionChecked = true
	report.Summary.FactCount += validationReport.FactCount
	report.Summary.DuplicateCount += validationReport.DuplicateCount
	report.Summary.RedactionCount += validationReport.RedactionCount
	report.Summary.TombstoneCount += validationReport.TombstoneCount
	report.Summary.StatusCount += validationReport.StatusCount
}

func readFixture(report *Report, fixturePath string) (sdkcollector.Result, bool) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		addBlockingFinding(report, FindingFixtureReadFailed, err.Error(), fixturePath)
		return sdkcollector.Result{}, false
	}
	var result sdkcollector.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		addBlockingFinding(report, FindingFixtureReadFailed, err.Error(), fixturePath)
		return sdkcollector.Result{}, false
	}
	return result, true
}

func addBlockingFinding(report *Report, code FindingCode, message string, fixturePath string) {
	report.Findings = append(report.Findings, Finding{
		Code:                   code,
		Message:                message,
		FixturePath:            fixturePath,
		BlocksPublication:      true,
		BlocksHostedActivation: true,
	})
}

func hasBlockingFindings(report Report) bool {
	for _, finding := range report.Findings {
		if finding.BlocksPublication || finding.BlocksHostedActivation {
			return true
		}
	}
	return false
}

func conformanceError(report Report) error {
	if len(report.Findings) == 0 {
		return fmt.Errorf("fixture conformance failed")
	}
	return fmt.Errorf("fixture conformance failed: %s", report.Findings[0].Code)
}
