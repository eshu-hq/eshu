// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// PilotEvidenceArtifact is machine-readable, independent run evidence for all
// cases required by the pilot manifest. The validator does not execute SQL or
// Cypher; producers must preserve separate oracle and database provenance.
type PilotEvidenceArtifact struct {
	Version     int                  `json:"version"`
	Environment PilotRunEnvironment  `json:"environment"`
	Base        PilotBuildIdentity   `json:"base"`
	Candidate   PilotBuildIdentity   `json:"candidate"`
	Entries     []PilotEvidenceEntry `json:"entries"`
}

// PilotRunEnvironment fixes the common engine, configuration, dataset,
// fixture, and workload identity for a paired comparison.
type PilotRunEnvironment struct {
	Engine            string          `json:"engine"`
	EngineVersion     string          `json:"engine_version"`
	EngineImage       string          `json:"engine_image"`
	Config            json.RawMessage `json:"config"`
	ConfigSHA256      string          `json:"config_sha256"`
	Dataset           json.RawMessage `json:"dataset"`
	DatasetSHA256     string          `json:"dataset_sha256"`
	FixtureDefinition string          `json:"fixture_definition"`
	FixtureSHA256     string          `json:"fixture_sha256"`
	Workload          json.RawMessage `json:"workload"`
	WorkloadSHA256    string          `json:"workload_sha256"`
	HarnessSHA256     string          `json:"harness_sha256"`
	BinarySHA256      string          `json:"binary_sha256"`
	ColdReset         string          `json:"cold_reset"`
}

// PilotBuildIdentity binds one measured build and its separately declared
// schema, migrations, and indexes. These may differ between paired builds.
type PilotBuildIdentity struct {
	Commit           string   `json:"commit"`
	SchemaSHA256     string   `json:"schema_sha256"`
	MigrationsSHA256 string   `json:"migrations_sha256"`
	IndexesSHA256    string   `json:"indexes_sha256"`
	SchemaDDL        []string `json:"schema_ddl"`
	Migrations       []string `json:"migrations"`
	IndexDDL         []string `json:"index_ddl"`
}

// PilotEvidenceEntry binds the recorded cases to one current manifest entry.
type PilotEvidenceEntry struct {
	EntryID        string              `json:"entry_id"`
	ContractSHA256 string              `json:"contract_sha256"`
	SourceSHA256   string              `json:"source_sha256"`
	Cases          []PilotCaseEvidence `json:"cases"`
}

// PilotCaseEvidence records one exact emitted variant, safe parameters,
// independently produced expected rows, database rows, complete plans/work,
// and paired repeated timings.
type PilotCaseEvidence struct {
	VariantID            string                     `json:"variant_id"`
	CaseID               string                     `json:"case_id"`
	ScopeMode            string                     `json:"scope_mode"`
	EmittedSHA256        string                     `json:"emitted_sha256"`
	EmittedText          string                     `json:"emitted_text"`
	BaseEmittedSHA256    string                     `json:"base_emitted_sha256"`
	BaseEmittedText      string                     `json:"base_emitted_text"`
	Parameters           map[string]json.RawMessage `json:"parameters"`
	OracleProducer       string                     `json:"oracle_producer"`
	OracleArtifactSHA256 string                     `json:"oracle_artifact_sha256"`
	OracleRecordedAt     string                     `json:"oracle_recorded_at"`
	MeasuredAt           string                     `json:"measured_at"`
	Expected             json.RawMessage            `json:"expected"`
	Actual               json.RawMessage            `json:"actual"`
	Base                 PilotCaseRun               `json:"base"`
	Candidate            PilotCaseRun               `json:"candidate"`
}

// PilotCaseRun contains an unabridged plan and metrics or an explicit reason
// and independent alternate proof when a backend cannot return a plan.
type PilotCaseRun struct {
	Result              json.RawMessage `json:"result"`
	Plan                json.RawMessage `json:"plan,omitempty"`
	Work                json.RawMessage `json:"work,omitempty"`
	PlanUnavailable     string          `json:"plan_unavailable,omitempty"`
	AlternateProof      json.RawMessage `json:"alternate_proof,omitempty"`
	ColdMilliseconds    []float64       `json:"cold_ms"`
	WarmMilliseconds    []float64       `json:"warm_ms"`
	ColdPreparation     string          `json:"cold_preparation"`
	ColdProof           string          `json:"cold_proof"`
	ColdProofSHA256     string          `json:"cold_proof_sha256"`
	PlanCaptureSeparate bool            `json:"plan_capture_separate"`
}

// ValidatePilotEvidence checks exact pilot membership, fresh hashes, distinct
// oracle provenance, result equality, and complete paired measurement. A
// caller must also validate manifest source hashes against the current tree.
func ValidatePilotEvidence(manifest Manifest, artifact *PilotEvidenceArtifact) error {
	if err := ValidatePilotContracts(manifest); err != nil {
		return err
	}
	if artifact == nil {
		if len(manifest.PilotRequiredIDs) > 0 {
			return errors.New("missing pilot evidence artifact")
		}
		return nil
	}
	var violations []string
	if artifact.Version != 1 {
		violations = append(violations, "unsupported evidence version")
	}
	violations = append(violations, validatePilotRunIdentity(*artifact)...)
	entries := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entries[entry.ID] = entry
	}
	required := make(map[string]struct{}, len(manifest.PilotRequiredIDs))
	for _, id := range manifest.PilotRequiredIDs {
		required[id] = struct{}{}
	}
	seenEntries := make(map[string]struct{}, len(artifact.Entries))
	for _, recorded := range artifact.Entries {
		entry, ok := entries[recorded.EntryID]
		if _, needed := required[recorded.EntryID]; !ok || !needed {
			violations = append(violations, fmt.Sprintf("unknown pilot evidence entry %s", recorded.EntryID))
			continue
		}
		if _, duplicate := seenEntries[recorded.EntryID]; duplicate {
			violations = append(violations, fmt.Sprintf("duplicate pilot evidence entry %s", recorded.EntryID))
			continue
		}
		seenEntries[recorded.EntryID] = struct{}{}
		if recorded.ContractSHA256 != PilotContractSHA256(*entry.Contract) {
			violations = append(violations, fmt.Sprintf("%s: stale contract hash", entry.ID))
		}
		if recorded.SourceSHA256 != entry.Source.SourceSHA256 {
			violations = append(violations, fmt.Sprintf("%s: stale source hash", entry.ID))
		}
		if artifact.Environment.Engine != entry.Contract.Environment.Engine || artifact.Environment.EngineVersion != entry.Contract.Environment.Version {
			violations = append(violations, fmt.Sprintf("%s: engine/version differs from contract", entry.ID))
		}
		candidateSchema := append(append([]string(nil), artifact.Candidate.SchemaDDL...), artifact.Candidate.IndexDDL...)
		violations = append(violations, validatePilotCases(entry, recorded, artifact.Environment.FixtureSHA256,
			artifact.Environment.ColdReset, candidateSchema)...)
	}
	for id := range required {
		if _, ok := seenEntries[id]; !ok {
			violations = append(violations, fmt.Sprintf("%s: missing evidence entry and required cases", id))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func validatePilotRunIdentity(artifact PilotEvidenceArtifact) []string {
	var violations []string
	for label, value := range map[string]string{
		"config":               artifact.Environment.ConfigSHA256,
		"dataset":              artifact.Environment.DatasetSHA256,
		"fixture":              artifact.Environment.FixtureSHA256,
		"workload":             artifact.Environment.WorkloadSHA256,
		"harness":              artifact.Environment.HarnessSHA256,
		"binary":               artifact.Environment.BinarySHA256,
		"base commit":          artifact.Base.Commit,
		"candidate commit":     artifact.Candidate.Commit,
		"base schema":          artifact.Base.SchemaSHA256,
		"candidate schema":     artifact.Candidate.SchemaSHA256,
		"base migrations":      artifact.Base.MigrationsSHA256,
		"candidate migrations": artifact.Candidate.MigrationsSHA256,
		"base indexes":         artifact.Base.IndexesSHA256,
		"candidate indexes":    artifact.Candidate.IndexesSHA256,
	} {
		if !isSHA256(value) {
			violations = append(violations, fmt.Sprintf("malformed or missing %s identity", label))
		}
	}
	if strings.TrimSpace(artifact.Environment.Engine) == "" || strings.TrimSpace(artifact.Environment.EngineVersion) == "" ||
		(artifact.Environment.ColdReset != "cold_plan_warm_buffers" && artifact.Environment.ColdReset != "cold_cache") {
		violations = append(violations, "engine, version, and cold reset provenance are required")
	}
	if strings.TrimSpace(artifact.Environment.EngineImage) == "" ||
		artifact.Environment.ConfigSHA256 != PilotJSONSHA256(artifact.Environment.Config) ||
		artifact.Environment.DatasetSHA256 != PilotJSONSHA256(artifact.Environment.Dataset) ||
		artifact.Environment.WorkloadSHA256 != PilotJSONSHA256(artifact.Environment.Workload) ||
		artifact.Environment.FixtureSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(artifact.Environment.FixtureDefinition))) ||
		strings.TrimSpace(artifact.Environment.FixtureDefinition) == "" {
		violations = append(violations, "environment definitions must match config/dataset/fixture/workload identities")
	}
	var dataset map[string]json.RawMessage
	if json.Unmarshal(artifact.Environment.Dataset, &dataset) != nil {
		violations = append(violations, "dataset definition must be an object")
	} else {
		for _, field := range []string{"version", "seed", "distribution", "topology", "storage"} {
			if !substantialPilotJSON(dataset[field]) {
				violations = append(violations, "dataset missing reproducibility field "+field)
			}
		}
	}
	if artifact.Base.Commit == artifact.Candidate.Commit {
		violations = append(violations, "base and candidate commits must differ")
	}
	for label, build := range map[string]PilotBuildIdentity{"base": artifact.Base, "candidate": artifact.Candidate} {
		if len(build.SchemaDDL) == 0 || len(build.IndexDDL) == 0 ||
			build.SchemaSHA256 != PilotDefinitionsSHA256(build.SchemaDDL) ||
			build.MigrationsSHA256 != PilotDefinitionsSHA256(build.Migrations) ||
			build.IndexesSHA256 != PilotDefinitionsSHA256(build.IndexDDL) {
			violations = append(violations, label+": schema, migration, or index definitions do not match identity")
		}
	}
	return violations
}

func validatePilotCases(entry Entry, recorded PilotEvidenceEntry, fixtureSHA, coldMode string, candidateSchema []string) []string {
	required := make(map[string]PilotCase, len(entry.Contract.RequiredCases))
	for _, candidate := range entry.Contract.RequiredCases {
		required[candidate.VariantID+"/"+candidate.CaseID] = candidate
	}
	seen := make(map[string]struct{}, len(recorded.Cases))
	var violations []string
	for _, candidate := range recorded.Cases {
		key := candidate.VariantID + "/" + candidate.CaseID
		want, ok := required[key]
		if !ok {
			violations = append(violations, fmt.Sprintf("%s: unknown variant/case %s", entry.ID, key))
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			violations = append(violations, fmt.Sprintf("%s: duplicate variant/case %s", entry.ID, key))
			continue
		}
		seen[key] = struct{}{}
		if candidate.EmittedSHA256 != want.QuerySHA256 {
			violations = append(violations, fmt.Sprintf("%s/%s: stale emitted query hash", entry.ID, key))
		}
		if candidate.ScopeMode != want.ScopeMode {
			violations = append(violations, fmt.Sprintf("%s/%s: scope mode differs from contract", entry.ID, key))
		}
		if candidate.EmittedText == "" || fmt.Sprintf("%x", sha256.Sum256([]byte(candidate.EmittedText))) != candidate.EmittedSHA256 {
			violations = append(violations, fmt.Sprintf("%s/%s: emitted text does not match hash", entry.ID, key))
		}
		if candidate.BaseEmittedText == "" || fmt.Sprintf("%x", sha256.Sum256([]byte(candidate.BaseEmittedText))) != candidate.BaseEmittedSHA256 {
			violations = append(violations, fmt.Sprintf("%s/%s: base emitted text does not match hash", entry.ID, key))
		}
		if err := ValidatePilotCaseQuery(entry.QueryKind, candidate.EmittedText, *entry.Contract, want,
			candidateSchema); err != nil {
			violations = append(violations, fmt.Sprintf("%s/%s: candidate query structure: %v", entry.ID, key, err))
		}
		if err := validatePilotParameters(candidate.Parameters, fixtureSHA); err != nil {
			violations = append(violations, fmt.Sprintf("%s/%s: %v", entry.ID, key, err))
		}
		if candidate.OracleProducer != entry.Contract.Environment.Oracle || candidate.OracleProducer == entry.Contract.Environment.Runner || !isSHA256(candidate.OracleArtifactSHA256) {
			violations = append(violations, fmt.Sprintf("%s/%s: independent oracle provenance missing", entry.ID, key))
		}
		oracleTime, oracleErr := time.Parse(time.RFC3339Nano, candidate.OracleRecordedAt)
		measureTime, measureErr := time.Parse(time.RFC3339Nano, candidate.MeasuredAt)
		if oracleErr != nil || measureErr != nil || !oracleTime.Before(measureTime) {
			violations = append(violations, fmt.Sprintf("%s/%s: oracle must predate DB measurement", entry.ID, key))
		}
		if !json.Valid(candidate.Expected) || !json.Valid(candidate.Actual) || !jsonEqual(candidate.Expected, candidate.Actual) {
			violations = append(violations, fmt.Sprintf("%s/%s: expected and actual results differ or are missing", entry.ID, key))
		}
		if !jsonEqual(candidate.Expected, candidate.Base.Result) || !jsonEqual(candidate.Expected, candidate.Candidate.Result) || !jsonEqual(candidate.Actual, candidate.Candidate.Result) {
			violations = append(violations, fmt.Sprintf("%s/%s: base/candidate results differ from independent expected rows", entry.ID, key))
		}
		if candidate.Base.ColdPreparation != coldMode || candidate.Candidate.ColdPreparation != coldMode {
			violations = append(violations, fmt.Sprintf("%s/%s: cold preparation differs from environment declaration", entry.ID, key))
		}
		violations = append(violations, validatePilotCaseRun(entry.ID+"/"+key+"/base", candidate.Base, candidate.Expected, entry.Contract.Budget)...)
		violations = append(violations, validatePilotCaseRun(entry.ID+"/"+key+"/candidate", candidate.Candidate, candidate.Expected, entry.Contract.Budget)...)
	}
	for key := range required {
		if _, ok := seen[key]; !ok {
			violations = append(violations, fmt.Sprintf("%s: unexercised required variant/case %s", entry.ID, key))
		}
	}
	return violations
}

func validatePilotCaseRun(key string, run PilotCaseRun, expected json.RawMessage, budget PilotBudget) []string {
	var violations []string
	if !structuredPilotJSON(run.Plan) || !structuredPilotJSON(run.Work) || !pilotHasNumber(run.Work) {
		if strings.TrimSpace(run.PlanUnavailable) == "" || len(run.AlternateProof) == 0 || !json.Valid(run.AlternateProof) {
			violations = append(violations, key+": full plan and work or alternate proof required")
		}
	}
	if !jsonEqual(run.Result, expected) {
		violations = append(violations, key+": result differs from independent oracle")
	}
	if !run.PlanCaptureSeparate {
		violations = append(violations, key+": plan capture must be separate from normal timings")
	}
	if run.ColdPreparation != "cold_plan_warm_buffers" && run.ColdPreparation != "cold_cache" ||
		strings.TrimSpace(run.ColdProof) == "" ||
		run.ColdProofSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(run.ColdProof))) {
		violations = append(violations, key+": explicit cold preparation and proof required")
	}
	if len(run.ColdMilliseconds) < 2 || len(run.WarmMilliseconds) < 2 {
		violations = append(violations, key+": repeated cold and warm measurements required")
	}
	for _, sample := range append(append([]float64(nil), run.ColdMilliseconds...), run.WarmMilliseconds...) {
		if sample <= 0 || sample > 1e12 || math.IsNaN(sample) || math.IsInf(sample, 0) {
			violations = append(violations, key+": invalid timing sample")
			break
		}
		if sample > budget.MaxNormalMilliseconds {
			violations = append(violations, fmt.Sprintf("%s: normal timing %.3fms exceeds %.3fms budget", key, sample, budget.MaxNormalMilliseconds))
		}
	}
	work := run.Work
	if !structuredPilotJSON(work) {
		work = run.AlternateProof
	}
	if count, ok := pilotWorkNumber(work, "query_count"); !ok || count <= 0 || count > float64(budget.MaxQueryCount) {
		violations = append(violations, key+": query count absent or exceeds budget")
	}
	for metric, maximum := range budget.MaxWork {
		if actual, ok := pilotWorkNumber(work, metric); !ok || actual < 0 || actual > maximum {
			violations = append(violations, fmt.Sprintf("%s: work metric %s absent or exceeds %.3f budget", key, metric, maximum))
		}
	}
	return violations
}

func ValidatePilotEvidenceForBackend(manifest Manifest, artifact *PilotEvidenceArtifact) error {
	if artifact == nil {
		return errors.New("missing pilot backend artifact")
	}
	if err := ValidatePilotContracts(manifest); err != nil {
		return err
	}
	selected := manifest
	selected.PilotRequiredIDs = nil
	for _, id := range manifest.PilotRequiredIDs {
		entry, _ := pilotEntry(manifest, id)
		if entry.Contract.Environment.Engine == artifact.Environment.Engine {
			selected.PilotRequiredIDs = append(selected.PilotRequiredIDs, id)
		}
	}
	if len(selected.PilotRequiredIDs) == 0 {
		return fmt.Errorf("unknown pilot backend %s", artifact.Environment.Engine)
	}
	selected.Entries = nil
	for _, entry := range manifest.Entries {
		if entry.Contract != nil && entry.Contract.Environment.Engine == artifact.Environment.Engine {
			selected.Entries = append(selected.Entries, entry)
		}
	}
	return ValidatePilotEvidence(selected, artifact)
}

// ValidatePilotEvidenceSet requires exactly one valid artifact per engine in
// the unmodified pilot manifest; an omitted backend cannot pass this gate.
func ValidatePilotEvidenceSet(manifest Manifest, artifacts []PilotEvidenceArtifact) error {
	if err := ValidatePilotContracts(manifest); err != nil {
		return err
	}
	requiredEngines := make(map[string]struct{})
	for _, id := range manifest.PilotRequiredIDs {
		entry, _ := pilotEntry(manifest, id)
		requiredEngines[entry.Contract.Environment.Engine] = struct{}{}
	}
	seen := make(map[string]struct{})
	var violations []string
	for i := range artifacts {
		engine := artifacts[i].Environment.Engine
		if _, ok := requiredEngines[engine]; !ok {
			violations = append(violations, "unknown pilot backend artifact "+engine)
			continue
		}
		if _, duplicate := seen[engine]; duplicate {
			violations = append(violations, "duplicate pilot backend artifact "+engine)
			continue
		}
		seen[engine] = struct{}{}
		if err := ValidatePilotEvidenceForBackend(manifest, &artifacts[i]); err != nil {
			violations = append(violations, err.Error())
		}
	}
	for engine := range requiredEngines {
		if _, ok := seen[engine]; !ok {
			violations = append(violations, "missing pilot backend artifact "+engine)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}
