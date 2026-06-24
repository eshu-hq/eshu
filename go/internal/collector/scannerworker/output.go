// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ValidateFactOutput validates scanner-worker output before fact commit.
func ValidateFactOutput(input ClaimInput, output FactOutput) error {
	if err := input.validate(); err != nil {
		return err
	}
	if output.TargetCount <= 0 {
		return fmt.Errorf("target_count must be positive")
	}
	if output.ResultCount < 0 {
		return fmt.Errorf("result_count must not be negative")
	}
	if len(output.Facts) == 0 {
		return fmt.Errorf("scanner workers must emit an explicit source fact or warning; silent clean output is not allowed")
	}
	if len(output.Facts) > input.Limits.MaxFacts {
		return fmt.Errorf("scanner worker emitted %d facts above max_facts %d", len(output.Facts), input.Limits.MaxFacts)
	}
	if output.ResultCount > len(output.Facts) {
		return fmt.Errorf("result_count %d exceeds emitted fact count %d", output.ResultCount, len(output.Facts))
	}

	for idx, fact := range output.Facts {
		if err := validateSourceFact(input, fact); err != nil {
			return fmt.Errorf("facts[%d]: %w", idx, err)
		}
	}
	return nil
}

func validateSourceFact(input ClaimInput, fact facts.Envelope) error {
	if reducerOwnedFactKind(fact.FactKind) {
		return errors.New("scanner workers emit source facts only; reducers own user-facing findings")
	}
	if !sourceFactKindAllowed(fact.FactKind) {
		return fmt.Errorf("fact_kind %q is not a scanner-worker source fact kind", fact.FactKind)
	}
	if strings.TrimSpace(fact.FactID) == "" {
		return fmt.Errorf("fact_id must not be blank")
	}
	if fact.ScopeID != input.Target.ScopeID {
		return fmt.Errorf("scope_id %q does not match target scope_id", fact.ScopeID)
	}
	if fact.GenerationID != input.GenerationID {
		return fmt.Errorf("generation_id %q does not match claim generation_id", fact.GenerationID)
	}
	collectorKind := strings.TrimSpace(fact.CollectorKind)
	if collectorKind == "" {
		return fmt.Errorf("collector_kind must not be blank")
	}
	if fact.CollectorKind != collectorKind {
		return fmt.Errorf("collector_kind must be normalized")
	}
	if fact.FencingToken != input.FencingToken {
		return fmt.Errorf("fencing_token %d does not match claim fencing_token %d", fact.FencingToken, input.FencingToken)
	}
	if err := facts.ValidateSourceConfidence(fact.SourceConfidence); err != nil {
		return err
	}
	if fact.SourceConfidence == facts.SourceConfidenceDerived {
		return fmt.Errorf("scanner-worker facts must be source facts, got source_confidence %q", fact.SourceConfidence)
	}
	if fact.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at must not be zero")
	}
	if strings.TrimSpace(fact.StableFactKey) == "" {
		return fmt.Errorf("stable_fact_key must not be blank")
	}
	expectedSchema, ok := sourceFactSchemaVersion(fact.FactKind)
	if !ok {
		return fmt.Errorf("fact_kind %q has no scanner-worker source schema", fact.FactKind)
	}
	if fact.SchemaVersion != expectedSchema {
		return fmt.Errorf("schema_version %q does not match expected %q", fact.SchemaVersion, expectedSchema)
	}
	sourceSystem := strings.TrimSpace(fact.SourceRef.SourceSystem)
	if sourceSystem == "" {
		return fmt.Errorf("source_ref.source_system must not be blank")
	}
	if fact.SourceRef.SourceSystem != sourceSystem {
		return fmt.Errorf("source_ref.source_system must be normalized")
	}
	if fact.SourceRef.SourceSystem != fact.CollectorKind {
		return fmt.Errorf("source_ref.source_system %q does not match collector_kind %q", fact.SourceRef.SourceSystem, fact.CollectorKind)
	}
	if strings.TrimSpace(fact.SourceRef.ScopeID) == "" {
		return fmt.Errorf("source_ref.scope_id must not be blank")
	}
	if fact.SourceRef.ScopeID != input.Target.ScopeID {
		return fmt.Errorf("source_ref.scope_id does not match target scope_id")
	}
	if strings.TrimSpace(fact.SourceRef.GenerationID) == "" {
		return fmt.Errorf("source_ref.generation_id must not be blank")
	}
	if fact.SourceRef.GenerationID != input.GenerationID {
		return fmt.Errorf("source_ref.generation_id does not match claim generation_id")
	}
	if strings.TrimSpace(fact.SourceRef.FactKey) == "" {
		return fmt.Errorf("source_ref.fact_key must not be blank")
	}
	return nil
}

func sourceFactKindAllowed(factKind string) bool {
	if slices.Contains(facts.ScannerWorkerFactKinds(), factKind) {
		return true
	}
	if slices.Contains(facts.SBOMAttestationFactKinds(), factKind) {
		return true
	}
	return factKind == facts.VulnerabilityOSPackageFactKind ||
		factKind == facts.VulnerabilityWarningFactKind
}

func sourceFactSchemaVersion(factKind string) (string, bool) {
	if version, ok := facts.ScannerWorkerSchemaVersion(factKind); ok {
		return version, true
	}
	if version, ok := facts.SBOMAttestationSchemaVersion(factKind); ok {
		return version, true
	}
	switch factKind {
	case facts.VulnerabilityOSPackageFactKind, facts.VulnerabilityWarningFactKind:
		return facts.VulnerabilityIntelligenceSchemaVersion(factKind)
	default:
		return "", false
	}
}

func reducerOwnedFactKind(factKind string) bool {
	return strings.HasPrefix(factKind, "reducer_") ||
		strings.HasPrefix(factKind, "reducer.") ||
		strings.HasSuffix(factKind, "_finding")
}
