// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/scope"
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
	// Source identity is either analyzer-owned (source_system equals the
	// envelope collector kind) or claim-plane-owned (#9407: os_package
	// envelopes keep the analyzer collector kind for durable provenance but
	// carry the planner-owned scanner_worker claim identity the SDK
	// satellite contract admits).
	if fact.SourceRef.SourceSystem != fact.CollectorKind &&
		fact.SourceRef.SourceSystem != string(scope.CollectorScannerWorker) {
		return fmt.Errorf("source_ref.source_system %q must match collector_kind %q or the scanner-worker claim plane",
			fact.SourceRef.SourceSystem, fact.CollectorKind)
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

// SplitFactOutput partitions a FactOutput into pages whose SDK wire bytes
// each fit maxPageBytes, so every page fits one bounded satellite call under
// the collector-sdk/v1alpha3 pagination contract. Order is preserved, no
// fact is lost or duplicated, every page carries at least one fact, and each
// page keeps the original TargetCount with its own ResultCount.
//
// Wire bytes are measured by projecting every envelope through
// factenvelope.SDKFactFromInternal and marshaling the SDK shape: the exact
// bytes a future stdio satellite would write for the fact. The partition
// rules mirror collector.SplitFacts in sdk/go/collector (which works on SDK
// facts for satellites); this copy works on internal envelopes because the
// pinned eshu-sdk/collector module predates pagination and cannot be
// imported for the new helpers. Reconcile the two when the pin advances: the
// hostile-shape tests in both modules must keep agreeing.
//
// This never reduces the MaxFacts contract silently: facts that do not fit
// one page move to the next, and only a single fact larger than the whole
// bound fails closed naming its kind and stable key.
func SplitFactOutput(output FactOutput, maxPageBytes int64) ([]FactOutput, error) {
	if len(output.Facts) == 0 {
		return nil, fmt.Errorf("at least one fact is required to page")
	}
	if maxPageBytes <= 0 {
		return nil, fmt.Errorf("page bound must be positive")
	}
	sizes := make([]int64, len(output.Facts))
	for i, envelope := range output.Facts {
		wire, err := factenvelope.SDKFactFromInternal(envelope)
		if err != nil {
			return nil, fmt.Errorf("facts[%d]: %w", i, err)
		}
		size, err := wireFactBytes(wire)
		if err != nil {
			return nil, fmt.Errorf("facts[%d]: %w", i, err)
		}
		if size > maxPageBytes {
			return nil, fmt.Errorf(
				"fact %q stable key %q is %d bytes above page bound %d",
				envelope.FactKind, envelope.StableFactKey, size, maxPageBytes,
			)
		}
		sizes[i] = size
	}
	var pages []FactOutput
	start := 0
	var currentBytes int64
	for i := range output.Facts {
		if i > start && currentBytes+sizes[i] > maxPageBytes {
			pages = append(pages, pageOutput(output, start, i))
			start = i
			currentBytes = 0
		}
		currentBytes += sizes[i]
	}
	pages = append(pages, pageOutput(output, start, len(output.Facts)))
	return pages, nil
}

// pageOutput slices one page out of the parent output, keeping the claim
// identity and naming its own fact count.
func pageOutput(output FactOutput, start, end int) FactOutput {
	paged := append([]facts.Envelope(nil), output.Facts[start:end]...)
	return FactOutput{
		TargetCount: output.TargetCount,
		ResultCount: len(paged),
		Facts:       paged,
	}
}

// wireFactBytes measures the exact JSON wire size of one projected SDK fact.
// It mirrors collector.FactWireBytes until the eshu-sdk/collector pin
// carries pagination.
func wireFactBytes(fact sdkcollector.Fact) (int64, error) {
	raw, err := json.Marshal(fact)
	if err != nil {
		return 0, fmt.Errorf("marshal fact %q: %w", fact.Kind, err)
	}
	return int64(len(raw)), nil
}

func sourceFactKindAllowed(factKind string) bool {
	if slices.Contains(facts.ScannerWorkerFactKinds(), factKind) {
		return true
	}
	if slices.Contains(chain.SBOMAttestationFactKinds(), factKind) {
		return true
	}
	return factKind == chain.VulnerabilityOSPackageFactKind ||
		factKind == chain.VulnerabilityWarningFactKind
}

func sourceFactSchemaVersion(factKind string) (string, bool) {
	if version, ok := facts.ScannerWorkerSchemaVersion(factKind); ok {
		return version, true
	}
	if version, ok := chain.SBOMAttestationSchemaVersion(factKind); ok {
		return version, true
	}
	switch factKind {
	case chain.VulnerabilityOSPackageFactKind, chain.VulnerabilityWarningFactKind:
		return chain.VulnerabilityIntelligenceSchemaVersion(factKind)
	default:
		return "", false
	}
}

func reducerOwnedFactKind(factKind string) bool {
	return strings.HasPrefix(factKind, "reducer_") ||
		strings.HasPrefix(factKind, "reducer.") ||
		strings.HasSuffix(factKind, "_finding")
}
