// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	collector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// SchemaVersion is the stable JSON report schema emitted by the collector
// conformance harness. It is shared with the in-tree host wrapper so reports
// produced inside and outside the monorepo are byte-comparable.
const SchemaVersion = "eshu.extension.conformance.v1"

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
	// FindingUnsupportedMode means the request named a conformance mode this
	// package does not support.
	FindingUnsupportedMode FindingCode = "unsupported_mode"
	// FindingManifestInvalid means the manifest violated the proof-metadata
	// contract: missing identity, unpinned artifact, or unversioned fact kinds.
	FindingManifestInvalid FindingCode = "manifest_invalid"
	// FindingMissingReducerConsumer means the manifest declares reducer truth
	// phases that core does not consume for optional component facts yet.
	FindingMissingReducerConsumer FindingCode = "missing_reducer_consumer"
	// FindingFixtureRequired means the request did not include result fixtures.
	FindingFixtureRequired FindingCode = "fixture_required"
	// FindingFixtureContractFailed means a fixture violates the manifest-derived
	// collector SDK contract.
	FindingFixtureContractFailed FindingCode = "fixture_contract_failed"
	// FindingPayloadSchemaInvalid means a fixture fact payload failed validation
	// against the checked-in JSON Schema for its fact kind: a required field was
	// absent or null, a field carried the wrong type, or the supplied schema
	// used a construct the harness cannot validate (which fails closed rather
	// than passing unchecked). The finding message names the offending field.
	FindingPayloadSchemaInvalid FindingCode = "payload_schema_invalid"
)

// SourceEvidenceOnlyReducerPhase is the only reducer consumer phase optional
// component facts may declare today: source evidence that is committed but not
// promoted to canonical graph truth.
const SourceEvidenceOnlyReducerPhase = "source_evidence_only:no_graph_truth"

// Mode selects the conformance proof mode.
type Mode string

// Status is the overall conformance result.
type Status string

// FindingCode identifies one stable conformance failure class.
type FindingCode string

// Request describes one conformance run against an already-decoded manifest and
// already-decoded SDK result fixtures. It performs no file or network I/O so it
// is safe to call from out-of-tree package tests and from the in-tree host.
type Request struct {
	// Manifest is the component package manifest under test.
	Manifest Manifest
	// Fixtures are decoded collector SDK results to validate against the
	// manifest-derived contract.
	Fixtures []collector.Result
	// Mode selects the proof mode; empty defaults to ModeFixture.
	Mode Mode
	// ReservedFactKinds are fact kinds the host owns and a component may not
	// claim. The in-tree host passes the core fact-kind registry; out-of-tree
	// callers may leave it nil and rely on namespacing alone. A manifest that
	// declares a reserved kind fails closed.
	ReservedFactKinds []string
	// PayloadSchemas maps an emitted fixture fact kind to the JSON Schema its
	// payloads must satisfy. The key is the exact fact-kind string the fixture
	// emits, which for an out-of-tree collector is its own namespaced kind mapped
	// to a core payload shape (for example "dev.acme.collector.resource" pointing
	// at the aws_resource schema shape), because the bare core kinds are
	// host-owned and reserved and cannot be emitted directly. The caller supplies
	// the schema bytes so this package performs no file I/O and takes no
	// schema-library dependency; an out-of-tree collector reads the shape from
	// the pinned sdk/go/factschema/fixturepack. A fixture fact whose kind has an
	// entry here is validated against it and fails closed on a missing required
	// field, a wrong-typed field, or a schema construct the harness cannot
	// validate. A fixture fact whose kind has no entry is not payload-validated
	// (provenance-only kinds carry no registered schema), and leaving the map nil
	// disables payload validation entirely, preserving the pre-payload-validation
	// behavior.
	PayloadSchemas map[string]json.RawMessage
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
	Code    FindingCode `json:"code"`
	Message string      `json:"message"`
	// FixtureIndex is the zero-based index of the offending fixture, or nil for
	// manifest- or request-level findings. It is a pointer so an index of 0 is
	// reported rather than elided by omitempty.
	FixtureIndex           *int `json:"fixture_index,omitempty"`
	BlocksPublication      bool `json:"blocks_publication"`
	BlocksHostedActivation bool `json:"blocks_hosted_activation"`
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

// OK reports whether the run produced no blocking findings.
func (r Report) OK() bool {
	return r.Status == StatusPassed
}

// Run executes one read-only collector conformance check. It never mutates
// state, performs no I/O, and always returns a populated Report; callers
// inspect Report.Status (or Report.OK) for the verdict.
func Run(req Request) Report {
	mode := normalizeMode(req.Mode)
	report := Report{
		SchemaVersion:    SchemaVersion,
		Mode:             mode,
		Status:           StatusFailed,
		ComponentID:      strings.TrimSpace(req.Manifest.Metadata.ID),
		ComponentVersion: strings.TrimSpace(req.Manifest.Metadata.Version),
	}

	if mode != ModeFixture && mode != ModeCompose {
		addBlockingFinding(&report, FindingUnsupportedMode, fmt.Sprintf("conformance mode %q is unsupported", req.Mode), -1)
		return report
	}

	if err := req.Manifest.validateProofMetadata(); err != nil {
		addBlockingFinding(&report, FindingManifestInvalid, err.Error(), -1)
		return report
	}

	if kind, ok := reservedFactKindClaim(req.Manifest, req.ReservedFactKinds); ok {
		addBlockingFinding(&report, FindingManifestInvalid, fmt.Sprintf("fact kind %q is host-owned and cannot be claimed by a component", kind), -1)
		return report
	}

	addReducerConsumerFindings(&report, req.Manifest)

	if len(req.Fixtures) == 0 {
		addBlockingFinding(&report, FindingFixtureRequired, "at least one collector SDK result fixture is required", -1)
		return report
	}

	schemas, schemaErr := compileSchemas(req.PayloadSchemas)
	if schemaErr != nil {
		// A schema that cannot be compiled fails closed: the harness refuses to
		// pass a fixture it cannot actually validate against the declared
		// contract, rather than silently skipping payload validation.
		addBlockingFinding(&report, FindingPayloadSchemaInvalid, schemaErr.Error(), -1)
		return report
	}

	validator := collector.NewValidator(req.Manifest.Contract())
	for index, fixture := range req.Fixtures {
		validateFixture(&report, validator, schemas, index, fixture)
	}

	if hasBlockingFindings(report) {
		return report
	}
	report.Status = StatusPassed
	return report
}

// compileSchemas compiles every supplied payload schema up front so a malformed
// or unsupported schema is a single request-level failure, and each compiled
// schema is reused across every fixture. It returns the first compile error so
// Run can fail closed rather than validate fixtures against a schema it cannot
// interpret. Kinds are compiled in sorted order so the reported error is
// deterministic across runs.
func compileSchemas(raw map[string]json.RawMessage) (map[string]payloadSchema, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	kinds := make([]string, 0, len(raw))
	for kind := range raw {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	compiled := make(map[string]payloadSchema, len(raw))
	for _, kind := range kinds {
		schema, err := compileSchema(raw[kind])
		if err != nil {
			return nil, fmt.Errorf("payload schema for fact kind %q: %w", kind, err)
		}
		compiled[kind] = schema
	}
	return compiled, nil
}

func normalizeMode(mode Mode) Mode {
	if strings.TrimSpace(string(mode)) == "" {
		return ModeFixture
	}
	return Mode(strings.TrimSpace(string(mode)))
}

// reservedFactKindClaim reports the first emitted fact kind that collides with a
// host-reserved (core-owned) fact kind, if any. It lets the in-tree host enforce
// the same core-ownership boundary the component manifest path enforces, without
// the dependency-free SDK module importing a core fact-kind registry.
func reservedFactKindClaim(manifest Manifest, reserved []string) (string, bool) {
	if len(reserved) == 0 {
		return "", false
	}
	reservedSet := make(map[string]struct{}, len(reserved))
	for _, kind := range reserved {
		reservedSet[strings.TrimSpace(kind)] = struct{}{}
	}
	for _, fact := range manifest.Spec.EmittedFacts {
		if _, ok := reservedSet[strings.TrimSpace(fact.Kind)]; ok {
			return fact.Kind, true
		}
	}
	return "", false
}

func addReducerConsumerFindings(report *Report, manifest Manifest) {
	for _, phase := range manifest.Spec.ConsumerContracts.Reducer.Phases {
		if strings.TrimSpace(phase) == SourceEvidenceOnlyReducerPhase {
			continue
		}
		addBlockingFinding(
			report,
			FindingMissingReducerConsumer,
			fmt.Sprintf("reducer phase %q is not available for optional component facts", phase),
			-1,
		)
	}
}

func validateFixture(report *Report, validator collector.Validator, schemas map[string]payloadSchema, index int, fixture collector.Result) {
	report.Summary.FixtureCount++

	validationReport, err := validator.ValidateResult(fixture)
	if err != nil {
		addBlockingFinding(report, FindingFixtureContractFailed, err.Error(), index)
		return
	}
	// Re-validate to prove idempotent re-emission produces the same verdict.
	if _, err := validator.ValidateResult(fixture); err != nil {
		addBlockingFinding(report, FindingFixtureContractFailed, fmt.Sprintf("idempotent re-emission failed: %v", err), index)
		return
	}
	// Validate each fact payload against its checked-in JSON Schema when the
	// caller supplied one for that kind. A kind with no supplied schema is not
	// payload-validated, matching the provenance-only change-matrix row.
	if err := validateFixturePayloads(schemas, fixture); err != nil {
		addBlockingFinding(report, FindingPayloadSchemaInvalid, err.Error(), index)
		return
	}
	report.Summary.IdempotentReemissionChecked = true
	report.Summary.FactCount += validationReport.FactCount
	report.Summary.DuplicateCount += validationReport.DuplicateCount
	report.Summary.RedactionCount += validationReport.RedactionCount
	report.Summary.TombstoneCount += validationReport.TombstoneCount
	report.Summary.StatusCount += validationReport.StatusCount
}

// validateFixturePayloads checks every fact in the fixture against the compiled
// payload schema for its kind, when one exists. It returns the first violation,
// naming the fact kind and the offending field, so the finding message an
// external collector reads points straight at the payload key to fix.
func validateFixturePayloads(schemas map[string]payloadSchema, fixture collector.Result) error {
	if len(schemas) == 0 {
		return nil
	}
	for _, fact := range fixture.Facts {
		schema, ok := schemas[fact.Kind]
		if !ok {
			continue
		}
		if err := schema.validatePayload(fact.Payload); err != nil {
			return fmt.Errorf("fact %q payload: %w", fact.Kind, err)
		}
	}
	return nil
}

func addBlockingFinding(report *Report, code FindingCode, message string, fixtureIndex int) {
	finding := Finding{
		Code:                   code,
		Message:                message,
		BlocksPublication:      true,
		BlocksHostedActivation: true,
	}
	if fixtureIndex >= 0 {
		index := fixtureIndex
		finding.FixtureIndex = &index
	}
	report.Findings = append(report.Findings, finding)
}

func hasBlockingFindings(report Report) bool {
	for _, finding := range report.Findings {
		if finding.BlocksPublication || finding.BlocksHostedActivation {
			return true
		}
	}
	return false
}
