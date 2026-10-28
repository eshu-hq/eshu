// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"encoding/json"
	"fmt"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
	facts := append([]facts.Envelope(nil), output.Facts[start:end]...)
	return FactOutput{
		TargetCount: output.TargetCount,
		ResultCount: len(facts),
		Facts:       facts,
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
