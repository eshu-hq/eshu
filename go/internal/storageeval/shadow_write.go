// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var factSchemaVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

const (
	// BackendPostgresFactStore is the current production Postgres fact baseline.
	BackendPostgresFactStore Backend = "postgres_fact_store"
	// BackendNornicDBShadowFactStore is the candidate NornicDB shadow writer.
	BackendNornicDBShadowFactStore Backend = "nornicdb_shadow_fact_store"
)

// FactFamily groups fact kinds into one bounded migration proof lane.
type FactFamily string

// FactRecordState records whether the compared fact is active or tombstoned.
type FactRecordState string

const (
	// FactRecordActive means the fact is an active ledger row.
	FactRecordActive FactRecordState = "active"
	// FactRecordTombstone means the fact is a tombstone ledger row.
	FactRecordTombstone FactRecordState = "tombstone"
)

// FactGenerationState records generation activity for one write result.
type FactGenerationState string

const (
	// FactGenerationActive means the compared generation is active.
	FactGenerationActive FactGenerationState = "active"
	// FactGenerationStale means the compared generation is stale.
	FactGenerationStale FactGenerationState = "stale"
	// FactGenerationSuperseded means a newer generation superseded this one.
	FactGenerationSuperseded FactGenerationState = "superseded"
)

// FactSupersessionState records whether the compared fact was superseded.
type FactSupersessionState string

const (
	// FactSupersessionCurrent means the fact is current within the generation.
	FactSupersessionCurrent FactSupersessionState = "current"
	// FactSupersessionSuperseded means a newer fact superseded this record.
	FactSupersessionSuperseded FactSupersessionState = "superseded"
)

// RollbackBehavior records how shadow writes are removed or isolated.
type RollbackBehavior string

const (
	// RollbackDropShadowWrites discards the shadow fact-store write set.
	RollbackDropShadowWrites RollbackBehavior = "drop_shadow_writes"
	// RollbackKeepPostgresFactStore keeps the production Postgres fact ledger.
	RollbackKeepPostgresFactStore RollbackBehavior = "keep_postgres_fact_store"
	// RollbackFailClosed refuses a promoted write when parity cannot be proven.
	RollbackFailClosed RollbackBehavior = "fail_closed"
)

// FactWriteVerdict is the shadow-write comparison outcome.
type FactWriteVerdict string

const (
	// FactWriteVerdictMatch means the shadow write matched the Postgres baseline.
	FactWriteVerdictMatch FactWriteVerdict = "match"
	// FactWriteVerdictMissingShadow means the shadow write was absent.
	FactWriteVerdictMissingShadow FactWriteVerdict = "missing_shadow_write"
	// FactWriteVerdictStaleGeneration means the shadow generation was stale.
	FactWriteVerdictStaleGeneration FactWriteVerdict = "stale_generation"
	// FactWriteVerdictSuperseded means the shadow write was superseded.
	FactWriteVerdictSuperseded FactWriteVerdict = "superseded"
	// FactWriteVerdictSchemaMismatch means schema versions diverged.
	FactWriteVerdictSchemaMismatch FactWriteVerdict = "schema_version_mismatch"
	// FactWriteVerdictDivergentGeneration means active generation diverged.
	FactWriteVerdictDivergentGeneration FactWriteVerdict = "divergent_active_generation"
	// FactWriteVerdictTombstoneMismatch means tombstone state diverged.
	FactWriteVerdictTombstoneMismatch FactWriteVerdict = "tombstone_mismatch"
	// FactWriteVerdictUnsupportedCapability means shadow writes were unsupported.
	FactWriteVerdictUnsupportedCapability FactWriteVerdict = "unsupported_capability"
	// FactWriteVerdictUnboundedScan means read-back proof was unbounded.
	FactWriteVerdictUnboundedScan FactWriteVerdict = "unbounded_scan"
)

// FactWriteFailureClass identifies a failed fact-write comparison.
type FactWriteFailureClass string

const (
	// FactWriteFailureNone means no failure was observed.
	FactWriteFailureNone FactWriteFailureClass = "none"
	// FactWriteFailureMissingShadowWrite records absent shadow write output.
	FactWriteFailureMissingShadowWrite FactWriteFailureClass = "missing_shadow_write"
	// FactWriteFailureStaleGeneration records stale generation output.
	FactWriteFailureStaleGeneration FactWriteFailureClass = "stale_generation"
	// FactWriteFailureSuperseded records superseded shadow state.
	FactWriteFailureSuperseded FactWriteFailureClass = "superseded"
	// FactWriteFailureSchemaMismatch records schema-version drift.
	FactWriteFailureSchemaMismatch FactWriteFailureClass = "schema_version_mismatch"
	// FactWriteFailureDivergentGeneration records active-generation drift.
	FactWriteFailureDivergentGeneration FactWriteFailureClass = "divergent_active_generation"
	// FactWriteFailureTombstoneMismatch records tombstone-state drift.
	FactWriteFailureTombstoneMismatch FactWriteFailureClass = "tombstone_mismatch"
	// FactWriteFailureUnsupportedCapability records unsupported shadow writes.
	FactWriteFailureUnsupportedCapability FactWriteFailureClass = "unsupported_capability"
	// FactWriteFailureUnboundedScan records unbounded fact read-back proof.
	FactWriteFailureUnboundedScan FactWriteFailureClass = "unbounded_scan"
)

// FactWriteResult summarizes one fact-store write and bounded read-back result.
type FactWriteResult struct {
	Backend            Backend               `json:"backend"`
	FactID             string                `json:"fact_id"`
	StableFactKey      string                `json:"stable_fact_key"`
	ScopeID            string                `json:"scope_id"`
	GenerationID       string                `json:"generation_id"`
	FactKind           string                `json:"fact_kind"`
	SchemaVersion      string                `json:"schema_version"`
	RecordState        FactRecordState       `json:"record_state"`
	GenerationState    FactGenerationState   `json:"generation_state"`
	SupersessionState  FactSupersessionState `json:"supersession_state"`
	Digest             string                `json:"digest"`
	ObservedAt         time.Time             `json:"observed_at"`
	Latency            time.Duration         `json:"latency_ns"`
	Supported          bool                  `json:"supported"`
	BoundedResultCount int                   `json:"bounded_result_count"`
}

// FactWriteComparison is one evidence record for the #1288 proof gate.
type FactWriteComparison struct {
	FactFamily       FactFamily            `json:"fact_family"`
	FactKind         string                `json:"fact_kind"`
	ScopeID          string                `json:"scope_id"`
	GenerationID     string                `json:"generation_id"`
	IdempotencyKey   string                `json:"idempotency_key"`
	Limit            int                   `json:"limit"`
	ReplayCount      int                   `json:"replay_count"`
	Baseline         FactWriteResult       `json:"baseline"`
	Shadow           FactWriteResult       `json:"shadow"`
	Verdict          FactWriteVerdict      `json:"verdict"`
	FallbackBehavior FallbackBehavior      `json:"fallback_behavior"`
	RollbackBehavior RollbackBehavior      `json:"rollback_behavior"`
	FailureClass     FactWriteFailureClass `json:"failure_class"`
}

// ValidateFactWriteComparison verifies one passing shadow-write proof record.
func ValidateFactWriteComparison(comparison FactWriteComparison) error {
	if strings.TrimSpace(string(comparison.FactFamily)) == "" {
		return fmt.Errorf("fact family is required")
	}
	if strings.TrimSpace(comparison.FactKind) == "" {
		return fmt.Errorf("fact kind is required")
	}
	if strings.TrimSpace(comparison.IdempotencyKey) == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if strings.TrimSpace(comparison.ScopeID) == "" {
		return fmt.Errorf("scope id is required")
	}
	if strings.TrimSpace(comparison.GenerationID) == "" {
		return fmt.Errorf("generation id is required")
	}
	if comparison.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if comparison.ReplayCount < 0 {
		return fmt.Errorf("replay count must not be negative")
	}
	if err := validateFallbackBehavior(comparison.FallbackBehavior); err != nil {
		return err
	}
	if err := validateRollbackBehavior(comparison.RollbackBehavior); err != nil {
		return err
	}
	if err := validateFactWriteResult("baseline", comparison.Baseline, BackendPostgresFactStore, comparison); err != nil {
		return err
	}
	if err := validateFactWriteResult("shadow", comparison.Shadow, BackendNornicDBShadowFactStore, comparison); err != nil {
		return err
	}
	if comparison.Baseline.FactID != comparison.Shadow.FactID {
		return fmt.Errorf("fact ids must match")
	}
	if comparison.Baseline.StableFactKey != comparison.Shadow.StableFactKey {
		return fmt.Errorf("stable fact keys must match")
	}
	if comparison.Baseline.SchemaVersion != comparison.Shadow.SchemaVersion {
		return fmt.Errorf("schema versions must match")
	}
	if comparison.Baseline.RecordState != comparison.Shadow.RecordState {
		return fmt.Errorf("record states must match")
	}
	if comparison.Baseline.Digest != comparison.Shadow.Digest {
		return fmt.Errorf("shadow write digest differs from baseline")
	}
	if comparison.Verdict == "" {
		return fmt.Errorf("verdict is required")
	}
	if comparison.Verdict != FactWriteVerdictMatch {
		return fmt.Errorf("verdict must be match")
	}
	if comparison.FailureClass == "" {
		return fmt.Errorf("failure class is required")
	}
	if comparison.FailureClass != FactWriteFailureNone {
		return fmt.Errorf("failure class must be none for match verdict")
	}
	return nil
}

func validateFactWriteResult(
	role string,
	result FactWriteResult,
	backend Backend,
	comparison FactWriteComparison,
) error {
	if result.Backend != backend {
		return fmt.Errorf("%s backend must be %q", role, backend)
	}
	if strings.TrimSpace(result.FactID) == "" {
		return fmt.Errorf("%s fact_id is required", role)
	}
	if strings.TrimSpace(result.StableFactKey) == "" {
		return fmt.Errorf("%s stable_fact_key is required", role)
	}
	if result.StableFactKey != comparison.IdempotencyKey {
		return fmt.Errorf("%s stable_fact_key must match idempotency key", role)
	}
	if strings.TrimSpace(result.ScopeID) != comparison.ScopeID {
		return fmt.Errorf("%s scope_id must match comparison scope_id", role)
	}
	if strings.TrimSpace(result.GenerationID) != comparison.GenerationID {
		return fmt.Errorf("%s generation_id must match comparison generation_id", role)
	}
	if strings.TrimSpace(result.FactKind) != comparison.FactKind {
		return fmt.Errorf("%s fact_kind must match comparison fact_kind", role)
	}
	if strings.TrimSpace(result.SchemaVersion) == "" {
		return fmt.Errorf("%s schema_version is required", role)
	}
	if !factSchemaVersionPattern.MatchString(result.SchemaVersion) {
		return fmt.Errorf("%s schema_version must be semantic version", role)
	}
	if err := validateFactRecordState(role, result.RecordState); err != nil {
		return err
	}
	if result.GenerationState != FactGenerationActive {
		return fmt.Errorf("%s generation state must be active", role)
	}
	if result.SupersessionState != FactSupersessionCurrent {
		return fmt.Errorf("%s supersession state must be current", role)
	}
	if strings.TrimSpace(result.Digest) == "" {
		return fmt.Errorf("%s write digest is required", role)
	}
	if result.ObservedAt.IsZero() {
		return fmt.Errorf("%s observed_at is required", role)
	}
	if result.Latency < 0 {
		return fmt.Errorf("%s latency must not be negative", role)
	}
	if !result.Supported {
		return fmt.Errorf("%s capability is unsupported", role)
	}
	if result.BoundedResultCount < 0 {
		return fmt.Errorf("%s bounded result count must not be negative", role)
	}
	if result.BoundedResultCount > comparison.Limit {
		return fmt.Errorf("%s bounded result count exceeds limit", role)
	}
	return nil
}

func validateFactRecordState(role string, state FactRecordState) error {
	switch state {
	case FactRecordActive, FactRecordTombstone:
		return nil
	case "":
		return fmt.Errorf("%s record state is required", role)
	default:
		return fmt.Errorf("%s record state %q is unsupported", role, state)
	}
}

func validateRollbackBehavior(rollback RollbackBehavior) error {
	switch rollback {
	case "":
		return fmt.Errorf("rollback behavior is required")
	case RollbackDropShadowWrites, RollbackKeepPostgresFactStore, RollbackFailClosed:
		return nil
	default:
		return fmt.Errorf("rollback behavior %q is unsupported", rollback)
	}
}
